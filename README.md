
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
SHUTDOWN_TIMEOUT=15s
MAX_BODY_BYTES=1048576
```

`DATABASE_URL` is the only required variable. Everything else falls back to a sane default if unset.

Start Postgres and run the API:

```bash
make db up
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
| PATCH  | `/v1/users/{id}` | partial update                  |
| DELETE | `/v1/users/{id}` | delete a user                   |

`GET /v1/users` accepts `limit` (default 20, capped at 100) and `offset` query parameters and returns a `{items, total, limit, offset}` envelope rather than a bare array, so pagination metadata can grow later without breaking clients.

`PATCH` uses pointer fields (`*string`) in its input struct to distinguish a field the client did not send from a field explicitly set to empty, the same distinction JSON forces you to confront everywhere else.

Every failure path returns a `problem+json` body: domain errors like "not found" or "duplicate" map to specific status codes through one function; anything unexpected is logged with its request ID and returned to the client as a generic 500, never leaking internal detail.

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