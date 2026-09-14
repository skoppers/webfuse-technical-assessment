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

Everyday targets (`make <target>`):

| Target | Does |
|---|---|
| `run` | Loads `.env` when present, then `go run ./cmd/server`. |
| `lint` | `go vet ./...`, plus `staticcheck ./...` when it is installed. |
| `test` | `go test ./...` (no database needed). |
| `test-db` | Integration tests against `TEST_DATABASE_URL` (see Database). |
| `sqlc` | Regenerate `internal/store/gen/`. |
| `docker-build` | Build the production image `saa-server` (override with `IMAGE=`). |
| `docker-run` | Run that image with `.env` on `localhost:8080`. |
| `smoke` | End-to-end check against a running server: signed webhooks, ingest, SSE, read API. |

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

The reaper ends any live session with no events for `REAPER_IDLE_SECONDS` (a missed `ended`
webhook, or a session only ever seen through the extension), sweeping once at startup and
then every `REAPER_TICK_SECONDS`.

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

Store, session, reaper, ingest, webhook and API integration tests run only when `TEST_DATABASE_URL` is set.
They migrate and truncate the tables, so use a throwaway database:

```sh
createdb saa_test
make test-db
```

Regenerate queries after editing `internal/store/queries.sql` or the migrations: `make sqlc`
(runs a pinned sqlc via `go run`; nothing to install).

## Deploy

Build the image from the repo root (the Dockerfile's context is the whole repo so it can
pick up the dashboard sources):

```sh
docker build -f server/Dockerfile -t saa-server .
docker run --rm --env-file server/.env -p 8080:8080 saa-server
```

Or from `server/`: `make docker-build` and `make docker-run`.

Configuration is environment only. Required: `DATABASE_URL`, `WEBHOOK_SIGNING_KEY`.
Optional: `PORT`, `PUBLIC_URL`, `CORS_ORIGIN` and the reaper vars, all described in
[Environment](#environment).

The image:

- is `gcr.io/distroless/static-debian12:nonroot` plus one static Go binary, no shell,
  runs as a non-root user;
- listens on a single port, `8080` by default (`PORT` to change it);
- has the dashboard assets and the migrations embedded, and applies the migrations on
  every start, so the database only needs to exist and be reachable;
- answers `GET /healthz` with `{"ok":true}` for the host's health check.

Any Docker host that gives the container a public HTTPS URL works (Render is one option);
set `PUBLIC_URL` to that URL and point the Space webhook and the extension at it.

## Layout

| Path | Purpose |
|---|---|
| `cmd/server/main.go` | Wiring only: config → deps → routes → run, graceful shutdown. |
| `internal/config/` | `Config` + `Load()` from env. |
| `internal/jsonx/` | `OrEmptyObject`: substitutes `{}` for absent JSON so jsonb columns, stream payloads and API responses never carry null. |
| `internal/httpx/` | JSON read/write, error responses, request-logging and no-store cache middleware. |
| `internal/store/` | Postgres handle (`Open`), embedded goose migrations (`Migrate`), schema in `migrations/`, queries in `queries.sql`, `Store` wrapper in `store.go`. |
| `internal/store/gen/` | sqlc output for `queries.sql` (generated, do not edit). |
| `internal/event/` | The session event contract: the seven event types, `Valid`, `IsKey`. Mirrors the extension's `types.ts`. |
| `internal/session/` | The session lifecycle: `Lifecycle` applies webhooks (`Started`, `Ended`, `ParticipantsChanged`), ingest batches (`RecordEvents`) and the reaper (`ReapIdle`), persisting via `store` and publishing to the `stream` hub only when a row changed. Ingest, webhook, reaper and API handlers call this, never `store` or `Hub` directly. |
| `internal/reaper/` | The reaper loop: `Run` sweeps once at startup, then every tick until the context is done, calling `Lifecycle.ReapIdle` and logging each ended session. Owns only the schedule; the idle rule and the ending live in `session`. |
| `internal/stream/` | In-process pub/sub `Hub`, SSE wire payload types (`SessionPayload`, `ActivityPayload`), and the `/stream` handlers. |
| `internal/ingest/` | `POST /ingest`: the extension's batch wire shape (`Batch`, `Event`), `Validate` against the wire contract, and the handler that hands batches to `Lifecycle.RecordEvents`. |
| `internal/webhook/` | The security boundary for Space lifecycle webhooks: `ReadBody` (gunzip, size cap), `Verify` (HMAC-SHA256, every header encoding and both raw/plain bytes tried, match logged), `Parse` into `Envelope` plus `SessionData`/`ParticipantData`, the `RequireSignature` middleware, and the `POST /webhooks/webfuse` handler that routes each verified envelope by category to `Lifecycle.Started`/`Ended`/`ParticipantsChanged`. Owns the webhook wire shapes; stores and publishes nothing itself. |
| `internal/api/` | The dashboard's read API under `/api`: session list, session detail and the full ordered event log for replay. Owns the response types (`Session`, `Event`) and maps them from `session` rows; calls `Lifecycle.List`/`Get`/`Events` only. |
| `web/` | Static dashboard embedded via `embed.FS`, served at `/`: `index.html` (live session list), `session.html?id=` (live feed or replay), `app.js`, `style.css`. No build step. |

## Endpoints

| Route | Purpose |
|---|---|
| `GET /healthz` | Liveness check, `{"ok":true}`. |
| `GET /api/sessions?limit=N` | Session list for the overview: `{"sessions": [...]}`, live first then newest first, each session plus `key_event_count` and `last_key_event` (an event, or `null` when it has none). `limit` defaults to 100, is clamped to 500, and must be a positive integer (`400 {"error": ...}` otherwise). |
| `GET /api/sessions/{id}` | One session, `404 {"error":"not found"}` when unknown. |
| `GET /api/sessions/{id}/events` | The session's full event log for replay: `{"session_id", "events": [...]}` ordered by `ts`, `seq`, `client_id`; `404` when the session is unknown. |
| `POST /ingest` | Extension event batch; `202 {"accepted": n, "duplicates": m}`, `400 {"error": ...}` on a bad batch. Answers CORS preflight for `CORS_ORIGIN`. |
| `GET /stream` | SSE overview: every `session` message plus key `activity` events. |
| `GET /stream/{id}` | SSE for one session: every `session` and `activity` message for `{id}`. |
| `POST /webhooks/webfuse` | Signed Space webhook (`Webhook-Signature` HMAC-SHA256 under `WEBHOOK_SIGNING_KEY`, gzip body accepted). Handles `space.session.started`, `.ended`, `.participant_joined`, `.participant_left`; every verified delivery is `200 {"outcome": "applied|stale|ignored"}` (`stale` for replays, out-of-order retries and ended sessions; `ignored` for other categories or a `participant_left` on an unknown session), `400` for an unparseable body, `401` when unsigned or mis-signed. |

`/api` responses are `Cache-Control: no-store`. A session is `{"session_id", "space_id",
"status": "live|ended", "started_at", "ended_at", "participant_count", "source": "webhook|event",
"metadata"}`, the same shape as the SSE `session` payload plus `source` and `metadata`;
timestamps are RFC 3339, `ended_at` is `null` while the session is live and `metadata` is
always an object. An event is `{"client_id", "seq", "type", "ts", "received_at", "data"}` with
`client_id` the sending background boot (`seq` is unique only within it), `ts` in epoch
milliseconds, the unit the extension sends to `/ingest`, `received_at` RFC 3339 and `data`
always an object.

SSE frames are `id: N`, `event: session|activity`, `data: <single-line JSON>`, blank line;
`: ping` is sent every 15 s while idle. A client that falls more than 64 messages behind
is disconnected (no replay); it should reconnect and re-read `GET /api/sessions`.
`Last-Event-ID` is accepted and logged but not used for replay.

`POST /ingest` takes `{"session_id", "space_id", "client_id", "events": [{"type", "seq", "ts", "data"}]}`:
`client_id` 1-64 characters (a random id the extension background generates once per boot,
so every participant and every service-worker restart counts `seq` from 1 independently),
1-500 events, `seq` in 1..2^31-1, `ts` epoch milliseconds within a day of server time,
`data` an optional JSON object of at most 4 KiB, body at most 1 MiB. Resending a batch
is safe: events already stored for `(session_id, client_id, seq)` are counted as duplicates.
