/**
 * Popup: renders the live meter from background snapshots + incremental updates.
 * Holds no source-of-truth state.
 */
import { EVENT_TYPES, isKeyEvent, type EventType } from "../../shared/types";
import { isMeterMessage, type MeterState, type MeterSubscribeMessage } from "./messages";
import { formatEventLabel, rateToWidth, relativeTime } from "./popup/render";

const LABELS: Record<EventType, string> = {
  click: "clicks",
  keydown: "keys",
  input: "inputs",
  scroll: "scrolls",
  navigation: "navs",
  form_submit: "submits",
  sensitive_url: "sensitive",
};

const getElementByIdOrThrow = (id: string): HTMLElement => {
  const element = document.getElementById(id)
  if (element == null) throw Error(`${id} element is missing`)
  return element
}

const rateEl = getElementByIdOrThrow("rate");
const barEl = getElementByIdOrThrow("bar");
const countersEl = getElementByIdOrThrow("counters");
const feedEl = getElementByIdOrThrow("feed");

const counterCells = new Map<EventType, HTMLElement>();
for (const type of EVENT_TYPES) {
  const row = document.createElement("div");
  if (isKeyEvent({ type })) row.className = "key";
  const label = document.createElement("span");
  label.textContent = LABELS[type];
  const value = document.createElement("span");
  value.textContent = "0";
  row.append(label, value);
  countersEl.append(row);
  counterCells.set(type, value);
}

let current: MeterState | null = null;

const render = (state: MeterState, now = Date.now()): void => {
  current = state;
  rateEl.textContent = String(state.rate);
  barEl.style.width = `${Math.round(rateToWidth(state.rate) * 100)}%`;
  for (const type of EVENT_TYPES) {
    const cell = counterCells.get(type);
    if (cell) cell.textContent = String(state.counters[type] ?? 0);
  }

  feedEl.replaceChildren();
  if (state.recent.length === 0) {
    const li = document.createElement("li");
    li.className = "empty";
    li.textContent = "waiting for activity…";
    feedEl.append(li);
    return;
  }
  for (const event of [...state.recent].reverse()) {
    const li = document.createElement("li");
    if (isKeyEvent(event)) li.className = "key";
    const type = document.createElement("span");
    type.className = "type";
    type.textContent = event.type;
    const label = document.createElement("span");
    label.textContent = formatEventLabel(event);
    label.title = label.textContent;
    const time = document.createElement("span");
    time.className = "time";
    time.textContent = relativeTime(event.ts, now);
    li.append(type, label, time);
    feedEl.append(li);
  }
};

browser.runtime.onMessage.addListener((message) => {
  if (isMeterMessage(message)) render(message.state);
  return undefined;
});

async function subscribe(): Promise<void> {
  const msg: MeterSubscribeMessage = { type: "meter:subscribe" };
  try {
    const reply = await browser.runtime.sendMessage(msg);
    if (reply && typeof reply === "object" && "counters" in reply) render(reply as MeterState);
  } catch (err) {
    console.warn("[saa] subscribe failed", err);
  }
}
void subscribe();

// Keep relative times and the decaying rate honest while idle.
setInterval(() => {
  if (current) render(current);
}, 1000);
