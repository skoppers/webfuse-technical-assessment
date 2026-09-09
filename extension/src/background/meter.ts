/**
 * Live meter state: rolling events/window rate, per-type counters and a
 * ring buffer of recent events. Pure and clock-injectable; the background owns the
 * single instance and the popup only ever receives snapshots.
 */
import { EVENT_TYPES, type EventType, type SessionEvent } from "../../../shared/types";
import type { MeterState } from "../messages";
import { systemClock, type Clock } from "./clock";

export const METER_WINDOW_MS = 10_000;
export const RECENT_SIZE = 5;

export interface MeterOptions {
  clock?: Clock;
  windowMs?: number;
  recentSize?: number;
}

export interface Meter {
  /** `now` defaults to the clock; the recorder passes the event's own `ts`. */
  push(event: SessionEvent, now?: number): MeterState;
  snapshot(now?: number): MeterState;
}

function emptyCounters(): Record<EventType, number> {
  const counters = {} as Record<EventType, number>;
  for (const type of EVENT_TYPES) counters[type] = 0;
  return counters;
}

export function createMeter({ clock = systemClock, windowMs = METER_WINDOW_MS, recentSize = RECENT_SIZE }: MeterOptions = {}): Meter {
  const counters = emptyCounters();
  const recent: SessionEvent[] = [];
  let rateWindowTs: number[] = [];

  const prune = (now: number) => {
    const cutoff = now - windowMs;
    let i = 0;
    while (i < rateWindowTs.length && (rateWindowTs[i] as number) <= cutoff) i++;
    if (i > 0) rateWindowTs = rateWindowTs.slice(i);
  };

  const snapshot = (now: number = clock.now()): MeterState => {
    prune(now);
    return { rate: rateWindowTs.length, counters: { ...counters }, recent: [...recent] };
  };

  return {
    push(event, now = clock.now()) {
      counters[event.type] += 1;
      recent.push(event);
      if (recent.length > recentSize) recent.splice(0, recent.length - recentSize);
      rateWindowTs.push(now);
      return snapshot(now);
    },
    snapshot,
  };
}
