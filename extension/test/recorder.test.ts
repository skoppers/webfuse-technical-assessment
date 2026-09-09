import { beforeEach, describe, expect, it, vi } from "vitest";
import type { IngestBatch } from "../../shared/types";
import type { CapturedEvent } from "../src/messages";
import { createRecorder } from "../src/background/recorder";
import { createManualClock } from "./helpers/manualClock";

const cap = (type: CapturedEvent["type"], ts: number, data: Record<string, unknown> = {}): CapturedEvent => ({
  type,
  ts,
  data,
});
const nav = (url: string, ts: number): CapturedEvent => cap("navigation", ts, { url });

function setup(sendImpl?: (b: IngestBatch) => Promise<void>) {
  const t = createManualClock();
  const send = vi.fn(sendImpl ?? (async () => undefined));
  const onUpdate = vi.fn();
  const r = createRecorder({ sessionId: "s1", spaceId: "sp", send, onUpdate, clock: t });
  return { r, send, onUpdate, t };
}

const sentSeqs = (send: ReturnType<typeof vi.fn>, call = 0) =>
  (send.mock.calls[call]?.[0] as IngestBatch).events.map((e) => e.seq);

describe("recorder", () => {
  beforeEach(() => {
    vi.spyOn(console, "debug").mockImplementation(() => undefined);
    vi.spyOn(console, "warn").mockImplementation(() => undefined);
  });

  it("assigns monotonic seq across sources starting at 1", async () => {
    const { r, onUpdate, send, t } = setup();
    expect(r.record(cap("click", 0))).toBe(true);
    t.advance(10);
    expect(r.observeUrl("https://a/checkout", "tabs")).toBe(true);
    expect(r.record(cap("input", 20))).toBe(true);
    expect(onUpdate.mock.calls.map((c) => c[0].seq)).toEqual([1, 2, 3]);
    expect(onUpdate.mock.calls.map((c) => c[0].type)).toEqual(["click", "sensitive_url", "input"]);
    expect(onUpdate.mock.calls[1]?.[0]).toEqual({
      type: "sensitive_url",
      seq: 2,
      ts: 10,
      data: { url: "https://a/checkout", matched: "checkout", source: "tabs" },
    });
    await Promise.resolve();
    await t.tick();
    expect(send.mock.calls.flatMap((c) => c[0].events.map((e) => e.seq))).toEqual([1, 2, 3]);
  });

  it("a navigation to a sensitive path yields navigation then sensitive_url from the content source", async () => {
    const { r, onUpdate, send } = setup();
    expect(r.record(nav("https://a/checkout?step=2", 100))).toBe(true);
    expect(onUpdate.mock.calls.map((c) => c[0])).toEqual([
      { type: "navigation", seq: 1, ts: 100, data: { url: "https://a/checkout?step=2" } },
      {
        type: "sensitive_url",
        seq: 2,
        ts: 100,
        data: { url: "https://a/checkout?step=2", matched: "checkout", source: "content" },
      },
    ]);
    await Promise.resolve(); // key event ⇒ immediate flush of both
    expect(send).toHaveBeenCalledTimes(1);
    expect(sentSeqs(send)).toEqual([1, 2]);
  });

  it("a navigation to an ordinary path yields navigation only", () => {
    const { r, onUpdate } = setup();
    expect(r.record(nav("https://a/products/1", 0))).toBe(true);
    expect(onUpdate.mock.calls.map((c) => c[0].type)).toEqual(["navigation"]);
  });

  it("dedups sensitive_url across sources on the observation's own ts", async () => {
    const { r, onUpdate, send, t } = setup();
    expect(r.record(nav("https://a/checkout", 0))).toBe(true);
    t.advance(500);
    expect(r.observeUrl("https://a/checkout", "tabs")).toBe(false);
    await Promise.resolve();
    expect(onUpdate).toHaveBeenCalledTimes(2);
    expect(r.snapshot().counters.sensitive_url).toBe(1);
    expect(send).toHaveBeenCalledTimes(1);
    expect(sentSeqs(send)).toEqual([1, 2]);

    t.advance(1500); // window elapsed
    expect(r.observeUrl("https://a/checkout", "tabs")).toBe(true);
    await Promise.resolve();
    expect(onUpdate).toHaveBeenCalledTimes(3);
    expect(onUpdate.mock.calls[2]?.[0]).toMatchObject({ seq: 3, type: "sensitive_url", data: { source: "tabs" } });
    expect(r.snapshot().counters.sensitive_url).toBe(2);
  });

  it("dedups in the other direction too: tabs first, then content navigation", () => {
    const { r, onUpdate, t } = setup();
    expect(r.observeUrl("https://a/billing", "tabs")).toBe(true);
    t.advance(100);
    expect(r.record(nav("https://a/billing", 100))).toBe(true);
    expect(onUpdate.mock.calls.map((c) => c[0].type)).toEqual(["sensitive_url", "navigation"]);
  });

  it("accepts different sensitive URLs independently", () => {
    const { r, onUpdate } = setup();
    expect(r.record(nav("https://a/checkout", 0))).toBe(true);
    expect(r.observeUrl("https://a/billing", "tabs")).toBe(true);
    expect(onUpdate.mock.calls.map((c) => c[0].type)).toEqual(["navigation", "sensitive_url", "sensitive_url"]);
  });

  it("observeUrl records nothing for ordinary or garbage URLs", () => {
    const { r, onUpdate } = setup();
    expect(r.observeUrl("https://a/products/1", "tabs")).toBe(false);
    expect(r.observeUrl("not a url", "tabs")).toBe(false);
    expect(onUpdate).not.toHaveBeenCalled();
    expect(r.snapshot().counters.sensitive_url).toBe(0);
  });

  it("rejects an inbound sensitive_url: callers hand over URLs, not the key event", () => {
    const { r, onUpdate } = setup();
    expect(r.record(cap("sensitive_url", 0, { url: "https://a/checkout", matched: "checkout", source: "content" }))).toBe(false);
    expect(onUpdate).not.toHaveBeenCalled();
  });

  it("flushes immediately on a key event, including earlier queued events, in seq order", async () => {
    const { r, send, t } = setup();
    r.record(cap("click", 0));
    r.record(cap("keydown", 10));
    expect(send).not.toHaveBeenCalled();
    r.record(cap("form_submit", 20, { fieldCount: 2 }));
    await Promise.resolve();
    expect(send).toHaveBeenCalledTimes(1);
    const batch = send.mock.calls[0]?.[0] as IngestBatch;
    expect(batch.session_id).toBe("s1");
    expect(batch.space_id).toBe("sp");
    expect(batch.events.map((e) => [e.seq, e.type])).toEqual([[1, "click"], [2, "keydown"], [3, "form_submit"]]);
    expect(t.pending()).toEqual([]);
  });

  it("coalesces ordinary events until the timer fires", async () => {
    const { r, send, t } = setup();
    r.record(cap("click", 0));
    r.record(cap("scroll", 10));
    r.record(cap("navigation", 20));
    expect(send).not.toHaveBeenCalled();
    expect(t.pending()).toEqual([500]);
    await t.tick();
    expect(send).toHaveBeenCalledTimes(1);
    expect(sentSeqs(send)).toEqual([1, 2, 3]);
  });

  it("end() flushes queued events and rejects everything after, including mid-flush", async () => {
    let resolveSend: () => void = () => undefined;
    const { r, send, onUpdate, t } = setup(
      () =>
        new Promise<void>((resolve) => {
          resolveSend = resolve;
        }),
    );
    r.record(cap("click", 0));
    r.record(cap("input", 10));
    const ending = r.end();
    expect(send).toHaveBeenCalledTimes(1);
    expect(sentSeqs(send)).toEqual([1, 2]);

    // arrives while the final send is in flight
    expect(r.record(cap("click", 20))).toBe(false);
    resolveSend();
    await ending;

    expect(r.record(cap("click", 30))).toBe(false);
    expect(r.observeUrl("https://a/checkout", "tabs")).toBe(false);
    expect(onUpdate).toHaveBeenCalledTimes(2);
    expect(send).toHaveBeenCalledTimes(1);
    t.advance(30);
    expect(r.snapshot().counters.click).toBe(1);
  });

  it("onUpdate receives the seq'd event and a state matching snapshot()", () => {
    const { r, onUpdate, t } = setup();
    r.record(cap("click", 100, { tag: "button" }));
    const state = r.record(cap("keydown", 200)) && onUpdate.mock.calls[1]?.[1];
    expect(onUpdate.mock.calls[1]?.[0]).toEqual({ type: "keydown", seq: 2, ts: 200, data: {} });
    expect(state.counters.click).toBe(1);
    expect(state.counters.keydown).toBe(1);
    expect(state.rate).toBe(2);
    expect(state.recent.map((e: { seq: number }) => e.seq)).toEqual([1, 2]);
    t.advance(200);
    expect(r.snapshot()).toEqual(state);
  });

  it("re-queues a failed batch and later sends it with seq order preserved", async () => {
    let fail = true;
    const { r, send, t } = setup(async () => {
      if (fail) throw new Error("boom");
    });
    r.record(cap("click", 0));
    await t.tick();
    expect(send).toHaveBeenCalledTimes(1);
    expect(t.pending()).toEqual([1000]);

    r.record(cap("scroll", 10));
    fail = false;
    await t.tick();
    expect(send).toHaveBeenCalledTimes(2);
    expect(sentSeqs(send, 1)).toEqual([1, 2]);
  });
});
