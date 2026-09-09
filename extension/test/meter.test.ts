import { describe, expect, it } from "vitest";
import type { SessionEvent } from "../../shared/types";
import { createMeter } from "../src/background/meter";

const ev = (seq: number, type: SessionEvent["type"] = "click", ts = seq * 100): SessionEvent => ({
  type,
  seq,
  ts,
  data: {},
});

describe("meter", () => {
  it("initialises every counter to zero", () => {
    const s = createMeter().snapshot(0);
    expect(s.counters).toEqual({
      click: 0, keydown: 0, input: 0, scroll: 0, navigation: 0, form_submit: 0, sensitive_url: 0,
    });
    expect(s.rate).toBe(0);
    expect(s.recent).toEqual([]);
  });

  it("increments per-type counters", () => {
    const m = createMeter();
    m.push(ev(1, "click"), 0);
    m.push(ev(2, "click"), 0);
    const s = m.push(ev(3, "form_submit"), 0);
    expect(s.counters.click).toBe(2);
    expect(s.counters.form_submit).toBe(1);
  });

  it("caps recent at recentSize, oldest first", () => {
    const m = createMeter({ recentSize: 5 });
    for (let i = 1; i <= 7; i++) m.push(ev(i), 0);
    expect(m.snapshot(0).recent.map((e) => e.seq)).toEqual([3, 4, 5, 6, 7]);
  });

  it("rate counts only events inside the window and decays", () => {
    const m = createMeter({ windowMs: 10_000 });
    m.push(ev(1), 0);
    m.push(ev(2), 5_000);
    expect(m.push(ev(3), 9_000).rate).toBe(3);
    expect(m.snapshot(10_001).rate).toBe(2); // first one aged out
    expect(m.snapshot(19_001).rate).toBe(0);
  });

  it("snapshot returns copies and does not mutate state", () => {
    const m = createMeter();
    m.push(ev(1), 0);
    const a = m.snapshot(0);
    a.counters.click = 99;
    a.recent.length = 0;
    expect(m.snapshot(0).counters.click).toBe(1);
    expect(m.snapshot(0).recent).toHaveLength(1);
  });
});
