import { api } from "./api";
import { attempt, loadServices, loadSites, loadStatus, toast } from "./store.svelte";
import { isDatabase } from "./catalog";
import type { Service } from "./types";
import { sitesBulkMessage, startAllMessage, stopAllMessage } from "./ux";

export const isActive = (s: Service) => s.state === "running" || s.state === "starting";

// The 202 body of start/stop/restart is a snapshot taken before the daemon's own
// `service.status` events, and a fast stop can deliver those events first. Applying the
// body would put a stale state back ("stopping" forever), so we refetch the list instead.
async function afterAction(r: unknown): Promise<void> {
  if (r) await loadServices();
}

export async function startService(s: Service): Promise<void> {
  await afterAction(await attempt(() => api.startService(s.id)));
}
export async function stopService(s: Service): Promise<void> {
  await afterAction(await attempt(() => api.stopService(s.id)));
}
/**
 * Starts every service that is not running (or those of one project) in one request. The daemon
 * does the ordering (databases before the apps that need them) and answers when they are up.
 * One summary toast; the Dashboard, the Services page and the project pages all use this.
 */
export async function startAll(projectId?: string): Promise<void> {
  const r = await attempt(() => api.startAllServices(projectId));
  if (!r) return;
  await loadServices();
  const m = startAllMessage(r);
  toast(m.kind, m.text);
}
/**
 * Stops every running service (or those of one project) in one request. The daemon does the
 * ordering (apps, then databases, then the site server) and answers when they are down.
 */
export async function stopAll(projectId?: string): Promise<void> {
  const r = await attempt(() => api.stopAllServices(projectId));
  if (!r) return;
  await loadServices();
  const m = stopAllMessage(r);
  toast(m.kind, m.text);
}
/**
 * Switches every site (or those of one project) on or off with one config write and one site
 * server restart, then tells the user how many changed.
 */
export async function setAllSites(on: boolean, projectId?: string): Promise<void> {
  const r = await attempt(() => (on ? api.enableAllSites(projectId) : api.disableAllSites(projectId)));
  if (!r) return;
  await loadSites();
  const m = sitesBulkMessage(r, on);
  toast(m.kind, m.text);
}
/** Opens a console in the service's folder with the environment the service runs with. */
export async function openTerminal(s: Service): Promise<void> {
  await attempt(() => api.openTerminal(s.id), "Terminal opened.");
}
export async function restartService(s: Service): Promise<void> {
  await afterAction(await attempt(() => api.restartService(s.id)));
}
export async function deleteService(s: Service): Promise<boolean> {
  const ok = await attempt(async () => {
    await api.deleteService(s.id);
    return true;
  }, `${s.name} deleted.`);
  if (ok) {
    await loadServices();
    void loadStatus();
  }
  return !!ok;
}

/** The browser address of a service; databases have none (they are not web servers). */
export function serviceUrl(s: Service): string | null {
  return s.port > 0 && !isDatabase(s.kind) ? `http://127.0.0.1:${s.port}` : null;
}
