import { describe, expect, it } from "vite-plus/test";
import {
  ACCENT_PRESETS,
  DEFAULT_ACCENT,
  buildTheme,
  contrast,
  normalizeAccent,
  normalizeMode,
  palette,
  resolveScheme,
  themeCache,
} from "./theme";

describe("palette", () => {
  it("matches antd's blue exactly", () => {
    expect(palette("#1677ff", "light")).toEqual([
      "#e6f4ff",
      "#bae0ff",
      "#91caff",
      "#69b1ff",
      "#4096ff",
      "#1677ff",
      "#0958d9",
      "#003eb3",
      "#002c8c",
      "#001d66",
    ]);
    // antd's dark algorithm (blueDark)
    expect(palette("#1677ff", "dark")[5]).toBe("#1668dc");
  });
});

describe("buildTheme", () => {
  it("maps antd tokens for the default accent", () => {
    const t = buildTheme(DEFAULT_ACCENT, "light");
    expect(t.accent).toBe("#1677ff"); // colorPrimary
    expect(t["accent-soft"]).toBe("#e6f4ff"); // colorPrimaryBg
    expect(t["accent-border"]).toBe("#91caff"); // colorPrimaryBorder
    expect(t.bg).toBe("#f5f5f5");
    expect(t.line).toBe("#f0f0f0");
    expect(t["line-strong"]).toBe("#d9d9d9");
  });

  it("keeps the primary button readable: antd's #1677ff with white is 4.1:1, so it goes one step deeper", () => {
    const t = buildTheme(DEFAULT_ACCENT, "light");
    expect(contrast("#1677ff", "#ffffff")).toBeLessThan(4.5);
    expect(t["accent-solid"]).toBe("#0958d9");
    expect(t["accent-ink"]).toBe("#ffffff");
    expect(t["accent-text"]).toBe("#0958d9");
  });

  for (const scheme of ["light", "dark"] as const) {
    for (const p of [
      ...ACCENT_PRESETS,
      { id: "yellow", label: "Yellow", color: "#fadb14" },
      { id: "lime", label: "Lime", color: "#a0d911" },
    ]) {
      it(`${p.label} in ${scheme}: text and buttons reach 4.5:1`, () => {
        const t = buildTheme(p.color, scheme);
        expect(contrast(t["accent-text"], t.surface)).toBeGreaterThanOrEqual(4.5);
        expect(contrast(t["accent-solid"], t["accent-ink"])).toBeGreaterThanOrEqual(4.5);
        expect(contrast(t["accent-hover"], t["accent-ink"])).toBeGreaterThanOrEqual(4.5);
        for (const k of ["ok", "warn", "bad"]) expect(contrast(t[k], t.surface)).toBeGreaterThanOrEqual(4.5);
        expect(contrast(t["bad-solid"], t["bad-ink"])).toBeGreaterThanOrEqual(4.5);
        expect(contrast(t["log-sys"], t["log-bg"])).toBeGreaterThanOrEqual(4.5);
      });
    }
  }

  it("puts dark text on a light custom accent", () => {
    expect(buildTheme("#fadb14", "light")["accent-ink"]).toBe("#1f1f1f");
  });

  it("uses antd's dark neutrals", () => {
    const t = buildTheme(DEFAULT_ACCENT, "dark");
    expect(t.bg).toBe("#000000");
    expect(t.surface).toBe("#141414");
    expect(t.accent).toBe("#1668dc");
  });
});

describe("inputs", () => {
  it("falls back on bad values", () => {
    expect(normalizeAccent("blue")).toBe(DEFAULT_ACCENT);
    expect(normalizeAccent("#ABCDEF")).toBe("#abcdef");
    expect(normalizeAccent(undefined)).toBe(DEFAULT_ACCENT);
    expect(normalizeMode("dark")).toBe("dark");
    expect(normalizeMode("neon")).toBe("system");
  });
  it("resolves system against the OS", () => {
    expect(resolveScheme("system", true)).toBe("dark");
    expect(resolveScheme("system", false)).toBe("light");
    expect(resolveScheme("light", true)).toBe("light");
  });
  it("caches both schemes", () => {
    const c = themeCache("dark", "#722ED1");
    expect(c.accent).toBe("#722ed1");
    expect(c.light.accent).toBe("#722ed1");
    expect(c.dark.accent).not.toBe(c.light.accent);
  });
});
