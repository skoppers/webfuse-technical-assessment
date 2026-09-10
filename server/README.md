# server

Go backend for the Webfuse Activity Analyzer: ingest, webhooks, SSE stream, read API and
the embedded dashboard.

## Run locally

```sh
cp .env.example .env   # edit values
set -a; . ./.env; set +a
go run ./cmd/server
curl localhost:8080/healthz   # {"ok":true}
```

`go build ./...`, `go vet ./...`, `go test ./...` must stay green.

## Environment

| Var | Default | Purpose |
|---|---|---|
| `PORT` | `8080` | Listen port. |
| `DATABASE_URL` | required | Postgres connection string. |
| `WEBHOOK_SIGNING_KEY` | required | Space webhook key for HMAC verification. |
| `PUBLIC_URL` | — | Server's public origin. |
| `REAPER_IDLE_SECONDS` | `60` | Idle time before a live session is ended. |
| `REAPER_TICK_SECONDS` | `30` | Reaper sweep interval. |
| `CORS_ORIGIN` | `*` | `Access-Control-Allow-Origin` for `/ingest`. |

## Database

The server needs Postgres 14+ and applies its embedded migrations on every start
(a second start is a no-op). Point `DATABASE_URL` at either:

- a hosted instance (e.g. an Aiven service URL with `?sslmode=require`), or
- a local container:

  ```sh
  docker run --rm -d --name saa-pg -e POSTGRES_HOST_AUTH_METHOD=trust \
    -e POSTGRES_DB=saa -p 5432:5432 postgres:16
  export DATABASE_URL='postgres://postgres@localhost:5432/saa?sslmode=disable'
  ```

Store integration tests run only when `TEST_DATABASE_URL` is set. They migrate and
truncate the tables, so use a throwaway database:

```sh
createdb saa_test
TEST_DATABASE_URL='postgres://localhost:5432/saa_test?sslmode=disable' go test ./internal/store/...
```

Regenerate queries after editing `internal/store/queries.sql` or the migrations: `make sqlc`
(runs a pinned sqlc via `go run`; nothing to install).

## Layout

| Path | Purpose |
|---|---|
| `cmd/server/main.go` | Wiring only: config → deps → routes → run, graceful shutdown. |
| `internal/config/` | `Config` + `Load()` from env. |
| `internal/httpx/` | JSON read/write, error responses, request-logging middleware. |
| `internal/store/` | Postgres handle (`Open`), embedded goose migrations (`Migrate`), schema in `migrations/`, queries in `queries.sql`, `Store` wrapper in `store.go`. |
| `internal/store/gen/` | sqlc output for `queries.sql` (generated, do not edit). |
| `web/` | Dashboard assets embedded via `embed.FS`, served at `/`. |

Reserved route prefixes for later packages: `/api`, `/ingest`, `/stream`, `/webhooks`.
