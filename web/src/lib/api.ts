import type {
  CatalogEntry,
  CatalogRefresh,
  CatalogStatus,
  CommandPreview,
  InstallOptions,
  DetectedRuntime,
  ErrorCode,
  FolderPick,
  Job,
  LogLine,
  PHPExtensions,
  Runtime,
  RuntimeKind,
  Service,
  ServiceInput,
  Settings,
  Site,
  SiteInput,
  Status,
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

  getSettings: () => request<Settings>("GET", "/api/settings"),
  putSettings: (b: Settings) => request<Settings>("PUT", "/api/settings", b),
  /** Changes only the given fields (the daemon treats missing ones as unchanged). */
  patchSettings: (b: Partial<Settings>) => request<Settings>("PUT", "/api/settings", b),

  eventTicket: () => request<{ ticket: string }>("POST", "/api/events/ticket"),

  /** Stops every service and exits the daemon (Quit Zero2Dev). */
  shutdown: () => request<{ stopping: boolean }>("POST", "/api/shutdown"),

  /** Opens the native folder dialog on the daemon's PC; resolves when the user closes it. */
  pickFolder: (title: string, start: string) => request<FolderPick>("POST", "/api/dialog/folder", { title, start }),
};
