import type { Clock } from "../../src/background/clock";

export interface ManualClock extends Clock {
  /** Move wall-clock forward without firing timers. */
  advance(ms: number): void;
  /** Fire the earliest pending timer, then flush two microtasks. */
  tick(): Promise<void>;
  /** Delays of pending timers, in scheduling order. */
  pending(): number[];
}

/** Manual clock harness so tests control time deterministically. */
export function createManualClock(start = 0): ManualClock {
  const timers: Array<{ fn: () => void; ms: number; id: number }> = [];
  let id = 0;
  let time = start;
  return {
    now: () => time,
    setTimer: (fn, ms) => {
      timers.push({ fn, ms, id: ++id });
      return id;
    },
    clearTimer: (h) => {
      const i = timers.findIndex((t) => t.id === h);
      if (i >= 0) timers.splice(i, 1);
    },
    advance: (ms) => {
      time += ms;
    },
    tick: async () => {
      const next = timers.shift();
      next?.fn();
      await Promise.resolve();
      await Promise.resolve();
    },
    pending: () => timers.map((t) => t.ms),
  };
}
