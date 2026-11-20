// Mirrors docs/API.md exactly.

export type DatabaseKind = "mariadb" | "mysql" | "postgres" | "redis";
export const DATABASE_KINDS: DatabaseKind[] = ["mariadb", "mysql", "postgres", "redis"];
export type MailKind = "mailpit";
export const MAIL_KINDS: MailKind[] = ["mailpit"];
export type ServiceKind = "custom" | "node" | "php" | "go" | "java" | "caddy" | "nginx" | DatabaseKind | MailKind;
export const SERVICE_KINDS: ServiceKind[] = [
  "custom",
  "node",
  "php",
  "go",
  "java",
  "caddy",
  "nginx",
  ...DATABASE_KINDS,
  ...MAIL_KINDS,
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
  /** Database administrator password ("" for none); changed only through the password route (0.0.5). */
  adminPassword?: string;
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
  | MailKind
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
  ...MAIL_KINDS,
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
  // 0.0.5: set only by the jobs that use them (database, snapshot and migration jobs).
  serviceId?: string;
  projectId?: string;
  snapshotId?: string;
  migrationId?: string;
  /** The file a db-export wrote. */
  path?: string;
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
  /** Which runtime caches live in the data folder; changed by Bring in only (0.0.5). */
  portable?: Portable;
}

export interface Portable {
  npmGlobal: boolean;
  composer: boolean;
  goCache: boolean;
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

// ---------------------------------------------------------------------------
// 0.0.5: databases, snapshots, Bring in. Mirrors docs/API.md "Amendment: data,
// databases and environment snapshots (0.0.5)".
// ---------------------------------------------------------------------------

export type JobType =
  | "install"
  | "import"
  | "xdebug"
  | "db-import"
  | "db-export"
  | "snapshot"
  | "snapshot-restore"
  | "snapshot-clone"
  | "migrate"
  | "migrate-revert";

/** One database of a server. */
export interface DbDatabase {
  name: string;
  /** 0 when the server does not say. */
  sizeBytes: number;
}

/** GET /api/services/{id}/database */
export interface DatabaseInfo {
  serviceId: string;
  kind: ServiceKind;
  running: boolean;
  /** The administrator: root or postgres ("" for redis). */
  user: string;
  hasPassword: boolean;
  /** Empty while stopped or for redis. */
  databases: DbDatabase[];
  users: string[];
  supports: { databases: boolean; dumps: boolean; password: boolean; reset: boolean };
  dataDir: string;
}

export interface DbImportInput {
  database: string;
  /** Absolute path of a .sql or gzip-compressed dump on the daemon's PC. */
  filePath: string;
  /** Create the database first when it does not exist. */
  create?: boolean;
}

export interface DbExportInput {
  database: string;
  /** Absolute path to write; ending in .gz writes gzip. */
  filePath: string;
  overwrite?: boolean;
}

export interface FileFilter {
  name: string;
  /** e.g. "*.sql;*.sql.gz" */
  pattern: string;
}

export interface FilePickInput {
  title?: string;
  start?: string;
  save?: boolean;
  defaultName?: string;
  filters?: FileFilter[];
}

export type SnapshotScope = "project" | "service";

export interface RuntimeRef {
  kind: string;
  version: string;
  /** "" for a registered program. */
  catalogId: string;
  binary: string;
  /** The service used the default of its kind (no runtimeId of its own). */
  followedDefault: boolean;
}

export interface SnapshotService {
  service: Service;
  runtime: RuntimeRef | null;
  hasData: boolean;
  phpIni: boolean;
}

export interface Snapshot {
  format: number;
  id: string;
  name: string;
  scope: SnapshotScope;
  ownerId: string;
  createdAt: string;
  /** Zero2Dev version that took it. */
  version: string;
  sizeBytes: number;
  project: Project | null;
  sites: Site[];
  services: SnapshotService[];
  warnings: string[];
}

export interface MissingRuntime {
  kind: string;
  version: string;
  catalogId: string;
  /** The catalog has it, so it can be installed before restoring. */
  installable: boolean;
  /** Names of the services that need it. */
  services: string[];
}

/** GET /api/snapshots/{id} */
export interface SnapshotDetail extends Snapshot {
  missing: MissingRuntime[];
}

export interface TakeSnapshotInput {
  name: string;
  /** Stop running services first and start them again afterwards. */
  restart?: boolean;
}

export interface CloneSnapshotInput {
  name: string;
  /** Added to the first label of each site domain; default "-copy". */
  domainSuffix?: string;
}

export type MigrateProgram =
  | "vscode"
  | "vscodium"
  | "notepadpp"
  | "heidisql"
  | "dbeaver"
  | "runtime"
  | "npm-global"
  | "npm-cache"
  | "go-cache"
  | "composer"
  | "database";

export type MigrateMode = "copy" | "move";
export type RevertHow = "discard" | "copy-back" | "move-back";

export interface MigrateItem {
  label: string;
  kind: "dir" | "file" | "registry" | "runtime" | "databases";
  from: string;
  to: string;
  sizeBytes: number;
  exists: boolean;
}

/** A program found on this PC that can be brought in (GET /api/migrations/sources). */
export interface MigrateSource {
  /** "vscode", "runtime:rt_1a2b3c4d", "database" ... */
  id: string;
  program: MigrateProgram;
  label: string;
  found: boolean;
  items: MigrateItem[];
  modes: MigrateMode[];
  /** Why Move is not offered; "" when it is. */
  copyOnlyReason: string;
  /** What must happen first; empty when it can run. */
  blockers: string[];
  /** The portable place already holds data (it is backed up first). */
  targetHasData: boolean;
  runtimeId?: string;
  note: string;
}

export interface MigrateDatabase {
  kind: "mariadb" | "mysql" | "postgres";
  host: string;
  port: number;
  user: string;
  password: string;
  databases: string[];
  targetServiceId: string;
}

export interface MigrateRequest {
  source: string;
  mode: MigrateMode;
  /** Labels of the items to include; empty means all. */
  items?: string[];
  database?: MigrateDatabase;
}

export interface Migration {
  id: string;
  source: string;
  program: MigrateProgram;
  label: string;
  mode: MigrateMode;
  createdAt: string;
  state: "done" | "reverted" | "failed";
  revertedAt: string | null;
  items: MigrateItem[];
  revertOptions: RevertHow[];
  databases?: string[];
  serviceId?: string;
  runtimeId?: string;
  description: string;
}
