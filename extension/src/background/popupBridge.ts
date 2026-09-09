/**
 * Background side of the popup protocol: pins the popup open on session start and
 * pushes `meter:event` updates. (`meter:subscribe` is answered in index.ts.)
 */
import type { SessionEvent } from "../../../shared/types";
import type { MeterEventMessage, MeterState } from "../messages";

export const POPUP_WIDTH = 300;
export const POPUP_HEIGHT = 230;

export function pinPopup(): void {
  try {
    const action = browser.browserAction;
    action.setPopupStyles({ backgroundColor: "transparent", boxShadow: "none", padding: 0 });
    action.setPopupPosition({ bottom: "16px", right: "16px" });
    action.resizePopup(POPUP_WIDTH, POPUP_HEIGHT);
    action.openPopup(true);
  } catch (err) {
    console.warn("[saa] could not pin popup", err);
  }
}

/** Fire-and-forget: rejects/pends when no popup is open, which is fine. */
export function pushMeterEvent(event: SessionEvent, state: MeterState): void {
  const message: MeterEventMessage = { type: "meter:event", event, state };
  try {
    browser.runtime.sendMessage(message).catch(() => undefined);
  } catch {
    /* no listeners */
  }
}
