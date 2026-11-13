// Pure helpers behind the simplified UI: the getting-started checklist, friendly service
// kinds, the readable command line and the "points to" choice of the site form.
import { isDatabase, kindLabel } from "./catalog";
import type { Runtime, Service, Site, SiteType } from "./types";

// ---- getting started ----

export interface SetupStep {
  id: "runtime" | "service" | "site";
  title: string;
  text: string;
  done: boolean;
  href: string;
  action: string;
  optional?: boolean;
}

/** The three steps a new user goes through, in order. `next` is the first one not done. */
export function setupSteps(counts: { runtimes: number; services: number; sites: number }): {
  steps: SetupStep[];
  next: SetupStep | undefined;
  complete: boolean;
} {
  const steps: SetupStep[] = [
    {
      id: "runtime",
      title: "Install a runtime",
      text: "Node.js, PHP, Go, Java, Caddy or nginx. Or let Zero2Dev find the ones already on this PC.",
      done: counts.runtimes > 0,
      href: "#/runtimes",
      action: "Open runtimes",
    },
    {
      id: "service",
      title: "Add your app as a service",
      text: "Point Zero2Dev at your project folder. It starts the app, checks its port and keeps the logs.",
      done: counts.services > 0,
      href: "#/services/new",
      action: "Add a service",
    },
    {
      id: "site",
      title: "Give it a local domain",
      text: "Open your app at an address like shop.localhost instead of a port number.",
      done: counts.sites > 0,
      href: "#/sites?new",
      action: "Add a site",
      optional: true,
    },
  ];
  const next = steps.find((s) => !s.done);
  return { steps, next, complete: !next };
}

// ---- services ----

/** Label of a service kind in the form and lists: "Node.js", "Custom command". */
export function serviceKindLabel(k: string): string {
  return k === "custom" ? "Custom command" : kindLabel(k);
}

/** The runtime a service would use: its own pick, else the default (or first) of its kind. */
export function serviceRuntime(s: Pick<Service, "kind" | "runtimeId">, runtimes: Runtime[]): Runtime | undefined {
  if (s.runtimeId) return runtimes.find((r) => r.id === s.runtimeId);
  const ofKind = runtimes.filter((r) => r.kind === s.kind);
  return ofKind.find((r) => r.default) ?? ofKind[0];
}

// ---- sites ----

/** What a site points at, in the words of the form. */
export type SiteTarget = "service" | "port" | "folder";

export function siteTargetOf(s: Pick<Site, "type" | "serviceId">): SiteTarget {
  if (s.type === "static") return "folder";
  return s.serviceId ? "service" : "port";
}

export const SITE_TARGET_LABEL: Record<SiteTarget, string> = {
  service: "A service",
  port: "A port",
  folder: "A folder",
};

/** The API type for a target. A PHP service keeps the dedicated php type. */
export function siteTypeFor(target: SiteTarget, serviceKind?: string): SiteType {
  if (target === "folder") return "static";
  return target === "service" && serviceKind === "php" ? "php" : "proxy";
}

/** Domain suggested from a site name: "My Shop!" -> "my-shop.localhost". Empty when nothing usable is left. */
export function suggestDomain(name: string, suffix: string): string {
  const label = name
    .toLowerCase()
    .normalize("NFKD")
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "")
    .slice(0, 63)
    .replace(/-+$/, "");
  return label ? `${label}.${suffix}` : "";
}

// ---- editors and database tools ----

const EDITOR_KINDS = ["vscode", "vscodium", "notepadpp"];
const DBTOOL_KINDS = ["heidisql", "dbeaver"];

/** One runtime per kind (its default, else the first), in a fixed kind order. */
function onePerKind(runtimes: Runtime[], kinds: string[]): Runtime[] {
  const out: Runtime[] = [];
  for (const k of kinds) {
    const of = runtimes.filter((r) => r.kind === k);
    const pick = of.find((r) => r.default) ?? of[0];
    if (pick) out.push(pick);
  }
  return out;
}

/** Installed code editors to offer "Open in" for a project folder. */
export function editorsFor(runtimes: Runtime[]): Runtime[] {
  return onePerKind(runtimes, EDITOR_KINDS);
}

/** Installed database tools that can open a service of this kind (none for Redis). */
export function dbToolsFor(runtimes: Runtime[], serviceKind: string): Runtime[] {
  if (!isDatabase(serviceKind) || serviceKind === "redis") return [];
  return onePerKind(runtimes, DBTOOL_KINDS);
}

// ---- databases ----

/** The port a new database service gets (the usual one for each server). */
export interface ConnectionInfo {
  host: string;
  port: number;
  user: string;
  password: string;
  url: string;
}

/** How to connect to a database service Zero2Dev set up (local only, no password). */
export function connectionInfo(s: Pick<Service, "kind" | "port">): ConnectionInfo | null {
  if (!isDatabase(s.kind) || s.port <= 0) return null;
  const host = "127.0.0.1";
  switch (s.kind) {
    case "mariadb":
    case "mysql":
      return { host, port: s.port, user: "root", password: "none", url: `mysql://root@${host}:${s.port}` };
    case "postgres":
      return {
        host,
        port: s.port,
        user: "postgres",
        password: "none",
        url: `postgresql://postgres@${host}:${s.port}/postgres`,
      };
    default:
      return { host, port: s.port, user: "", password: "none", url: `redis://${host}:${s.port}` };
  }
}

/** The delete confirmation text; database services keep their data folder. */
export function deleteServiceMessage(s: Pick<Service, "kind" | "id">, running: boolean, dataDir: string): string {
  const what = running
    ? "This service is running. Zero2Dev will stop it, then delete it and its log file."
    : "This deletes the service and its log file.";
  if (isDatabase(s.kind))
    return `${what} The database files are kept in ${dataDir}; delete that folder yourself if you no longer need them.`;
  return `${what} This cannot be undone.`;
}

// ---- remembered view choices ----

export type RuntimeView = "list" | "grid";
const VIEW_KEY = "zero2dev.runtimes.view";

/** The Runtimes page layout the user picked last (list when none, or storage is unavailable). */
export function getRuntimeView(storage?: Pick<Storage, "getItem">): RuntimeView {
  try {
    return (storage ?? globalThis.localStorage)?.getItem(VIEW_KEY) === "grid" ? "grid" : "list";
  } catch {
    return "list";
  }
}

export function setRuntimeView(v: RuntimeView, storage?: Pick<Storage, "setItem">): void {
  try {
    (storage ?? globalThis.localStorage)?.setItem(VIEW_KEY, v);
  } catch {
    /* private mode: the choice lasts for this page only */
  }
}
