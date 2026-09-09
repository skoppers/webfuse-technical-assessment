/**
 * The one time dependency the background pipeline reads. Injected so tests can drive
 * timers and wall-clock deterministically; production uses `systemClock`.
 */
export interface Clock {
  now(): number;
  setTimer(fn: () => void, ms: number): unknown;
  clearTimer(handle: unknown): void;
}

export const systemClock: Clock = {
  now: () => Date.now(),
  setTimer: (fn, ms) => setTimeout(fn, ms),
  clearTimer: (h) => clearTimeout(h as ReturnType<typeof setTimeout>),
};
