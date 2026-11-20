// Pure helpers for the Import page (#/migrate) and its entry points: grouping the sources found
// on this PC, sizes, the review text shown before a migration starts, plain-words explanations of
// the revert choices, blockers turned into actions, and the database copy form's checks.
import type {
  MigrateItem,
  MigrateMode,
  MigrateProgram,
  MigrateSource,
  Migration,
  RevertHow,
  Runtime,
  Service,
} from "./types";

// ---------- sizes ----------

/** 0 -> "0 bytes", 2048000 -> "2.0 MB", 412000000 -> "412 MB", 1.3e9 -> "1.3 GB". Decimal units, like Explorer's "size" column in spirit. */
export function fmtSize(bytes: number): string {
  if (!Number.isFinite(bytes) || bytes <= 0) return "0 bytes";
  if (bytes < 1000) return `${Math.round(bytes)} bytes`;
  const units = ["KB", "MB", "GB", "TB"];
  let v = bytes / 1000;
  let i = 0;
  while (v >= 1000 && i < units.length - 1) {
    v /= 1000;
    i++;
  }
  return `${v < 10 ? v.toFixed(1) : Math.round(v)} ${units[i]}`;
}

export function totalSize(items: { sizeBytes: number }[]): number {
  return items.reduce((n, i) => n + (i.sizeBytes > 0 ? i.sizeBytes : 0), 0);
}

// ---------- groups ----------

export interface SourceGroup {
  id: "tools" | "runtimes" | "caches" | "databases";
  title: string;
  text: string;
  programs: MigrateProgram[];
}

export const SOURCE_GROUPS: SourceGroup[] = [
  {
    id: "tools",
    title: "Editors and tools",
    text: "Settings, extensions and saved connections of the editors and database tools installed on this PC.",
    programs: ["vscode", "vscodium", "notepadpp", "heidisql", "dbeaver"],
  },
  {
    id: "runtimes",
    title: "Runtimes",
    text: "Programs you registered by path. Adopting one copies it into Zero2Dev's folder; services that use it keep working.",
    programs: ["runtime"],
  },
  {
    id: "caches",
    title: "Caches and global packages",
    text: "Global npm packages and the download caches of npm, Go and Composer.",
    programs: ["npm-global", "npm-cache", "go-cache", "composer"],
  },
  {
    id: "databases",
    title: "Databases",
    text: "Databases on a MySQL, MariaDB or PostgreSQL server you name, copied into a Zero2Dev database service.",
    programs: ["database"],
  },
];

export interface GroupedSources {
  group: SourceGroup;
  /** Found on this PC, in the order of the group's programs. */
  found: MigrateSource[];
  /** Looked for but not found. */
  missing: MigrateSource[];
}

/** The groups that have at least one source, each split into found and not found. */
export function groupSources(sources: MigrateSource[]): GroupedSources[] {
  const out: GroupedSources[] = [];
  for (const group of SOURCE_GROUPS) {
    const mine = sources
      .filter((s) => group.programs.includes(s.program))
      .sort((a, b) => group.programs.indexOf(a.program) - group.programs.indexOf(b.program));
    if (!mine.length) continue;
    out.push({ group, found: mine.filter((s) => s.found), missing: mine.filter((s) => !s.found) });
  }
  return out;
}

/** The kind whose brand icon stands for a source. */
export function sourceIconKind(s: MigrateSource, runtimes: Runtime[] = []): string {
  switch (s.program) {
    case "runtime":
      return runtimes.find((r) => r.id === s.runtimeId)?.kind ?? "other";
    case "npm-global":
    case "npm-cache":
      return "node";
    case "go-cache":
      return "go";
    case "composer":
      return "php";
    case "database":
      return "mariadb";
    default:
      return s.program;
  }
}

/** The source a `?source=` link names: its id, or `runtime:<runtimeId>`. "" when it is not listed. */
export function preselect(sources: MigrateSource[], wanted: string): string {
  if (!wanted) return "";
  const hit =
    sources.find((s) => s.id === wanted) ??
    sources.find((s) => s.program === "runtime" && `runtime:${s.runtimeId}` === wanted);
  return hit ? hit.id : "";
}

export const adoptHref = (runtimeId: string): string =>
  `#/migrate?source=${encodeURIComponent(`runtime:${runtimeId}`)}`;
export const sourceHref = (sourceId: string): string => `#/migrate?source=${encodeURIComponent(sourceId)}`;

// ---------- the plan ----------

/** One line under a source: what it holds, or why it cannot run yet. */
export function sourceSummary(s: MigrateSource): string {
  if (!s.found) return "Not found on this PC";
  if (s.program === "database") return "Pick a server and the databases to copy";
  const present = s.items.filter((i) => i.exists);
  if (!present.length) return "Found, but there is nothing to import";
  const names = present.map((i) => i.label);
  const list = names.length <= 3 ? joinWords(names) : `${names.length} items`;
  return `${list}, ${fmtSize(totalSize(present))}`;
}

/** The items a migration takes: the checked labels, or every present item when none is checked. */
export function pickedItems(s: MigrateSource, checked: string[]): MigrateItem[] {
  const present = s.items.filter((i) => i.exists);
  return checked.length ? present.filter((i) => checked.includes(i.label)) : present;
}

/** `items` for the request: [] (all) when every present item is checked. */
export function requestItems(s: MigrateSource, checked: string[]): string[] {
  const present = s.items.filter((i) => i.exists).map((i) => i.label);
  const picked = present.filter((l) => checked.includes(l));
  return picked.length === present.length ? [] : picked;
}

export const modeAllowed = (s: MigrateSource, mode: MigrateMode): boolean => s.modes.includes(mode);

/** The mode to start with: copy when offered (it keeps the originals), else the first offered. */
export function defaultMode(s: MigrateSource): MigrateMode {
  return s.modes.includes("copy") ? "copy" : (s.modes[0] ?? "copy");
}

export interface ReviewDb {
  databases: { name: string; sizeBytes: number }[];
  host: string;
  port: number;
  target: string;
}

/** The sentence shown before a migration starts: "This copies 414 MB ...". */
export function reviewText(s: MigrateSource, mode: MigrateMode, items: MigrateItem[], db?: ReviewDb): string {
  if (s.program === "database" && db) {
    const n = db.databases.length;
    const size = totalSize(db.databases);
    const what = n === 1 ? `the database ${db.databases[0].name}` : `${n} databases`;
    return `This copies ${what}${size ? ` (${fmtSize(size)})` : ""} from ${db.host}:${db.port} into ${db.target}. The server you copy from is not changed.`;
  }
  const size = fmtSize(totalSize(items));
  if (s.program === "runtime")
    return `This copies ${s.label} (${size}) into Zero2Dev's folder. Services that use it keep working, now from the copy. The program on this PC is not changed.`;
  const what = items.length === 1 ? items[0].label.toLowerCase() : joinWords(items.map((i) => i.label.toLowerCase()));
  if (mode === "move")
    return `This moves ${size} (${what}) of ${s.label} into Zero2Dev's folder. The originals are saved first and then removed from their old place, so the installed ${s.label} starts fresh.`;
  return `This copies ${size} (${what}) of ${s.label} into Zero2Dev's folder. The originals stay where they are.`;
}

/** What happens to the data Zero2Dev's folder holds already. */
export function targetText(s: MigrateSource): string {
  return s.targetHasData
    ? `Zero2Dev's ${s.label} folder already holds data. What is there now is saved first, and comes back if you revert.`
    : "";
}

export const startLabel = (mode: MigrateMode, s: MigrateSource): string =>
  s.program === "runtime" ? "Adopt into Zero2Dev" : mode === "move" ? "Start moving" : "Start copying";

// ---------- blockers ----------

export type BlockerAction =
  | { type: "link"; href: string; label: string }
  | { type: "recheck"; label: string }
  | { type: "none" };

/** Turns a blocker from the daemon into something the user can do about it. */
export function blockerAction(text: string): BlockerAction {
  if (/^install .+ in zero2dev/i.test(text)) return { type: "link", href: "#/runtimes", label: "Open Runtimes" };
  if (/^(close|quit|stop) /i.test(text) || /is running/i.test(text)) return { type: "recheck", label: "Check again" };
  if (/(create|add) .*service/i.test(text)) return { type: "link", href: "#/services/new", label: "New service" };
  return { type: "none" };
}

// ---------- history and revert ----------

export function stateLabel(m: Migration): { tone: "ok" | "idle" | "bad"; text: string } {
  if (m.state === "done")
    return { tone: "ok", text: m.mode === "move" ? "Moved" : m.program === "runtime" ? "Adopted" : "Copied" };
  if (m.state === "reverted") return { tone: "idle", text: "Reverted" };
  return { tone: "bad", text: "Failed" };
}

export function revertLabel(how: RevertHow): string {
  return how === "discard"
    ? "Discard the copy"
    : how === "copy-back"
      ? "Copy back to the installed program"
      : "Move back";
}

/** What a revert choice does, in plain words. */
export function revertText(how: RevertHow, m: Migration, serviceName = "the Zero2Dev service"): string {
  if (m.program === "runtime")
    return "Zero2Dev uses the program from its old place again, and the copy in Zero2Dev's folder is removed. Services keep working.";
  if (m.program === "database") {
    const dbs = m.databases?.length ? joinWords(m.databases) : "the databases";
    return `Drops ${dbs} from ${serviceName}. The server you copied from was never changed.`;
  }
  switch (how) {
    case "discard":
      return `Removes the copy in Zero2Dev's folder and brings back what was there before. The installed ${m.label} is not touched.`;
    case "copy-back":
      return `Copies what Zero2Dev has now over the installed ${m.label}, so changes you made in Zero2Dev reach it. Its current files are saved first.`;
    case "move-back":
      return `Puts the moved items back where they were, so the installed ${m.label} has them again. Whatever is there by then is saved first.`;
  }
}

/** The revert choice to preselect: the one that loses nothing. */
export function defaultRevert(m: Migration): RevertHow | undefined {
  for (const how of ["move-back", "discard", "copy-back"] as RevertHow[]) if (m.revertOptions.includes(how)) return how;
  return undefined;
}

export const canRevert = (m: Migration): boolean => m.state === "done" && m.revertOptions.length > 0;

// ---------- entry points ----------

const EDITOR_PROGRAMS = ["vscode", "vscodium", "notepadpp", "heidisql", "dbeaver"];

/** The note on a Runtimes card: for an editor whose installed copy was found, or a registered runtime. */
export function noteFor(
  kind: string,
  sources: MigrateSource[],
  migrations: Migration[],
  runtimes: Runtime[] = [],
): { text: string; href: string; link: string } | null {
  if (EDITOR_PROGRAMS.includes(kind)) {
    const s = sources.find((x) => x.program === kind && x.found && x.items.some((i) => i.exists));
    if (!s) return null;
    if (migrations.some((m) => m.source === s.id && m.state === "done")) return null;
    const what = kind === "heidisql" || kind === "dbeaver" ? "settings and connections" : "settings";
    return {
      text: `Your installed ${s.label} ${what} can be imported.`,
      href: sourceHref(s.id),
      link: "Import them",
    };
  }
  const ofKind = new Set(runtimes.filter((r) => r.kind === kind && r.source === "registered").map((r) => r.id));
  const mine = sources.filter((x) => x.program === "runtime" && x.found && !!x.runtimeId && ofKind.has(x.runtimeId));
  if (!mine.length) return null;
  if (mine.length === 1)
    return {
      text: `${mine[0].label} is registered by path. You can copy it into Zero2Dev's folder.`,
      href: sourceHref(mine[0].id),
      link: "Adopt into Zero2Dev",
    };
  return { text: `${mine.length} versions are registered by path.`, href: "#/migrate", link: "Adopt them in Import" };
}

// ---------- database copy form ----------

export type DbKind = "mariadb" | "mysql" | "postgres";
export const DB_KINDS: { id: DbKind; label: string }[] = [
  { id: "mysql", label: "MySQL" },
  { id: "mariadb", label: "MariaDB" },
  { id: "postgres", label: "PostgreSQL" },
];
export const defaultPort = (k: DbKind): number => (k === "postgres" ? 5432 : 3306);
export const defaultUser = (k: DbKind): string => (k === "postgres" ? "postgres" : "root");

/** Zero2Dev database services a dump of this kind can go into. */
export function dbTargets(services: Service[], k: DbKind): Service[] {
  const family = k === "postgres" ? ["postgres"] : ["mariadb", "mysql"];
  return services.filter((s) => family.includes(s.kind));
}

export interface DbForm {
  kind: DbKind;
  host: string;
  port: string;
  user: string;
}
export type DbField = "host" | "port" | "user" | "databases" | "target";

const LOCAL = /^(localhost|127\.0\.0\.1|::1|\[::1\])$/i;

/** Checks before "List databases". `zero2dev` are the services Zero2Dev runs (a copy from one of them is not what Import is for). */
export function validateDbForm(f: DbForm, zero2dev: Service[] = []): Partial<Record<DbField, string>> {
  const e: Partial<Record<DbField, string>> = {};
  const host = f.host.trim();
  if (!host) e.host = "Enter the server's host name or address, such as 127.0.0.1.";
  else if (/[\s/\\]/.test(host)) e.host = "A host name has no spaces or slashes.";
  const port = f.port.trim();
  const n = Number(port);
  if (!port) e.port = "Enter the port.";
  else if (!/^\d+$/.test(port) || n < 1 || n > 65535) e.port = "The port is a number from 1 to 65535.";
  else if (host && LOCAL.test(host)) {
    const own = zero2dev.find((s) => s.port === n && ["mariadb", "mysql", "postgres"].includes(s.kind));
    if (own)
      e.port = `Port ${n} is ${own.name}, a Zero2Dev service. Enter the port of the server installed on this PC.`;
  }
  if (!f.user.trim()) e.user = "Enter the user name to sign in with.";
  return e;
}

/** Checks before the review: at least one database and a target service. */
export function validateDbCopy(
  picked: string[],
  targetId: string,
  targets: Service[],
): Partial<Record<DbField, string>> {
  const e: Partial<Record<DbField, string>> = {};
  if (!picked.length) e.databases = "Pick at least one database.";
  if (!targetId || !targets.some((t) => t.id === targetId)) e.target = "Pick the Zero2Dev service to copy into.";
  return e;
}

// ---------- words ----------

export function joinWords(words: string[]): string {
  if (words.length <= 1) return words.join("");
  return `${words.slice(0, -1).join(", ")} and ${words[words.length - 1]}`;
}

// ---------- is it worth importing? ----------

export type AdviceLevel = "recommended" | "optional" | "skip";
export interface Advice {
  level: AdviceLevel;
  reason: string;
}

export const adviceLabel = (l: AdviceLevel): string =>
  l === "recommended" ? "Recommended" : l === "optional" ? "Optional" : "Not worth it";

const SMALL = "Small, and it keeps your keybindings, snippets, themes and saved connections.";

/** Whether an item is worth importing, and why. Without an item: the source as a whole. */
export function adviceFor(s: MigrateSource, item?: MigrateItem): Advice {
  switch (s.program) {
    case "vscode":
    case "vscodium":
      return item?.label === "Extensions"
        ? {
            level: "recommended",
            reason: "Keeps the exact versions you use. Large, but it saves downloading them all again.",
          }
        : { level: "recommended", reason: SMALL };
    case "notepadpp":
    case "heidisql":
    case "dbeaver":
      return { level: "recommended", reason: SMALL };
    case "npm-global":
      return {
        level: "recommended",
        reason: "Your global tools (npm install -g). They work in Zero2Dev's terminal afterwards.",
      };
    case "composer":
      return {
        level: "recommended",
        reason: "Your global PHP packages and auth.json. They work in Zero2Dev's terminal afterwards.",
      };
    case "go-cache":
      return { level: "optional", reason: "It rebuilds by itself, but bringing it in saves a long first build." };
    case "npm-cache":
      return {
        level: "skip",
        reason: "It fills up again by itself, and it is mostly wasted space. Skip it unless you work offline.",
      };
    case "runtime":
      return {
        level: "optional",
        reason: "Only if you want Zero2Dev to own this install. It is copy only; the installed program stays.",
      };
    case "database":
      return { level: "optional", reason: "A one-off copy of the databases you pick. The server keeps its own." };
    default:
      return { level: "optional", reason: "" };
  }
}

/** The labels to tick when a plan opens: the recommended items, or all when there is one item or none is recommended. */
export function defaultChecked(s: MigrateSource): string[] {
  const present = s.items.filter((i) => i.exists);
  if (present.length <= 1) return present.map((i) => i.label);
  const rec = present.filter((i) => adviceFor(s, i).level === "recommended");
  return (rec.length ? rec : present).map((i) => i.label);
}

// ---------- status of a source ----------

export interface SourceStatus {
  key: "new" | "copied" | "moved" | "adopted" | "blocked";
  text: string;
  tone: "ok" | "idle" | "accent";
}

/** The migration that holds a source now: done and not reverted. */
export const activeMigration = (history: Migration[] | null, sourceId: string): Migration | undefined =>
  history?.find((m) => m.source === sourceId && m.state === "done");

export function sourceStatus(s: MigrateSource, history: Migration[] | null): SourceStatus {
  const act = activeMigration(history, s.id);
  if (act) {
    const key = act.mode === "move" ? "moved" : act.program === "runtime" ? "adopted" : "copied";
    return { key, text: stateLabel(act).text, tone: "ok" };
  }
  if (s.blockers.length) return { key: "blocked", text: "Not ready", tone: "idle" };
  return { key: "new", text: "Not imported", tone: "accent" };
}

/** Whether a row can be ticked for a batch: found, ready, not the database form, not imported already. */
export const selectable = (s: MigrateSource, history: Migration[] | null): boolean =>
  s.found && s.program !== "database" && !s.blockers.length && !activeMigration(history, s.id);

// ---------- summary and the recommended list ----------

export interface Recommended {
  source: MigrateSource;
  items: MigrateItem[];
  bytes: number;
}

/** What is worth importing and can start now, source by source. */
export function recommended(sources: MigrateSource[], history: Migration[] | null): Recommended[] {
  const out: Recommended[] = [];
  for (const s of sources) {
    if (!selectable(s, history)) continue;
    const items = s.items.filter((i) => i.exists && adviceFor(s, i).level === "recommended");
    if (items.length) out.push({ source: s, items, bytes: totalSize(items) });
  }
  return out;
}

export interface Summary {
  found: number;
  bringable: number;
  broughtIn: number;
  backups: number;
}

export function summary(sources: MigrateSource[], history: Migration[]): Summary {
  const found = sources.filter((s) => s.found && s.program !== "database" && s.items.some((i) => i.exists));
  const open = found.filter((s) => !activeMigration(history, s.id));
  return {
    found: found.length,
    bringable: totalSize(open.flatMap((s) => s.items.filter((i) => i.exists))),
    broughtIn: history.filter((m) => m.state === "done").length,
    backups: backupTotal(history),
  };
}

// ---------- free space ----------

export interface FreeSpace {
  state: "unknown" | "ok" | "tight" | "short";
  needed: number;
  free: number;
  text: string;
}

/** Compares what is about to be written with the free space of the drive. A move counts like a copy: across drives it needs the room. */
export function freeSpace(s: MigrateSource, needed: number): FreeSpace {
  const free = s.targetFreeBytes;
  if (!(free > 0) || needed <= 0) return { state: "unknown", needed, free: 0, text: "" };
  const n = fmtSize(needed);
  const f = fmtSize(free);
  if (free < needed)
    return {
      state: "short",
      needed,
      free,
      text: `Needs ${n}, but only ${f} is free on Zero2Dev's drive. Free some space first.`,
    };
  if (free < needed * 1.2)
    return { state: "tight", needed, free, text: `Needs ${n}, and ${f} is free on Zero2Dev's drive. That is tight.` };
  return { state: "ok", needed, free, text: `Needs ${n}; ${f} is free on Zero2Dev's drive.` };
}

// ---------- several at once ----------

export type BatchState = "waiting" | "running" | "done" | "failed";
export interface BatchEntry {
  sourceId: string;
  label: string;
  mode: MigrateMode;
  /** The `items` of the request: [] means all. */
  items: string[];
  bytes: number;
  state: BatchState;
  error: string;
  /** Percent while it runs. */
  percent: number;
}

/** One entry per ticked source: its recommended items, moved when asked and allowed, else copied. */
export function buildBatch(
  sources: MigrateSource[],
  history: Migration[] | null,
  ids: string[],
  preferMove: boolean,
): BatchEntry[] {
  const out: BatchEntry[] = [];
  for (const id of ids) {
    const s = sources.find((x) => x.id === id);
    if (!s || !selectable(s, history)) continue;
    const labels = defaultChecked(s);
    const items = pickedItems(s, labels);
    if (!items.length) continue;
    out.push({
      sourceId: s.id,
      label: s.label,
      mode: preferMove && modeAllowed(s, "move") ? "move" : "copy",
      items: requestItems(s, labels),
      bytes: totalSize(items),
      state: "waiting",
      error: "",
      percent: 0,
    });
  }
  return out;
}

export const nextWaiting = (b: BatchEntry[]): BatchEntry | undefined => b.find((e) => e.state === "waiting");
export const batchBusy = (b: BatchEntry[]): boolean => b.some((e) => e.state === "running" || e.state === "waiting");

export function batchSummary(b: BatchEntry[]): string {
  const done = b.filter((e) => e.state === "done").length;
  const failed = b.filter((e) => e.state === "failed");
  if (!failed.length) return `All ${done} are in Zero2Dev now.`;
  const names = failed.map((e) => e.label).join(", ");
  return `${done} of ${b.length} were imported. ${names} ${failed.length === 1 ? "was" : "were"} not: what was there before is kept.`;
}

// ---------- history ----------

export type HistoryFilter = "all" | "active" | "reverted" | "failed";
export const HISTORY_FILTERS: { id: HistoryFilter; label: string }[] = [
  { id: "all", label: "All" },
  { id: "active", label: "In use" },
  { id: "reverted", label: "Reverted" },
  { id: "failed", label: "Failed" },
];

export function filterHistory(list: Migration[], state: HistoryFilter, program = ""): Migration[] {
  return list.filter(
    (m) =>
      (state === "all" || (state === "active" ? m.state === "done" : m.state === state)) &&
      (!program || m.program === program),
  );
}

export const backupTotal = (list: Migration[]): number =>
  list.reduce((n, m) => n + (m.backupBytes > 0 ? m.backupBytes : 0), 0);

/** "took 12 s", from the start to the end of the job; "" when unknown. */
export function tookText(m: Migration): string {
  if (!m.finishedAt) return "";
  const ms = new Date(m.finishedAt).getTime() - new Date(m.createdAt).getTime();
  if (!Number.isFinite(ms) || ms < 0) return "";
  if (ms < 1000) return "took under a second";
  const s = Math.round(ms / 1000);
  return s < 60 ? `took ${s} s` : `took ${Math.floor(s / 60)} min ${s % 60} s`;
}

export function cleanupText(m: Migration): string {
  return `Deletes the old ${m.label} files that the copy left behind (${fmtSize(m.originalsBytes)}). Zero2Dev checks that its copy is complete first. Afterwards the installed ${m.label} starts fresh, and Revert can put the files back from Zero2Dev's copy.`;
}

export function backupsText(m: Migration): string {
  return `Deletes the backups of ${m.label} (${fmtSize(m.backupBytes)}): what Zero2Dev's folder held before, and anything a revert saved. Your data in Zero2Dev stays, but a revert will no longer bring those back.`;
}

/** Why the old files of a copy cannot be removed (runtimes and databases), or "". */
export function noCleanupReason(m: Migration): string {
  if (m.state !== "done") return "";
  if (m.program === "runtime")
    return "The old program belongs to the installer that put it there, so Zero2Dev leaves it.";
  if (m.program === "database") return "The databases stay on the server you copied them from.";
  return "";
}
