// Pure helpers of the Environment page. Three things are kept apart and combined here:
//   draft  - what the user has on screen (switches, order, new items);
//   saved  - the config the daemon last applied;
//   report - what the Windows user environment holds now.
// Every row gets one plain status line out of them, and the review bar gets the list of
// what Apply will really do.
import { kindLabel, runtimeTitle } from "./catalog";
import type { EnvConfig, EnvPath, EnvPathStatus, EnvReport, EnvVar, EnvVarStatus, Runtime } from "./types";

/** Kinds whose default version a variable can follow. Same list as the daemon's. */
export const ENV_KINDS = [
  "node",
  "php",
  "java",
  "go",
  "python",
  "rust",
  "composer",
  "mysql",
  "mariadb",
  "postgres",
  "redis",
] as const;

/** The commands each kind's folder gives a terminal (the daemon's shim list), for items not applied yet. */
export const KIND_COMMANDS: Record<string, string[]> = {
  node: ["node", "npm", "npx"],
  php: ["php"],
  java: ["jar", "java", "javac"],
  go: ["go", "gofmt"],
  python: ["python", "pip"],
  rust: ["cargo", "rustc", "rustup"],
  composer: ["composer"],
  mysql: ["mysql", "mysqldump"],
  mariadb: ["mariadb", "mariadb-dump", "mysql"],
  postgres: ["pg_dump", "pg_restore", "psql"],
  redis: ["redis-cli"],
};

/** The usual variable for a language runtime, offered as a one-click suggestion. */
export const SUGGESTED_NAMES: Record<string, string> = {
  java: "JAVA_HOME",
  go: "GOROOT",
  node: "NODE_HOME",
  php: "PHP_HOME",
};

const NAME_RE = /^[A-Za-z_][A-Za-z0-9_]{0,63}$/;
const ABS_RE = /^([A-Za-z]:[\\/]|\\\\[^\\])/;

// ---------- small utilities ----------

/** A new item id; the daemon accepts any id of this shape. */
export function newItemId(prefix: "envp_" | "envv_"): string {
  const b = new Uint8Array(4);
  crypto.getRandomValues(b);
  return prefix + Array.from(b, (x) => x.toString(16).padStart(2, "0")).join("");
}

export function cloneConfig(c: EnvConfig): EnvConfig {
  return { seeded: c.seeded, path: c.path.map((p) => ({ ...p })), vars: c.vars.map((v) => ({ ...v })) };
}

/** True when two configs hold the same items in the same order. */
export function sameConfig(a: EnvConfig, b: EnvConfig): boolean {
  const norm = (c: EnvConfig) => JSON.stringify({ path: c.path.map(pathKey), vars: c.vars.map(varKey) });
  return norm(a) === norm(b);
}
const pathKey = (p: EnvPath) => [p.id, p.source, p.runtimeId ?? "", p.dir ?? "", p.enabled];
const varKey = (v: EnvVar) => [v.id, v.name, v.source, v.kind ?? "", v.runtimeId ?? "", v.value ?? "", v.enabled];

export function setAllEnabled(c: EnvConfig, part: "path" | "vars", on: boolean): EnvConfig {
  const out = cloneConfig(c);
  for (const item of out[part]) item.enabled = on;
  return out;
}

/** Moves an item one place up (-1) or down (+1); out of range is a no-op. */
export function moveItem<T extends { id: string }>(list: T[], id: string, by: -1 | 1): T[] {
  const i = list.findIndex((x) => x.id === id);
  const j = i + by;
  if (i < 0 || j < 0 || j >= list.length) return list;
  const out = [...list];
  [out[i], out[j]] = [out[j], out[i]];
  return out;
}

const parent = (f: string) => f.replace(/[\\/][^\\/]*$/, "");
const runtimeOf = (rts: Runtime[], id?: string) => rts.find((r) => r.id === id);
export function defaultOf(rts: Runtime[], kind: string): Runtime | undefined {
  const all = rts.filter((r) => r.kind === kind);
  return all.find((r) => r.default) ?? all[0];
}

/** The value of a variable that points at a runtime: like model.RuntimeHome, the install folder for Java and Go, the program's folder otherwise. */
export function runtimeValue(rt: Runtime): string {
  return rt.kind === "java" || rt.kind === "go" ? parent(parent(rt.binary)) : parent(rt.binary);
}

const same = (a: string, b: string) => a.toLowerCase() === b.toLowerCase();

// ---------- what an item is ----------

export function pathTitle(p: EnvPath, rts: Runtime[]): string {
  switch (p.source) {
    case "shims":
      return "Default versions";
    case "runtime": {
      const r = runtimeOf(rts, p.runtimeId);
      return r ? `${runtimeTitle(r)} only` : "A runtime that was removed";
    }
    default:
      return "Your folder";
  }
}

export function pathNote(p: EnvPath): string {
  switch (p.source) {
    case "shims":
      return "Small launchers that always run the version marked Default on the Runtimes page.";
    case "runtime":
      return "Always this exact version, whatever the default is.";
    default:
      return "A folder you added yourself.";
  }
}

/** The folder an item stands for: what the daemon resolved, else what can be worked out here. */
export function pathDir(p: EnvPath, st: EnvPathStatus | undefined, rts: Runtime[], shimsDir: string): string {
  if (st?.dir) return st.dir;
  if (p.source === "custom") return p.dir ?? "";
  if (p.source === "shims") return shimsDir;
  const r = runtimeOf(rts, p.runtimeId);
  return r ? parent(r.binary) : "";
}

/** The commands a folder gives: the daemon's list, else what the kind normally has. */
export function pathCommands(p: EnvPath, st: EnvPathStatus | undefined, rts: Runtime[]): string[] {
  if (st?.commands) return st.commands;
  if (p.source === "runtime") return KIND_COMMANDS[runtimeOf(rts, p.runtimeId)?.kind ?? ""] ?? [];
  if (p.source === "shims") {
    const kinds = ENV_KINDS.filter((k) => defaultOf(rts, k));
    return [...new Set(kinds.flatMap((k) => KIND_COMMANDS[k]))].sort((a, b) => a.localeCompare(b));
  }
  return [];
}

/** Where a variable's value comes from, in words. */
export function varSourceLabel(v: EnvVar, rts: Runtime[]): string {
  switch (v.source) {
    case "default":
      return `Follows the default ${kindLabel(v.kind ?? "")} version`;
    case "runtime": {
      const r = runtimeOf(rts, v.runtimeId);
      return r ? `Always ${runtimeTitle(r)}` : "A runtime that was removed";
    }
    default:
      return "A value you typed";
  }
}

/** The value a variable stands for: the daemon's, else what can be worked out here ("" when unknown). */
export function varValue(v: EnvVar, st: EnvVarStatus | undefined, rts: Runtime[]): string {
  if (v.source === "custom") return v.value ?? "";
  if (st?.value && sameDef(v, st)) return st.value;
  const r = v.source === "default" ? defaultOf(rts, v.kind ?? "") : runtimeOf(rts, v.runtimeId);
  return r ? runtimeValue(r) : "";
}
// The daemon's value is for the saved definition; a renamed or re-pointed draft computes its own.
const sameDef = (v: EnvVar, st: EnvVarStatus) => st.name === v.name;

// ---------- one status line per row ----------

export type Tone = "ok" | "pending" | "warn" | "muted";
export interface RowStatus {
  tone: Tone;
  text: string;
  /** A way out of a problem: a link, or "reapply" (apply the saved config again). */
  fix?: { label: string; href?: string; reapply?: boolean };
}

const orderOf = (list: { id: string; enabled: boolean }[]) => list.filter((x) => x.enabled).map((x) => x.id);

/** The status line of a folder row. `draftList` / `savedList` give the PATH order. */
export function pathStatus(
  p: EnvPath,
  saved: EnvPath | undefined,
  st: EnvPathStatus | undefined,
  ctx: { supported: boolean; draftList: EnvPath[]; savedList: EnvPath[] },
): RowStatus {
  if (!ctx.supported) return { tone: "muted", text: "Windows only. Nothing is changed on this system." };
  if (p.enabled && st?.problem)
    return { tone: "warn", text: st.problem, fix: { label: "Open Runtimes", href: "#/runtimes" } };
  if (!saved)
    return p.enabled
      ? { tone: "pending", text: "Will be added to your PATH when you apply." }
      : { tone: "muted", text: "Off" };
  if (p.enabled && !saved.enabled) return { tone: "pending", text: "Will be added to your PATH when you apply." };
  if (!p.enabled && saved.enabled) {
    if (st?.present && !st.owned) return { tone: "muted", text: "Stays on your PATH: it was there before Zero2Dev." };
    return st?.present
      ? { tone: "pending", text: "Will be removed from your PATH when you apply." }
      : { tone: "muted", text: "Off" };
  }
  if (p.enabled) {
    // Only a change of order among folders that were already on counts as a move; a new
    // folder above this one is reported on its own row.
    const draftOn = orderOf(ctx.draftList);
    const savedOn = new Set(orderOf(ctx.savedList));
    const now = draftOn.filter((id) => savedOn.has(id));
    const before = orderOf(ctx.savedList).filter((id) => draftOn.includes(id));
    if (now.indexOf(p.id) !== before.indexOf(p.id))
      return { tone: "pending", text: `Will move to place ${draftOn.indexOf(p.id) + 1} on your PATH when you apply.` };
    if (!st?.present)
      return {
        tone: "warn",
        text: "Not on your PATH: it was removed outside Zero2Dev.",
        fix: { label: "Add it back", reapply: true },
      };
    return { tone: "ok", text: st.owned ? "On your PATH" : "On your PATH (it was already there)" };
  }
  if (st?.present && st.owned)
    return { tone: "warn", text: "Still on your PATH.", fix: { label: "Remove it now", reapply: true } };
  if (st?.present) return { tone: "muted", text: "Off. The folder is on your PATH anyway, from your own settings." };
  return { tone: "muted", text: "Off" };
}

const defChanged = (a: EnvVar, b: EnvVar) =>
  a.name !== b.name ||
  a.source !== b.source ||
  (a.kind ?? "") !== (b.kind ?? "") ||
  (a.runtimeId ?? "") !== (b.runtimeId ?? "") ||
  (a.value ?? "") !== (b.value ?? "");

/** The status line of a variable row. `value` is what the draft item stands for. */
export function varStatus(
  v: EnvVar,
  saved: EnvVar | undefined,
  st: EnvVarStatus | undefined,
  value: string,
  supported: boolean,
): RowStatus {
  if (!supported) return { tone: "muted", text: "Windows only. Nothing is changed on this system." };
  // A missing runtime is worded here (with the kind's name); other problems come from the daemon.
  const problem =
    !value && v.source !== "custom" ? missingText(v) : st?.problem && saved && !defChanged(v, saved) ? st.problem : "";
  if (problem && (v.enabled || !st?.hasCurrent)) {
    const fix =
      v.source === "default"
        ? { label: `Install ${kindLabel(v.kind ?? "")}`, href: "#/runtimes" }
        : { label: "Open Runtimes", href: "#/runtimes" };
    return { tone: v.enabled ? "warn" : "muted", text: v.enabled ? problem : `Off. ${problem}`, fix };
  }
  const theirs = st && st.hasCurrent && !st.owned && saved && saved.name === v.name;
  const keeps = (cur: string) => ({
    tone: "warn" as const,
    text: `You already have ${v.name} = ${cur}. Zero2Dev keeps yours and never overwrites it.`,
  });
  if (!saved || defChanged(v, saved)) {
    if (!v.enabled) return { tone: "muted", text: "Off" };
    if (theirs) return keeps(st.current ?? "");
    return {
      tone: "pending",
      text: saved?.enabled ? `Will change to ${value} when you apply.` : `Will be set to ${value} when you apply.`,
    };
  }
  if (v.enabled && !saved.enabled) {
    if (theirs) return keeps(st.current ?? "");
    return { tone: "pending", text: `Will be set to ${value} when you apply.` };
  }
  if (!v.enabled && saved.enabled) {
    return st?.hasCurrent && st.owned
      ? { tone: "pending", text: "Will be removed when you apply." }
      : { tone: "muted", text: "Off" };
  }
  if (v.enabled) {
    if (!st?.hasCurrent)
      return {
        tone: "warn",
        text: "Not set: it was removed outside Zero2Dev.",
        fix: { label: "Set it again", reapply: true },
      };
    if (!st.owned) return keeps(st.current ?? "");
    if (!same(st.current ?? "", value))
      return {
        tone: "warn",
        text: `Out of date: still ${st.current}.`,
        fix: { label: "Update it now", reapply: true },
      };
    return { tone: "ok", text: "Set" };
  }
  if (st?.hasCurrent && st.owned)
    return { tone: "warn", text: "Still set by Zero2Dev.", fix: { label: "Remove it now", reapply: true } };
  if (st?.hasCurrent) return { tone: "muted", text: `Off. You have your own ${v.name} = ${st.current}.` };
  return { tone: "muted", text: "Off" };
}
/** A change as it reads after Apply: "Add X to your PATH" -> "Added X to your PATH". */
export function doneText(c: Change): string {
  const past: Record<Change["kind"], [RegExp, string]> = {
    add: [/^Add /, "Added "],
    remove: [/^Remove /, "Removed "],
    move: [/^Reorder /, "Reordered "],
    set: [/^Set /, "Set "],
    unset: [/^Remove /, "Removed "],
    keep: [/^Keep /, "Kept "],
    list: [/^Save the list\./, "Saved the list."],
  };
  const [re, to] = past[c.kind];
  return c.text.replace(re, to);
}

function missingText(v: EnvVar): string {
  return v.source === "default" ? `No ${kindLabel(v.kind ?? "")} runtime is installed.` : "That runtime was removed.";
}

// ---------- which folder wins a command ----------

/**
 * For each folder that is on (in PATH order), the commands an earlier folder already gives.
 * Returns id -> [{ command, by }], where `by` is the title of the folder that wins.
 */
export function shadowed(
  list: EnvPath[],
  commandsOf: (p: EnvPath) => string[],
  titleOf: (p: EnvPath) => string,
): Map<string, { command: string; by: string }[]> {
  const owner = new Map<string, string>();
  const out = new Map<string, { command: string; by: string }[]>();
  for (const p of list) {
    if (!p.enabled) continue;
    const lost: { command: string; by: string }[] = [];
    for (const c of commandsOf(p)) {
      const by = owner.get(c);
      if (by) lost.push({ command: c, by });
      else owner.set(c, titleOf(p));
    }
    if (lost.length) out.set(p.id, lost);
  }
  return out;
}

/** "node and npm come from Node.js 26.13.1 only, which is higher on the list." */
export function shadowText(lost: { command: string; by: string }[]): string {
  const byWinner = new Map<string, string[]>();
  for (const l of lost) byWinner.set(l.by, [...(byWinner.get(l.by) ?? []), l.command]);
  return [...byWinner]
    .map(([by, cmds]) => `${listWords(cmds)} ${cmds.length === 1 ? "comes" : "come"} from ${by}, higher on the list.`)
    .join(" ");
}
function listWords(xs: string[]): string {
  return xs.length <= 1 ? (xs[0] ?? "") : `${xs.slice(0, -1).join(", ")} and ${xs[xs.length - 1]}`;
}

// ---------- what Apply will do ----------

export interface Change {
  kind: "add" | "remove" | "move" | "set" | "unset" | "keep" | "list";
  text: string;
}

/** The changes Apply makes in Windows, in plain words, plus list-only edits. */
export function changeList(draft: EnvConfig, saved: EnvConfig, report: EnvReport, rts: Runtime[]): Change[] {
  const out: Change[] = [];
  const sp = new Map(saved.path.map((p) => [p.id, p]));
  const pst = new Map(report.path.map((p) => [p.id, p]));
  const dp = new Map(draft.path.map((p) => [p.id, p]));
  const dirOf = (p: EnvPath) => pathDir(p, pst.get(p.id), rts, report.shimsDir);
  for (const p of draft.path) {
    if (p.enabled && !sp.get(p.id)?.enabled) out.push({ kind: "add", text: `Add ${dirOf(p)} to your PATH` });
  }
  for (const p of saved.path) {
    const now = dp.get(p.id);
    if (p.enabled && !now?.enabled && pst.get(p.id)?.owned)
      out.push({ kind: "remove", text: `Remove ${dirOf(p)} from your PATH` });
  }
  const common = (list: EnvPath[], other: Map<string, EnvPath>) => orderOf(list).filter((id) => other.get(id)?.enabled);
  const a = common(draft.path, sp);
  const b = common(saved.path, dp);
  if (a.length > 1 && a.join() !== b.join()) {
    out.push({
      kind: "move",
      text: `Reorder your PATH: ${a.map((id) => pathTitle(dp.get(id)!, rts)).join(", then ")}`,
    });
  }

  const sv = new Map(saved.vars.map((v) => [v.id, v]));
  const vst = new Map(report.vars.map((v) => [v.id, v]));
  const dv = new Map(draft.vars.map((v) => [v.id, v]));
  for (const v of draft.vars) {
    const before = sv.get(v.id);
    const st = vst.get(v.id);
    const value = varValue(v, st, rts);
    if (!v.enabled) continue;
    const changed = !before || !before.enabled || defChanged(v, before);
    if (!changed) continue;
    if (before?.enabled && before.name !== v.name && st?.owned)
      out.push({ kind: "unset", text: `Remove ${before.name}` });
    if (!value) continue; // nothing to set (the runtime is missing): the row says so
    if (st?.hasCurrent && !st.owned && before?.name === v.name)
      out.push({ kind: "keep", text: `Keep your own ${v.name} (not changed)` });
    else if (value) out.push({ kind: "set", text: `Set ${v.name} = ${value}` });
  }
  for (const v of saved.vars) {
    const now = dv.get(v.id);
    if (v.enabled && !now?.enabled && vst.get(v.id)?.owned) out.push({ kind: "unset", text: `Remove ${v.name}` });
  }
  if (out.length === 0 && !sameConfig(draft, saved))
    out.push({ kind: "list", text: "Save the list. Nothing changes in Windows." });
  return out;
}

/** The number on the Apply button: changes that reach Windows (at least 1 when the list changed). */
export function changeCount(changes: Change[]): number {
  const real = changes.filter((c) => c.kind !== "keep" && c.kind !== "list").length;
  return real || (changes.length ? 1 : 0);
}

// ---------- summaries, suggestions, quick setup ----------

/** "Right now Zero2Dev adds 2 folders and 1 variable to your Windows environment." */
export function nowSummary(report: EnvReport, saved: EnvConfig): string {
  if (!report.supported) return "This system is not Windows, so nothing here can change its environment.";
  const on = new Set([...saved.path, ...saved.vars].filter((i) => i.enabled).map((i) => i.id));
  const folders = report.path.filter((p) => on.has(p.id) && p.present && p.owned).length;
  const vars = report.vars.filter((v) => on.has(v.id) && v.hasCurrent && v.owned).length;
  if (!folders && !vars) return "Right now Zero2Dev adds nothing to your Windows environment.";
  const n = (k: number, word: string) => `${k} ${word}${k === 1 ? "" : "s"}`;
  const parts = [folders && n(folders, "folder"), vars && n(vars, "variable")].filter(Boolean);
  return `Right now Zero2Dev adds ${parts.join(" and ")} to your Windows environment.`;
}

/** True when Zero2Dev owns anything in the user environment (for "Remove everything"). */
export function ownsAnything(report: EnvReport): boolean {
  return report.path.some((p) => p.present && p.owned) || report.vars.some((v) => v.hasCurrent && v.owned);
}

/** One-click variables for installed language runtimes that have none yet. */
export function suggestions(rts: Runtime[], vars: EnvVar[]): { name: string; kind: string; label: string }[] {
  return Object.entries(SUGGESTED_NAMES)
    .filter(
      ([kind, name]) =>
        defaultOf(rts, kind) && !vars.some((v) => same(v.name, name) || (v.source === "default" && v.kind === kind)),
    )
    .map(([kind, name]) => ({ name, kind, label: `${name} (default ${kindLabel(kind)})` }));
}

/** "Use the default versions everywhere": the shims folder first, plus JAVA_HOME / GOROOT for installed kinds. */
export function quickSetup(c: EnvConfig, rts: Runtime[]): EnvConfig {
  const out = cloneConfig(c);
  const shims = out.path.find((p) => p.source === "shims");
  if (shims) shims.enabled = true;
  else out.path.unshift({ id: newItemId("envp_"), source: "shims", enabled: true });
  for (const kind of ["java", "go"]) {
    if (!defaultOf(rts, kind)) continue;
    const name = SUGGESTED_NAMES[kind];
    const v =
      out.vars.find((x) => x.source === "default" && x.kind === kind) ?? out.vars.find((x) => same(x.name, name));
    if (v) v.enabled = true;
    else out.vars.push({ id: newItemId("envv_"), name, source: "default", kind, enabled: true });
  }
  return out;
}

/** The first command worth trying in a new terminal after Apply, e.g. "node -v". */
export function tryCommand(draft: EnvConfig, report: EnvReport, rts: Runtime[]): string {
  const st = new Map(report.path.map((p) => [p.id, p]));
  for (const p of draft.path) {
    if (!p.enabled) continue;
    const cmds = pathCommands(p, st.get(p.id), rts);
    for (const [cmd, flag] of [
      ["node", "-v"],
      ["php", "-v"],
      ["java", "-version"],
      ["go", "version"],
    ] as const) {
      if (cmds.includes(cmd)) return `${cmd} ${flag}`;
    }
    if (cmds[0]) return `where ${cmds[0]}`;
  }
  const v = draft.vars.find((x) => x.enabled);
  return v ? `echo %${v.name}%` : "";
}

// ---------- input checks ----------

/** A problem with a variable name the user is typing, or "". `others` are the other variables' names. */
export function nameError(name: string, others: string[]): string {
  const n = name.trim();
  if (!n) return "Enter a name.";
  if (!NAME_RE.test(n)) return "Use letters, digits and _, and do not start with a digit.";
  if (n.toUpperCase() === "PATH") return "PATH is edited through the folders above.";
  if (others.some((o) => same(o, n))) return `${n} is already in the list.`;
  return "";
}

/** A problem with a folder the user is typing, or "". `others` are the folders already listed. */
export function dirError(dir: string, others: string[]): string {
  const d = dir.trim();
  if (!d) return "Enter a folder.";
  if (!ABS_RE.test(d)) return "Use a full path such as C:\\tools\\bin.";
  if (/[%;"]/.test(d)) return 'A folder with % ; or " in its name cannot go on PATH.';
  const norm = (s: string) => s.trim().replace(/\//g, "\\").replace(/\\+$/, "").toLowerCase();
  if (others.some((o) => norm(o) === norm(d))) return "That folder is already in the list.";
  return "";
}

/** Runtimes that have no folder item yet. */
export function runtimesWithoutPath(rts: Runtime[], path: EnvPath[]): Runtime[] {
  const used = new Set(path.filter((p) => p.source === "runtime").map((p) => p.runtimeId));
  return rts.filter((r) => !used.has(r.id));
}
