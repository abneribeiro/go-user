# **api reference**

## **endpoints**

| method | path             | description                          |
|--------|------------------|--------------------------------------|
| GET    | `/healthz`       | liveness, no dependencies            |
| GET    | `/readyz`        | readiness, pings the database        |
| POST   | `/v1/users`      | create a user                        |
| GET    | `/v1/users`      | list users, paginated                |
| GET    | `/v1/users/{id}` | fetch a single user                  |
| PATCH  | `/v1/users/{id}` | partial update, requires `If-Match`  |
| DELETE | `/v1/users/{id}` | delete a user                        |

## **behaviour**

-   `GET /v1/users` returns a `{items, total, limit, offset}` envelope; `limit` defaults to 20 and is capped at 100
-   `PATCH` is a read-modify-write, so it requires the `ETag` returned by `GET` in `If-Match`: no header is a `428`, a version that moved on is a `412`
-   `DELETE` is not silently idempotent: a user that is not there is a `404`
-   bodies must be `application/json` (`415`), fit `MAX_BODY_BYTES` (`413`), carry no unknown fields and exactly one json document
-   emails are compared against the parsed address, so `Victim <attacker@evil.com>` is rejected, then lowercased to match the `lower(email)` unique index
-   a handler over `HANDLER_TIMEOUT` is cut off with a `503`; every response carries `nosniff`, `X-Frame-Options: DENY` and `Referrer-Policy: no-referrer`
-   `X-Request-ID` is reused when the caller sends one, but only after a charset and length check

## **example: conditional update**

```bash
ETAG=$(curl -si localhost:8080/v1/users/1 | grep -i '^etag' | cut -d' ' -f2 | tr -d '\r')
curl -X PATCH localhost:8080/v1/users/1 \
     -H 'Content-Type: application/json' -H "If-Match: $ETAG" \
     -d '{"name":"new name"}'
```

## **configuration**

read from the environment (`.env` is loaded by the `Makefile`). only `DATABASE_URL` is required; an unparseable value is a startup error.

| variable           | default       |
|--------------------|---------------|
| `APP_ENV`          | `development` |
| `PORT`             | `8080`        |
| `DATABASE_URL`     | required      |
| `READ_TIMEOUT`     | `5s`          |
| `WRITE_TIMEOUT`    | `10s`         |
| `HANDLER_TIMEOUT`  | `5s`          |
| `SHUTDOWN_TIMEOUT` | `15s`         |
| `MAX_BODY_BYTES`   | `1048576`     |
