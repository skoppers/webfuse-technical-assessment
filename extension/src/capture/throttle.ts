/** Leading-edge throttle: the first call fires, subsequent calls within `ms` are dropped. */
export function throttle<A extends unknown[]>(fn: (...args: A) => void, ms: number): (...args: A) => void {
  let last = -Infinity;
  return (...args: A) => {
    const t = Date.now();
    if (t - last < ms) return;
    last = t;
    fn(...args);
  };
}
