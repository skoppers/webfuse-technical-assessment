import { beforeEach, describe, expect, it, vi } from "vitest";
import type { IngestBatch, SessionEvent } from "../../shared/types";
import { createBatcher } from "../src/background/batcher";
import { createManualClock } from "./helpers/manualClock";

const ev = (seq: number, type: SessionEvent["type"] = "click"): SessionEvent => ({ type, seq, ts: seq, data: {} });

function setup(sendImpl?: (b: IngestBatch) => Promise<void>, extra: Record<string, unknown> = {}) {
  const t = createManualClock();
  const send = vi.fn(sendImpl ?? (async () => undefined));
  const b = createBatcher({
    sessionId: "s1",
    spaceId: "sp",
    send,
    clock: t,
    flushIntervalMs: 500,
    ...extra,
  });
  return { b, send, t };
}

describe("batcher", () => {
  beforeEach(() => {
    vi.spyOn(console, "warn").mockImplementation(() => undefined);
  });

  it("coalesces events within the flush interval into one batch", async () => {
    const { b, send, t } = setup();
    b.add(ev(1));
    b.add(ev(2));
    b.add(ev(3));
    expect(send).not.toHaveBeenCalled();
    expect(t.pending()).toEqual([500]);
    await t.tick();
    expect(send).toHaveBeenCalledTimes(1);
    expect(send.mock.calls[0]?.[0]).toEqual({
      session_id: "s1",
      space_id: "sp",
      events: [ev(1), ev(2), ev(3)],
    });
    expect(b.size()).toBe(0);
  });

  it("flushes immediately on a key event, including everything queued so far", async () => {
    const { b, send } = setup();
    b.add(ev(1));
    b.add(ev(2, "form_submit"));
    await Promise.resolve();
    expect(send).toHaveBeenCalledTimes(1);
    expect(send.mock.calls[0]?.[0].events.map((e) => e.seq)).toEqual([1, 2]);
  });

  it("re-queues a failed batch at the front, preserving seq order, and backs off", async () => {
    let fail = true;
    const { b, send, t } = setup(async () => {
      if (fail) throw new Error("boom");
    });
    b.add(ev(1));
    await t.tick();
    expect(send).toHaveBeenCalledTimes(1);
    expect(b.failures()).toBe(1);
    expect(b.size()).toBe(1);
    expect(t.pending()).toEqual([1000]); // 500 * 2^1

    b.add(ev(2)); // arrives while backing off
    fail = false;
    await t.tick();
    expect(send).toHaveBeenCalledTimes(2);
    expect(send.mock.calls[1]?.[0].events.map((e) => e.seq)).toEqual([1, 2]);
    expect(b.failures()).toBe(0);
  });

  it("caps backoff at maxBackoffMs", async () => {
    const { b, t } = setup(async () => { throw new Error("boom"); }, { maxBackoffMs: 3000 });
    b.add(ev(1));
    for (let i = 0; i < 6; i++) await t.tick();
    expect(t.pending()).toEqual([3000]);
  });

  it("splits at maxBatchSize", async () => {
    const { b, send, t } = setup(undefined, { maxBatchSize: 2 });
    b.add(ev(1));
    b.add(ev(2)); // hits max → immediate flush
    await Promise.resolve();
    b.add(ev(3));
    await t.tick();
    expect(send).toHaveBeenCalledTimes(2);
    expect(send.mock.calls[0]?.[0].events.map((e) => e.seq)).toEqual([1, 2]);
    expect(send.mock.calls[1]?.[0].events.map((e) => e.seq)).toEqual([3]);
  });

  it("flush on an empty queue does not call send", async () => {
    const { b, send } = setup();
    await b.flush();
    expect(send).not.toHaveBeenCalled();
  });
});
