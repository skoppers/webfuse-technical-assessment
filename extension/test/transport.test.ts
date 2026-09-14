import { beforeEach, describe, expect, it, vi } from "vitest";
import type { IngestBatch } from "../../shared/types";
import { createTransport, ingestUrl } from "../src/background/transport";

const batch: IngestBatch = { session_id: "s1", space_id: "sp", client_id: "c1", events: [{ type: "click", seq: 1, ts: 1, data: {} }] };

function mockFetch(status: number, body = "") {
  return vi.fn(async () => new Response(body, { status })) as unknown as typeof fetch;
}

describe("transport", () => {
  beforeEach(() => {
    vi.spyOn(console, "error").mockImplementation(() => undefined);
  });

  it("appends /ingest and strips trailing slashes", () => {
    expect(ingestUrl("https://c.example")).toBe("https://c.example/ingest");
    expect(ingestUrl("https://c.example/")).toBe("https://c.example/ingest");
    expect(ingestUrl(" https://c.example// ")).toBe("https://c.example/ingest");
  });

  it("POSTs the batch as JSON to the ingest URL", async () => {
    const fetchImpl = mockFetch(202, '{"accepted":1}');
    await createTransport({ collectorUrl: "https://c.example", fetchImpl })(batch);
    expect(fetchImpl).toHaveBeenCalledTimes(1);
    const [url, init] = (fetchImpl as unknown as ReturnType<typeof vi.fn>).mock.calls[0] as [string, RequestInit];
    expect(url).toBe("https://c.example/ingest");
    expect(init.method).toBe("POST");
    expect(init.headers).toEqual({ "content-type": "application/json" });
    expect(JSON.parse(init.body as string)).toEqual(batch);
  });

  it("throws on a non-2xx response so the batcher retries", async () => {
    const send = createTransport({ collectorUrl: "https://c.example", fetchImpl: mockFetch(500, "boom") });
    await expect(send(batch)).rejects.toThrow("ingest 500: boom");
  });

  it("propagates network errors", async () => {
    const fetchImpl = vi.fn(async () => {
      throw new TypeError("Failed to fetch");
    }) as unknown as typeof fetch;
    await expect(createTransport({ collectorUrl: "https://c.example", fetchImpl })(batch)).rejects.toThrow("Failed to fetch");
  });

  it("drops batches without throwing when COLLECTOR_URL is empty", async () => {
    const fetchImpl = mockFetch(202);
    await expect(createTransport({ collectorUrl: "", fetchImpl })(batch)).resolves.toBeUndefined();
    expect(fetchImpl).not.toHaveBeenCalled();
    expect(console.error).toHaveBeenCalled();
  });
});
