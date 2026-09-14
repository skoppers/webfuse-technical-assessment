/**
 * Event contract shared by the extension, the dashboard and (by hand) the Go server.
 *
 * Privacy posture: only event *types*, counts, tags and URLs. Never field values,
 * never key values, never mouse coordinates.
 */

export type EventType =
  | "click"
  | "keydown"
  | "input"
  | "scroll"
  | "navigation"
  | "form_submit"
  | "sensitive_url";

export const EVENT_TYPES: readonly EventType[] = [
  "click",
  "keydown",
  "input",
  "scroll",
  "navigation",
  "form_submit",
  "sensitive_url",
];

/** Events the assignment calls out; flushed immediately and highlighted in every UI. */
export const KEY_EVENT_TYPES: readonly EventType[] = ["form_submit", "sensitive_url"];

export function isEventType(value: unknown): value is EventType {
  return typeof value === "string" && (EVENT_TYPES as readonly string[]).includes(value);
}

export function isKeyEvent(event: Pick<SessionEvent, "type">): boolean {
  return KEY_EVENT_TYPES.includes(event.type);
}

// ---- per-type payloads (narrowed at the edges) ----------------------------------------

export interface ClickData {
  tag: string;
  id?: string;
  role?: string;
}

/**
 * The key pressed (`KeyboardEvent.key`) and its physical code. `key` is omitted and
 * `redacted` set when the target is a password input.
 */
export interface KeydownData {
  key?: string;
  code?: string;
  redacted?: true;
}

export interface InputData {
  tag: string;
  field?: string;
}

export interface ScrollData {
  y: number;
  /** Change in `y` since the previous emitted scroll on the same element. */
  deltaY: number;
  /** `"document"` for page scroll, otherwise the scrolled element's tag. */
  tag: string;
  id?: string;
}

export interface NavigationData {
  url: string;
  title?: string;
}

/** No field values, ever. */
export interface FormSubmitData {
  action?: string;
  fieldCount: number;
}

export interface SensitiveUrlData {
  url: string;
  /** The sensitive path segment that matched, e.g. "checkout". */
  matched: string;
  /** Which detector produced it: content script or background tabs.onUpdated. */
  source?: "content" | "tabs";
}

export interface EventDataByType {
  click: ClickData;
  keydown: KeydownData;
  input: InputData;
  scroll: ScrollData;
  navigation: NavigationData;
  form_submit: FormSubmitData;
  sensitive_url: SensitiveUrlData;
}

// ---- wire shapes ------------------------------------------------------------------------

export interface SessionEvent {
  type: EventType;
  /** Monotonic per client (one background boot), assigned by the extension background. */
  seq: number;
  /** Client epoch-ms at capture time. */
  ts: number;
  data: Record<string, unknown>;
}

/** Body of `POST /ingest`. */
export interface IngestBatch {
  session_id: string;
  space_id: string;
  /**
   * Random id generated once per boot of one participant's background. With `seq` it
   * forms the server's idempotency key `(session_id, client_id, seq)`, so two participants
   * (or a restarted background) each counting from seq 1 never collide.
   */
  client_id: string;
  events: SessionEvent[];
}
