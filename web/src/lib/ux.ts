// Pure helpers behind the simplified UI: the getting-started checklist, friendly service
// kinds, the readable command line and the "points to" choice of the site form.
import { isDatabase, kindLabel } from "./catalog";
import type { Runtime, Service, ServiceKind, Site, SiteType } from "./types";

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
      href: "#/services?new",
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

/** Program names looked up on PATH when no runtime is registered (supervisor progName). */
const PATH_NAMES: Record<string, string> = { mariadb: "mariadbd", mysql: "mysqld", redis: "redis-server" };

/** The arguments the daemon gives a database service with none (supervisor defaultDBArgs). */
export function defaultDBArgs(kind: string, data: string, windows = true): string[] {
  switch (kind) {
    case "mariadb":
    case "mysql": {
      const a = ["--no-defaults", `--datadir=${data}`, "--port={port}", "--bind-address=127.0.0.1"];
      if (windows) a.push("--console");
      if (kind === "mysql") a.push("--mysqlx=OFF");
      return a;
    }
    case "postgres":
      return ["-D", data, "-p", "{port}", "-c", "listen_addresses=127.0.0.1"];
    case "redis":
      return ["--port", "{port}", "--bind", "127.0.0.1", "--dir", data.replaceAll("\\", "/")];
  }
  return [];
}

/** Kinds that cannot run without arguments (there is no sensible default). */
export const needsArgs = (k: ServiceKind): boolean => k === "node" || k === "java";

/** What runs when the arguments are left empty, or "" when they are required. */
export function defaultArgsText(k: ServiceKind): string {
  switch (k) {
    case "php":
      return "Leave empty to run PHP's built-in web server on the port, serving the project folder.";
    case "go":
      return "Leave empty to run go run . in the project folder.";
    case "caddy":
      return "Leave empty to run Caddy with the sites Zero2Dev generates.";
    case "nginx":
      return "Leave empty to run nginx with its own configuration.";
    case "node":
      return "The script to run, for example server.js. Use {port} to pass the port.";
    case "java":
      return "For example -jar app.jar. Use {port} to pass the port.";
    case "mariadb":
    case "mysql":
    case "postgres":
    case "redis":
      return "Leave empty: Zero2Dev keeps the data in its own folder, listens on 127.0.0.1 only and creates the database files on the first start.";
    default:
      return "One value per row. {port} is replaced with the port.";
  }
}

function baseName(p: string): string {
  return p.split(/[\\/]/).pop() || p;
}

/** The runtime a service would use: its own pick, else the default (or first) of its kind. */
export function serviceRuntime(s: Pick<Service, "kind" | "runtimeId">, runtimes: Runtime[]): Runtime | undefined {
  if (s.runtimeId) return runtimes.find((r) => r.id === s.runtimeId);
  const ofKind = runtimes.filter((r) => r.kind === s.kind);
  return ofKind.find((r) => r.default) ?? ofKind[0];
}

/**
 * The command line as the user would type it, e.g. `node.exe server.js --port 3000`.
 * Placeholders are filled in; the program is the command, else the runtime binary, else the kind's name on PATH.
 */
export function commandLine(
  s: Service,
  runtimes: Runtime[],
  opts: { dataDir?: string; windows?: boolean } = {},
): string {
  let prog = s.command.trim();
  if (!prog) {
    const rt = s.kind === "custom" ? undefined : serviceRuntime(s, runtimes);
    prog = rt ? baseName(rt.binary) : (PATH_NAMES[s.kind] ?? s.kind);
  }
  let args = s.args;
  if (args.length === 0) {
    if (s.kind === "php") args = ["-S", "127.0.0.1:{port}", "-t", s.cwd];
    else if (s.kind === "go") args = ["run", "."];
    else if (isDatabase(s.kind)) args = defaultDBArgs(s.kind, opts.dataDir ?? "<data folder>", opts.windows ?? true);
  }
  const port = String(s.port);
  const quote = (a: string) => (/\s/.test(a) ? `"${a}"` : a);
  return [prog, ...args.map((a) => quote(a.replaceAll("{port}", port)))].join(" ");
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
export const DB_PORTS: Record<string, number> = { mariadb: 3306, mysql: 3306, postgres: 5432, redis: 6379 };

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
