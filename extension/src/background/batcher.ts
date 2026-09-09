/**
 * Batches SessionEvents into IngestBatch payloads.
 * Flushes every `flushIntervalMs`, immediately on a key event, or when `maxBatchSize` is
 * reached. A failed send re-queues the batch at the front (server dedups on
 * (session_id, seq)) and backs off exponentially.
 */
import { isKeyEvent as defaultIsKeyEvent, type IngestBatch, type SessionEvent } from "../../../shared/types";
import { systemClock, type Clock } from "./clock";

export const FLUSH_INTERVAL_MS = 500;
export const MAX_BATCH_SIZE = 100;
export const MAX_BACKOFF_MS = 10_000;

export interface BatcherOptions {
  sessionId: string;
  spaceId: string;
  send: (batch: IngestBatch) => Promise<void>;
  clock?: Clock;
  // internal-seam tuning; the recorder uses the defaults.
  flushIntervalMs?: number;
  maxBatchSize?: number;
  maxBackoffMs?: number;
  isKeyEvent?: (event: SessionEvent) => boolean;
}

export interface Batcher {
  add(event: SessionEvent): void;
  flush(): Promise<void>;
  size(): number;
  /** Consecutive failed sends; resets on success. */
  failures(): number;
}

export function createBatcher(opts: BatcherOptions): Batcher {
  const {
    sessionId,
    spaceId,
    send,
    clock = systemClock,
    flushIntervalMs = FLUSH_INTERVAL_MS,
    maxBatchSize = MAX_BATCH_SIZE,
    maxBackoffMs = MAX_BACKOFF_MS,
    isKeyEvent = defaultIsKeyEvent,
  } = opts;

  let queue: SessionEvent[] = [];
  let timer: unknown = null;
  let sending = false;
  let failures = 0;

  const schedule = (ms: number) => {
    if (timer !== null) return;
    timer = clock.setTimer(() => {
      timer = null;
      void flush();
    }, ms);
  };

  const backoffMs = () => Math.min(flushIntervalMs * 2 ** failures, maxBackoffMs);

  async function flush(): Promise<void> {
    if (sending || queue.length === 0) return;
    if (timer !== null) {
      clock.clearTimer(timer);
      timer = null;
    }
    const events = queue.slice(0, maxBatchSize);
    queue = queue.slice(maxBatchSize);
    sending = true;
    try {
      await send({ session_id: sessionId, space_id: spaceId, events });
      failures = 0;
    } catch (err) {
      failures += 1;
      queue = [...events, ...queue];
      console.warn(`[saa] send failed (${failures}), retrying in ${backoffMs()}ms`, err);
    } finally {
      sending = false;
    }
    if (queue.length > 0) schedule(failures > 0 ? backoffMs() : 0);
  }

  return {
    add(event) {
      queue.push(event);
      if (isKeyEvent(event) || queue.length >= maxBatchSize) {
        void flush();
      } else {
        schedule(failures > 0 ? backoffMs() : flushIntervalMs);
      }
    },
    flush,
    size: () => queue.length,
    failures: () => failures,
  };
}
