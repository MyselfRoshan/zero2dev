// DESIGN.md (repo root) documents the theme's default tokens; keep it in step with theme.ts.
import { describe, expect, it } from "vite-plus/test";
import design from "../../../DESIGN.md?raw";
import { DEFAULT_ACCENT, NEUTRALS, SEEDS } from "./theme";

/** The YAML front matter between the first two "---" lines. */
const front = design.split(/^---$/m)[1] ?? "";

/** A quoted colour under `colors:`, e.g. `primary: '#1677FF'`. */
function color(name: string): string {
  const m = front.match(new RegExp(`^  ${name}: '([^']+)'$`, "m"));
  if (!m) throw new Error(`DESIGN.md has no colors.${name}`);
  return m[1].toLowerCase();
}

describe("DESIGN.md front matter", () => {
  it("has the default accent and status seeds", () => {
    expect(color("primary")).toBe(DEFAULT_ACCENT);
    expect(color("success")).toBe(SEEDS.success);
    expect(color("warning")).toBe(SEEDS.warning);
    expect(color("error")).toBe(SEEDS.error);
  });

  it("has antd's neutrals as theme.ts uses them", () => {
    expect(color("surface-layout")).toBe(NEUTRALS.light.bg);
    expect(color("surface")).toBe(NEUTRALS.light.surface);
    expect(color("outline-variant")).toBe(NEUTRALS.light.line);
    expect(color("outline")).toBe(NEUTRALS.light["line-strong"]);
    expect(color("dark-surface")).toBe(NEUTRALS.dark.surface);
    expect(color("dark-surface-layout")).toBe(NEUTRALS.dark.bg);
    expect(color("on-surface")).toBe(NEUTRALS.light.text);
  });

  it("documents the control radius and height", () => {
    expect(front).toMatch(/^  DEFAULT: 6px$/m);
    expect(front).toMatch(/^  control-height: 32px$/m);
  });
});
