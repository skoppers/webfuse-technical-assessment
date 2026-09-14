# Webfuse Session Activity Analyzer

Live activity analytics for Webfuse sessions: a browser extension captures in-session
events, a Go server ingests them and receives the Space's lifecycle webhooks, and an
embedded dashboard shows live and past sessions.

Viewable live at: https://webfuse-technical-assessment.onrender.com/

It's on free tiers of render and aiven postgres, so it might not always be up.

Connected to this space: https://surfly.online/studio/spaces/3330/overview

## What I built
- A Webfuse extension that captures events, shows some metrics in a popup and sends the events to a server
- A Go server that ingests events and stores them in a postgres database. Streams the events through SSE to the dashboard.
- Dashboard in plain js/html to display a live session list and a per-session live feed with timeline scrubber  

## Decisions to discuss:
- The lifecyle reconciliation that happens on the server. Instead of fully trusting webhooks or events, I decided to reduce the best information from both sources.
- I used SSE for streaming the dashboard over websockets.

## Run local instructions:

| Part | Docs |
|---|---|
| `extension/` | [extension/README.md](extension/README.md) |
| `server/` | [server/README.md](server/README.md) |
| `shared/` | `types.ts`, the event contract shared by extension and server |
