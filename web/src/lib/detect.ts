// Project detection in the service form (0.0.6): what a detected folder fills in, and the line
// that says so. Pure functions; the form applies the plan and keeps the values for Undo.
import type { DetectResult } from "./types";

const FRAMEWORKS: Record<string, string> = {
  laravel: "Laravel",
  symfony: "Symfony",
  wordpress: "WordPress",
  nextjs: "Next.js",
  nestjs: "NestJS",
  astro: "Astro",
  "vite-plus": "Vite+",
  vite: "Vite",
  "vite-react": "Vite + React",
  "vite-vue": "Vite + Vue",
  "vite-svelte": "Vite + Svelte",
  express: "Express",
  gin: "Gin",
  fiber: "Fiber",
  "go-nethttp": "Go",
  springboot: "Spring Boot",
  django: "Django",
  fastapi: "FastAPI",
  flask: "Flask",
  html: "HTML",
};

/** The name of a framework id; the id itself when it is not one of the known ones. */
export function frameworkLabel(id: string): string {
  return FRAMEWORKS[id] ?? id;
}

/** What the form holds right now (before detection touches anything). */
export interface FormValues {
  kind: string;
  name: string;
  portText: string;
  envFile: string;
  settings: Record<string, string>;
  /** The port the current kind fills in by itself, "" when none: that is not something the person typed. */
  defaultPort: string;
}

export interface PrefillPlan {
  /** The kind to choose, when none is chosen yet. */
  kind: string;
  name: string;
  port: string;
  envFile: string;
  /** Only keys that have no value yet. */
  settings: Record<string, string>;
  /** The words of the "Filled in" list: "name", "port 8000", ... */
  filled: string[];
}

/** True when the plan changes nothing. */
export const planIsEmpty = (p: PrefillPlan) =>
  !p.kind && !p.name && !p.port && !p.envFile && !Object.keys(p.settings).length;

/**
 * What to fill in for a detection result. Never replaces what the person wrote: a field is filled
 * only when empty (the port also when it is just the kind's usual port). A result of another kind
 * than the chosen one is ignored, because its settings would not fit.
 */
export function planPrefill(
  r: DetectResult,
  cur: FormValues,
  optionLabel: (key: string) => string = (k) => k,
): PrefillPlan {
  const plan: PrefillPlan = { kind: "", name: "", port: "", envFile: "", settings: {}, filled: [] };
  if (!r.kind || (cur.kind && cur.kind !== r.kind)) return plan;
  if (!cur.kind) plan.kind = r.kind;
  if (!cur.name.trim() && r.name) {
    plan.name = r.name;
    plan.filled.push("name");
  }
  for (const [k, v] of Object.entries(r.settings ?? {})) {
    if (v && !(cur.settings[k] ?? "").trim()) {
      plan.settings[k] = v;
      plan.filled.push(optionLabel(k));
    }
  }
  const portEmpty =
    cur.portText.trim() === "" || cur.portText.trim() === "0" || cur.portText.trim() === cur.defaultPort;
  if (r.port > 0 && portEmpty && cur.portText.trim() !== String(r.port)) {
    plan.port = String(r.port);
    plan.filled.push(`port ${r.port}`);
  }
  if (r.envFile && !cur.envFile.trim()) {
    plan.envFile = r.envFile;
    plan.filled.push(r.envFile);
  }
  return plan;
}

/** "Looks like a Laravel project (composer.json, artisan)." */
export function lookedLikeText(r: DetectResult, kindLabel: string): string {
  const what = r.framework ? frameworkLabel(r.framework) : kindLabel;
  const markers = r.markers?.length ? ` (${r.markers.join(", ")})` : "";
  return `Looks like a ${what} project${markers}.`;
}
