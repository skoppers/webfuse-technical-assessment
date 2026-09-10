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

## Layout

| Path | Purpose |
|---|---|
| `cmd/server/main.go` | Wiring only: config → deps → routes → run, graceful shutdown. |
| `internal/config/` | `Config` + `Load()` from env. |
| `internal/httpx/` | JSON read/write, error responses, request-logging middleware. |
| `web/` | Dashboard assets embedded via `embed.FS`, served at `/`. |

Reserved route prefixes for later packages: `/api`, `/ingest`, `/stream`, `/webhooks`.
