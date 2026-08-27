
# go-user

A small, production-shaped REST API for user management, written in idiomatic Go using only the standard library plus a Postgres driver. No web framework, no ORM, no dependency injection container. Just `net/http`, `database/sql`, and code you can read start to finish in one sitting.

This project exists to practice the discipline of writing Go the way the standard library itself is written: explicit error handling, small interfaces defined by the consumer, and boundaries you can actually reason about.

## what this is

* a CRUD API over a single `users` resource
* backed by Postgres through `pgx`, accessed via the standard `database/sql` interface
* embedded SQL migrations that run on boot, guarded by a Postgres advisory lock so multiple instances never race each other
* structured logging with `log/slog`, request IDs threaded through `context.Context`
* errors returned to clients as `application/problem+json` (RFC 7807), with a single translation point between domain errors and HTTP status codes
* graceful shutdown on `SIGTERM`/`SIGINT`, with in flight requests drained before the process exits
* a multistage Dockerfile producing a static, nonroot, distroless image

## project layout

```
cmd/
  api/            entry point: wiring only, no logic
internal/
  config/         environment based configuration, fails fast if incomplete
  database/       connection pool setup and embedded migrations
  logger/         slog wrapper that injects request IDs into every log line
  problem/        RFC 7807 error responses
  server/         router assembly and middleware chain
  user/           the actual feature: domain type, handlers, repository, routes
```

Every package under `internal/` is unimportable outside this module by construction. `user/` is organized by feature, not by technical layer: the domain type, its HTTP handlers, its repository, and its route registration all live together and expose one small surface (`RegisterRoutes`) to the rest of the app.

## getting started

You need Go 1.26 and either a running Postgres instance or Docker.

Create a `.env` file:

```
APP_ENV=development
PORT=8080
DATABASE_URL=postgres://postgres:dev@localhost:5433/users?sslmode=disable
READ_TIMEOUT=5s
WRITE_TIMEOUT=10s
HANDLER_TIMEOUT=5s
SHUTDOWN_TIMEOUT=15s
MAX_BODY_BYTES=1048576
```

`DATABASE_URL` is the only required variable. Everything else falls back to a sane default if unset — but a variable that is *set* to something unparseable is a startup error, not a silent fallback: a body limit misconfigured into its default is a security control quietly turned off. `HANDLER_TIMEOUT` must be shorter than `WRITE_TIMEOUT`, or the connection is cut before the timeout response can leave.

A copyable `.env.example` is versioned in the repository.

Start Postgres and run the API:

```bash
make db-up
make run
```

Or run the whole stack in containers:

```bash
docker compose up --build
```

Migrations in `internal/database/migrations` are embedded into the binary and applied automatically on startup, in filename order, each tracked in a `schema_migrations` table so restarts are idempotent.

## api

| method | path             | description                     |
|--------|------------------|---------------------------------|
| GET    | `/healthz`       | liveness, no dependencies       |
| GET    | `/readyz`        | readiness, pings the database   |
| POST   | `/v1/users`      | create a user                   |
| GET    | `/v1/users`      | list users, paginated           |
| GET    | `/v1/users/{id}` | fetch a single user             |
| PATCH  | `/v1/users/{id}` | partial update, requires `If-Match` |
| DELETE | `/v1/users/{id}` | delete a user                   |

`GET /v1/users` accepts `limit` (default 20, capped at 100) and `offset` query parameters and returns a `{items, total, limit, offset}` envelope rather than a bare array, so pagination metadata can grow later without breaking clients.

`PATCH` uses pointer fields (`*string`) in its input struct to distinguish a field the client did not send from a field explicitly set to empty, the same distinction JSON forces you to confront everywhere else.

`PATCH` is a read-modify-write, so it is guarded by optimistic locking. Every user carries a `version`, surfaced as an `ETag`, and `PATCH` requires the `If-Match` header carrying the version the client read:

```bash
ETAG=$(curl -si localhost:8080/v1/users/1 | grep -i '^etag' | cut -d' ' -f2)
curl -X PATCH localhost:8080/v1/users/1 \
     -H 'Content-Type: application/json' -H "If-Match: $ETAG" \
     -d '{"name":"new name"}'
```

Without it the request is a `428 Precondition Required`; with a version that has moved on, a `412 Precondition Failed`. The alternative is what the endpoint used to do: two concurrent updates, each rewriting the whole row from a stale read, both answered `200`, and a stored record in a state neither client asked for.

Request bodies are checked before they are parsed: anything but `application/json` is a `415`, a body over `MAX_BODY_BYTES` is a `413` rather than a shrugging `400`, unknown fields are rejected, and a body carrying more than one JSON document is refused instead of having the remainder silently dropped. Emails are validated against the parsed address rather than merely parsed — `Victim <attacker@evil.com>` is a valid RFC 5322 address and is not a valid user — and normalized to lowercase to match the `lower(email)` unique index.

Every failure path returns a `problem+json` body: domain errors like "not found" or "duplicate" map to specific status codes through one function; anything unexpected is logged with its request ID and returned to the client as a generic 500, never leaking internal detail. Database constraint names stay in the log for the same reason.

`X-Request-ID` is honoured when the caller sends one, so a trace survives across services, but only after it is checked against a conservative character set and length: the id ends up in every log line for the request, and an id carrying newlines is log injection.

## testing

```bash
go test ./...                        # unit tests, no external dependencies
go test -race ./...                  # same, with the race detector
go test -tags=integration ./...      # exercises the repository against real Postgres
```

Integration tests are isolated behind a build tag and skip themselves if `TEST_DATABASE_URL` is not set, so the default test run stays fast and requires nothing beyond the Go toolchain. They run the actual migration path before asserting behavior, so the schema under test is never assumed, only produced.

## design notes

A few decisions worth calling out, since they are the point of the exercise more than the CRUD itself:

* the repository is an interface with exactly the methods the handler layer calls, defined next to its consumer, not a generic data access abstraction designed up front
* Postgres error codes are translated into domain errors in a single function; nothing above the repository knows what a `pgconn.PgError` looks like
* the HTTP layer never touches `sql.DB` directly and the domain never imports `net/http`
* configuration fails at boot if `DATABASE_URL` is missing, rather than starting in a half working state
* request bodies are decoded with `DisallowUnknownFields`, so a typo in a client payload becomes a 400 instead of a silently ignored field

## license

Distributed under the MIT license. See `LICENSE` for the full text.