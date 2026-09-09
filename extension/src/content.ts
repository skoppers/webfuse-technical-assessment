/**
 * Content script: runs inside the proxied page on the tab owner's side.
 * Wiring only — installs the Capture module with the runtime message adapter as its sink.
 * No UI, no network, no state.
 */
import { installCapture } from "./capture";
import type { CaptureMessage } from "./messages";

installCapture(window, (event) => {
  const message: CaptureMessage = { type: "capture:event", event };
  try {
    // The background may not be listening yet on very early loads; that's fine.
    browser.runtime.sendMessage(message).catch(() => undefined);
  } catch {
    /* runtime unavailable (e.g. script evaluated outside a session) */
  }
});
