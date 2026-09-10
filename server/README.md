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

Store, session and ingest integration tests run only when `TEST_DATABASE_URL` is set.
They migrate and truncate the tables, so use a throwaway database:

```sh
createdb saa_test
make test-db
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
| `internal/event/` | The session event contract: the seven event types, `Valid`, `IsKey`. Mirrors the extension's `types.ts`. |
| `internal/session/` | The session lifecycle: `Lifecycle` applies webhooks (`Started`, `Ended`, `ParticipantsChanged`), ingest batches (`RecordEvents`) and the reaper (`ReapIdle`), persisting via `store` and publishing to the `stream` hub only when a row changed. Ingest, webhook, reaper and API handlers call this, never `store` or `Hub` directly. |
| `internal/stream/` | In-process pub/sub `Hub`, SSE wire payload types (`SessionPayload`, `ActivityPayload`), and the `/stream` handlers. |
| `internal/ingest/` | `POST /ingest`: the extension's batch wire shape (`Batch`, `Event`), `Validate` against the wire contract, and the handler that hands batches to `Lifecycle.RecordEvents`. |
| `internal/webhook/` | The security boundary for Space lifecycle webhooks: `ReadBody` (gunzip, size cap), `Verify` (HMAC-SHA256, every header encoding and both raw/plain bytes tried, match logged), `Parse` into `Envelope` plus `SessionData`/`ParticipantData`, and the `RequireSignature` middleware. Stores and publishes nothing. |
| `web/` | Dashboard assets embedded via `embed.FS`, served at `/`. |

Reserved route prefixes not yet mounted: `/api`, `/webhooks`.

## Endpoints

| Route | Purpose |
|---|---|
| `GET /healthz` | Liveness check, `{"ok":true}`. |
| `POST /ingest` | Extension event batch; `202 {"accepted": n, "duplicates": m}`, `400 {"error": ...}` on a bad batch. Answers CORS preflight for `CORS_ORIGIN`. |
| `GET /stream` | SSE overview: every `session` message plus key `activity` events. |
| `GET /stream/{id}` | SSE for one session: every `session` and `activity` message for `{id}`. |

SSE frames are `id: N`, `event: session|activity`, `data: <single-line JSON>`, blank line;
`: ping` is sent every 15 s while idle. A client that falls more than 64 messages behind
is disconnected (no replay); it should reconnect and re-read `GET /api/sessions`.
`Last-Event-ID` is accepted and logged but not used for replay.

`POST /ingest` takes `{"session_id", "space_id", "events": [{"type", "seq", "ts", "data"}]}`:
1-500 events, `seq` in 1..2^31-1, `ts` epoch milliseconds within a day of server time,
`data` an optional JSON object of at most 4 KiB, body at most 1 MiB. Resending a batch
is safe: events already stored for `(session_id, seq)` are counted as duplicates.
