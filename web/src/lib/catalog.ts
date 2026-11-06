// Pure logic for the runtime cards: many versions side by side, a default per kind, version lists.
import type { CatalogEntry, CatalogSource, Runtime, RuntimeKind } from "./types";

export const KIND_LABEL: Record<string, string> = {
  nginx: "nginx",
  mariadb: "MariaDB",
  mysql: "MySQL",
  postgres: "PostgreSQL",
  redis: "Redis",
  vscode: "VS Code",
  vscodium: "VSCodium",
  notepadpp: "Notepad++",
  heidisql: "HeidiSQL",
  dbeaver: "DBeaver",
  php: "PHP",
  java: "Java",
  node: "Node.js",
  go: "Go",
  caddy: "Caddy",
  other: "Other",
};
export const kindLabel = (k: string): string => KIND_LABEL[k] ?? k;

/** A category of the Runtimes page. Web servers serve sites; they are not what services run on. */
export interface RuntimeGroup {
  id: "languages" | "servers" | "databases" | "editors" | "dbtools";
  title: string;
  text: string;
  kinds: RuntimeKind[];
}

/** Runtime cards by category, in display order. */
export const RUNTIME_GROUPS: RuntimeGroup[] = [
  { id: "languages", title: "Languages", text: "What your services run on.", kinds: ["node", "php", "go", "java"] },
  {
    id: "servers",
    title: "Web servers",
    text: "Serve your sites at addresses like shop.localhost. One of them serves at a time.",
    kinds: ["caddy", "nginx"],
  },
  {
    id: "databases",
    title: "Databases",
    text: "Run as services on this PC only, with their data in the Zero2Dev folder.",
    kinds: ["mariadb", "mysql", "postgres", "redis"],
  },
  {
    id: "editors",
    title: "Code editors",
    text: "Open a project folder from its service page. Portable: settings stay in the Zero2Dev folder.",
    kinds: ["vscode", "vscodium", "notepadpp"],
  },
  {
    id: "dbtools",
    title: "Database tools",
    text: "Open a database service with the connection filled in.",
    kinds: ["heidisql", "dbeaver"],
  },
];

/** Groups whose kinds can be services (editors and database tools are opened, not run as services). */
export const SERVICE_GROUP_IDS: RuntimeGroup["id"][] = ["languages", "servers", "databases"];

/** Desktop programs that are opened (launched), not supervised. */
export function isLaunchable(kind: string): boolean {
  return RUNTIME_GROUPS.some(
    (g) => (g.id === "editors" || g.id === "dbtools") && g.kinds.includes(kind as RuntimeKind),
  );
}

/** Database server kinds. */
export function isDatabase(kind: string): boolean {
  return RUNTIME_GROUPS.some((g) => g.id === "databases" && g.kinds.includes(kind as RuntimeKind));
}

/** Kinds that get a card (and a version list), in display order. */
export const CARD_KINDS: RuntimeKind[] = RUNTIME_GROUPS.flatMap((g) => g.kinds);

/** Compares dotted versions numerically ("8.10.1" > "8.9.9"); non-numeric parts compare as text. */
export function compareVersions(a: string, b: string): number {
  const pa = a.split(/[.+_-]/);
  const pb = b.split(/[.+_-]/);
  for (let i = 0; i < Math.max(pa.length, pb.length); i++) {
    const x = pa[i] ?? "0";
    const y = pb[i] ?? "0";
    const nx = /^\d+$/.test(x);
    const ny = /^\d+$/.test(y);
    if (nx && ny) {
      const d = Number(x) - Number(y);
      if (d !== 0) return d;
    } else if (x !== y) {
      return x < y ? -1 : 1;
    }
  }
  return 0;
}

/** Newest first. */
export function sortNewestFirst(entries: CatalogEntry[]): CatalogEntry[] {
  return [...entries].sort((a, b) => compareVersions(b.version, a.version));
}

export function entriesForKind(catalog: CatalogEntry[], kind: RuntimeKind): CatalogEntry[] {
  return catalog.filter((c) => c.kind === kind);
}

/** Catalog entries for this machine (`supported`), newest first. */
export function supportedForKind(catalog: CatalogEntry[], kind: RuntimeKind): CatalogEntry[] {
  return sortNewestFirst(entriesForKind(catalog, kind).filter((c) => c.supported));
}

/** Every runtime of a kind (installed by Zero2Dev and registered by path). Newest version first, default pinned on top. */
export function runtimesOfKind(runtimes: Runtime[], kind: RuntimeKind): Runtime[] {
  return runtimes
    .filter((r) => r.kind === kind)
    .sort((a, b) => Number(!!b.default) - Number(!!a.default) || compareVersions(b.version || "0", a.version || "0"));
}

export function defaultRuntime(runtimes: Runtime[], kind: RuntimeKind): Runtime | undefined {
  const list = runtimes.filter((r) => r.kind === kind);
  return list.find((r) => r.default) ?? undefined;
}

/** Make default is offered for a non-default runtime when the kind has more than one. */
export function canMakeDefault(r: Runtime, all: Runtime[]): boolean {
  return !r.default && all.filter((x) => x.kind === r.kind).length > 1;
}

export function sourceLabel(r: Runtime): string {
  return r.source === "installed" ? "installed" : "registered by path";
}

/** A catalog entry counts as installed when the daemon says so or a runtime came from it. */
export function isEntryInstalled(entry: CatalogEntry, runtimes: Runtime[]): boolean {
  return entry.installed || runtimes.some((r) => !!r.catalogId && r.catalogId === entry.id);
}

/** Tags shown after the version in the picker: latest, channel, installed, unverified or MD5. */
export function entryTags(c: CatalogEntry, runtimes: Runtime[]): string[] {
  const tags: string[] = [];
  if (c.latest) tags.push("latest");
  if (c.channel === "lts") tags.push("LTS");
  else if (c.channel) tags.push(c.channel);
  if (isEntryInstalled(c, runtimes)) tags.push("installed");
  if (c.unverified) tags.push("unverified");
  else if (c.checksum?.type === "md5") tags.push("MD5");
  return tags;
}

export function optionLabel(c: CatalogEntry, runtimes: Runtime[]): string {
  const tags = entryTags(c, runtimes);
  return tags.length ? `${c.version} (${tags.join(", ")})` : c.version;
}

export interface OptionGroup {
  /** "" when entries have no line. */
  label: string;
  entries: CatalogEntry[];
}

/** Grouped by version line, newest line first; entries inside are newest first. */
export function optionGroups(entries: CatalogEntry[], kind: RuntimeKind): OptionGroup[] {
  const lines = new Map<string, CatalogEntry[]>();
  for (const e of sortNewestFirst(entries)) lines.set(e.line, [...(lines.get(e.line) ?? []), e]);
  return [...lines.entries()]
    .sort((a, b) => compareVersions(b[0] || "0", a[0] || "0"))
    .map(([line, list]) => ({ label: line ? `${kindLabel(kind)} ${line}` : "", entries: list }));
}

/**
 * Updates to offer: for each runtime that came from the catalog, an entry with the same kind and line that is
 * `latest`, supported and not installed. De-duplicated, newest first. Nothing is ever removed automatically.
 */
export function updatesAvailable(catalog: CatalogEntry[], runtimes: Runtime[]): CatalogEntry[] {
  const found = new Map<string, CatalogEntry>();
  for (const r of runtimes) {
    if (!r.catalogId) continue;
    const cur = catalog.find((c) => c.id === r.catalogId);
    if (!cur) continue;
    const next = catalog.find(
      (c) => c.kind === cur.kind && c.line === cur.line && c.latest && c.supported && !isEntryInstalled(c, runtimes),
    );
    if (next) found.set(next.id, next);
  }
  return sortNewestFirst([...found.values()]);
}

/** Before the user picks: the newest version that is not installed yet, else the newest. A valid choice is kept. */
export function defaultSelection(entries: CatalogEntry[], runtimes: Runtime[], previous?: string): string {
  if (previous && entries.some((e) => e.id === previous)) return previous;
  const sorted = sortNewestFirst(entries);
  return (sorted.find((e) => !isEntryInstalled(e, runtimes)) ?? sorted[0])?.id ?? "";
}

export type Availability = "check" | "unsupported" | "ok";
/** "check": no entries yet, run Check for versions. "unsupported": entries exist but none for this machine. */
export function availability(catalog: CatalogEntry[], kind: RuntimeKind): Availability {
  const all = entriesForKind(catalog, kind);
  if (all.length === 0) return "check";
  return all.some((c) => c.supported) ? "ok" : "unsupported";
}

export function runtimeTitle(r: Runtime): string {
  return `${kindLabel(r.kind)} ${r.version}`.trim();
}

/** Label in the service form's Runtime select: "PHP 7.1.33", with "(default)" on the default. */
export function runtimeOptionLabel(r: Runtime): string {
  return r.default ? `${runtimeTitle(r)} (default)` : runtimeTitle(r);
}

/** The empty option in the service form: names the current default, or says PATH is used. */
export function defaultOptionLabel(runtimes: Runtime[], kind: RuntimeKind): string {
  const d = defaultRuntime(runtimes, kind);
  return d ? `Default (${runtimeTitle(d)})` : "Default (from PATH)";
}

export const UNVERIFIED_MESSAGE =
  "The publisher does not publish a checksum for this download. It will be downloaded over HTTPS from the publisher's site and its SHA-256 recorded in the installer log, but it cannot be verified. Install anyway?";

export function noDownloadText(os: string, arch: string): string {
  return `No download for ${os}/${arch}. Use Detect on this PC or Register by path.`;
}
export const CHECK_TEXT = "Check for versions to list what is available.";

/** One short line per failing source, e.g. "Java: could not reach api.adoptium.net". */
export function sourceErrors(sources: CatalogSource[]): string[] {
  return sources
    .filter((s) => !s.ok || s.error)
    .map((s) => `${kindLabel(s.kind)}: ${s.error || `could not reach ${s.name}`}`);
}

export function addedMessage(added: string[] | undefined): string {
  const n = added?.length ?? 0;
  return n === 0 ? "Already up to date" : n === 1 ? "1 new version found" : `${n} new versions found`;
}

/** Small label next to a service's kind, e.g. "PHP 7.1.33"; empty when the service uses the default or the runtime is unknown. */
export function serviceRuntimeLabel(runtimeId: string | undefined, runtimes: Runtime[]): string {
  if (!runtimeId) return "";
  const r = runtimes.find((x) => x.id === runtimeId);
  return r ? runtimeTitle(r) : "";
}
