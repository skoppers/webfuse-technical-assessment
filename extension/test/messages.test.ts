import { describe, expect, it } from "vitest";
import { isCaptureMessage, isMeterMessage, isMeterSubscribeMessage } from "../src/messages";
import { isEventType, isKeyEvent } from "../../shared/types";

describe("message guards", () => {
  it("accepts a well-formed capture message", () => {
    expect(
      isCaptureMessage({ type: "capture:event", event: { type: "click", ts: 1, data: { tag: "a" } } }),
    ).toBe(true);
  });

  it("rejects capture messages with missing fields", () => {
    expect(isCaptureMessage({ type: "capture:event" })).toBe(false);
    expect(isCaptureMessage({ type: "capture:event", event: { type: "click", ts: "1", data: {} } })).toBe(false);
    expect(isCaptureMessage({ type: "capture:event", event: { type: "click", ts: 1, data: null } })).toBe(false);
    expect(isCaptureMessage(null)).toBe(false);
    expect(isCaptureMessage("capture:event")).toBe(false);
  });

  it("discriminates meter messages", () => {
    const state = { rate: 0, counters: {}, recent: [] };
    expect(isMeterSubscribeMessage({ type: "meter:subscribe" })).toBe(true);
    expect(isMeterMessage({ type: "meter:snapshot", state })).toBe(true);
    expect(isMeterMessage({ type: "meter:event", event: {}, state })).toBe(true);
    expect(isMeterMessage({ type: "meter:event" })).toBe(false);
    expect(isMeterMessage({ type: "capture:event", state })).toBe(false);
  });
});

describe("shared types helpers", () => {
  it("validates event types", () => {
    expect(isEventType("form_submit")).toBe(true);
    expect(isEventType("mousemove")).toBe(false);
    expect(isEventType(3)).toBe(false);
  });

  it("identifies key events", () => {
    expect(isKeyEvent({ type: "sensitive_url" })).toBe(true);
    expect(isKeyEvent({ type: "scroll" })).toBe(false);
  });
});
