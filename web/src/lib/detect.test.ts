import { describe, expect, it } from "vite-plus/test";
import { frameworkLabel, lookedLikeText, planIsEmpty, planPrefill, type FormValues } from "./detect";
import type { DetectResult } from "./types";

const laravel: DetectResult = {
  kind: "php",
  framework: "laravel",
  name: "shop",
  confidence: "high",
  settings: { docroot: "public" },
  envFile: ".env",
  port: 8000,
  markers: ["composer.json", "artisan"],
};
const blank: FormValues = { kind: "", name: "", portText: "", envFile: "", settings: {}, defaultPort: "" };

describe("planPrefill", () => {
  it("chooses the kind and fills everything on a blank form", () => {
    const p = planPrefill(laravel, blank, (k) => k);
    expect(p).toMatchObject({
      kind: "php",
      name: "shop",
      port: "8000",
      envFile: ".env",
      settings: { docroot: "public" },
    });
    expect(p.filled).toEqual(["name", "docroot", "port 8000", ".env"]);
    expect(planIsEmpty(p)).toBe(false);
  });
  it("never replaces what was typed", () => {
    const p = planPrefill(
      laravel,
      { ...blank, kind: "php", name: "My shop", portText: "9000", envFile: ".env.local", settings: { docroot: "web" } },
      (k) => k,
    );
    expect(planIsEmpty(p)).toBe(true);
    expect(p.filled).toEqual([]);
  });
  it("replaces a port that is only the kind's usual one", () => {
    const p = planPrefill(
      { ...laravel, port: 3000 },
      { ...blank, kind: "php", portText: "8080", defaultPort: "8080" },
      (k) => k,
    );
    expect(p.port).toBe("3000");
    expect(p.kind).toBe("");
  });
  it("does not report a port that is already right", () => {
    const p = planPrefill(laravel, { ...blank, kind: "php", portText: "8000", defaultPort: "8000" }, (k) => k);
    expect(p.port).toBe("");
  });
  it("ignores a result of another kind than the chosen one", () => {
    expect(planIsEmpty(planPrefill(laravel, { ...blank, kind: "node" }, (k) => k))).toBe(true);
  });
  it("does nothing for a folder that says nothing", () => {
    expect(planIsEmpty(planPrefill({ ...laravel, kind: "" }, blank, (k) => k))).toBe(true);
  });
  it("uses the label function for settings", () => {
    const p = planPrefill(laravel, blank, (k) => `the ${k}`);
    expect(p.filled).toContain("the docroot");
  });
});

describe("lookedLikeText", () => {
  it("names the framework and the markers", () => {
    expect(lookedLikeText(laravel, "PHP")).toBe("Looks like a Laravel project (composer.json, artisan).");
  });
  it("falls back to the kind", () => {
    expect(lookedLikeText({ ...laravel, framework: "", markers: [] }, "PHP")).toBe("Looks like a PHP project.");
  });
  it("knows the shipped framework names", () => {
    expect(frameworkLabel("nextjs")).toBe("Next.js");
    expect(frameworkLabel("vite-svelte")).toBe("Vite + Svelte");
    expect(frameworkLabel("somethingnew")).toBe("somethingnew");
  });
});
