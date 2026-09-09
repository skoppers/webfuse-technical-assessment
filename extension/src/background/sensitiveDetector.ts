/**
 * Sensitive URL detector: the one place the `sensitive_url` key event is made.
 * Owns the segment list, the whole-segment path matcher, the event's data shape and the
 * dedup across the two URL sources (content-script navigation and background
 * tabs.onUpdated) by (url, short time window). Internal to the Session Recorder.
 */
import type { SensitiveUrlData } from "../../../shared/types";
import type { CapturedEvent } from "../messages";
import { systemClock, type Clock } from "./clock";

// hardcoded for the demo
export const SENSITIVE_SEGMENTS: readonly string[] = [
  "checkout",
  "billing",
  "payment",
  "settings",
  "account",
  "admin",
];

export const SENSITIVE_WINDOW_MS = 2000;

export type SensitiveSource = NonNullable<SensitiveUrlData["source"]>;

/**
 * Matches if any whole path segment equals a sensitive name, anywhere in the path:
 * "/settings", "/settings/profile" and "/manage/pages/384/settings/general" all match
 * "settings"; "/settingsfoo" and "/blog/settings-guide" do not.
 * Returns the matched segment name, or null.
 */
export function matchSensitivePath(
  pathname: string,
  segments: readonly string[] = SENSITIVE_SEGMENTS,
): string | null {
  const parts = pathname.toLowerCase().split("/").filter(Boolean);
  for (const name of segments) {
    if (parts.includes(name.toLowerCase())) return name;
  }
  return null;
}

export interface SensitiveDetectorOptions {
  clock?: Clock;
  segments?: readonly string[];
  /** Dedup window across sources. */
  windowMs?: number;
}

export interface SensitiveDetector {
  /**
   * Returns a `sensitive_url` captured event if `url` parses, matches a sensitive segment,
   * and has not been emitted within the window; otherwise null. Only emitted hits are
   * recorded for dedup, so a suppressed duplicate does not extend the window.
   */
  observe(url: string, source: SensitiveSource, ts?: number): CapturedEvent | null;
  /** URLs currently inside the dedup window (tests). */
  size(): number;
}

export function createSensitiveDetector({
  clock = systemClock,
  segments = SENSITIVE_SEGMENTS,
  windowMs = SENSITIVE_WINDOW_MS,
}: SensitiveDetectorOptions = {}): SensitiveDetector {
  const seen = new Map<string, number>();

  const prune = (now: number) => {
    for (const [url, ts] of seen) {
      if (now - ts >= windowMs) seen.delete(url);
    }
  };

  return {
    observe(url, source, ts = clock.now()) {
      let pathname: string;
      try {
        pathname = new URL(url).pathname;
      } catch {
        return null;
      }
      const matched = matchSensitivePath(pathname, segments);
      if (!matched) return null;

      prune(ts);
      const last = seen.get(url);
      if (last !== undefined && ts - last < windowMs) return null;
      seen.set(url, ts);

      const data: SensitiveUrlData = { url, matched, source };
      return { type: "sensitive_url", ts, data: data as unknown as Record<string, unknown> };
    },
    size: () => seen.size,
  };
}
