// @vitest-environment happy-dom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { CapturedEvent } from "../src/messages";
import { installCapture } from "../src/capture";

const ORIGIN = "http://localhost:3000";

/** Install against the happy-dom window; every test starts from a clean page at "/". */
function setup() {
  const events: CapturedEvent[] = [];
  const uninstall = installCapture(window, (e) => events.push(e));
  const ofType = (type: CapturedEvent["type"]) => events.filter((e) => e.type === type);
  return { events, ofType, uninstall };
}

/** happy-dom has no layout, so scroll offsets are stubbed on the node that "scrolled". */
function setScrollTop(el: Element, value: number) {
  Object.defineProperty(el, "scrollTop", { value, configurable: true });
}
function setWindowScrollY(value: number) {
  Object.defineProperty(window, "scrollY", { value, configurable: true });
}
function scroll(target: EventTarget) {
  target.dispatchEvent(new Event("scroll", { bubbles: true }));
}

describe("Capture module", () => {
  let uninstall: () => void = () => undefined;

  beforeEach(() => {
    vi.useFakeTimers();
    vi.setSystemTime(1_000_000);
    document.body.innerHTML = "";
    document.title = "";
    history.replaceState({}, "", "/");
    setWindowScrollY(0);
    setScrollTop(document.scrollingElement ?? document.documentElement, 0);
  });

  afterEach(() => {
    uninstall();
    vi.useRealTimers();
  });

  it("emits the initial navigation on install, stamped with the clock", () => {
    const s = setup();
    uninstall = s.uninstall;
    expect(s.events).toEqual([{ type: "navigation", ts: 1_000_000, data: { url: `${ORIGIN}/` } }]);
  });

  describe("click", () => {
    it("on a button ⇒ one click event with tag and id, nothing else", () => {
      const s = setup();
      uninstall = s.uninstall;
      const btn = document.createElement("button");
      btn.id = "buy";
      btn.textContent = "Buy now";
      document.body.append(btn);
      btn.dispatchEvent(new MouseEvent("click", { bubbles: true }));
      expect(s.ofType("click")).toEqual([{ type: "click", ts: 1_000_000, data: { tag: "button", id: "buy" } }]);
      expect(JSON.stringify(s.events)).not.toContain("Buy now");
    });

    it("carries the role attribute when present", () => {
      const s = setup();
      uninstall = s.uninstall;
      const div = document.createElement("div");
      div.id = "cta";
      div.setAttribute("role", "button");
      div.textContent = "Buy now";
      document.body.append(div);
      div.dispatchEvent(new MouseEvent("click", { bubbles: true }));
      expect(s.ofType("click").map((e) => e.data)).toEqual([{ tag: "div", id: "cta", role: "button" }]);
      expect(JSON.stringify(s.events)).not.toContain("Buy now");
    });

    it("on a non-element target (the document itself) ⇒ tag 'unknown'", () => {
      const s = setup();
      uninstall = s.uninstall;
      document.dispatchEvent(new MouseEvent("click", { bubbles: true }));
      expect(s.ofType("click").map((e) => e.data)).toEqual([{ tag: "unknown" }]);
    });
  });

  describe("keydown", () => {
    it("outside password fields carries key and code", () => {
      const s = setup();
      uninstall = s.uninstall;
      const input = document.createElement("input");
      document.body.append(input);
      input.dispatchEvent(new KeyboardEvent("keydown", { key: "a", code: "KeyA", bubbles: true }));
      expect(s.ofType("keydown").map((e) => e.data)).toEqual([{ key: "a", code: "KeyA" }]);
    });

    it("in a password input is redacted: code only, no key", () => {
      const s = setup();
      uninstall = s.uninstall;
      const pw = document.createElement("input");
      pw.type = "password";
      document.body.append(pw);
      pw.dispatchEvent(new KeyboardEvent("keydown", { key: "s", code: "KeyS", bubbles: true }));
      expect(s.ofType("keydown").map((e) => e.data)).toEqual([{ code: "KeyS", redacted: true }]);
      expect(JSON.stringify(s.ofType("keydown"))).not.toContain('"key"');
    });
  });

  describe("input", () => {
    it("⇒ field name, never the value", () => {
      const s = setup();
      uninstall = s.uninstall;
      const input = document.createElement("input");
      input.name = "email";
      input.value = "secret@example.com";
      document.body.append(input);
      input.dispatchEvent(new Event("input", { bubbles: true }));
      expect(s.ofType("input").map((e) => e.data)).toEqual([{ tag: "input", field: "email" }]);
      expect(JSON.stringify(s.events)).not.toContain("secret");
    });

    it("falls back to id for the field, and omits field when neither name nor id exists", () => {
      const s = setup();
      uninstall = s.uninstall;
      const ta = document.createElement("textarea");
      ta.id = "bio";
      ta.value = "my life story";
      const select = document.createElement("select");
      document.body.append(ta, select);
      ta.dispatchEvent(new Event("input", { bubbles: true }));
      select.dispatchEvent(new Event("input", { bubbles: true }));
      expect(s.ofType("input").map((e) => e.data)).toEqual([{ tag: "textarea", field: "bio" }, { tag: "select" }]);
      expect(JSON.stringify(s.events)).not.toContain("life story");
    });
  });

  describe("form submit", () => {
    it("⇒ fieldCount and action, no values", () => {
      const s = setup();
      uninstall = s.uninstall;
      const form = document.createElement("form");
      form.setAttribute("action", "/checkout/pay");
      form.innerHTML = `<input name="card" value="4111111111111111"><input type="hidden" name="csrf" value="tok"><button>Go</button>`;
      document.body.append(form);
      form.dispatchEvent(new Event("submit", { bubbles: true, cancelable: true }));
      expect(s.ofType("form_submit").map((e) => e.data)).toEqual([{ action: "/checkout/pay", fieldCount: 1 }]);
      expect(JSON.stringify(s.events)).not.toContain("4111");
    });

    it("counts select/textarea but not buttons, submit/hidden inputs or fieldsets; omits action when absent", () => {
      const s = setup();
      uninstall = s.uninstall;
      const form = document.createElement("form");
      form.innerHTML = `
        <fieldset>
          <input name="a" value="x">
          <input type="hidden" name="csrf" value="tok">
          <select name="b"><option>1</option></select>
          <textarea name="c">hello</textarea>
          <button type="submit">Go</button>
          <input type="submit" value="Go">
        </fieldset>`;
      document.body.append(form);
      form.dispatchEvent(new Event("submit", { bubbles: true, cancelable: true }));
      expect(s.ofType("form_submit").map((e) => e.data)).toEqual([{ fieldCount: 3 }]);
      expect(JSON.stringify(s.events)).not.toContain("hello");
    });
  });

  describe("navigation", () => {
    it("pushState to a sensitive path ⇒ navigation only; sensitive_url is the recorder's job", () => {
      const s = setup();
      uninstall = s.uninstall;
      s.events.length = 0;
      document.title = "Checkout";
      history.pushState({}, "", "/checkout?step=2");
      expect(s.events.map((e) => [e.type, e.data])).toEqual([
        ["navigation", { url: `${ORIGIN}/checkout?step=2`, title: "Checkout" }],
      ]);
    });

    it("pushState to an ordinary path ⇒ navigation only", () => {
      const s = setup();
      uninstall = s.uninstall;
      s.events.length = 0;
      history.pushState({}, "", "/products/1");
      expect(s.events.map((e) => e.type)).toEqual(["navigation"]);
    });

    it("replaceState emits only when the URL changes", () => {
      const s = setup();
      uninstall = s.uninstall;
      s.events.length = 0;
      history.replaceState({}, "", "/one");
      history.replaceState({}, "", "/one"); // same URL → no emit
      history.replaceState({ extra: true }, "", "/one"); // state-only change → no emit
      expect(s.events.map((e) => e.data.url)).toEqual([`${ORIGIN}/one`]);
    });

    it("popstate (history.back) emits on URL change and stays quiet when the URL is unchanged", () => {
      const s = setup();
      uninstall = s.uninstall;
      history.pushState({}, "", "/one");
      s.events.length = 0;
      history.back();
      expect(s.events.map((e) => [e.type, e.data.url])).toEqual([["navigation", `${ORIGIN}/`]]);
      window.dispatchEvent(new PopStateEvent("popstate")); // no URL change → no emit
      expect(s.events).toHaveLength(1);
    });

    it("hashchange emits on URL change", () => {
      // happy-dom updates location.hash but does not fire hashchange itself, so the event is dispatched by hand.
      const s = setup();
      uninstall = s.uninstall;
      s.events.length = 0;
      location.hash = "#section-2";
      window.dispatchEvent(new Event("hashchange"));
      expect(s.events.map((e) => e.data.url)).toEqual([`${ORIGIN}/#section-2`]);
      window.dispatchEvent(new Event("hashchange")); // unchanged → no emit
      expect(s.events).toHaveLength(1);
    });

    it("uninstall restores the original history methods and stops listening for popstate/hashchange", () => {
      const originalPush = history.pushState;
      const originalReplace = history.replaceState;
      const s = setup();
      expect(history.pushState).not.toBe(originalPush);
      expect(history.replaceState).not.toBe(originalReplace);
      s.uninstall();
      expect(history.pushState).toBe(originalPush);
      expect(history.replaceState).toBe(originalReplace);

      s.events.length = 0;
      history.pushState({}, "", "/after-push");
      history.replaceState({}, "", "/after-replace");
      location.hash = "#after";
      window.dispatchEvent(new Event("hashchange"));
      window.dispatchEvent(new PopStateEvent("popstate"));
      expect(s.events).toEqual([]);
    });
  });

  describe("scroll", () => {
    it("throttle is global: two scrollables inside 1 s ⇒ one scroll event", () => {
      const s = setup();
      uninstall = s.uninstall;
      const a = document.createElement("div");
      a.id = "list";
      const b = document.createElement("section");
      document.body.append(a, b);
      setScrollTop(a, 100);
      setScrollTop(b, 30);

      scroll(a);
      scroll(b);
      expect(s.ofType("scroll").map((e) => e.data)).toEqual([{ y: 100, deltaY: 100, tag: "div", id: "list" }]);

      vi.setSystemTime(1_001_000);
      scroll(b);
      expect(s.ofType("scroll").map((e) => e.data)).toEqual([
        { y: 100, deltaY: 100, tag: "div", id: "list" },
        { y: 30, deltaY: 30, tag: "section" },
      ]);
    });

    it("throttle is leading-edge: fires at exactly 1000 ms, not at 999 ms", () => {
      const s = setup();
      uninstall = s.uninstall;
      const div = document.createElement("div");
      document.body.append(div);
      setScrollTop(div, 10);

      scroll(div);
      scroll(div);
      vi.setSystemTime(1_000_999);
      scroll(div);
      expect(s.ofType("scroll")).toHaveLength(1);

      vi.setSystemTime(1_001_000);
      scroll(div);
      expect(s.ofType("scroll")).toHaveLength(2);
      expect(s.ofType("scroll").map((e) => e.ts)).toEqual([1_000_000, 1_001_000]);
    });

    it("rounds an inner element's fractional scrollTop", () => {
      const s = setup();
      uninstall = s.uninstall;
      const div = document.createElement("div");
      document.body.append(div);
      setScrollTop(div, 240.4);
      scroll(div);
      expect(s.ofType("scroll").map((e) => e.data)).toEqual([{ y: 240, deltaY: 240, tag: "div" }]);
    });

    it("document scroll reads window.scrollY, whether the event targets document or window", () => {
      const s = setup();
      uninstall = s.uninstall;
      setWindowScrollY(120);
      scroll(document);
      vi.setSystemTime(1_001_000);
      setWindowScrollY(180);
      scroll(window);
      expect(s.ofType("scroll").map((e) => e.data)).toEqual([
        { y: 120, deltaY: 120, tag: "document" },
        { y: 180, deltaY: 60, tag: "document" },
      ]);
    });

    it("document scroll falls back to scrollingElement.scrollTop when window.scrollY is 0", () => {
      const s = setup();
      uninstall = s.uninstall;
      setWindowScrollY(0);
      setScrollTop(document.scrollingElement ?? document.documentElement, 77);
      scroll(document);
      expect(s.ofType("scroll").map((e) => e.data)).toEqual([{ y: 77, deltaY: 77, tag: "document" }]);
    });

    it("document deltaY is relative to the offset at install time (page opened mid-scroll), and can be negative", () => {
      setWindowScrollY(50);
      const s = setup();
      uninstall = s.uninstall;
      setWindowScrollY(200);
      scroll(document);
      vi.setSystemTime(1_001_000);
      setWindowScrollY(120);
      scroll(document);
      expect(s.ofType("scroll").map((e) => e.data)).toEqual([
        { y: 200, deltaY: 150, tag: "document" },
        { y: 120, deltaY: -80, tag: "document" },
      ]);
    });

    it("tracks each inner element's delta independently of the others and of the document", () => {
      const s = setup();
      uninstall = s.uninstall;
      const a = document.createElement("div");
      a.id = "list";
      const b = document.createElement("section");
      document.body.append(a, b);
      setScrollTop(a, 100);
      setScrollTop(b, 30);
      setWindowScrollY(500);

      scroll(a);
      vi.setSystemTime(1_001_000);
      scroll(b);
      vi.setSystemTime(1_002_000);
      setScrollTop(a, 160);
      scroll(a);
      vi.setSystemTime(1_003_000);
      scroll(document);
      vi.setSystemTime(1_004_000);
      setScrollTop(a, 40);
      scroll(a);
      expect(s.ofType("scroll").map((e) => e.data)).toEqual([
        { y: 100, deltaY: 100, tag: "div", id: "list" },
        { y: 30, deltaY: 30, tag: "section" },
        { y: 160, deltaY: 60, tag: "div", id: "list" },
        { y: 500, deltaY: 500, tag: "document" },
        { y: 40, deltaY: -120, tag: "div", id: "list" },
      ]);
    });
  });

  describe("lifecycle", () => {
    it("installs once per window: a second install is a no-op", () => {
      const s = setup();
      uninstall = s.uninstall;
      const other: CapturedEvent[] = [];
      const second = installCapture(window, (e) => other.push(e));
      document.body.dispatchEvent(new MouseEvent("click", { bubbles: true }));
      expect(other).toEqual([]);
      expect(s.ofType("click")).toHaveLength(1);
      second(); // must not tear down the first install
      document.body.dispatchEvent(new MouseEvent("click", { bubbles: true }));
      expect(s.ofType("click")).toHaveLength(2);
    });

    it("uninstall stops every source and allows a fresh install", () => {
      const s = setup();
      s.uninstall();
      document.body.dispatchEvent(new MouseEvent("click", { bubbles: true }));
      document.body.dispatchEvent(new KeyboardEvent("keydown", { key: "a", code: "KeyA", bubbles: true }));
      document.body.dispatchEvent(new Event("input", { bubbles: true }));
      document.body.dispatchEvent(new Event("scroll", { bubbles: true }));
      history.pushState({}, "", "/checkout");
      expect(s.events.map((e) => e.type)).toEqual(["navigation"]); // only the initial one

      const again = setup();
      uninstall = again.uninstall;
      expect(again.events.map((e) => [e.type, e.data.url])).toEqual([["navigation", `${ORIGIN}/checkout`]]);
    });
  });
});
