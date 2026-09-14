/**
 * Background entry: browser-API wiring only. Every event from every source is handed to
 * the session recorder (`recorder.ts`), which owns the pipeline.
 * Runs once per participant, as soon as they join the session.
 */
import { isCaptureMessage, isMeterSubscribeMessage } from "../messages";
import { pinPopup, pushMeterEvent } from "./popupBridge";
import { createRecorder } from "./recorder";
import { loadSessionContext } from "./session";
import { createTransport, readCollectorUrl } from "./transport";

async function main(): Promise<void> {
  const ctx = await loadSessionContext();
  const recorder = createRecorder({
    sessionId: ctx.sessionId,
    spaceId: ctx.spaceId,
    // Once per service-worker boot; a restart gets a fresh id so its seq restarting at 1 does not collide.
    clientId: crypto.randomUUID(),
    send: createTransport({ collectorUrl: readCollectorUrl() }),
    onUpdate: pushMeterEvent,
  });

  browser.runtime.onMessage.addListener((message) => {
    if (isCaptureMessage(message)) {
      recorder.record(message.event);
      return undefined;
    }
    if (isMeterSubscribeMessage(message)) return recorder.snapshot();
    return undefined;
  });

  // Redundant URL source for the sensitive_url key event only (never a navigation event);
  // the recorder matches and dedups.
  try {
    browser.tabs.onUpdated.addListener((_tabId, changeInfo) => {
      if (changeInfo?.url) recorder.observeUrl(changeInfo.url, "tabs");
    });
  } catch (err) {
    console.warn("[saa] tabs.onUpdated unavailable", err);
  }

  // Session lifecycle: stop + flush on end. Both listener spellings exist in official docs.
  const onSessionPayload = (payload: WebfuseExt.SessionPayload) => {
    if (payload?.event_type !== "session_ended") return;
    console.info("[saa] session_ended", payload.final_location);
    void recorder.end();
  };
  try {
    browser.webfuseSession.on.addListener(onSessionPayload);
  } catch {
    /* not available in this context */
  }
  try {
    browser.webfuseSession.onMessage?.addListener(onSessionPayload);
  } catch {
    /* not available in this context */
  }

  pinPopup();
}

void main();
