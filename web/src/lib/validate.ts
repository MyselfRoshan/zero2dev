import type { RuntimeKind, ServiceInput, ServiceKind, SiteInput } from "./types";
import { SERVICE_KINDS } from "./types";

export type Errors<K extends string = string> = Partial<Record<K, string>>;

export function isAbsolutePath(p: string): boolean {
  if (!p) return false;
  if (/^[A-Za-z]:[\\/]/.test(p)) return true; // C:\ or C:/
  if (/^\\\\[^\\]+\\[^\\]+/.test(p)) return true; // UNC
  return p.startsWith("/");
}

export function isValidPort(n: unknown, allowZero = true): boolean {
  return typeof n === "number" && Number.isInteger(n) && n >= (allowZero ? 0 : 1) && n <= 65535;
}

/** Parses a text field that must hold an integer. Returns NaN when it is not one. */
export function parsePort(text: string): number {
  const t = text.trim();
  if (t === "") return 0;
  if (!/^\d+$/.test(t)) return NaN;
  return Number(t);
}

export function runtimeKindsFor(kind: ServiceKind): RuntimeKind[] {
  return kind === "custom" ? [] : [kind];
}

export type ServiceField = "name" | "kind" | "command" | "args" | "cwd" | "env" | "port" | "healthPath";

export function validateService(v: ServiceInput): Errors<ServiceField> {
  const e: Errors<ServiceField> = {};
  const name = v.name.trim();
  if (!name) e.name = "Enter a name.";
  else if (name.length > 64) e.name = "Use 64 characters or fewer.";
  if (!SERVICE_KINDS.includes(v.kind)) e.kind = "Choose a kind.";
  if (v.kind === "custom" && !v.command.trim())
    e.command = "Custom services need a command (a full path or a name on PATH).";
  if ((v.kind === "node" || v.kind === "java") && v.args.filter((a) => a.trim()).length === 0) {
    e.args = `${v.kind === "node" ? "Node" : "Java"} services have no default arguments. Add at least one, for example server.js.`;
  }
  if (!v.cwd.trim()) e.cwd = "Enter the working directory.";
  else if (!isAbsolutePath(v.cwd.trim())) e.cwd = "Use an absolute path, for example C:\\work\\app.";
  if (!isValidPort(v.port)) e.port = "Port must be a whole number from 0 to 65535 (0 means no port).";
  const hp = v.healthPath.trim();
  if (hp && !hp.startsWith("/")) e.healthPath = "Health path must start with /.";
  else if (hp && v.port === 0) e.healthPath = "A health path needs a port. Set a port or clear this field.";
  const keys = Object.keys(v.env);
  if (keys.some((k) => !k.trim() || k.includes("="))) e.env = "Environment names cannot be empty or contain =.";
  return e;
}

/** Turns editable env rows into a map. Duplicate/blank names are reported through the second value. */
export function envRowsToMap(rows: { key: string; value: string }[]): { env: Record<string, string>; error?: string } {
  const env: Record<string, string> = {};
  for (const r of rows) {
    const k = r.key.trim();
    if (!k && !r.value) continue; // untouched row
    if (!k) return { env: {}, error: "Environment names cannot be empty." };
    if (k.includes("=")) return { env: {}, error: "Environment names cannot contain =." };
    if (k in env) return { env: {}, error: `Environment name ${k} is listed twice.` };
    env[k] = r.value;
  }
  return { env };
}

export function parseSuffixes(suffix: string): string[] {
  return suffix
    .split(/[\s,]+/)
    .map((s) => s.trim().replace(/^\./, "").toLowerCase())
    .filter(Boolean);
}

const LABEL = /^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$/;

export function validateDomain(domain: string, suffixes: string[]): string | undefined {
  const d = domain.trim();
  if (!d) return "Enter a domain.";
  if (d !== d.toLowerCase()) return "Use lowercase letters only.";
  const labels = d.split(".");
  if (labels.length < 2 || labels.some((l) => !LABEL.test(l))) return "Use a hostname such as shop.localhost.";
  const list = suffixes.length ? suffixes : ["localhost"];
  if (!list.some((s) => d.endsWith("." + s))) {
    return `The domain must end with ${list.map((s) => "." + s).join(" or ")}.`;
  }
  return undefined;
}

export type SiteField = "name" | "domain" | "root" | "serviceId" | "upstreamPort";

export function validateSite(v: SiteInput, suffixes: string[]): Errors<SiteField> {
  const e: Errors<SiteField> = {};
  const name = v.name.trim();
  if (!name) e.name = "Enter a name.";
  else if (name.length > 64) e.name = "Use 64 characters or fewer.";
  const de = validateDomain(v.domain, suffixes);
  if (de) e.domain = de;
  if (v.type === "static") {
    if (!v.root.trim()) e.root = "Choose the folder to serve.";
    else if (!isAbsolutePath(v.root.trim())) e.root = "Use an absolute path, for example C:\\work\\shop\\public.";
  } else if (v.type === "php") {
    if (!v.serviceId) e.serviceId = "Choose a PHP service.";
  } else if (v.type === "proxy") {
    if (!v.serviceId && !v.upstreamPort) e.serviceId = "Choose a service or enter an upstream port.";
    if (!v.serviceId && v.upstreamPort && !isValidPort(v.upstreamPort, false)) e.upstreamPort = "Port must be 1-65535.";
  }
  return e;
}

export type RegisterField = "kind" | "binary";
export function validateRegister(v: { kind: string; binary: string }): Errors<RegisterField> {
  const e: Errors<RegisterField> = {};
  if (!v.kind) e.kind = "Choose a kind.";
  if (!v.binary.trim()) e.binary = "Enter the path to the program.";
  else if (!isAbsolutePath(v.binary.trim())) e.binary = "Use an absolute path, for example C:\\php\\php.exe.";
  return e;
}

export function isValidSha256(s: string): boolean {
  // SHA-256 is 64 hex characters, SHA-512 (what Caddy publishes) is 128.
  return /^([0-9a-fA-F]{64}|[0-9a-fA-F]{128})$/.test(s.trim());
}

/** Empty is valid (use the environment). Otherwise an http(s) URL with a host, optional user:pass@ and port. */
export function validateProxyUrl(raw: string): string | undefined {
  const v = raw.trim();
  if (!v) return undefined;
  let u: URL;
  try {
    u = new URL(v);
  } catch {
    return "Use a URL such as http://proxy.company.com:8080.";
  }
  if (u.protocol !== "http:" && u.protocol !== "https:") return "The proxy must start with http:// or https://.";
  if (!u.hostname) return "The proxy URL needs a host name.";
  if ((u.pathname && u.pathname !== "/") || u.search || u.hash)
    return "Use only scheme, host and port, for example http://proxy:8080.";
  return undefined;
}

export type ImportField = "filePath" | "sha256";
export function validateImport(v: { filePath: string; sha256: string }): Errors<ImportField> {
  const e: Errors<ImportField> = {};
  if (!v.filePath.trim()) e.filePath = "Enter the path of the file you downloaded.";
  else if (!isAbsolutePath(v.filePath.trim()))
    e.filePath = "Use an absolute path, for example C:\\Users\\me\\Downloads\\file.zip.";
  if (v.sha256.trim() && !isValidSha256(v.sha256))
    e.sha256 = "A SHA-256 hash has 64 hexadecimal characters and a SHA-512 hash has 128 (0-9, a-f).";
  return e;
}

export type SettingsField = "domainSuffix" | "proxyHttpPort" | "proxyHttpsPort" | "downloadProxy";
export function validateSettings(v: {
  domainSuffix: string;
  proxyHttpPort: number;
  proxyHttpsPort: number;
  downloadProxy?: string;
}): Errors<SettingsField> {
  const e: Errors<SettingsField> = {};
  const pe = validateProxyUrl(v.downloadProxy ?? "");
  if (pe) e.downloadProxy = pe;
  const sf = parseSuffixes(v.domainSuffix);
  if (sf.length === 0) e.domainSuffix = "Enter a suffix such as localhost.";
  else if (sf.some((s) => !s.split(".").every((l) => LABEL.test(l))))
    e.domainSuffix = "Use letters, digits and hyphens, for example localhost or test.";
  if (!isValidPort(v.proxyHttpPort, false)) e.proxyHttpPort = "Port must be 1-65535.";
  if (!isValidPort(v.proxyHttpsPort, false)) e.proxyHttpsPort = "Port must be 1-65535.";
  if (!e.proxyHttpPort && !e.proxyHttpsPort && v.proxyHttpPort === v.proxyHttpsPort)
    e.proxyHttpsPort = "HTTP and HTTPS ports must differ.";
  return e;
}

export function hasErrors(e: object): boolean {
  return Object.keys(e).length > 0;
}
