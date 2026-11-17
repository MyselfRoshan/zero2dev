// Mirrors docs/API.md exactly.

export type DatabaseKind = "mariadb" | "mysql" | "postgres" | "redis";
export const DATABASE_KINDS: DatabaseKind[] = ["mariadb", "mysql", "postgres", "redis"];
export type ServiceKind = "custom" | "node" | "php" | "go" | "java" | "caddy" | "nginx" | DatabaseKind;
export const SERVICE_KINDS: ServiceKind[] = [
  "custom",
  "node",
  "php",
  "go",
  "java",
  "caddy",
  "nginx",
  ...DATABASE_KINDS,
];
export type ServiceState = "stopped" | "starting" | "running" | "stopping" | "failed";

/** The answer of POST /api/services/start-all. */
export interface StartAllResult {
  started: string[];
  failed: { id: string; name: string; error: string }[];
}

/** The answer of POST /api/sites/enable-all and /disable-all: how many sites changed. */
export interface SitesBulkResult {
  changed: number;
}

/** The answer of POST /api/services/stop-all. */
export interface StopAllResult {
  stopped: string[];
  failed: { id: string; name: string; error: string }[];
}

export interface Service {
  id: string;
  name: string;
  kind: ServiceKind;
  command: string;
  /** Extra arguments after the generated ones, or the whole list when customArgs is true. */
  args: string[];
  /** Per-kind settings (GET /api/kinds); missing keys use their defaults. */
  settings: Record<string, string>;
  customArgs: boolean;
  cwd: string;
  env: Record<string, string>;
  port: number;
  healthPath: string;
  runtimeId: string;
  autostart: boolean;
  /** Services that must be running before this one starts (and stop after it). */
  dependsOn: string[];
  /** A database service this one uses; it is a dependency too. "" for none. */
  databaseId: string;
  /** A .env file, relative to the folder, whose variables the service gets. "" for none. */
  envFile: string;
  /** Start the service again after it exits unexpectedly. */
  restartOnCrash: boolean;
  /** At most this many restarts in a row, 1-20 (default 5). */
  restartLimit: number;
  /** Crash restarts since the last manual start (filled in by the daemon). */
  restarts: number;
  state: ServiceState;
  pid: number;
  startedAt: string | null;
  lastError: string;
}

export interface ServiceInput {
  name: string;
  kind: ServiceKind;
  command: string;
  args: string[];
  settings: Record<string, string>;
  customArgs: boolean;
  cwd: string;
  env: Record<string, string>;
  port: number;
  healthPath: string;
  runtimeId: string;
  autostart: boolean;
  dependsOn: string[];
  databaseId: string;
  envFile: string;
  restartOnCrash: boolean;
  restartLimit: number;
}

/** A group of services and sites that are started and stopped together. */
export interface Project {
  id: string;
  name: string;
  serviceIds: string[];
  siteIds: string[];
}

export interface ProjectInput {
  name: string;
  serviceIds: string[];
  siteIds: string[];
}

/** What a service runs (POST /api/services/preview, GET /api/services/{id}/command). */
export interface CommandPreview {
  program: string;
  args: string[];
  /** The variables Zero2Dev adds, KEY=VALUE. */
  env: string[];
  workdir: string;
  dataDir: string;
  files: string[];
  warnings: string[];
}

export interface PHPExtension {
  name: string;
  file: string;
  zend: boolean;
}

export interface PHPExtensions {
  runtimeId: string;
  version: string;
  extDir: string;
  /** false: no ext folder next to php (a system PHP); its extensions are not Zero2Dev's to manage. */
  managed: boolean;
  extensions: PHPExtension[];
  xdebug: boolean;
}

export type EditorKind = "vscode" | "vscodium" | "notepadpp";
export type DBToolKind = "heidisql" | "dbeaver";
export type RuntimeKind =
  | "node"
  | "php"
  | "go"
  | "java"
  | "caddy"
  | "nginx"
  | DatabaseKind
  | EditorKind
  | DBToolKind
  | "other";
export const RUNTIME_KINDS: RuntimeKind[] = [
  "node",
  "php",
  "go",
  "java",
  "caddy",
  "nginx",
  ...DATABASE_KINDS,
  "vscode",
  "vscodium",
  "notepadpp",
  "heidisql",
  "dbeaver",
  "other",
];

export interface Runtime {
  id: string;
  kind: RuntimeKind;
  version: string;
  name: string;
  binary: string;
  source: "installed" | "registered";
  installedAt: string;
  /** The catalog entry it was installed from (omitted when empty). */
  catalogId?: string;
  /** The default runtime of its kind (at most one per kind). */
  default?: boolean;
}

export interface DetectedRuntime {
  kind: string;
  version: string;
  binary: string;
  /** Set for tools reported with kind "other", e.g. "nginx". */
  name?: string;
}

export interface Checksum {
  type: "sha256" | "md5" | "shasums" | "none";
  value?: string;
  url?: string;
}

export interface CatalogEntry {
  id: string;
  kind: RuntimeKind;
  version: string;
  name: string;
  url: string;
  checksum: Checksum;
  homepage: string;
  fileName: string;
  binaryRelPath: string;
  stripComponents: number;
  os: string;
  arch: string;
  installed: boolean;
  supported: boolean;
  /** Version line such as "8.4", "21", "1.26"; "" for node/go/caddy. */
  line: string; // node "22", go "1.23", caddy "2.8", php "8.4", java "21", nginx "1.26"
  channel: "lts" | "stable" | "mainline" | "";
  source: "builtin" | "remote";
  /** Newest version of this kind/os/arch/line. */
  latest: boolean;
  /** The publisher has no checksum (nginx). */
  unverified: boolean;
}

export interface CatalogSource {
  kind: string;
  name: string;
  ok: boolean;
  count: number;
  error: string;
  checkedAt: string;
}

export interface CatalogStatus {
  checkedAt: string | null;
  sources: CatalogSource[];
}

export interface CatalogRefresh extends CatalogStatus {
  added: string[];
}

export interface InstallOptions {
  acceptUnverified?: boolean;
}

export type SiteType = "static" | "proxy" | "php";

export interface SiteInput {
  name: string;
  domain: string;
  type: SiteType;
  root: string;
  serviceId: string;
  upstreamPort: number;
  https: boolean;
  enabled: boolean;
}

export interface Site extends SiteInput {
  id: string;
  url: string;
}

export type JobState = "running" | "done" | "failed";

export interface Job {
  id: string;
  type: string;
  state: JobState;
  percent: number;
  step: string;
  error: string;
  runtimeId: string;
}

export interface Settings {
  apiPort: number;
  homeDir: string;
  domainSuffix: string;
  proxyHttpPort: number;
  proxyHttpsPort: number;
  openBrowserOnStart: boolean;
  trustLocalCA: boolean;
  downloadProxy: string;
  /** Which program serves the sites. Missing (older daemon) means caddy. */
  siteServer?: SiteServer;
  /** Dashboard colour scheme. Missing (older daemon) means system. */
  theme?: "system" | "light" | "dark";
  /** Dashboard accent colour, #rrggbb. Missing (older daemon) means #1677ff. */
  accent?: string;
}

/** A folder on the Windows user PATH, switched on or off on its own. */
export interface EnvPath {
  id: string;
  source: "shims" | "runtime" | "custom";
  /** runtime: the folder of this runtime's program. */
  runtimeId?: string;
  /** custom: a full path. */
  dir?: string;
  enabled: boolean;
}
/** A Windows user environment variable, switched on or off on its own. */
export interface EnvVar {
  id: string;
  name: string;
  source: "default" | "runtime" | "custom";
  /** default: the runtime kind whose default version is used. */
  kind?: string;
  /** runtime: one specific version. */
  runtimeId?: string;
  /** custom: the value. */
  value?: string;
  enabled: boolean;
}
export interface EnvConfig {
  seeded?: boolean;
  path: EnvPath[];
  vars: EnvVar[];
}
/** What the user environment holds for one folder item. */
export interface EnvPathStatus {
  id: string;
  /** The folder the item stands for ("" when it cannot be worked out). */
  dir: string;
  /** On the user PATH now. */
  present: boolean;
  /** Zero2Dev added it and takes it off again. */
  owned: boolean;
  problem?: string;
  /** The commands the folder gives a terminal (node, npm ...), sorted. Missing on older daemons. */
  commands?: string[];
}
/** What the user environment holds for one variable item. */
export interface EnvVarStatus {
  id: string;
  name: string;
  /** The value the item stands for ("" when it cannot be worked out). */
  value: string;
  current?: string;
  hasCurrent: boolean;
  /** The current value is the one Zero2Dev set. */
  owned: boolean;
  problem?: string;
}
export interface EnvReport {
  /** False off Windows: nothing can be changed. */
  supported: boolean;
  shimsDir: string;
  error?: string;
  path: EnvPathStatus[];
  vars: EnvVarStatus[];
}
/** GET and PUT /api/environment. */
export interface Environment {
  config: EnvConfig;
  report: EnvReport;
}

export type SiteServer = "caddy" | "nginx";

export type HomeSource = "env" | "cwd" | "exe";

export interface Status {
  version: string;
  uptimeSeconds: number;
  os: string;
  arch: string;
  homeDir: string;
  homeSource: HomeSource;
  portable: boolean;
  exeDir: string;
  /** The daemon can open the native "Select Folder" dialog (Windows). Missing on older daemons. */
  folderPicker?: boolean;
  counts: { services: number; running: number; runtimes: number; sites: number };
}

export interface FolderPick {
  path: string;
  cancelled: boolean;
}

export type LogStream = "stdout" | "stderr" | "system";
export interface LogLine {
  ts: string;
  stream: LogStream;
  text: string;
}

export type ErrorCode =
  | "bad_request"
  | "unauthorized"
  | "forbidden_host"
  | "forbidden_origin"
  | "not_found"
  | "conflict"
  | "unverified"
  | "internal"
  | "not_supported"
  // client-side only
  | "network"
  | "unknown";

export interface ServiceStatusEvent {
  id: string;
  state: ServiceState;
  pid: number;
  startedAt: string | null;
  lastError: string;
  /** Crash restarts since the last manual start. Missing on older daemons. */
  restarts?: number;
}
/** `service.crashed`: a service with restart on crash exited unexpectedly. */
export interface ServiceCrashedEvent {
  id: string;
  name: string;
  error: string;
  /** 1-based number of this crash since the last manual start. */
  attempt: number;
  limit: number;
  /** Seconds until the next start; 0 when gaveUp. */
  retryInSec: number;
  /** The limit is reached: the service is not restarted again. */
  gaveUp: boolean;
}
export interface LogLineEvent extends LogLine {
  serviceId: string;
}
export interface JobProgressEvent {
  id: string;
  percent: number;
  step: string;
}
export interface JobDoneEvent {
  id: string;
  state: JobState;
  error: string;
  runtimeId: string;
}
