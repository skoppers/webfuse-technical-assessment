import { describe, expect, it } from "vitest";
import { createSensitiveDetector, matchSensitivePath } from "../src/background/sensitiveDetector";

describe("matchSensitivePath", () => {
  it("matches a sensitive segment at the root or nested", () => {
    expect(matchSensitivePath("/checkout")).toBe("checkout");
    expect(matchSensitivePath("/checkout/")).toBe("checkout");
    expect(matchSensitivePath("/settings/profile")).toBe("settings");
    expect(matchSensitivePath("/manage/pages/384/settings/general")).toBe("settings");
  });

  it("requires a whole segment, not a substring", () => {
    expect(matchSensitivePath("/settingsfoo")).toBeNull();
    expect(matchSensitivePath("/checkout-faq")).toBeNull();
    expect(matchSensitivePath("/blog/settings-guide")).toBeNull();
  });

  it("is case-insensitive and returns null for ordinary paths", () => {
    expect(matchSensitivePath("/Billing/history")).toBe("billing");
    expect(matchSensitivePath("/")).toBeNull();
    expect(matchSensitivePath("/products/1")).toBeNull();
  });

  it("returns the first list entry that matches", () => {
    expect(matchSensitivePath("/admin/billing")).toBe("billing");
  });
});

describe("sensitive detector", () => {
  const url = "https://shop.example/checkout?step=2";

  it("builds the sensitive_url event from a matching URL, stamped with the given ts", () => {
    const d = createSensitiveDetector();
    expect(d.observe(url, "content", 0)).toEqual({
      type: "sensitive_url",
      ts: 0,
      data: { url, matched: "checkout", source: "content" },
    });
    expect(d.observe("https://beta.bash.social/manage/pages/384/settings/general", "tabs", 0)?.data.matched).toBe("settings");
  });

  it("returns null for ordinary paths and garbage", () => {
    const d = createSensitiveDetector();
    expect(d.observe("https://shop.example/products/1", "content", 0)).toBeNull();
    expect(d.observe("not a url", "tabs", 0)).toBeNull();
    expect(d.size()).toBe(0);
  });

  it("emits once per URL inside the window, regardless of source", () => {
    const d = createSensitiveDetector({ windowMs: 2000 });
    expect(d.observe(url, "content", 0)).not.toBeNull();
    expect(d.observe(url, "tabs", 1500)).toBeNull();
  });

  it("emits again after the window", () => {
    const d = createSensitiveDetector({ windowMs: 2000 });
    d.observe(url, "content", 0);
    expect(d.observe(url, "tabs", 2000)?.data.source).toBe("tabs");
  });

  it("a suppressed duplicate does not extend the window", () => {
    const d = createSensitiveDetector({ windowMs: 2000 });
    d.observe(url, "content", 0);
    expect(d.observe(url, "tabs", 500)).toBeNull();
    expect(d.observe(url, "tabs", 2000)).not.toBeNull();
  });

  it("treats different URLs independently and prunes old entries", () => {
    const d = createSensitiveDetector({ windowMs: 2000 });
    expect(d.observe("https://a/checkout", "content", 0)).not.toBeNull();
    expect(d.observe("https://a/billing", "tabs", 10)).not.toBeNull();
    expect(d.size()).toBe(2);
    d.observe("https://a/settings", "content", 5000);
    expect(d.size()).toBe(1);
  });

  it("honours a custom segment list", () => {
    const d = createSensitiveDetector({ segments: ["vault"] });
    expect(d.observe("https://a/checkout", "content", 0)).toBeNull();
    expect(d.observe("https://a/vault/1", "content", 0)?.data.matched).toBe("vault");
  });
});
