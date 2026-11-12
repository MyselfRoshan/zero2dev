// The per-kind service profiles (internal/kinds/profiles.json, served at GET /api/kinds)
// and pure helpers the service form uses. The daemon builds the real command line;
// these helpers only decide what the form shows.

export type FieldUse = "required" | "optional" | "hidden";
export type OptionType = "text" | "number" | "bool" | "select" | "multi" | "size" | "lines";

export interface Choice {
  value: string;
  label: string;
  args?: string[];
}

export interface KindOption {
  key: string;
  group: string;
  label: string;
  type: OptionType;
  default: string;
  help: string;
  placeholder?: string;
  choices?: Choice[];
  choicesFrom?: "phpExtensions";
  suggest?: "npmScripts";
  min?: number;
  max?: number;
  pattern?: string;
  show?: Record<string, string>;
  minVersion?: string;
  required?: boolean;
  at?: "end";
  emit?: {
    arg?: string[];
    argOff?: string[];
    ini?: string;
    env?: string;
    envSet?: Record<string, string>;
    program?: Record<string, string>;
    special?: string;
  };
}

export interface KindProfile {
  kind: string;
  label: string;
  managed?: boolean;
  note?: string;
  folder: { use: FieldUse; label?: string; hint?: string };
  port: { use: FieldUse; default?: number; hint?: string };
  health: boolean;
  dataDir: "service" | "shared" | "none";
  program?: boolean;
  extraArgs: string;
  extraArgsHint: string;
  fixed?: { args: string[]; help: string; windows?: boolean }[];
  env?: { name: string; value: string; help: string }[];
  options: KindOption[];
}

export type Settings = Record<string, string>;

export function profileFor(profiles: KindProfile[], kind: string): KindProfile | undefined {
  return profiles.find((p) => p.kind === kind);
}

export function defaultsFor(p: KindProfile | undefined): Settings {
  const out: Settings = {};
  for (const o of p?.options ?? []) out[o.key] = o.default;
  return out;
}

/** The saved settings with defaults for every key they lack (as the daemon does). */
export function effective(p: KindProfile | undefined, s: Settings | undefined): Settings {
  const out = defaultsFor(p);
  for (const [k, v] of Object.entries(s ?? {})) if (k in out) out[k] = v;
  return out;
}

export function splitList(v: string): string[] {
  return v
    .split(",")
    .map((x) => x.trim())
    .filter(Boolean);
}

/** Whether an option applies with the current settings (its `show` conditions, recursively). */
export function isVisible(p: KindProfile, o: KindOption, s: Settings): boolean {
  for (const [k, want] of Object.entries(o.show ?? {})) {
    const got = s[k] ?? "";
    const dep = p.options.find((x) => x.key === k);
    if (want === "*") {
      if (got === "" || got === "false") return false;
    } else if (dep?.type === "multi") {
      if (!splitList(got).includes(want)) return false;
    } else if (got !== want) return false;
    if (dep && !isVisible(p, dep, s)) return false;
  }
  return true;
}

/** Dotted version comparison; an unknown version counts as new enough (kinds.VersionAtLeast). */
export function versionAtLeast(have: string, want: string): boolean {
  const nums = (v: string) => {
    const out: number[] = [];
    for (const part of v.trim().replace(/^v/, "").split(".")) {
      const m = /^(\d+)/.exec(part);
      if (!m) break;
      out.push(Number(m[1]));
      if (m[1].length < part.length) break;
    }
    return out;
  };
  const h = nums(have),
    w = nums(want);
  if (!h.length || !w.length) return true;
  for (let i = 0; i < w.length; i++) {
    const hv = h[i] ?? 0;
    if (hv !== w[i]) return hv > w[i];
  }
  return true;
}

export interface OptionGroup {
  name: string;
  options: KindOption[];
}

/** Visible options grouped by `group`, in the order the groups first appear. */
export function groupedOptions(p: KindProfile, s: Settings): OptionGroup[] {
  const groups: OptionGroup[] = [];
  for (const o of p.options) {
    if (!isVisible(p, o, s)) continue;
    let g = groups.find((x) => x.name === o.group);
    if (!g) groups.push((g = { name: o.group, options: [] }));
    g.options.push(o);
  }
  return groups;
}

/** The flag part of an argument list: ["-c", "max_connections={v}"] -> "-c max_connections", ["--port", "{v}"] -> "--port". */
function flagOf(args: string[]): string {
  const strip = (a: string) => a.replace(/=.*$/, "").replace(/\{.*$/, "");
  const first = args.find((a) => a.startsWith("-"));
  if (!first) return "";
  const i = args.indexOf(first);
  const next = args[i + 1];
  if (!first.includes("=") && next !== undefined && !next.startsWith("-") && /^[A-Za-z_][\w.]*=/.test(next)) {
    return `${first} ${strip(next)}`;
  }
  return strip(first);
}

/** A short "what this writes" label: --max-connections, -c max_connections, php.ini memory_limit, NODE_ENV. */
export function writesLabel(o: KindOption): string {
  const e = o.emit ?? {};
  if (e.ini) return `php.ini ${e.ini}`;
  if (e.env) return e.env;
  if (e.envSet) return Object.keys(e.envSet).join(", ");
  if (e.arg?.length) return flagOf(e.arg);
  const perChoice = [
    ...new Set(
      (o.choices ?? []).flatMap((c) =>
        (c.args ?? []).filter((a) => a.startsWith("-")).map((a) => a.replace(/=.*$/, "")),
      ),
    ),
  ];
  if (perChoice.length) return perChoice.join(", ");
  if (e.special === "phpExtensions") return "php.ini extension=";
  if (e.special === "phpIniLines" || e.special === "phpIniBase") return "php.ini";
  return "";
}

const FRIENDLY: [string, string][] = [
  ["{port}", "<port>"],
  ["{home}", "<data folder>"],
  ["{data}", "<service data folder>"],
  ["{dataSlash}", "<service data folder>"],
  ["{ini}", "<service data folder>\\php.ini"],
  ["{docroot}", "<document root>"],
  ["{binDir}", "<program folder>"],
  ["{cwd}", "<project folder>"],
];

/** Fixed arguments with their placeholders in words, for the "Always added" list. */
export function friendlyArgs(args: string[]): string {
  return args
    .map((a) => {
      if (a === "") return '""';
      let s = a;
      for (const [k, v] of FRIENDLY) s = s.replaceAll(k, v);
      return /\s/.test(a) ? `"${s}"` : s;
    })
    .join(" ");
}

export function fixedFor(p: KindProfile, windows: boolean) {
  return (p.fixed ?? []).filter((f) => windows || !f.windows);
}

/** Client-side check of one value, mirroring kinds.Option.check for instant feedback. */
export function checkOption(o: KindOption, v: string): string {
  if (o.type !== "lines" && /[\r\n]/.test(v)) return "must be on one line";
  switch (o.type) {
    case "bool":
      return v === "true" || v === "false" ? "" : "must be on or off";
    case "number": {
      if (!/^-?\d+$/.test(v.trim())) return "must be a whole number";
      const n = Number(v);
      if ((o.min !== undefined && n < o.min) || (o.max !== undefined && n > o.max))
        return `must be between ${o.min} and ${o.max}`;
      return "";
    }
    case "select":
      return o.choices?.some((c) => c.value === v) ? "" : "pick one of the choices";
    case "multi":
    case "lines":
      return "";
  }
  if (o.pattern && v !== "" && !new RegExp(o.pattern).test(v)) return `"${v}" is not valid here`;
  return "";
}

/** Errors for the visible options, keyed by option key. */
export function settingErrors(p: KindProfile | undefined, s: Settings): Record<string, string> {
  const out: Record<string, string> = {};
  if (!p) return out;
  const eff = effective(p, s);
  for (const o of p.options) {
    if (!isVisible(p, o, eff)) continue;
    const v = eff[o.key] ?? "";
    if (o.required && v.trim() === "") out[o.key] = `${o.label} is required`;
    else {
      const e = checkOption(o, v);
      if (e) out[o.key] = `${o.label} ${e}`;
    }
  }
  return out;
}

/** Only the keys the profile knows, so switching the type drops the old type's settings. */
export function settingsForSave(p: KindProfile | undefined, s: Settings): Settings {
  const out: Settings = {};
  for (const o of p?.options ?? []) if (o.key in s) out[o.key] = s[o.key];
  return out;
}

/** Kinds offered for a new service: the profiles Zero2Dev does not manage itself. */
export function creatableKinds(profiles: KindProfile[]): string[] {
  return profiles.filter((p) => !p.managed).map((p) => p.kind);
}

/** A command line for display, quoting arguments with spaces (and empty ones). */
export function commandText(program: string, args: string[]): string {
  const q = (a: string) => (a === "" ? '""' : /[\s"]/.test(a) ? `"${a.replaceAll('"', '\\"')}"` : a);
  return [program, ...args].map(q).join(" ");
}
