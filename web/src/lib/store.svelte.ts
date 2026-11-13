import { addedMessage } from "./catalog";
import { ApiError, api, consumeTokenFromHash, getToken, onAuthFailure, setToken } from "./api";
import { EventConnection, type EventType, type StreamState } from "./events";
import type {
  CatalogEntry,
  CatalogStatus,
  InstallOptions,
  Job,
  JobDoneEvent,
  JobProgressEvent,
  LogLine,
  LogLineEvent,
  Runtime,
  Service,
  ServiceStatusEvent,
  Settings,
  Site,
  Status,
} from "./types";
import type { KindProfile } from "./kinds";
import {
  THEME_CACHE_KEY,
  applyTheme,
  normalizeAccent,
  normalizeMode,
  resolveScheme,
  themeCache,
  type ThemeMode,
} from "./theme";

export const MAX_LOG_LINES = 2000;

export interface LogEntry extends LogLine {
  seq: number;
  serviceId: string;
}
export interface Toast {
  id: number;
  kind: "info" | "success" | "error";
  text: string;
}

export const app = $state({
  phase: "loading" as "notoken" | "loading" | "ready",
  conn: "online" as "online" | "offline" | "unauthorized",
  stream: "idle" as StreamState,
  status: null as Status | null,
  settings: null as Settings | null,
  loaded: { services: false, runtimes: false, sites: false, catalog: false },
  /** Set after Quit Zero2Dev: the daemon is gone on purpose, not offline. */
  stopped: false,
  reconnects: 0,
});

// Reactive lists. Mutate through the loaders / upsert helpers.
export const lists = $state({
  services: [] as Service[],
  runtimes: [] as Runtime[],
  sites: [] as Site[],
  catalog: [] as CatalogEntry[],
  /** What each service type needs (GET /api/kinds); loaded once. */
  kinds: [] as KindProfile[],
});

export const jobs = $state<Record<string, Job>>({});
/** catalog id -> job id for installs started from this page */
export const installJobs = $state<Record<string, string>>({});
export const toasts = $state<Toast[]>([]);

// ---------- catalog versions ----------
export const catalogInfo = $state({
  status: null as CatalogStatus | null,
  refreshing: false,
});
let autoRefreshed = false;

export async function loadCatalogStatus(): Promise<CatalogStatus | null> {
  await load(api.catalogStatus, (v) => (catalogInfo.status = v));
  return catalogInfo.status;
}

/** Asks the publishers for new versions. `silent` suppresses the success toast (failures still toast). */
export async function refreshCatalog(silent = false): Promise<void> {
  if (catalogInfo.refreshing) return;
  catalogInfo.refreshing = true;
  try {
    const r = await api.refreshCatalog();
    catalogInfo.status = { checkedAt: r.checkedAt, sources: r.sources };
    await loadCatalog();
    if (!silent) toast("success", addedMessage(r.added));
  } catch (e) {
    handleError(e);
    toast("error", `Could not check for new versions: ${errMessage(e)}`);
  } finally {
    catalogInfo.refreshing = false;
  }
}

/** On the first visit to the Runtimes page: load the status and, if no check ever ran, check once (silently). */
export async function initCatalogStatus(): Promise<void> {
  const st = await loadCatalogStatus();
  if (st && st.checkedAt === null && !autoRefreshed) {
    autoRefreshed = true;
    await refreshCatalog(true);
  }
}

// ---------- toasts ----------
let toastSeq = 0;
export function toast(kind: Toast["kind"], text: string): void {
  if (app.stopped) return; // after Quit, failing requests are expected, not news
  const id = ++toastSeq;
  toasts.push({ id, kind, text });
  setTimeout(() => dismissToast(id), kind === "error" ? 9000 : 5000);
  if (toasts.length > 5) toasts.splice(0, toasts.length - 5);
}
export function dismissToast(id: number): void {
  const i = toasts.findIndex((t) => t.id === id);
  if (i >= 0) toasts.splice(i, 1);
}

export function errMessage(e: unknown): string {
  return e instanceof ApiError ? e.message : e instanceof Error ? e.message : String(e);
}

/** Runs an API action; shows a toast on failure. Returns undefined when it failed. */
export async function attempt<T>(fn: () => Promise<T>, okMessage?: string): Promise<T | undefined> {
  try {
    const r = await fn();
    markOnline();
    if (okMessage) toast("success", okMessage);
    return r;
  } catch (e) {
    handleError(e);
    toast("error", errMessage(e));
    return undefined;
  }
}

function markOnline() {
  if (app.conn !== "unauthorized") app.conn = "online";
}
function handleError(e: unknown) {
  if (e instanceof ApiError) {
    if (e.isNetwork) app.conn = "offline";
    else if (e.isAuth) app.conn = "unauthorized";
  }
}

// ---------- loaders ----------
async function load<T>(fn: () => Promise<T>, apply: (v: T) => void, key?: keyof typeof app.loaded): Promise<void> {
  try {
    apply(await fn());
    if (key) app.loaded[key] = true;
    markOnline();
  } catch (e) {
    handleError(e);
    if (!(e instanceof ApiError && (e.isNetwork || e.isAuth))) toast("error", errMessage(e));
  }
}

export const loadServices = () => load(api.listServices, (v) => (lists.services = v), "services");
export const loadRuntimes = () => load(api.listRuntimes, (v) => (lists.runtimes = v), "runtimes");
export const loadSites = () => load(api.listSites, (v) => (lists.sites = v), "sites");
export const loadCatalog = () => load(api.catalog, (v) => (lists.catalog = v), "catalog");
export const loadStatus = () => load(api.status, (v) => (app.status = v));
export const loadSettings = () => load(api.getSettings, (v) => (app.settings = v));
export const loadKinds = () => load(api.kinds, (v) => (lists.kinds = v));

export function loadAll(): Promise<unknown> {
  return Promise.all([
    loadStatus(),
    loadSettings(),
    loadServices(),
    loadRuntimes(),
    loadSites(),
    loadCatalog(),
    loadKinds(),
  ]);
}

export function upsertService(s: Service): void {
  const i = lists.services.findIndex((x) => x.id === s.id);
  if (i >= 0) lists.services[i] = s;
  else lists.services.push(s);
  lists.services.sort((a, b) => a.name.localeCompare(b.name));
}

// ---------- logs ----------
let logSeq = 0;
class LogStore {
  /** Replaced wholesale on every flush (cheap: arrays are never proxied). */
  byService = $state.raw<Record<string, LogEntry[]>>({});
}
export const logs = new LogStore();
let pendingLogs: LogEntry[] = [];
let flushTimer: ReturnType<typeof setTimeout> | null = null;

function flushLogs() {
  flushTimer = null;
  if (!pendingLogs.length) return;
  const next = { ...logs.byService };
  for (const l of pendingLogs) {
    const arr = (next[l.serviceId] ??= []);
    next[l.serviceId] = arr.length >= MAX_LOG_LINES ? [...arr.slice(arr.length - MAX_LOG_LINES + 1), l] : [...arr, l];
  }
  pendingLogs = [];
  logs.byService = next;
}

function pushLog(e: LogLineEvent) {
  pendingLogs.push({ seq: ++logSeq, serviceId: e.serviceId, ts: e.ts, stream: e.stream, text: e.text });
  flushTimer ??= setTimeout(flushLogs, 80);
}

const logKey = (l: { ts: string; stream: string; text: string }) => `${l.ts}|${l.stream}|${l.text}`;

/** Loads the tail from the API and merges it with live lines already received. */
export async function seedLogs(serviceId: string, tail = 500): Promise<void> {
  const startedSeq = logSeq;
  try {
    const res = await api.serviceLogs(serviceId, tail);
    flushLogs();
    const seen = new Set(res.lines.map(logKey));
    const live = (logs.byService[serviceId] ?? []).filter((l) => l.seq > startedSeq && !seen.has(logKey(l)));
    const base: LogEntry[] = res.lines.map((l) => ({ ...l, serviceId, seq: ++logSeq }));
    logs.byService = { ...logs.byService, [serviceId]: [...base, ...live].slice(-MAX_LOG_LINES) };
    markOnline();
  } catch (e) {
    handleError(e);
    if (!(e instanceof ApiError && (e.isNetwork || e.isAuth))) toast("error", errMessage(e));
  }
}

export function clearLogs(serviceId?: string): void {
  flushLogs();
  if (serviceId) logs.byService = { ...logs.byService, [serviceId]: [] };
  else logs.byService = {};
}

// ---------- jobs ----------
const finishedJobs = new Set<string>();

function mergeJob(id: string, patch: Partial<Job>) {
  const base: Job = jobs[id] ?? {
    id,
    type: "install",
    state: "running",
    percent: 0,
    step: "",
    error: "",
    runtimeId: "",
  };
  jobs[id] = { ...base, ...patch, id };
}

function onJobProgress(e: JobProgressEvent) {
  if (finishedJobs.has(e.id)) return;
  mergeJob(e.id, { percent: e.percent, step: e.step, state: "running" });
}

function onJobDone(e: JobDoneEvent) {
  if (finishedJobs.has(e.id)) return;
  finishedJobs.add(e.id);
  mergeJob(e.id, {
    state: e.state,
    error: e.error,
    runtimeId: e.runtimeId,
    percent: e.state === "done" ? 100 : (jobs[e.id]?.percent ?? 0),
  });
  const entry = lists.catalog.find((c) => installJobs[c.id] === e.id);
  const label = entry?.name ?? (jobs[e.id]?.type === "xdebug" ? "Xdebug" : "Runtime");
  if (e.state === "done") toast("success", `${label} installed.`);
  else {
    const short = (e.error || "unknown error").slice(0, 160);
    toast(
      "error",
      `${label} failed: ${short}${(e.error || "").length > 160 ? "… (full text on the Runtimes page)" : ""}`,
    );
  }
  void loadRuntimes();
  void loadCatalog();
  void loadStatus();
}

/**
 * Starts an install. Returns the daemon's error instead of toasting so callers can react to
 * `unverified`; network and auth problems are still handled globally.
 */
export async function tryInstall(c: CatalogEntry, opts?: InstallOptions): Promise<ApiError | null> {
  try {
    const job = await api.installRuntime(c.id, opts);
    markOnline();
    trackJob(c.id, job);
    return null;
  } catch (e) {
    handleError(e);
    return e instanceof ApiError ? e : new ApiError(0, "unknown", errMessage(e));
  }
}

export async function installRuntime(c: CatalogEntry, opts?: InstallOptions): Promise<void> {
  const err = await tryInstall(c, opts);
  if (err) toast("error", err.message);
}

function trackJob(catalogId: string, job: Job) {
  installJobs[catalogId] = job.id;
  if (!finishedJobs.has(job.id)) mergeJob(job.id, job);
  pollJob(job.id);
}

/** Downloads Xdebug into a PHP runtime's ext folder. Returns the job id, or undefined after a toast. */
export async function installXdebug(runtimeId: string): Promise<string | undefined> {
  const job = await attempt(() => api.installXdebug(runtimeId));
  if (!job) return undefined;
  if (!finishedJobs.has(job.id)) mergeJob(job.id, job);
  pollJob(job.id);
  return job.id;
}

/** Imports a file the user downloaded. Throws ApiError so the dialog can show the message inline. */
export async function importRuntime(
  c: CatalogEntry,
  filePath: string,
  sha256: string,
  opts?: InstallOptions,
): Promise<void> {
  const job = await api.importRuntime({ catalogId: c.id, filePath, sha256: sha256.trim() || undefined, ...opts });
  markOnline();
  trackJob(c.id, job);
}

/** Safety net for a missed job.done event (for example while the stream is reconnecting). */
function pollJob(id: string) {
  const tick = async () => {
    if (finishedJobs.has(id)) return;
    try {
      const j = await api.getJob(id);
      if (j.state === "running") {
        mergeJob(id, j);
        setTimeout(tick, 2000);
      } else {
        onJobDone({
          id,
          state: j.state,
          error: j.error,
          runtimeId: j.runtimeId,
        });
      }
    } catch {
      setTimeout(tick, 3000);
    }
  };
  setTimeout(tick, 2500);
}

// ---------- events ----------
let conn: EventConnection | null = null;

function onEvent(type: EventType, d: unknown) {
  switch (type) {
    case "service.status": {
      const e = d as ServiceStatusEvent;
      const s = lists.services.find((x) => x.id === e.id);
      if (s) {
        s.state = e.state;
        s.pid = e.pid;
        s.startedAt = e.startedAt;
        s.lastError = e.lastError;
        if (e.state === "failed" && e.lastError) toast("error", `${s.name} failed: ${e.lastError}`);
      }
      void loadStatus();
      break;
    }
    case "log.line":
      pushLog(d as LogLineEvent);
      break;
    case "job.progress":
      onJobProgress(d as JobProgressEvent);
      break;
    case "job.done":
      onJobDone(d as JobDoneEvent);
      break;
    case "services.changed":
      void loadServices();
      void loadStatus();
      break;
    case "runtimes.changed":
      void loadRuntimes();
      void loadCatalog();
      void loadStatus();
      break;
    case "sites.changed":
      void loadSites();
      void loadStatus();
      break;
  }
}

function startEvents() {
  conn?.stop();
  conn = new EventConnection({
    getTicket: async () => (await api.eventTicket()).ticket,
    createSource: (url) => new EventSource(url) as never,
    isAuthError: (e) => e instanceof ApiError && e.isAuth,
    onEvent,
    onState: (s) => {
      app.stream = s;
      if (s === "open" && app.conn === "offline") app.conn = "online";
      if (s === "retrying" && app.conn === "online") app.conn = "offline";
      if (s === "unauthorized") app.conn = "unauthorized";
    },
    onOpen: (reconnect) => {
      if (reconnect) {
        app.reconnects++;
        void loadAll();
      }
    },
  });
  conn.start();
}

// ---------- boot / auth ----------
let hashWatcher = false;
export function startApp(): void {
  onAuthFailure(() => {
    app.conn = "unauthorized";
  });
  if (!hashWatcher) {
    hashWatcher = true;
    // Opening the daemon's link in a tab that is already open only changes the hash.
    window.addEventListener("hashchange", () => {
      if (location.hash.startsWith("#token=")) {
        app.conn = "online";
        startApp();
      }
    });
  }
  consumeTokenFromHash();
  if (!getToken()) {
    app.phase = "notoken";
    return;
  }
  app.phase = "ready";
  void loadAll();
  startEvents();
}

export function submitToken(token: string): void {
  setToken(token.trim() || null);
  app.conn = "online";
  startApp();
}

export function forgetToken(): void {
  conn?.stop();
  setToken(null);
  app.phase = "notoken";
}

// ---------- appearance (theme + accent) ----------
const darkQuery = typeof matchMedia === "function" ? matchMedia("(prefers-color-scheme: dark)") : null;
/** The OS colour scheme, kept live for the "system" theme. */
export const appearance = $state({ prefersDark: darkQuery?.matches ?? false });
darkQuery?.addEventListener("change", (e) => (appearance.prefersDark = e.matches));

/** The saved theme and accent (defaults until the settings load or for an older daemon). */
export function currentAppearance(): { theme: ThemeMode; accent: string } {
  return { theme: normalizeMode(app.settings?.theme), accent: normalizeAccent(app.settings?.accent) };
}

let appliedKey = "";
function applyAppearance(): void {
  if (!app.settings || typeof document === "undefined") return; // until then the inline script in index.html has applied the cached theme
  const { theme, accent } = currentAppearance();
  const scheme = resolveScheme(theme, appearance.prefersDark);
  const key = `${theme}|${accent}|${scheme}`;
  if (key === appliedKey) return;
  appliedKey = key;
  const cache = themeCache(theme, accent);
  applyTheme(document.documentElement, cache[scheme], scheme, theme);
  try {
    localStorage.setItem(THEME_CACHE_KEY, JSON.stringify(cache)); // read by index.html on the next load: no flash
  } catch {
    /* storage blocked: the theme still applies, only the next load starts with the default */
  }
}
if (typeof document !== "undefined") {
  $effect.root(() => {
    $effect(() => applyAppearance());
  });
}

let appearanceSeq = 0;
/** Changes the theme and/or accent: applies at once, then saves. On failure it puts the old values back.
 *  Mutates app.settings in place so an open Settings form keeps its unsaved edits. */
export async function setAppearance(change: { theme?: ThemeMode; accent?: string }): Promise<boolean> {
  const s = app.settings;
  if (!s) return false;
  const before = { theme: s.theme, accent: s.accent };
  Object.assign(s, change);
  const seq = ++appearanceSeq;
  try {
    const r = await api.patchSettings(change);
    markOnline();
    if (seq === appearanceSeq && app.settings) Object.assign(app.settings, { theme: r.theme, accent: r.accent });
    return true;
  } catch (e) {
    handleError(e);
    if (seq === appearanceSeq && app.settings) Object.assign(app.settings, before);
    toast("error", errMessage(e));
    return false;
  }
}

/** Quit Zero2Dev: the daemon stops every service, then exits. Returns false when it refused. */
export async function quitDaemon(): Promise<boolean> {
  try {
    await api.shutdown();
  } catch (e) {
    // The daemon may close the connection while answering; anything but a clear refusal means it is stopping.
    if (e instanceof ApiError && !e.isNetwork) {
      toast("error", e.message);
      return false;
    }
  }
  conn?.stop();
  app.stopped = true;
  toasts.splice(0, toasts.length);
  return true;
}

/** Retry button on the offline / unauthorized banner. */
export async function retryConnection(): Promise<void> {
  app.conn = "online";
  await loadAll();
  if (app.stream !== "open") startEvents();
}

export async function makeDefaultRuntime(id: string): Promise<void> {
  const r = await attempt(() => api.setDefaultRuntime(id));
  if (r) await loadRuntimes();
}
