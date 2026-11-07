<div align="center">
  <br />
  <h3><b>go-user</b></h3>
  <p>a production-shaped rest api for user management, written with the go standard library and a postgres driver.</p>
</div>

<br />

## **overview**

-   a crud api over a single `users` resource, backed by postgres through `pgx`
-   no framework, no orm, no di container: just `net/http` and `database/sql`
-   errors as `application/problem+json` (rfc 9457), from a single domain-to-status translation point
-   structured logging with `log/slog`, request ids threaded through `context.Context`
-   stack: `go 1.26`, `postgres 16`, `docker`

## **design & trade-offs**

-   **optimistic concurrency over last-write-wins:** `PATCH` is a read-modify-write, so it requires the `ETag` from `GET` in `If-Match`. no header is a `428`, a stale version is a `412`. concurrent updates are rejected instead of silently merged.
-   **migrations on boot, behind a lock:** sql migrations are embedded with `go:embed` and applied at startup under a postgres advisory lock, so several replicas starting at once do not race each other.
-   **fail loudly on config:** `DATABASE_URL` is the only required variable. a variable set to something unparseable is a startup error, not a silent fallback.
-   **strict input:** bodies must be `application/json`, fit `MAX_BODY_BYTES`, carry no unknown fields and exactly one json document. emails are compared against the parsed address, so `Victim <attacker@evil.com>` is rejected.
-   **shutdown and packaging:** graceful shutdown on `SIGTERM`/`SIGINT`, and a multi-stage distroless image running as non-root.

## **quickstart**

needs go 1.26 and either docker or a running postgres.

```bash
cp .env.example .env
make db-up      # postgres on localhost:5433
make run        # api on localhost:8080
```

or run everything in containers:

```bash
docker compose up --build
```

## **testing**

```bash
go test ./...                        # unit tests, no external dependencies
go test -race ./...                  # same, with the race detector (needs a c toolchain)
TEST_DATABASE_URL=postgres://postgres:dev@localhost:5433/users?sslmode=disable \
  go test -tags=integration ./...    # repository against real postgres
```

-   integration tests sit behind a build tag and skip themselves without `TEST_DATABASE_URL`
-   ci runs `gofmt`, `go vet`, the unit suite with `-race` and the integration suite against a postgres service

## **docs**

-   [api reference and configuration](docs/api.md)
-   the design decisions behind this code (native routing, hand-written middleware, embedded migrations, `problem+json`) are walked through in [building a rest api in go without frameworks](https://www.abneribeiro.com/go-crud)

## **license**

-   [mit](LICENSE)
