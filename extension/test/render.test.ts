import { describe, expect, it } from "vitest";
import type { SessionEvent } from "../../shared/types";
import { formatEventLabel, rateToWidth, relativeTime } from "../src/popup/render";

const ev = (type: SessionEvent["type"], data: Record<string, unknown>): SessionEvent => ({ type, seq: 1, ts: 0, data });

describe("popup render helpers", () => {
  it("rateToWidth clamps to [0,1]", () => {
    expect(rateToWidth(0)).toBe(0);
    expect(rateToWidth(-3)).toBe(0);
    expect(rateToWidth(15)).toBe(0.5);
    expect(rateToWidth(300)).toBe(1);
    expect(rateToWidth(NaN)).toBe(0);
  });

  it("formats labels per type", () => {
    expect(formatEventLabel(ev("click", { tag: "button", id: "buy", role: "button" }))).toBe("button#buy[button]");
    expect(formatEventLabel(ev("click", { tag: "a" }))).toBe("a");
    expect(formatEventLabel(ev("keydown", {}))).toBe("key");
    expect(formatEventLabel(ev("input", { tag: "input", field: "email" }))).toBe("input email");
    expect(formatEventLabel(ev("scroll", { y: 240 }))).toBe("y=240");
    expect(formatEventLabel(ev("scroll", { y: 240, deltaY: 40, tag: "document" }))).toBe("y=240 (+40)");
    expect(formatEventLabel(ev("scroll", { y: 10, deltaY: -30, tag: "div", id: "list" }))).toBe("div#list y=10 (-30)");
    expect(formatEventLabel(ev("navigation", { url: "https://x.test/a/b?q=1" }))).toBe("/a/b?q=1");
    expect(formatEventLabel(ev("form_submit", { fieldCount: 3, action: "/login" }))).toBe("3 fields → /login");
    expect(formatEventLabel(ev("sensitive_url", { url: "https://x.test/checkout/pay", matched: "checkout" }))).toBe(
      "checkout /checkout/pay",
    );
  });

  it("labels never leak values even if extra keys sneak into data", () => {
    const label = formatEventLabel(ev("input", { tag: "input", field: "pw", value: "hunter2" }));
    expect(label).not.toContain("hunter2");
    const key = formatEventLabel(ev("keydown", { redacted: true, code: "KeyA", key: "a" }));
    expect(key).toBe("key •");
  });

  it("relativeTime formats seconds, minutes, hours", () => {
    expect(relativeTime(1000, 1000)).toBe("now");
    expect(relativeTime(0, 5_000)).toBe("5s");
    expect(relativeTime(0, 125_000)).toBe("2m");
    expect(relativeTime(0, 7_200_000)).toBe("2h");
    expect(relativeTime(5_000, 0)).toBe("now");
  });
});
