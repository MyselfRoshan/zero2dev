// Mirrors docs/API.md exactly.

export type ServiceKind = "custom" | "node" | "php" | "go" | "java" | "caddy";
export const SERVICE_KINDS: ServiceKind[] = ["custom", "node", "php", "go", "java", "caddy"];
export type ServiceState = "stopped" | "starting" | "running" | "stopping" | "failed";

export interface Service {
  id: string;
  name: string;
  kind: ServiceKind;
  command: string;
  args: string[];
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
  cwd: string;
  env: Record<string, string>;
  port: number;
  healthPath: string;
  runtimeId: string;
  autostart: boolean;
}

export type RuntimeKind = "node" | "php" | "go" | "java" | "caddy" | "other";
export const RUNTIME_KINDS: RuntimeKind[] = ["node", "php", "go", "java", "caddy", "other"];

export interface Runtime {
  id: string;
  kind: RuntimeKind;
  version: string;
  name: string;
  binary: string;
  source: "installed" | "registered";
  installedAt: string;
}

export interface DetectedRuntime {
  kind: string;
  version: string;
  binary: string;
}

export interface Checksum {
  type: "sha256" | "shasums";
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
}

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
  counts: { services: number; running: number; runtimes: number; sites: number };
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
