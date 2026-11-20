// Mailpit services (0.0.5): where apps send their mail, the web inbox and the .env lines to copy.
import type { Service } from "./types";

/** The SMTP port of a Mailpit service when the setting is not set (profiles.json smtpPort). */
export const DEFAULT_SMTP_PORT = 1025;
/** Mailpit only listens on this PC (the profile's fixed --listen and --smtp). */
export const MAIL_HOST = "127.0.0.1";

export const isMailKind = (kind: string): boolean => kind === "mailpit";

type MailService = Pick<Service, "kind" | "port" | "settings" | "args" | "customArgs">;

/** The value of a flag in an argument list: "--smtp x", "--smtp=x" or the short form "-s x". */
function flagValue(args: string[], long: string, short?: string): string | undefined {
  let found: string | undefined;
  for (let i = 0; i < args.length; i++) {
    const a = args[i];
    if (a === long || (short && a === short)) {
      if (i + 1 < args.length) found = args[i + 1];
      i++;
    } else if (a.startsWith(long + "=")) found = a.slice(long.length + 1);
  }
  return found; // the last one wins, as with Mailpit's own flag parser
}

function parsePort(v: string | undefined): number | null {
  if (v === undefined || !/^\d{1,5}$/.test(v.trim())) return null;
  const n = Number(v);
  return n >= 1 && n <= 65535 ? n : null;
}

/** "127.0.0.1:2525" or ":2525" -> 2525; null when there is no port. */
function portOfAddress(addr: string | undefined): number | null {
  if (!addr) return null;
  const i = addr.lastIndexOf(":");
  return i < 0 ? null : parsePort(addr.slice(i + 1));
}

/**
 * The SMTP port apps send to. With its own command line, the port is read from --smtp; when that
 * is missing it is Mailpit's default 1025. null when the setting is invalid.
 */
export function smtpPort(s: MailService): number | null {
  if (s.customArgs) {
    const addr = flagValue(s.args ?? [], "--smtp", "-s");
    return addr === undefined ? DEFAULT_SMTP_PORT : portOfAddress(addr);
  }
  const v = s.settings?.smtpPort;
  return v === undefined || v === "" ? DEFAULT_SMTP_PORT : parsePort(v);
}

/** The web inbox, e.g. http://127.0.0.1:8025/ (with --webroot when the arguments set one). "" without a port. */
export function webUrl(s: MailService): string {
  if (!s.port || s.port <= 0) return "";
  let root = flagValue(s.args ?? [], "--webroot") ?? "/";
  root = "/" + root.replace(/^\/+|\/+$/g, "");
  if (root !== "/") root += "/";
  return `http://${MAIL_HOST}:${s.port}${root}`;
}

/** The lines for a project's .env (Laravel and most frameworks read these names). */
export function mailEnvLines(port: number, host: string = MAIL_HOST): string[] {
  return ["MAIL_MAILER=smtp", `MAIL_HOST=${host}`, `MAIL_PORT=${port}`];
}

export interface MailInfo {
  host: string;
  /** null when the SMTP port setting is not a valid port. */
  smtpPort: number | null;
  webUrl: string;
  /** Empty when the SMTP port is unknown. */
  envLines: string[];
}

/** Everything the service page shows for a Mailpit service; null for every other kind. */
export function mailInfo(s: MailService): MailInfo | null {
  if (!isMailKind(s.kind)) return null;
  const port = smtpPort(s);
  return { host: MAIL_HOST, smtpPort: port, webUrl: webUrl(s), envLines: port ? mailEnvLines(port) : [] };
}
