# Session Activity Analyzer — Webfuse extension

Captures activity inside a live Webfuse session and shows a pinned live meter.

## Layout

```
src/
  manifest.json          MV3 manifest (Webfuse subset) — copied to dist/
  content.ts             wiring only: installCapture(window, runtime sink) (tab owner only)
  capture/index.ts       Capture module: DOM events → CapturedEvents behind installCapture(win, sink)
  capture/*.ts           internals: throttle, history hooks, scroll tracker, element → payload
  background/index.ts    browser-API wiring only: messages, tabs.onUpdated, session_ended → recorder
  background/recorder.ts session recorder: owns the pipeline (seq, sensitive_url detection + dedup, meter, batcher, popup fan-out)
  background/*.ts        internals: clock, meter, batcher, transport (fetch POST to /ingest), sensitiveDetector, popupBridge, session
  popup.html / popup.ts  pinned live meter (vanilla DOM)
  popup/render.ts        pure label/rate/time formatters
  messages.ts            internal message protocol + type guards
  webfuse.d.ts           ambient typings for the browser.* subset we use
../shared/types.ts       event contract shared with dashboard/server
```

## Build / test

```bash
npm install
npm run build        # → dist/ (flat files: manifest.json, background.js, content.js, popup.html, popup.js, icon24.png)
npm run watch
npm run test
npm run typecheck
```

`dist/` is the deployable artifact. **Zip `extension/dist`, not `extension/`** when using
`scripts/deploy-extension.sh` from `docs/deployment.md` (set `EXT_DIR=extension/dist`).

## Configuration

Manifest `env`: `COLLECTOR_URL` — public origin of the ingest server. No secrets. Override
per space in the Session Editor → Extension details. `host_permissions` is currently
`<all_urls>`; narrow it to the collector origin once known.

Sensitive paths are hardcoded in `src/background/sensitiveDetector.ts`

## Privacy
No field values or mouse coordinates are ever read; key names are redacted in password
inputs. See `capture/describe.ts` (the only element → payload code), the Capture module
tests (`test/capture-module.test.ts`, asserting emitted events never carry values) and
`popup/render.ts` tests that assert labels never leak.
