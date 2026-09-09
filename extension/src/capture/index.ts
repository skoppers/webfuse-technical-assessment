/**
 * Capture module: DOM events in, CapturedEvents out.
 * Owns the listeners for the six DOM-derived event types, the single (global) scroll
 * throttle, timestamping and the install-once guard. (`sensitive_url` is derived from
 * `navigation` by the Session Recorder in the background, not here.)
 * The element → payload helpers, scroll tracker, throttle and history hooks are internal.
 *
 * Interface: `installCapture(win, sink)` → uninstall. The sink is the only way out;
 * production passes the runtime message adapter, tests pass an array push.
 */
import type { EventDataByType, EventType } from "../../../shared/types";
import type { CapturedEvent } from "../messages";
import { describeClick, describeForm, describeInput, describeKeydown } from "./describe";
import { installNavigationHooks } from "./navigation";
import { createScrollTracker } from "./scroll";
import { throttle } from "./throttle";

declare global {
  interface Window {
    __saaInstalled?: boolean;
  }
}

export const SCROLL_THROTTLE_MS = 1000;

export type CaptureSink = (event: CapturedEvent) => void;

/**
 * Attaches capture to `win` and forwards every derived event to `sink`.
 * Idempotent per window: a second call while installed is a no-op and returns a no-op.
 * Emits the initial `navigation` synchronously.
 */
export function installCapture(win: Window, sink: CaptureSink): () => void {
  if (win.__saaInstalled) return () => undefined;
  win.__saaInstalled = true;

  const doc = win.document;
  let installed = true;

  function emit<T extends EventType>(type: T, data: EventDataByType[T]): void {
    sink({ type, ts: Date.now(), data: data as Record<string, unknown> });
  }

  function onNavigated(url: string, title: string | undefined): void {
    emit("navigation", title ? { url, title } : { url });
  }

  const onClick = (e: Event) => emit("click", describeClick(e.target));
  const onKeydown = (e: KeyboardEvent) => emit("keydown", describeKeydown(e));
  const onInput = (e: Event) => emit("input", describeInput(e.target));
  const onSubmit = (e: Event) => {
    const form = e.target instanceof HTMLFormElement ? e.target : null;
    if (form) emit("form_submit", describeForm(form));
  };
  const describeScroll = createScrollTracker(win);
  // One throttle for every scrollable, not one per element: protects the page as a whole.
  const onScroll = throttle((e: Event) => emit("scroll", describeScroll(e.target)), SCROLL_THROTTLE_MS);

  doc.addEventListener("click", onClick, true);
  doc.addEventListener("keydown", onKeydown, true);
  doc.addEventListener("input", onInput, true);
  doc.addEventListener("submit", onSubmit, true);
  win.addEventListener("scroll", onScroll, { capture: true, passive: true });

  const uninstallNavigation = installNavigationHooks(win, onNavigated);
  onNavigated(win.location.href, doc.title || undefined);

  return () => {
    if (!installed) return;
    installed = false;
    doc.removeEventListener("click", onClick, true);
    doc.removeEventListener("keydown", onKeydown, true);
    doc.removeEventListener("input", onInput, true);
    doc.removeEventListener("submit", onSubmit, true);
    win.removeEventListener("scroll", onScroll, true);
    uninstallNavigation();
    win.__saaInstalled = false;
  };
}
