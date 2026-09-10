// Shared dashboard module: API client, SSE subscription, the live meter, and
// the formatters both pages use. Plain ES module, no build step.
//
// Privacy posture mirrors shared/types.ts: labels are built from tags,
// counts and URLs only; the data never carries field values or key names for
// password inputs.

// ---- event contract (mirrors shared/types.ts and internal/event) ----------

export const EVENT_TYPES = [
  "click",
  "keydown",
  "input",
  "scroll",
  "navigation",
  "form_submit",
  "sensitive_url",
];

export const KEY_EVENT_TYPES = new Set(["form_submit", "sensitive_url"]);

export const isKey = (type) => KEY_EVENT_TYPES.has(type);

// ---- read API -------------------------------------------------------------

async function getJSON(url) {
  const res = await fetch(url, { headers: { Accept: "application/json" } });
  if (!res.ok) {
    const err = new Error(`${res.status} ${url}`);
    err.status = res.status;
    throw err;
  }
  return res.json();
}

export const api = {
  sessions: () => getJSON("/api/sessions").then((b) => b.sessions),
  session: (id) => getJSON(`/api/sessions/${encodeURIComponent(id)}`),
  events: (id) => getJSON(`/api/sessions/${encodeURIComponent(id)}/events`).then((b) => b.events),
};

// ---- SSE ------------------------------------------------------------------

// subscribe opens an EventSource on path and dispatches "session" and
// "activity" frames to handlers. The browser reconnects on its own and
// replays Last-Event-ID; onReconnect fires on every open after the first so
// the page can re-read state it may have missed. onState receives
// "connecting" | "open" | "error". Returns a close function.
export function subscribe(path, { onSession, onActivity, onReconnect, onState }) {
  const es = new EventSource(path);
  let opened = false;
  const parse = (fn) => (e) => {
    if (!fn) return;
    try {
      fn(JSON.parse(e.data));
    } catch (err) {
      console.error("stream: bad frame", err, e.data);
    }
  };
  onState?.("connecting");
  es.addEventListener("open", () => {
    onState?.("open");
    if (opened) onReconnect?.();
    opened = true;
  });
  es.addEventListener("error", () => onState?.("error"));
  es.addEventListener("session", parse(onSession));
  es.addEventListener("activity", parse(onActivity));
  return () => es.close();
}

// ---- live meter (port of extension/src/background/meter.ts) ----------------

export const METER_WINDOW_MS = 10_000;
export const RECENT_SIZE = 5;
export const RATE_FULL_SCALE = 30;

export function createMeter({ windowMs = METER_WINDOW_MS, recentSize = RECENT_SIZE } = {}) {
  let rateWindowTs = [];
  const counters = Object.fromEntries(EVENT_TYPES.map((t) => [t, 0]));
  let recent = [];
  let total = 0;

  const prune = (now) => {
    const cutoff = now - windowMs;
    let i = 0;
    while (i < rateWindowTs.length && rateWindowTs[i] <= cutoff) i++;
    if (i > 0) rateWindowTs = rateWindowTs.slice(i);
  };

  return {
    // add records event as observed at `now` (defaults to the event's own ts,
    // which is what replay wants; live pages pass Date.now()).
    add(event, now = event.ts) {
      prune(now);
      rateWindowTs.push(now);
      if (event.type in counters) counters[event.type]++;
      total++;
      recent = [event, ...recent].slice(0, recentSize);
    },
    snapshot(now = Date.now()) {
      prune(now);
      return { rate: rateWindowTs.length, counters: { ...counters }, recent: [...recent], total };
    },
  };
}

export function renderMeter(root, snap) {
  root.querySelector(".rate .n").textContent = String(snap.rate);
  root.querySelector(".bar > div").style.width = `${Math.min(snap.rate / RATE_FULL_SCALE, 1) * 100}%`;
  root.querySelector(".counters").innerHTML = EVENT_TYPES.map(
    (t) => `<dt data-key="${isKey(t) ? 1 : 0}">${t}</dt><dd>${snap.counters[t] ?? 0}</dd>`,
  ).join("") + `<dt>total</dt><dd>${snap.total}</dd>`;
  root.querySelector(".recent").innerHTML = snap.recent.length
    ? snap.recent
        .map((e) => `<li>${badge(e.type)}<span class="label">${esc(formatEventLabel(e))}</span></li>`)
        .join("")
    : `<li class="muted">no events yet</li>`;
}

// ---- formatting -----------------------------------------------------------

export function esc(s) {
  return String(s ?? "").replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[c]);
}

export const shortId = (id) => (id && id.length > 12 ? `${id.slice(0, 8)}…` : id ?? "");

export const badge = (type) => `<span class="badge" data-type="${esc(type)}">${esc(type)}</span>`;

export const statusPill = (status) => `<span class="status" data-status="${esc(status)}">${esc(status)}</span>`;

function pathOf(url) {
  if (typeof url !== "string") return "";
  try {
    const u = new URL(url);
    return u.pathname + u.search;
  } catch {
    return url;
  }
}

// formatEventLabel is the popup's label builder (extension/src/popup/render.ts).
export function formatEventLabel(event) {
  const d = event.data ?? {};
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

export function relativeTime(ts, now = Date.now()) {
  const s = Math.max(0, Math.round((now - ts) / 1000));
  if (s < 1) return "now";
  if (s < 60) return `${s}s`;
  const m = Math.floor(s / 60);
  if (m < 60) return `${m}m`;
  const h = Math.floor(m / 60);
  if (h < 48) return `${h}h`;
  return `${Math.floor(h / 24)}d`;
}

// duration renders the span between two instants (ISO strings or ms) as h:mm:ss.
export function duration(from, to = Date.now()) {
  const a = typeof from === "string" ? Date.parse(from) : from;
  const b = typeof to === "string" ? Date.parse(to) : to;
  const s = Math.max(0, Math.floor((b - a) / 1000));
  const h = Math.floor(s / 3600);
  const m = Math.floor((s % 3600) / 60);
  const sec = s % 60;
  const mm = String(m).padStart(2, "0");
  const ss = String(sec).padStart(2, "0");
  return h ? `${h}:${mm}:${ss}` : `${m}:${ss}`;
}

export const clockTime = (ms) =>
  new Date(ms).toLocaleTimeString(undefined, { hour12: false, hour: "2-digit", minute: "2-digit", second: "2-digit" });

export const dateTime = (iso) => (iso ? new Date(iso).toLocaleString() : "—");

// bindConnState wires a subscribe() onState callback to a .conn element.
export function bindConnState(el) {
  const labels = { connecting: "connecting", open: "live", error: "reconnecting…" };
  return (state) => {
    el.dataset.state = state;
    el.textContent = labels[state] ?? state;
  };
}
