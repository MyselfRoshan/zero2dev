// Pure helpers for the database tools on a service page (0.0.5): name checks that mirror the
// daemon's rules (docs/API.md, the 0.0.5 amendment, "Databases"), file filters, labels, job text,
// and which action is allowed in which state, with the reason when it is not.
import { ApiError } from "./api";
import type { DatabaseInfo, DbDatabase, FileFilter, Job, Service, ServiceKind } from "./types";

/** The kinds the database routes accept. */
export const DB_SERVICE_KINDS: ServiceKind[] = ["mariadb", "mysql", "postgres", "redis"];
export function isDbService(kind: string): boolean {
  return (DB_SERVICE_KINDS as string[]).includes(kind);
}

/** System databases: not created, imported into, exported or dropped through the routes. */
export const SYSTEM_DATABASES = [
  "mysql",
  "information_schema",
  "performance_schema",
  "sys",
  "postgres",
  "template0",
  "template1",
];
export function isSystemDatabase(name: string): boolean {
  return SYSTEM_DATABASES.includes(name.trim().toLowerCase());
}

const NAME_RE = /^[A-Za-z0-9_]{1,64}$/;

/** "" when the name is fine, else the message to show (mirrors the daemon's 400). */
export function databaseNameError(name: string): string {
  const n = name.trim();
  if (!n) return "Enter a database name.";
  if (!NAME_RE.test(n)) return "Database name must be 1-64 letters, digits or _.";
  if (isSystemDatabase(n)) return `${n} is a system database. Pick another name.`;
  return "";
}

export function userNameError(name: string): string {
  const n = name.trim();
  if (!n) return "Enter a user name.";
  if (!NAME_RE.test(n)) return "User name must be 1-64 letters, digits or _.";
  return "";
}

/** A password for a new user or the administrator: required, one line. */
export function passwordError(pw: string): string {
  if (!pw) return "Enter a password.";
  if (/[\r\n]/.test(pw)) return "The password cannot contain a line break.";
  if (pw.length > 128) return "Use at most 128 characters.";
  return "";
}

function isAbsolute(p: string): boolean {
  return /^[A-Za-z]:[\\/]/.test(p) || /^\\\\[^\\]+\\[^\\]+/.test(p) || p.startsWith("/");
}

/** "" when the dump path is usable. */
export function dumpPathError(path: string, example = "C:\\dumps\\shop.sql"): string {
  const p = path.trim();
  if (!p) return "Enter the path of the file, or use Browse.";
  if (!isAbsolute(p)) return `Use an absolute path, for example ${example}.`;
  if (/[\\/]$/.test(p)) return "This is a folder. Add the file name.";
  return "";
}

/** File filters for the native dialog. */
export const DUMP_FILTERS: FileFilter[] = [
  { name: "SQL dumps (*.sql, *.sql.gz)", pattern: "*.sql;*.sql.gz;*.gz" },
  { name: "All files", pattern: "*.*" },
];

/** The folder of a path ("" when it has none), for the dialog's start folder. */
export function dirOf(path: string): string {
  const p = path.trim();
  const i = Math.max(p.lastIndexOf("\\"), p.lastIndexOf("/"));
  return i > 0 ? p.slice(0, i) : "";
}
/** The file name of a path. */
export function baseName(path: string): string {
  const p = path.trim();
  const i = Math.max(p.lastIndexOf("\\"), p.lastIndexOf("/"));
  return i >= 0 ? p.slice(i + 1) : p;
}

/** The suggested file name of an export: shop-2026-11-18.sql. */
export function exportFileName(database: string, gzip = false, now = new Date()): string {
  const pad = (n: number) => String(n).padStart(2, "0");
  const day = `${now.getFullYear()}-${pad(now.getMonth() + 1)}-${pad(now.getDate())}`;
  return `${database}-${day}.sql${gzip ? ".gz" : ""}`;
}

export function isGzipPath(path: string): boolean {
  return /\.gz$/i.test(path.trim());
}

/** Sizes as the list shows them; 0 means the server did not say. */
export function fmtSize(bytes: number): string {
  if (!bytes || bytes < 0) return "-";
  const units = ["B", "KB", "MB", "GB", "TB"];
  let v = bytes;
  let u = 0;
  while (v >= 1024 && u < units.length - 1) {
    v /= 1024;
    u++;
  }
  return u === 0 ? `${v} B` : `${v < 10 ? v.toFixed(1) : Math.round(v)} ${units[u]}`;
}

/** The databases the tools work on: the system ones are left out. */
export function userDatabases(info: Pick<DatabaseInfo, "databases"> | null | undefined): DbDatabase[] {
  return (info?.databases ?? []).filter((d) => !isSystemDatabase(d.name)).sort((a, b) => a.name.localeCompare(b.name));
}

/** "root", "postgres", or a description for Redis (which has no user name). */
export function adminLabel(kind: string, user = ""): string {
  if (user) return user;
  if (kind === "postgres") return "postgres";
  if (kind === "redis") return "Redis";
  return "root";
}

/** What a kind supports when the daemon has not answered yet (it answers with `supports`). */
export function supportsFor(kind: string): DatabaseInfo["supports"] {
  if (!isDbService(kind)) return { databases: false, dumps: false, password: false, reset: false };
  if (kind === "redis") return { databases: false, dumps: false, password: true, reset: true };
  return { databases: true, dumps: true, password: true, reset: true };
}

export type DbAction = "create" | "user" | "import" | "export" | "password" | "reset";

export interface ActionState {
  allowed: boolean;
  /** Why it is not allowed ("" when it is). */
  reason: string;
}

const NEEDS_RUNNING: Record<Exclude<DbAction, "reset">, string> = {
  create: "a database is created inside the running server",
  user: "a user is created inside the running server",
  import: "the dump is read into the running server",
  export: "the dump is written by the running server",
  password: "the password is changed inside the running server",
};

/**
 * Whether an action can run now, and why not. `busy` is the label of a job working on the
 * service ("an import"), or "".
 */
export function actionState(
  action: DbAction,
  s: Pick<Service, "name" | "kind" | "state">,
  busy = "",
  supports: DatabaseInfo["supports"] = supportsFor(s.kind),
): ActionState {
  const no = (reason: string): ActionState => ({ allowed: false, reason });
  const supported =
    action === "create" || action === "user"
      ? supports.databases
      : action === "import" || action === "export"
        ? supports.dumps
        : action === "password"
          ? supports.password
          : supports.reset;
  if (!supported) return no(`${serviceKindName(s.kind)} does not support this.`);
  if (busy) return no(`Wait until ${busy} on ${s.name} has finished.`);
  if (s.state === "starting") return no(`Wait until ${s.name} has started.`);
  if (s.state === "stopping") return no(`Wait until ${s.name} has stopped.`);
  if (action === "reset") {
    if (s.state === "running")
      return no(`Stop ${s.name} first: its files can only be deleted while the server is stopped.`);
    return { allowed: true, reason: "" };
  }
  if (s.state !== "running") return no(`Start ${s.name} first: ${NEEDS_RUNNING[action]}.`);
  return { allowed: true, reason: "" };
}

export function serviceKindName(kind: string): string {
  switch (kind) {
    case "mariadb":
      return "MariaDB";
    case "mysql":
      return "MySQL";
    case "postgres":
      return "PostgreSQL";
    case "redis":
      return "Redis";
    default:
      return kind;
  }
}

/** The one line shown above the tools for a state, or "" when everything is available. */
export function stateNote(s: Pick<Service, "name" | "kind" | "state">): string {
  const redis = s.kind === "redis";
  switch (s.state) {
    case "running":
      return "";
    case "starting":
      return `${s.name} is starting. The tools are ready once it runs.`;
    case "stopping":
      return `${s.name} is stopping.`;
    default:
      return redis
        ? `${s.name} is not running. Start it to set or change its password. Reset works only while it is stopped.`
        : `${s.name} is not running. Start it to see its databases, create databases and users, import, export or change the password. Reset works only while it is stopped.`;
  }
}

/** Job types of the database tools. */
export type DbJobType = "db-import" | "db-export";

/** "an import" / "an export", for the busy reason. */
export function jobNoun(type: string): string {
  return type === "db-export" ? "an export" : type === "db-import" ? "an import" : "a job";
}

export interface DbJobRef {
  id: string;
  type: DbJobType;
  database: string;
  file: string;
}

/** The text next to the progress bar of an import or export. */
export function jobText(
  ref: Pick<DbJobRef, "type" | "database" | "file">,
  job: Pick<Job, "state" | "percent" | "step" | "error" | "path"> | undefined,
): string {
  const file = baseName(ref.file);
  const importing = ref.type === "db-import";
  if (!job || job.state === "running") {
    const pct = job?.percent ? ` ${Math.round(job.percent)}%` : "";
    const step = job?.step || (importing ? "Importing" : "Exporting");
    return importing ? `${step} ${file} into ${ref.database}...${pct}` : `${step} ${ref.database} to ${file}...${pct}`;
  }
  if (job.state === "done")
    return importing
      ? `Imported ${file} into ${ref.database}.`
      : `Exported ${ref.database} to ${job.path || ref.file}.`;
  const why = job.error || "unknown error";
  return importing
    ? `The import of ${file} into ${ref.database} failed: ${why}`
    : `The export of ${ref.database} failed: ${why}`;
}

/** An export answered 409 because the file is there (not "start first" or a running job). */
export function isFileExists(e: unknown): boolean {
  return e instanceof ApiError && e.status === 409 && !e.details?.jobId && /\bexists\b/i.test(e.message);
}

/** A random password for the Generate button: 20 letters and digits (no characters that need escaping). */
export function generatePassword(length = 20, rand: (n: number) => number = cryptoRandom): string {
  const chars = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789";
  let out = "";
  for (let i = 0; i < length; i++) out += chars[rand(chars.length)];
  return out;
}
function cryptoRandom(n: number): number {
  const a = new Uint32Array(1);
  globalThis.crypto.getRandomValues(a);
  return a[0] % n;
}

/** The exact text of the reset confirmation. */
export function resetMessage(s: Pick<Service, "name" | "kind">, dataDir: string, databases: string[]): string {
  const what =
    s.kind === "redis"
      ? "every key it stores and its password"
      : databases.length
        ? `every database (${databases.join(", ")}), every user and the administrator password`
        : "every database, every user and the administrator password";
  return `This deletes the data folder of ${s.name}${dataDir ? ` (${dataDir})` : ""}: ${what}. The next start sets the server up again, empty and without a password. This cannot be undone.`;
}
