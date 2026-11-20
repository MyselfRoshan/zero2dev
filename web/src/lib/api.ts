import type {
  DetectResult,
  Framework,
  NewProjectPlan,
  NewProjectRequest,
  TaskList,
  TaskRequest,
  CatalogEntry,
  CatalogRefresh,
  CatalogStatus,
  CommandPreview,
  InstallOptions,
  DetectedRuntime,
  EnvConfig,
  Environment,
  ErrorCode,
  FolderPick,
  Job,
  LogLine,
  Project,
  ProjectInput,
  PHPExtensions,
  Runtime,
  RuntimeKind,
  Service,
  StartAllResult,
  StopAllResult,
  ServiceInput,
  Settings,
  Site,
  SiteInput,
  SitesBulkResult,
  Status,
  CloneSnapshotInput,
  DatabaseInfo,
  DbDatabase,
  DbExportInput,
  DbImportInput,
  FilePickInput,
  MigrateDatabase,
  MigrateRequest,
  MigrateSource,
  Migration,
  RevertHow,
  Snapshot,
  SnapshotDetail,
  SnapshotScope,
  TakeSnapshotInput,
} from "./types";
import type { KindProfile } from "./kinds";

export const TOKEN_KEY = "zero2dev.token";

export class ApiError extends Error {
  readonly status: number;
  readonly code: ErrorCode;
  /** Optional `error.details` from the daemon (strings only). */
  readonly details: Record<string, string>;
  constructor(status: number, code: ErrorCode, message: string, details: Record<string, string> = {}) {
    super(message);
    this.details = details;
    this.name = "ApiError";
    this.status = status;
    this.code = code;
  }
  get isAuth(): boolean {
    return this.code === "unauthorized" || this.status === 401;
  }
  get isNetwork(): boolean {
    return this.code === "network";
  }
}

// ---- token handling (localStorage + URL fragment) ----

export function getToken(): string | null {
  try {
    return localStorage.getItem(TOKEN_KEY) || null;
  } catch {
    return null;
  }
}

export function setToken(token: string | null): void {
  try {
    if (token) localStorage.setItem(TOKEN_KEY, token);
    else localStorage.removeItem(TOKEN_KEY);
  } catch {
    /* storage unavailable */
  }
}

/**
 * Reads `#token=...` from the URL, stores it and strips it with history.replaceState.
 * Other fragment parameters (for example a route in `#/services&token=`) are not supported:
 * the daemon prints `/#token=<token>` only. Returns the token that was found, if any.
 */
export function consumeTokenFromHash(loc: Location = location, hist: History = history): string | null {
  const hash = loc.hash || "";
  const m = /^#token=([^&]+)/.exec(hash);
  if (!m) return null;
  let token: string;
  try {
    token = decodeURIComponent(m[1]);
  } catch {
    token = m[1];
  }
  setToken(token);
  hist.replaceState(null, "", loc.pathname + loc.search);
  return token;
}

// ---- transport ----

type Fetch = typeof fetch;
let fetchImpl: Fetch | null = null;
/** Test hook. */
export function setFetch(f: Fetch | null): void {
  fetchImpl = f;
}

let authFailureHandler: (() => void) | null = null;
export function onAuthFailure(h: (() => void) | null): void {
  authFailureHandler = h;
}

function errorCodeForStatus(status: number): ErrorCode {
  switch (status) {
    case 400:
      return "bad_request";
    case 401:
      return "unauthorized";
    case 404:
      return "not_found";
    case 409:
      return "conflict";
    case 403:
      return "forbidden_host";
    default:
      return status >= 500 ? "internal" : "unknown";
  }
}

export async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const headers: Record<string, string> = { Accept: "application/json" };
  const token = getToken();
  if (token) headers.Authorization = `Bearer ${token}`;
  const init: RequestInit = { method, headers };
  if (body !== undefined) {
    headers["Content-Type"] = "application/json";
    init.body = JSON.stringify(body);
  }
  let res: Response;
  try {
    res = await (fetchImpl ?? fetch)(path, init);
  } catch {
    throw new ApiError(0, "network", "Cannot reach the Zero2Dev daemon. Is it running?");
  }
  if (res.status === 204) return undefined as T;
  const text = await res.text();
  let data: unknown = undefined;
  if (text) {
    try {
      data = JSON.parse(text);
    } catch {
      data = undefined;
    }
  }
  if (!res.ok) {
    const e = (data as { error?: { code?: string; message?: string; details?: Record<string, string> } } | undefined)
      ?.error;
    const code = (e?.code as ErrorCode | undefined) ?? errorCodeForStatus(res.status);
    const err = new ApiError(res.status, code, e?.message || `Request failed (${res.status})`, e?.details);
    if (err.isAuth && authFailureHandler) authFailureHandler();
    throw err;
  }
  return data as T;
}

const enc = encodeURIComponent;

/** `?project=id` for the bulk routes, or nothing: without a project they act on everything. */
export function projectQuery(projectId?: string): string {
  return projectId ? `?project=${enc(projectId)}` : "";
}

/** Only fields listed in the contract are sent: the daemon rejects unknown fields. */
export function installBody(catalogId: string, opts: InstallOptions = {}): Record<string, unknown> {
  const b: Record<string, unknown> = { catalogId };
  if (opts.acceptUnverified) b.acceptUnverified = true;
  return b;
}

export function importBody(
  b: { catalogId: string; filePath: string; sha256?: string } & InstallOptions,
): Record<string, unknown> {
  const out: Record<string, unknown> = { catalogId: b.catalogId, filePath: b.filePath };
  if (b.sha256 && b.sha256.trim()) out.sha256 = b.sha256.trim();
  if (b.acceptUnverified) out.acceptUnverified = true;
  return out;
}

export const api = {
  status: () => request<Status>("GET", "/api/status"),

  listServices: () => request<Service[]>("GET", "/api/services"),
  getService: (id: string) => request<Service>("GET", `/api/services/${enc(id)}`),
  createService: (b: ServiceInput) => request<Service>("POST", "/api/services", b),
  updateService: (id: string, b: ServiceInput) => request<Service>("PUT", `/api/services/${enc(id)}`, b),
  deleteService: (id: string) => request<void>("DELETE", `/api/services/${enc(id)}`),
  startService: (id: string) => request<Service>("POST", `/api/services/${enc(id)}/start`),
  stopService: (id: string) => request<Service>("POST", `/api/services/${enc(id)}/stop`),
  restartService: (id: string) => request<Service>("POST", `/api/services/${enc(id)}/restart`),
  /** Starts every service that is not running (or those of one project), dependencies first. */
  startAllServices: (projectId?: string) =>
    request<StartAllResult>("POST", `/api/services/start-all${projectQuery(projectId)}`),
  /** Stops every running service (or those of one project), dependents first. */
  stopAllServices: (projectId?: string) =>
    request<StopAllResult>("POST", `/api/services/stop-all${projectQuery(projectId)}`),
  /** Opens a console in the service's folder with its environment (204; 501 where unsupported). */
  openTerminal: (id: string) => request<void>("POST", `/api/services/${enc(id)}/terminal`),

  listProjects: () => request<Project[]>("GET", "/api/projects"),
  createProject: (b: ProjectInput) => request<Project>("POST", "/api/projects", b),
  updateProject: (id: string, b: ProjectInput) => request<Project>("PUT", `/api/projects/${enc(id)}`, b),
  deleteProject: (id: string) => request<void>("DELETE", `/api/projects/${enc(id)}`),
  /** The folders and variables the user chose for the Windows user environment, and what is there now. */
  getEnvironment: () => request<Environment>("GET", "/api/environment"),
  /** Saves the whole config and applies it (409 with the reason when Windows refuses). */
  putEnvironment: (c: EnvConfig) => request<Environment>("PUT", "/api/environment", { path: c.path, vars: c.vars }),
  serviceCommand: (id: string) => request<CommandPreview>("GET", `/api/services/${enc(id)}/command`),
  previewService: (b: ServiceInput & { id?: string }) => request<CommandPreview>("POST", "/api/services/preview", b),
  kinds: () => request<KindProfile[]>("GET", "/api/kinds"),
  projectScripts: (dir: string) =>
    request<{ scripts: string[] }>("GET", `/api/project/scripts?dir=${encodeURIComponent(dir)}`),
  serviceLogs: (id: string, tail = 500) =>
    request<{ lines: LogLine[] }>("GET", `/api/services/${enc(id)}/logs?tail=${tail}`),

  listRuntimes: () => request<Runtime[]>("GET", "/api/runtimes"),
  detectRuntimes: () => request<DetectedRuntime[]>("GET", "/api/runtimes/detect"),
  registerRuntime: (b: { kind: RuntimeKind; binary: string; version?: string }) =>
    request<Runtime>("POST", "/api/runtimes/register", b),
  importRuntime: (b: { catalogId: string; filePath: string; sha256?: string } & InstallOptions) =>
    request<Job>("POST", "/api/runtimes/import", importBody(b)),
  installerLog: (tail = 200) => request<{ lines: string[] }>("GET", `/api/installer/log?tail=${tail}`),
  installRuntime: (catalogId: string, opts?: InstallOptions) =>
    request<Job>("POST", "/api/runtimes/install", installBody(catalogId, opts)),
  catalogStatus: () => request<CatalogStatus>("GET", "/api/catalog/status"),
  refreshCatalog: () => request<CatalogRefresh>("POST", "/api/catalog/refresh"),
  setDefaultRuntime: (id: string) => request<Runtime>("POST", `/api/runtimes/${enc(id)}/default`),
  /** Opens an editor (on `path`) or a database tool (on the database service `serviceId`). */
  launchRuntime: (id: string, b: { path?: string; serviceId?: string } = {}) =>
    request<void>("POST", `/api/runtimes/${enc(id)}/launch`, b),
  phpExtensions: (id: string) => request<PHPExtensions>("GET", `/api/runtimes/${enc(id)}/php-extensions`),
  installXdebug: (id: string) => request<Job>("POST", `/api/runtimes/${enc(id)}/xdebug`),
  deleteRuntime: (id: string) => request<void>("DELETE", `/api/runtimes/${enc(id)}`),
  catalog: () => request<CatalogEntry[]>("GET", "/api/catalog"),
  getJob: (id: string) => request<Job>("GET", `/api/jobs/${enc(id)}`),

  listSites: () => request<Site[]>("GET", "/api/sites"),
  createSite: (b: SiteInput) => request<Site>("POST", "/api/sites", b),
  updateSite: (id: string, b: SiteInput) => request<Site>("PUT", `/api/sites/${enc(id)}`, b),
  deleteSite: (id: string) => request<void>("DELETE", `/api/sites/${enc(id)}`),
  enableSite: (id: string) => request<Site>("POST", `/api/sites/${enc(id)}/enable`),
  disableSite: (id: string) => request<Site>("POST", `/api/sites/${enc(id)}/disable`),
  /** One config write and one site server restart for all sites (or those of one project). */
  enableAllSites: (projectId?: string) =>
    request<SitesBulkResult>("POST", `/api/sites/enable-all${projectQuery(projectId)}`),
  disableAllSites: (projectId?: string) =>
    request<SitesBulkResult>("POST", `/api/sites/disable-all${projectQuery(projectId)}`),

  getSettings: () => request<Settings>("GET", "/api/settings"),
  putSettings: (b: Settings) => request<Settings>("PUT", "/api/settings", b),
  /** Changes only the given fields (the daemon treats missing ones as unchanged). */
  patchSettings: (b: Partial<Settings>) => request<Settings>("PUT", "/api/settings", b),

  eventTicket: () => request<{ ticket: string }>("POST", "/api/events/ticket"),

  /** Stops every service and exits the daemon (Quit Zero2Dev). */
  shutdown: () => request<{ stopping: boolean }>("POST", "/api/shutdown"),

  /** Opens the native folder dialog on the daemon's PC; resolves when the user closes it. */
  pickFolder: (title: string, start: string) => request<FolderPick>("POST", "/api/dialog/folder", { title, start }),

  // ---- 0.0.5: databases, snapshots, Import (docs/API.md, the 0.0.5 amendment) ----

  /** The native Open / Save as dialog on the daemon's PC. */
  pickFile: (b: FilePickInput) => request<FolderPick>("POST", "/api/dialog/file", b),

  databaseInfo: (id: string) => request<DatabaseInfo>("GET", `/api/services/${enc(id)}/database`),
  createDatabase: (id: string, name: string) =>
    request<{ name: string }>("POST", `/api/services/${enc(id)}/database/databases`, { name }),
  createDatabaseUser: (id: string, b: { user: string; password: string; database?: string }) =>
    request<{ user: string; database: string }>("POST", `/api/services/${enc(id)}/database/users`, b),
  importDatabase: (id: string, b: DbImportInput) => request<Job>("POST", `/api/services/${enc(id)}/database/import`, b),
  exportDatabase: (id: string, b: DbExportInput) => request<Job>("POST", `/api/services/${enc(id)}/database/export`, b),
  /** Deletes the data folder (the service must be stopped); the next start sets it up again. */
  resetDatabase: (id: string) => request<Service>("POST", `/api/services/${enc(id)}/database/reset`),
  /** "" removes the password. The server must be running. */
  setDatabasePassword: (id: string, password: string) =>
    request<Service>("PUT", `/api/services/${enc(id)}/database/password`, { password }),

  listSnapshots: (scope: SnapshotScope, ownerId: string) =>
    request<Snapshot[]>("GET", `/api/${scope === "project" ? "projects" : "services"}/${enc(ownerId)}/snapshots`),
  takeSnapshot: (scope: SnapshotScope, ownerId: string, b: TakeSnapshotInput) =>
    request<Job>("POST", `/api/${scope === "project" ? "projects" : "services"}/${enc(ownerId)}/snapshots`, b),
  getSnapshot: (id: string) => request<SnapshotDetail>("GET", `/api/snapshots/${enc(id)}`),
  restoreSnapshot: (id: string, restart = false) =>
    request<Job>("POST", `/api/snapshots/${enc(id)}/restore`, { restart }),
  cloneSnapshot: (id: string, b: CloneSnapshotInput) => request<Job>("POST", `/api/snapshots/${enc(id)}/clone`, b),
  deleteSnapshot: (id: string) => request<void>("DELETE", `/api/snapshots/${enc(id)}`),

  migrationSources: () => request<MigrateSource[]>("GET", "/api/migrations/sources"),
  /** Lists the databases of another server, for the database source's form. */
  remoteDatabases: (b: Omit<MigrateDatabase, "databases" | "targetServiceId">) =>
    request<{ databases: DbDatabase[] }>("POST", "/api/migrations/databases", b),
  listMigrations: () => request<Migration[]>("GET", "/api/migrations"),
  startMigration: (b: MigrateRequest) => request<Job>("POST", "/api/migrations", b),
  revertMigration: (id: string, how: RevertHow) => request<Job>("POST", `/api/migrations/${enc(id)}/revert`, { how }),
  /** Removes the originals of a finished copy (it becomes a move). */
  cleanupMigration: (id: string) => request<Job>("POST", `/api/migrations/${enc(id)}/cleanup`),
  deleteMigrationBackups: (id: string) => request<Migration>("DELETE", `/api/migrations/${enc(id)}/backups`),
  /** Removes a reverted or failed migration from the history. */
  deleteMigration: (id: string) => request<void>("DELETE", `/api/migrations/${enc(id)}`),

  // ---- 0.0.6: tasks, detection, frameworks, New project (docs/API.md, the 0.0.6 amendment) ----
  listTasks: (serviceId: string) => request<TaskList>("GET", `/api/services/${enc(serviceId)}/tasks`),
  runTask: (serviceId: string, b: TaskRequest) => request<Job>("POST", `/api/services/${enc(serviceId)}/tasks`, b),
  detectProject: (dir: string) => request<DetectResult>("GET", `/api/project/detect?dir=${encodeURIComponent(dir)}`),
  listFrameworks: () => request<{ frameworks: Framework[] }>("GET", "/api/frameworks"),
  planNewProject: (b: NewProjectRequest) => request<NewProjectPlan>("POST", "/api/new-project/plan", b),
  startNewProject: (b: NewProjectRequest) => request<Job>("POST", "/api/new-project", b),
};
