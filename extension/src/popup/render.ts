/**
 * Pure presentation helpers for the popup. Must never surface input values or key names —
 * they are not in the data to begin with, but keep this the only place labels are built.
 */
import type { SessionEvent } from "../../../shared/types";

export const RATE_FULL_SCALE = 30;

export function rateToWidth(rate: number, fullScale = RATE_FULL_SCALE): number {
  if (!Number.isFinite(rate) || rate <= 0) return 0;
  return Math.min(rate / fullScale, 1);
}

function pathOf(url: unknown): string {
  if (typeof url !== "string") return "";
  try {
    const u = new URL(url);
    return u.pathname + u.search;
  } catch {
    return url;
  }
}

export function formatEventLabel(event: SessionEvent): string {
  const d = event.data;
  switch (event.type) {
    case "click":
      return [d.tag, d.id ? `#${d.id}` : "", d.role ? `[${d.role}]` : ""].filter(Boolean).join("");
    case "keydown":
      return d.redacted ? "key •" : `key ${String(d.key ?? d.code ?? "")}`.trim();
    case "input":
      return d.field ? `${d.tag} ${d.field}` : String(d.tag ?? "");
    case "scroll": {
      const delta = typeof d.deltaY === "number" ? ` (${d.deltaY >= 0 ? "+" : ""}${d.deltaY})` : "";
      const where = d.tag && d.tag !== "document" ? `${d.tag}${d.id ? `#${d.id}` : ""} ` : "";
      return `${where}y=${d.y ?? 0}${delta}`;
    }
    case "navigation":
      return pathOf(d.url) || "/";
    case "form_submit":
      return `${d.fieldCount ?? 0} fields${d.action ? ` → ${pathOf(d.action) || d.action}` : ""}`;
    case "sensitive_url":
      return `${d.matched ?? ""} ${pathOf(d.url)}`.trim();
    default:
      return "";
  }
}

export function relativeTime(ts: number, now: number = Date.now()): string {
  const s = Math.max(0, Math.round((now - ts) / 1000));
  if (s < 1) return "now";
  if (s < 60) return `${s}s`;
  const m = Math.floor(s / 60);
  if (m < 60) return `${m}m`;
  return `${Math.floor(m / 60)}h`;
}
