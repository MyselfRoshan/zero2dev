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
  // client-side only
  | "network"
  | "unknown";

export interface ServiceStatusEvent {
  id: string;
  state: ServiceState;
  pid: number;
  startedAt: string | null;
  lastError: string;
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
