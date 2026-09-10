# Webfuse Session Activity Analyzer

Live activity analytics for Webfuse sessions: a browser extension captures in-session
events, a Go server ingests them and receives the Space's lifecycle webhooks, and an
embedded dashboard shows live and past sessions.

| Part | Docs |
|---|---|
| `extension/` | [extension/README.md](extension/README.md) |
| `server/` | [server/README.md](server/README.md) — run, deploy, [decisions](server/README.md#decisions) |
| `docs/` | [deployment](docs/deployment.md), [first real session checklist](docs/first-real-session.md) |
| `shared/` | `types.ts`, the event contract shared by extension and server |
