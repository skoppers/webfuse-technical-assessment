/**
 * Transport for IngestBatch → server: one POST per batch to `${COLLECTOR_URL}/ingest`.
 *
 * Throws on network failure or a non-2xx response so the batcher re-queues the batch and
 * backs off (server dedups on (session_id, client_id, seq), so retries are safe). With no
 * COLLECTOR_URL configured it logs once and drops batches rather than throwing forever.
 *
 * Requires `host_permissions` for the collector origin and CORS headers from the server
 * (`CORS_ORIGIN`), both of which are in place.
 */
import type { IngestBatch } from "../../../shared/types";

export type SendBatch = (batch: IngestBatch) => Promise<void>;

export interface TransportOptions {
  collectorUrl: string;
  /** Injectable for tests; defaults to the global fetch. */
  fetchImpl?: typeof fetch;
}

export function ingestUrl(collectorUrl: string): string {
  return `${collectorUrl.trim().replace(/\/+$/, "")}/ingest`;
}

export function createTransport({ collectorUrl, fetchImpl = fetch }: TransportOptions): SendBatch {
  if (!collectorUrl.trim()) {
    console.error("[saa] COLLECTOR_URL is not set; events will not be sent");
    return async () => undefined;
  }
  const target = ingestUrl(collectorUrl);
  return async (batch) => {
    const res = await fetchImpl(target, {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify(batch),
    });
    if (!res.ok) {
      const body = await res.text().catch(() => "");
      throw new Error(`ingest ${res.status}${body ? `: ${body.slice(0, 200)}` : ""}`);
    }
  };
}

/** Reads the collector origin from manifest env; never a secret. */
export function readCollectorUrl(): string {
  try {
    return browser.webfuseSession.env?.COLLECTOR_URL ?? "";
  } catch {
    return "";
  }
}
