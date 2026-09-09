/**
 * Internal extension message protocol.
 * All messages travel over `browser.runtime.sendMessage` and arrive as `unknown`,
 * so every listener discriminates with the guards below.
 */
import type { EventType, SessionEvent } from "../../shared/types";

/** An event as captured by the content script; the background assigns `seq`. */
export interface CapturedEvent {
  type: EventType;
  ts: number;
  data: Record<string, unknown>;
}

/** content → background */
export interface CaptureMessage {
  type: "capture:event";
  event: CapturedEvent;
}

export interface MeterState {
  /** Events in the last 10 s. */
  rate: number;
  counters: Record<EventType, number>;
  /** Most recent events, oldest first, capped (5). */
  recent: SessionEvent[];
}

/** popup → background; the background replies with a MeterState snapshot. */
export interface MeterSubscribeMessage {
  type: "meter:subscribe";
}

/** background → popup */
export interface MeterSnapshotMessage {
  type: "meter:snapshot";
  state: MeterState;
}

/** background → popup, one per captured event. */
export interface MeterEventMessage {
  type: "meter:event";
  event: SessionEvent;
  state: MeterState;
}

export type MeterMessage = MeterSnapshotMessage | MeterEventMessage;
export type ExtensionMessage = CaptureMessage | MeterSubscribeMessage | MeterMessage;

function hasType(value: unknown): value is { type: unknown } {
  return typeof value === "object" && value !== null && "type" in value;
}

export function isCaptureMessage(value: unknown): value is CaptureMessage {
  if (!hasType(value) || value.type !== "capture:event") return false;
  const event = (value as { event?: unknown }).event;
  return (
    typeof event === "object" &&
    event !== null &&
    typeof (event as CapturedEvent).type === "string" &&
    typeof (event as CapturedEvent).ts === "number" &&
    typeof (event as CapturedEvent).data === "object" &&
    (event as CapturedEvent).data !== null
  );
}

export function isMeterSubscribeMessage(value: unknown): value is MeterSubscribeMessage {
  return hasType(value) && value.type === "meter:subscribe";
}

export function isMeterMessage(value: unknown): value is MeterMessage {
  if (!hasType(value)) return false;
  if (value.type !== "meter:snapshot" && value.type !== "meter:event") return false;
  const state = (value as { state?: unknown }).state;
  return typeof state === "object" && state !== null && "counters" in state;
}
