/**
 * Pure element → payload helpers. These are the privacy boundary: nothing here may read
 * `.value` or text content. Key names are captured, except inside password inputs.
 */
import type { ClickData, FormSubmitData, InputData, KeydownData } from "../../../shared/types";

const NON_FIELD_TAGS = new Set(["BUTTON", "FIELDSET", "OBJECT", "OUTPUT"]);
const NON_FIELD_INPUT_TYPES = new Set(["submit", "button", "reset", "image", "hidden"]);

function tagOf(el: Element | null | undefined): string {
  return (el?.tagName ?? "unknown").toLowerCase();
}

export function describeClick(target: EventTarget | null): ClickData {
  const el = target instanceof Element ? target : null;
  const data: ClickData = { tag: tagOf(el) };
  if (el?.id) data.id = el.id;
  const role = el?.getAttribute("role");
  if (role) data.role = role;
  return data;
}

export function describeInput(target: EventTarget | null): InputData {
  const el = target instanceof Element ? target : null;
  const data: InputData = { tag: tagOf(el) };
  const field = el?.getAttribute("name") || el?.id;
  if (field) data.field = field;
  return data;
}

function isPasswordField(target: EventTarget | null): boolean {
  return target instanceof HTMLInputElement && target.type === "password";
}

export function describeKeydown(e: KeyboardEvent): KeydownData {
  const data: KeydownData = {};
  if (e.code) data.code = e.code;
  if (isPasswordField(e.target)) data.redacted = true;
  else data.key = e.key;
  return data;
}

/** Counts user-facing fields; excludes buttons, hidden inputs and container elements. */
function countFormFields(form: HTMLFormElement): number {
  let count = 0;
  for (const el of Array.from(form.elements)) {
    if (NON_FIELD_TAGS.has(el.tagName)) continue;
    if (el.tagName === "INPUT" && NON_FIELD_INPUT_TYPES.has((el as HTMLInputElement).type)) continue;
    count += 1;
  }
  return count;
}

export function describeForm(form: HTMLFormElement): FormSubmitData {
  const data: FormSubmitData = { fieldCount: countFormFields(form) };
  const action = form.getAttribute("action");
  if (action) data.action = action;
  return data;
}
