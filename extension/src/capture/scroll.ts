/**
 * Scroll capture helpers.
 *
 * A capturing listener on `window` sees scrolls from every scrollable element,
 * not only the document. `window.scrollY` is 0 for those, so the offset must be
 * read from whichever node actually scrolled, and we record which node that was.
 */
import type { ScrollData } from "../../../shared/types";

const DOCUMENT_TAG = "document";

function isDocumentScroll(target: EventTarget | null, win: Window): boolean {
  return !(target instanceof Element) || target === win.document.scrollingElement;
}

function scrollPositionOf(target: EventTarget | null, win: Window): number {
  if (!isDocumentScroll(target, win)) return Math.round((target as Element).scrollTop);
  const doc = win.document;
  const y = win.scrollY || doc.scrollingElement?.scrollTop || doc.documentElement.scrollTop || 0;
  return Math.round(y);
}

/**
 * Builds `ScrollData` payloads and remembers the last reported offset per element so
 * `deltaY` is the movement since the previous *emitted* event for that element.
 * The document is seeded with its offset at creation so a page opened mid-scroll
 * (e.g. an anchor link) doesn't report a bogus first delta.
 */
export function createScrollTracker(win: Window): (target: EventTarget | null) => ScrollData {
  const last = new WeakMap<object, number>();
  const docKey = win.document;
  last.set(docKey, scrollPositionOf(null, win));

  return (target) => {
    const document = isDocumentScroll(target, win);
    const key: object = document ? docKey : (target as Element);
    const y = scrollPositionOf(target, win);
    const prev = last.get(key) ?? 0;
    last.set(key, y);

    const data: ScrollData = { y, deltaY: y - prev, tag: DOCUMENT_TAG };
    if (!document) {
      const el = target as Element;
      data.tag = el.tagName.toLowerCase();
      if (el.id) data.id = el.id;
    }
    return data;
  };
}
