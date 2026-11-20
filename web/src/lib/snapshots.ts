// Pure helpers for snapshots (0.0.5, snapshot-ui): sizes, summaries, the projects that follow a
// runtime kind's default, the domain preview of a clone, and whether a restore can go ahead.
import { kindLabel } from "./catalog";
import { serviceRuntime } from "./ux";
import {
  DATABASE_KINDS,
  type CatalogEntry,
  type MissingRuntime,
  type Project,
  type Runtime,
  type RuntimeRef,
  type Service,
  type Site,
  type Snapshot,
} from "./types";

/** "0 B", "512 B", "1.5 KB", "70 MB", "1.2 GB". */
export function fmtBytes(n: number): string {
  if (!Number.isFinite(n) || n <= 0) return "0 B";
  const units = ["B", "KB", "MB", "GB", "TB"];
  let i = 0;
  let v = n;
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i++;
  }
  const digits = i === 0 || v >= 10 ? 0 : 1;
  return `${v.toFixed(digits)} ${units[i]}`;
}

export const isDatabaseKind = (kind: string): boolean => (DATABASE_KINDS as string[]).includes(kind);

/** A service is in the way of a snapshot or a restore while it is not stopped (or failed). */
export const isBusyState = (s: Pick<Service, "state">): boolean => s.state !== "stopped" && s.state !== "failed";

/** "Node.js 26.13.1" for a runtime the snapshot recorded. */
export function runtimeRefLabel(r: Pick<RuntimeRef, "kind" | "version">): string {
  return `${kindLabel(r.kind)} ${r.version}`.trim();
}

/** The runtime versions a snapshot recorded, once each, sorted: ["MariaDB 11.4.4", "PHP 8.4.1"]. */
export function snapshotRuntimes(s: Pick<Snapshot, "services">): string[] {
  const out = new Set<string>();
  for (const x of s.services) if (x.runtime) out.add(runtimeRefLabel(x.runtime));
  return [...out].sort((a, b) => a.localeCompare(b));
}

/** The names of the services a snapshot holds, in its order. */
export function snapshotServiceNames(s: Pick<Snapshot, "services">): string[] {
  return s.services.map((x) => x.service.name);
}

/** "2 services, 1 site" (a service snapshot: "1 service"). */
export function snapshotCounts(s: Pick<Snapshot, "services" | "sites">): string {
  const n = s.services.length;
  const m = s.sites.length;
  const parts = [`${n} service${n === 1 ? "" : "s"}`];
  if (m) parts.push(`${m} site${m === 1 ? "" : "s"}`);
  return parts.join(", ");
}

/** "A, B and C". */
export function joinNames(names: string[]): string {
  if (names.length <= 1) return names[0] ?? "";
  return `${names.slice(0, -1).join(", ")} and ${names[names.length - 1]}`;
}

/** A name for a snapshot, 1-64 characters. "" when it is fine. */
export function validateSnapshotName(name: string): string {
  const n = name.trim();
  if (!n) return "Give the snapshot a name.";
  if (n.length > 64) return "Use at most 64 characters.";
  return "";
}

/** The name suggested for a new snapshot: "Snapshot 2026-11-18 09:30" (local time). */
export function defaultSnapshotName(d: Date = new Date()): string {
  const p = (x: number) => String(x).padStart(2, "0");
  return `Snapshot ${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`;
}

/** "before importing shop.sql" -> "Before importing shop.sql", cut to 64 characters. */
export function nameFromReason(reason: string): string {
  const r = reason.trim();
  if (!r) return defaultSnapshotName();
  const n = r[0].toUpperCase() + r.slice(1);
  return n.length > 64 ? n.slice(0, 63).trimEnd() + "…" : n;
}

/** The services of a project, or just the service, that a snapshot of the owner holds. */
export function ownerServices(
  scope: "project" | "service",
  ownerId: string,
  services: Service[],
  projects: Project[],
): Service[] {
  if (scope === "service") return services.filter((s) => s.id === ownerId);
  const p = projects.find((x) => x.id === ownerId);
  if (!p) return [];
  return p.serviceIds.map((id) => services.find((s) => s.id === id)).filter((s): s is Service => !!s);
}

// ---- Make default ----

export interface DefaultFollowers {
  /** The projects with at least one service that follows the default, and those services. */
  projects: { project: Project; services: Service[] }[];
  /** Services that follow the default and are in no project. */
  loose: Service[];
  /** Every service that follows the default. */
  services: Service[];
}

/** The services of `kind` with no version of their own (runtimeId ""), grouped by project. */
export function defaultFollowers(kind: string, services: Service[], projects: Project[]): DefaultFollowers {
  const following = services.filter((s) => s.kind === kind && !s.runtimeId);
  const ids = new Set(following.map((s) => s.id));
  const inProject = new Set<string>();
  const groups: DefaultFollowers["projects"] = [];
  for (const project of projects) {
    const members = project.serviceIds
      .filter((id) => ids.has(id))
      .map((id) => following.find((s) => s.id === id) as Service);
    if (members.length) {
      groups.push({ project, services: members });
      for (const m of members) inProject.add(m.id);
    }
  }
  return { projects: groups, loose: following.filter((s) => !inProject.has(s.id)), services: following };
}

// ---- clone ----

/** shop.localhost + "-copy" -> shop-copy.localhost, then shop-copy-2.localhost while it is taken. */
export function suffixDomain(domain: string, suffix: string, taken: Iterable<string>): string {
  const used = new Set([...taken].map((d) => d.toLowerCase()));
  const dot = domain.indexOf(".");
  const first = dot < 0 ? domain : domain.slice(0, dot);
  const rest = dot < 0 ? "" : domain.slice(dot);
  const base = first + suffix;
  let candidate = base + rest;
  for (let n = 2; used.has(candidate.toLowerCase()); n++) candidate = `${base}-${n}${rest}`;
  return candidate;
}

/** The domains a clone gets, in the snapshot's site order, each unique among the others and the new ones. */
export function clonePreview(
  sites: Pick<Site, "domain">[],
  suffix: string,
  existing: string[],
): { from: string; to: string }[] {
  const taken = [...existing];
  return sites.map((s) => {
    const to = suffixDomain(s.domain, suffix, taken);
    taken.push(to);
    return { from: s.domain, to };
  });
}

/** A domain suffix such as "-copy": letters, digits and "-", 1-30 characters. "" when it is fine. */
export function validateDomainSuffix(suffix: string): string {
  if (!suffix) return "Add something, such as -copy, so the domains differ.";
  if (!/^[A-Za-z0-9-]{1,30}$/.test(suffix)) return "Use letters, digits and - only (at most 30).";
  return "";
}

/** The code folders the clone shares with the original project. */
export function sharedFolders(s: Pick<Snapshot, "services">): string[] {
  return [...new Set(s.services.map((x) => x.service.cwd).filter(Boolean))];
}

// ---- restore ----

/** What a restore changes, one plain line each. */
export function restoreChanges(
  snap: Pick<Snapshot, "scope" | "services" | "sites" | "project">,
  services: Service[],
  sites: Site[],
  runtimes: Runtime[],
  currentProject?: Project,
): string[] {
  const out: string[] = [];
  for (const x of snap.services) {
    const now = services.find((s) => s.id === x.service.id);
    const what = x.hasData ? "settings and data" : "settings";
    if (!now) out.push(`${x.service.name} comes back (it was deleted), with its ${what}.`);
    else
      out.push(
        `${x.service.name} gets its ${what} back${now.name !== x.service.name ? ` (now called ${now.name})` : ""}.`,
      );
    if (x.runtime) {
      const cur = now ? serviceRuntime(now, runtimes) : undefined;
      const want = runtimeRefLabel(x.runtime);
      if (!cur) out.push(`${x.service.name} runs ${want} again.`);
      else if (cur.version !== x.runtime.version || cur.kind !== x.runtime.kind)
        out.push(`${x.service.name} runs ${want} again (now ${kindLabel(cur.kind)} ${cur.version}).`);
      else out.push(`${x.service.name} keeps ${want}, pinned to that version.`);
    }
    if (x.phpIni) out.push(`${x.service.name} gets its php.ini back.`);
  }
  for (const site of snap.sites) {
    const now = sites.find((s) => s.id === site.id);
    if (!now) out.push(`The site ${site.domain} comes back.`);
    else if (now.domain !== site.domain) out.push(`The site ${now.domain} is ${site.domain} again.`);
  }
  if (snap.scope === "project" && snap.project && currentProject) {
    const added = currentProject.serviceIds.filter((id) => !snap.project?.serviceIds.includes(id));
    const names = added.map((id) => services.find((s) => s.id === id)?.name ?? id);
    if (names.length)
      out.push(
        `${joinNames(names)} ${names.length === 1 ? "was" : "were"} added since; taken out of the project, not deleted.`,
      );
    const addedSites = currentProject.siteIds.filter((id) => !snap.project?.siteIds.includes(id));
    const siteNames = addedSites.map((id) => sites.find((s) => s.id === id)?.domain ?? id);
    if (siteNames.length)
      out.push(
        `The site${siteNames.length === 1 ? "" : "s"} ${joinNames(siteNames)}: taken out of the project, not deleted.`,
      );
  } else if (snap.scope === "project" && snap.project && !currentProject) {
    out.push(`The project ${snap.project.name} comes back (it was deleted).`);
  }
  return out;
}

export interface RestoreReadiness {
  /** Runtimes to install first. */
  missing: MissingRuntime[];
  /** Services of the snapshot that are not stopped now. */
  running: Service[];
  ready: boolean;
  /** Why not, in plain words ("" when ready). */
  reason: string;
}

/** Restore is possible when no runtime is missing and nothing runs (or the user lets Zero2Dev restart). */
export function restoreReadiness(
  snap: Pick<Snapshot, "services">,
  missing: MissingRuntime[],
  services: Service[],
  restart: boolean,
): RestoreReadiness {
  const ids = new Set(snap.services.map((x) => x.service.id));
  const running = services.filter((s) => ids.has(s.id) && isBusyState(s));
  let reason = "";
  if (missing.length) reason = `Install ${joinNames(missing.map(runtimeRefLabel))} first.`;
  else if (running.length && !restart) reason = `${joinNames(running.map((s) => s.name))} must stop first.`;
  return { missing, running, ready: !reason, reason };
}

/** The catalog entry that installs a missing runtime, when this PC can install it. */
export function catalogEntryFor(m: MissingRuntime, catalog: CatalogEntry[]): CatalogEntry | undefined {
  if (!m.installable) return undefined;
  return (
    catalog.find((c) => c.supported && !!m.catalogId && c.id === m.catalogId) ??
    catalog.find((c) => c.supported && c.kind === m.kind && c.version === m.version)
  );
}
