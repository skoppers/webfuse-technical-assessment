/**
 * Session Recorder: the whole background pipeline behind one interface.
 * Every event, whatever its source, goes through one path: session-ended gate → seq
 * assignment → meter → batcher → popup fan-out. The `sensitive_url` key event is made
 * here, by the Sensitive URL detector, from two URL sources: every `navigation` event
 * passed to `record`, and every URL passed to `observeUrl` (background tabs.onUpdated).
 * `index.ts` only wires browser APIs to this.
 *
 * Time comes from one injected `Clock`; tuning constants live with the internals that own them.
 */
import type { IngestBatch, SessionEvent } from "../../../shared/types";
import type { CapturedEvent, MeterState } from "../messages";
import { createBatcher } from "./batcher";
import { systemClock, type Clock } from "./clock";
import { createMeter } from "./meter";
import { createSensitiveDetector, type SensitiveSource } from "./sensitiveDetector";

export interface RecorderOptions {
  sessionId: string;
  spaceId: string;
  send: (batch: IngestBatch) => Promise<void>;
  /** Called after every accepted event with the event and the fresh meter state (popup fan-out). */
  onUpdate?: (event: SessionEvent, state: MeterState) => void;
  clock?: Clock;
}

export interface Recorder {
  /**
   * Single path for every captured event. A `navigation` event may additionally yield a
   * `sensitive_url` event (next seq) from the same URL. Returns false if dropped: session
   * ended, or an inbound `sensitive_url` — callers never build that event themselves;
   * hand over the URL instead (via a `navigation` event or `observeUrl`).
   */
  record(captured: CapturedEvent): boolean;
  /**
   * URL-only source (tabs.onUpdated). Never yields a `navigation` event. Returns true if a
   * `sensitive_url` event was recorded; false if ended, not sensitive, unparsable, or
   * deduped against a recent emission of the same URL from either source.
   */
  observeUrl(url: string, source: SensitiveSource): boolean;
  /** Session ended: stop accepting events immediately, then flush what is queued. */
  end(): Promise<void>;
  snapshot(): MeterState;
}

export function createRecorder({ sessionId, spaceId, send, onUpdate, clock = systemClock }: RecorderOptions): Recorder {
  const meter = createMeter({ clock });
  const sensitive = createSensitiveDetector({ clock });
  const batcher = createBatcher({ sessionId, spaceId, send, clock });

  /** Monotonic per-session sequence; the server's idempotency key is (session_id, seq). */
  let seq = 0;
  let ended = false;

  const accept = (captured: CapturedEvent): void => {
    const event: SessionEvent = { ...captured, seq: ++seq };
    // Meter is keyed on the event's own ts, not the wall clock.
    const state = meter.push(event, event.ts);
    batcher.add(event);
    console.debug(`[saa] #${event.seq} ${event.type}`, event.data);
    onUpdate?.(event, state);
  };

  const observe = (url: string, source: SensitiveSource, ts: number): boolean => {
    const hit = sensitive.observe(url, source, ts);
    if (!hit) return false;
    accept(hit);
    return true;
  };

  return {
    record(captured) {
      if (ended) return false;
      if (captured.type === "sensitive_url") return false;
      accept(captured);
      if (captured.type === "navigation") observe(String(captured.data.url ?? ""), "content", captured.ts);
      return true;
    },
    observeUrl(url, source) {
      if (ended) return false;
      return observe(url, source, clock.now());
    },
    async end() {
      ended = true;
      await batcher.flush();
    },
    snapshot: () => meter.snapshot(clock.now()),
  };
}
