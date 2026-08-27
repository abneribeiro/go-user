
# go-user

A small, production-shaped REST API for user management, written in idiomatic Go with only the standard library plus a Postgres driver. No framework, no ORM, no DI container: just `net/http` and `database/sql`.

## what this is

* a CRUD API over a single `users` resource, backed by Postgres through `pgx`
* SQL migrations embedded with `go:embed`, applied on boot behind a Postgres advisory lock
* structured logging with `log/slog`, request IDs threaded through `context.Context`
* errors as `application/problem+json` (RFC 9457), from a single domain-to-status translation point
* concurrent updates rejected instead of silently merged, via `ETag` / `If-Match`
* graceful shutdown on `SIGTERM`/`SIGINT`, and a multi-stage distroless non-root image

## getting started

Go 1.26 and either Docker or a running Postgres.

```bash
cp .env.example .env
make db-up
make run
```

`DATABASE_URL` is the only required variable; the rest default. A variable set to something unparseable is a startup error, not a silent fallback. Or run everything in containers with `docker compose up --build`.

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

* `GET /v1/users` returns a `{items, total, limit, offset}` envelope; `limit` defaults to 20 and is capped at 100
* `PATCH` is a read-modify-write, so it requires the `ETag` returned by `GET` in `If-Match`: no header is a `428`, a version that moved on is a `412`
* `DELETE` is not silently idempotent: a user that is not there is a `404`
* bodies must be `application/json` (`415`), fit `MAX_BODY_BYTES` (`413`), carry no unknown fields and exactly one JSON document
* emails are compared against the parsed address, so `Victim <attacker@evil.com>` is rejected, then lowercased to match the `lower(email)` unique index
* a handler over `HANDLER_TIMEOUT` is cut off with a `503`; every response carries `nosniff`, `X-Frame-Options: DENY` and `Referrer-Policy: no-referrer`
* `X-Request-ID` is reused when the caller sends one, but only after a charset and length check

```bash
ETAG=$(curl -si localhost:8080/v1/users/1 | grep -i '^etag' | cut -d' ' -f2 | tr -d '\r')
curl -X PATCH localhost:8080/v1/users/1 \
     -H 'Content-Type: application/json' -H "If-Match: $ETAG" \
     -d '{"name":"new name"}'
```

## testing

```bash
go test ./...                        # unit tests, no external dependencies
go test -race ./...                  # same, with the race detector (needs a C toolchain)
TEST_DATABASE_URL=postgres://postgres:dev@localhost:5433/users?sslmode=disable \
  go test -tags=integration ./...    # repository against real Postgres
```

Integration tests sit behind a build tag and skip themselves without `TEST_DATABASE_URL`. CI runs gofmt, `go vet`, both suites.

## more

The design decisions behind this code — native routing, hand-written middleware, embedded migrations, `problem+json` — are walked through in [Building a REST API in Go Without Frameworks](https://www.abneribeiro.com/go-crud).

## license

MIT. See `LICENSE`.
