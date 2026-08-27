
# go-user

A small, production-shaped REST API for user management, written in idiomatic Go using only the standard library plus a Postgres driver. No web framework, no ORM, no dependency injection container. Just `net/http`, `database/sql`, and code you can read start to finish in one sitting.

This project exists to practice the discipline of writing Go the way the standard library itself is written: explicit error handling, small interfaces defined by the consumer, and boundaries you can actually reason about.

## what this is

* a CRUD API over a single `users` resource
* backed by Postgres through `pgx`, accessed via the standard `database/sql` interface
* embedded SQL migrations that run on boot, guarded by a Postgres advisory lock so multiple instances never race each other
* structured logging with `log/slog`, request IDs threaded through `context.Context`
* errors returned to clients as `application/problem+json` (RFC 9457), with a single translation point between domain errors and HTTP status codes
* concurrent updates rejected instead of silently merged, through `ETag` / `If-Match` optimistic locking
* graceful shutdown on `SIGTERM`/`SIGINT`, with in-flight requests drained before the process exits
* a multi-stage Dockerfile producing a static, non-root, distroless image

## project layout

```
cmd/
  api/            entry point: wiring only, no logic
internal/
  config/         environment-based configuration, fails fast if incomplete
  database/       connection pool setup and embedded migrations
  logger/         slog wrapper that injects request IDs into every log line
  problem/        RFC 9457 error responses
  server/         router assembly and middleware chain
  user/           the actual feature: domain type, handlers, repository, routes
```

Every package under `internal/` is unimportable outside this module by construction. `user/` is organized by feature, not by technical layer: the domain type, its HTTP handlers, its repository, and its route registration all live together and expose one small surface (`RegisterRoutes`) to the rest of the app.

## getting started

You need Go 1.26 and either a running Postgres instance or Docker.

```bash
cp .env.example .env
make db-up
make run
```

`DATABASE_URL` is the only required variable; everything else falls back to a default. A variable that is *set* to something unparseable is a startup error, not a silent fallback — a body limit misconfigured into its default is a security control quietly turned off. `HANDLER_TIMEOUT` must be shorter than `WRITE_TIMEOUT`, or the connection is cut before the timeout response can leave.

Or run the whole stack in containers:

```bash
docker compose up --build
```

Migrations in `internal/database/migrations` are embedded into the binary and applied automatically on startup, in filename order, each tracked in a `schema_migrations` table so restarts are idempotent.

## api

| method | path             | description                          |
|--------|------------------|--------------------------------------|
| GET    | `/healthz`       | liveness, no dependencies            |
| GET    | `/readyz`        | readiness, pings the database        |
| POST   | `/v1/users`      | create a user                        |
| GET    | `/v1/users`      | list users, paginated                |
| GET    | `/v1/users/{id}` | fetch a single user                  |
| PATCH  | `/v1/users/{id}` | partial update, requires `If-Match`  |
| DELETE | `/v1/users/{id}` | delete a user                        |

`DELETE` is not silently idempotent: removing a user that is not there is a `404`, not a `204`.

```bash
curl -i -X POST localhost:8080/v1/users \
     -H 'Content-Type: application/json' \
     -d '{"name":"abner","email":"abner@example.com"}'
```

```json
{
  "id": 1,
  "name": "abner",
  "email": "abner@example.com",
  "version": 1,
  "created_at": "2026-08-27T12:00:00Z",
  "updated_at": "2026-08-27T12:00:00Z"
}
```

The response carries `Location: /v1/users/1` and `ETag: "1"`.

`GET /v1/users` accepts `limit` (default 20, capped at 100) and `offset` and returns an envelope rather than a bare array, so pagination metadata can grow later without breaking clients:

```json
{ "items": [ ... ], "total": 42, "limit": 20, "offset": 0 }
```

### updates

`PATCH` uses pointer fields (`*string`) in its input struct to distinguish a field the client did not send from a field explicitly set to empty.

Because `PATCH` is a read-modify-write, it is guarded by optimistic locking. Every user carries a `version`, surfaced as an `ETag`, and the request must echo it back in `If-Match`:

```bash
ETAG=$(curl -si localhost:8080/v1/users/1 | grep -i '^etag' | cut -d' ' -f2 | tr -d '\r')
curl -X PATCH localhost:8080/v1/users/1 \
     -H 'Content-Type: application/json' -H "If-Match: $ETAG" \
     -d '{"name":"new name"}'
```

Without the header the request is a `428 Precondition Required`; with a version that has moved on, a `412 Precondition Failed`. Neither client silently overwrites the other.

### request handling

Bodies are checked before they are parsed:

* anything but `application/json` is a `415`
* a body over `MAX_BODY_BYTES` is a `413`, not a shrugging `400`
* unknown fields are rejected, so a typo in a client payload is a `400` instead of a silently ignored field
* a body carrying more than one JSON document is refused instead of having the remainder dropped
* emails are compared against the *parsed* address, since `Victim <attacker@evil.com>` is a valid RFC 5322 address and not a valid user, then lowercased to match the `lower(email)` unique index

A handler that runs longer than `HANDLER_TIMEOUT` is cut off with a `503`, so a slow query cannot hold a goroutine and a pooled connection until `WRITE_TIMEOUT`. Every response also carries `X-Content-Type-Options: nosniff`, `X-Frame-Options: DENY` and `Referrer-Policy: no-referrer`.

`X-Request-ID` is honored when the caller sends one, so a trace survives across services, but only after it is checked against a conservative character set and length: the id ends up in every log line, and an id carrying newlines is log injection.

### errors

Every failure path returns a `problem+json` body. Domain errors like "not found" or "duplicate" map to specific status codes through one function; anything unexpected is logged with its request ID and returned as a generic `500`. Database constraint names stay in the log for the same reason.

```json
{
  "type": "about:blank",
  "title": "Unprocessable Entity",
  "status": 422,
  "detail": "invalid email: expected a plain address like name@example.com"
}
```

## testing

```bash
go test ./...                        # unit tests, no external dependencies
go test -race ./...                  # same, with the race detector (needs a C toolchain)
TEST_DATABASE_URL=postgres://postgres:dev@localhost:5433/users?sslmode=disable \
  go test -tags=integration ./...    # exercises the repository against real Postgres
```

Integration tests are isolated behind a build tag and skip themselves if `TEST_DATABASE_URL` is not set, so the default run stays fast and requires nothing beyond the Go toolchain. They run the actual migration path before asserting behavior.

CI (`.github/workflows/ci.yml`) runs `gofmt`, `go vet`, the unit tests under `-race`, and the integration suite against a Postgres service container.

## design notes

A few decisions worth calling out, since they are the point of the exercise more than the CRUD itself:

* the repository is an interface with exactly the methods the handler layer calls, defined next to its consumer, not a generic data access abstraction designed up front
* Postgres error codes are translated into domain errors in a single function; nothing above the repository knows what a `pgconn.PgError` looks like
* the HTTP layer never touches `sql.DB` directly and the domain never imports `net/http`
* the list query carries its own `count(*) OVER()`, so the total and the page it describes come from the same instant

## license

Distributed under the MIT license. See `LICENSE` for the full text.
