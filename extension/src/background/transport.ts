/**
 * Transport for IngestBatch → server.
 *
 * STUB: no backend calls yet. Resolves immediately and logs what would be sent.
 * Real implementation is a single function body:
 *
 *   const res = await fetch(`${collectorUrl}/ingest`, {
 *     method: "POST",
 *     headers: { "content-type": "application/json" },
 *     body: JSON.stringify(batch),
 *   });
 *   if (!res.ok) throw new Error(`ingest ${res.status}`);   // batcher re-queues + backs off
 *
 * Requires `host_permissions` for the collector origin (CSP connect-src on the
 * extension origin) and CORS headers from the server.
 */
import type { IngestBatch } from "../../../shared/types";

export type SendBatch = (batch: IngestBatch) => Promise<void>;

export interface TransportOptions {
  collectorUrl: string;
}

export function createTransport({ collectorUrl }: TransportOptions): SendBatch {
  const target = collectorUrl ? `${collectorUrl.replace(/\/$/, "")}/ingest` : "(no COLLECTOR_URL)";
  return async (batch) => {
    const first = batch.events[0]?.seq;
    const last = batch.events[batch.events.length - 1]?.seq;
    console.debug(`[saa] would POST ${batch.events.length} events (seq ${first}–${last}) to ${target}`);
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
