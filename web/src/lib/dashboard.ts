// Pure helpers behind the dashboard: the overview numbers, the one-line status and the
// recent activity across services.
import { isDatabase, updatesAvailable } from "./catalog";
import { isSiteServerService, serverLabel, siteServer } from "./sites";
import type { CatalogEntry, LogLine, Runtime, Service, Settings, Site } from "./types";

export interface Overview {
  services: { total: number; running: number; failed: number; states: Service["state"][] };
  sites: { total: number; enabled: number; server: string; serverRunning: boolean; serverInstalled: boolean };
  runtimes: { kinds: number; versions: number; updates: number };
  databases: { total: number; running: number };
}

const up = (s: Service) => s.state === "running";

export function overview(
  d: { services: Service[]; sites: Site[]; runtimes: Runtime[]; catalog: CatalogEntry[] },
  settings: Pick<Settings, "siteServer"> | null | undefined,
): Overview {
  const server = siteServer(settings);
  const serverSvc = d.services.filter((s) => isSiteServerService(s, server));
  const apps = d.services;
  const dbs = apps.filter((s) => isDatabase(s.kind));
  return {
    services: {
      total: apps.length,
      running: apps.filter(up).length,
      failed: apps.filter((s) => s.state === "failed").length,
      states: apps.map((s) => s.state),
    },
    sites: {
      total: d.sites.length,
      enabled: d.sites.filter((s) => s.enabled).length,
      server: serverLabel(server),
      serverRunning: serverSvc.some(up),
      serverInstalled: d.runtimes.some((r) => r.kind === server),
    },
    runtimes: {
      kinds: new Set(d.runtimes.map((r) => r.kind)).size,
      versions: d.runtimes.length,
      updates: updatesAvailable(d.catalog, d.runtimes).length,
    },
    databases: { total: dbs.length, running: dbs.filter(up).length },
  };
}

/** "2 of 3 services running · 1 failed · sites served by Caddy (stopped) · 1 update available" */
export function statusSentence(o: Overview): string {
  const parts: string[] = [];
  if (o.services.total === 0) parts.push("No services yet");
  else parts.push(`${o.services.running} of ${o.services.total} service${o.services.total === 1 ? "" : "s"} running`);
  if (o.services.failed) parts.push(`${o.services.failed} failed`);
  if (o.sites.total) parts.push(`sites served by ${o.sites.server} (${o.sites.serverRunning ? "running" : "stopped"})`);
  if (o.runtimes.updates) parts.push(`${o.runtimes.updates} update${o.runtimes.updates === 1 ? "" : "s"} available`);
  return parts.join(" · ");
}

export interface Activity extends LogLine {
  serviceId: string;
  serviceName: string;
  seq: number;
}

/** The newest n log lines across all services, newest first. */
export function recentActivity(
  byService: Record<string, (LogLine & { seq: number })[]>,
  services: Pick<Service, "id" | "name">[],
  n = 8,
): Activity[] {
  const names = new Map(services.map((s) => [s.id, s.name]));
  const all: Activity[] = [];
  for (const [id, lines] of Object.entries(byService)) {
    const name = names.get(id);
    if (!name) continue;
    for (const l of lines.slice(-n)) all.push({ ...l, serviceId: id, serviceName: name });
  }
  all.sort((a, b) => (a.ts === b.ts ? b.seq - a.seq : a.ts < b.ts ? 1 : -1));
  return all.slice(0, n);
}

/** The latest output line of a service, for its dashboard card. */
export function lastLine(lines: LogLine[] | undefined): LogLine | undefined {
  if (!lines) return undefined;
  for (let i = lines.length - 1; i >= 0; i--) if (lines[i].text.trim()) return lines[i];
  return undefined;
}
