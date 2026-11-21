// Pure helpers behind the project file UI (0.0.6, docs/API.md "Project file"): the suggested export
// path, the 409 "exists" answer, the words of an export result and of an import plan.
import { ApiError } from "./api";
import { baseName } from "./databases";
import type { FileFilter, NewProjectInstall, Project, ProjectExportResult, ProjectImportPlan, Service } from "./types";

export const PROJECT_FILE = "zero2dev.json";

export const PROJECT_FILE_FILTERS: FileFilter[] = [
  { name: "Zero2Dev project file", pattern: "*.json" },
  { name: "All files", pattern: "*.*" },
];

const isAbs = (p: string) => /^[A-Za-z]:[\\/]/.test(p) || p.startsWith("\\\\");
const back = (p: string) => p.trim().replace(/\//g, "\\").replace(/\\+$/, "");

/** The folder of a project's code: the folder of its first service that has an absolute one. "" when none. */
export function projectFolder(p: Pick<Project, "serviceIds">, services: Pick<Service, "id" | "cwd">[]): string {
  for (const id of p.serviceIds) {
    const cwd = services.find((s) => s.id === id)?.cwd?.trim() ?? "";
    if (cwd && isAbs(cwd)) return back(cwd);
  }
  return "";
}

/** `<project folder>\zero2dev.json`, or "" when the project has no folder to suggest. */
export function suggestExportPath(folder: string): string {
  const f = back(folder);
  return f ? `${f}\\${PROJECT_FILE}` : "";
}

/** A path is fine to export to: absolute, and either a folder or a file name. */
export function exportPathError(path: string): string {
  const p = path.trim();
  if (!p) return "Choose where to save the file.";
  if (!isAbs(p)) return "Use an absolute path, for example D:\\work\\shop\\zero2dev.json.";
  return "";
}

/** The daemon says `<file> exists` (409) when the file is there and Overwrite was not asked for. */
export function isExistsError(e: unknown): e is ApiError {
  return e instanceof ApiError && e.status === 409 && /exists/i.test(e.message);
}

/** What the export said, one sentence per warning (the daemon's words are kept). */
export function exportSummary(r: ProjectExportResult): { saved: string; warnings: string[] } {
  return { saved: `Saved to ${r.path}.`, warnings: r.warnings.map((w) => w.trim()).filter(Boolean) };
}

/** "1 service, 1 site" for the head of a plan. */
export function planCounts(plan: Pick<ProjectImportPlan, "services" | "sites" | "installs">): string {
  const n = (c: number, one: string, many = `${one}s`) => `${c} ${c === 1 ? one : many}`;
  const parts = [n(plan.services.length, "service"), n(plan.sites.length, "site")];
  if (plan.installs.length) parts.push(`${n(plan.installs.length, "runtime")} to install`);
  return parts.join(", ");
}

export const installLine = (i: Pick<NewProjectInstall, "name" | "version">): string =>
  `${i.name}${i.name.includes(i.version) ? "" : ` ${i.version}`}`.trim();

export type ImportPhase = "idle" | "loading" | "ready" | "error";

/** Whether Import may run, and if not, why (shown under the button). */
export function importState(
  path: string,
  plan: ProjectImportPlan | null,
  phase: ImportPhase,
  starting: boolean,
): { ok: boolean; reason: string } {
  if (!path.trim())
    return { ok: false, reason: "Choose a zero2dev.json file or the folder of a cloned project first." };
  if (phase === "loading") return { ok: false, reason: "Checking the file..." };
  if (!plan) return { ok: false, reason: "Press Check to see what the import would do." };
  if (plan.problems.length)
    return { ok: false, reason: "Fix the problems above first: Import is off until the file can be imported." };
  if (starting) return { ok: false, reason: "Starting..." };
  return { ok: true, reason: "" };
}

/** The name the file's project would get, or the name typed over it. */
export const importName = (plan: Pick<ProjectImportPlan, "name">, typed: string): string => typed.trim() || plan.name;

/** The file or folder name, for the head of a plan. */
export const planSource = (plan: Pick<ProjectImportPlan, "path">): string => baseName(plan.path) || plan.path;
