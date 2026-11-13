// app.css carries the default-accent tokens so the first paint (before any script runs)
// already looks right. They must match what buildTheme() would write.
import { describe, expect, it } from "vite-plus/test";
import css from "../app.css?raw";
import { DEFAULT_ACCENT, buildTheme } from "./theme";

/** The custom properties declared directly in the first block whose selector matches. */
function block(selector: string): Record<string, string> {
  const start = css.indexOf(selector + " {");
  expect(start, selector).toBeGreaterThanOrEqual(0);
  const open = css.indexOf("{", start);
  const close = css.indexOf("}", open);
  const out: Record<string, string> = {};
  for (const m of css.slice(open + 1, close).matchAll(/--([\w-]+):\s*([^;]+);/g)) out[m[1]] = m[2].trim();
  return out;
}

describe("app.css defaults", () => {
  const light = buildTheme(DEFAULT_ACCENT, "light");
  const dark = buildTheme(DEFAULT_ACCENT, "dark");

  it("light tokens match buildTheme", () => {
    const got = block(":root");
    for (const [k, v] of Object.entries(light)) expect(got[k], k).toBe(v);
  });

  it("dark tokens match buildTheme in both dark blocks", () => {
    for (const sel of [':root:not([data-theme="light"])', ':root[data-theme="dark"]']) {
      const got = block(sel);
      for (const [k, v] of Object.entries(dark)) expect(got[k], `${sel} ${k}`).toBe(v);
    }
  });

  it("declares antd's shape and motion tokens", () => {
    const got = block(":root");
    expect(got.radius).toBe("6px");
    expect(got["radius-lg"]).toBe("8px");
    expect(got["radius-sm"]).toBe("4px");
    expect(got["motion-mid"]).toBe("0.2s");
  });
});
