// Pure helpers for install/import failures and the portable data folder.
import type { HomeSource } from "./types";

/** The daemon formats errors as `<Step>: <cause>` (for example `Downloading: Get "...": i/o timeout`). */
export function splitJobError(error: string): { step: string; cause: string } {
  const m = /^(Downloading|Importing|Verifying|Extracting|Registering|Starting)\s*:\s*([\s\S]*)$/.exec(error.trim());
  return m ? { step: m[1], cause: m[2] } : { step: "", cause: error.trim() };
}

const NETWORK = [
  /dial tcp/i,
  /i\/o timeout/i,
  /no such host/i,
  /connection (refused|reset)/i,
  /TLS handshake/i,
  /proxyconnect/i,
  /proxy authentication/i,
  /\bEOF\b/,
  /context deadline exceeded/i,
  /network is unreachable/i,
  /\bGet "https?:/,
  /certificate/i,
  /\b(403|407|429|50[0-4])\b/,
];

/** True when a failed download is probably blocked by the network rather than a problem with the file. */
export function isNetworkError(error: string): boolean {
  const { step, cause } = splitJobError(error);
  if (step && step !== "Downloading") return false;
  return NETWORK.some((re) => re.test(cause));
}

/** Failures that look like antivirus holding a freshly extracted file. */
export function isAntivirusError(error: string): boolean {
  return /antivirus|access is denied|sharing violation/i.test(error);
}

export function pathSep(home: string): string {
  return home.includes("\\") ? "\\" : "/";
}

export function joinPath(home: string, ...parts: string[]): string {
  const sep = pathSep(home);
  return [home.replace(/[\\/]+$/, ""), ...parts].join(sep);
}

/** The folder of each tool inside the data folder (model.DataDirName): data/code, data/postgres ... */
export const DATA_DIR_NAMES: Record<string, string> = {
  vscode: "code",
  vscodium: "codium",
  notepadpp: "notepadpp",
  heidisql: "heidisql",
  dbeaver: "dbeaver",
  mariadb: "mariadb",
  mysql: "mysql",
  postgres: "postgres",
  redis: "redis",
};

/** Where a database service keeps its files (supervisor DataDir): <data>/<kind>/<service id>. */
export function dbDataPath(homeDir: string, kind: string, serviceId: string): string {
  return joinPath(homeDir, DATA_DIR_NAMES[kind] ?? kind, serviceId);
}

export function caRootPath(homeDir: string): string {
  return joinPath(homeDir, "caddy", "data", "caddy", "pki", "authorities", "local", "root.crt");
}

export function portableNote(source: HomeSource | undefined): string {
  switch (source) {
    case "exe":
      return "Everything Zero2Dev stores lives in this folder next to the program. Move the whole folder to move Zero2Dev.";
    case "env":
      return "Using the folder from ZERO2DEV_HOME.";
    case "cwd":
      return "Development mode: using ./data";
    default:
      return "";
  }
}

/** Keeps the last `max` lines; the daemon already limits the tail but this guards the UI. */
export function tailLines(lines: string[], max = 200): string[] {
  return lines.length > max ? lines.slice(lines.length - max) : lines;
}
