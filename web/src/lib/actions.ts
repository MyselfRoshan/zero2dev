import { api } from "./api";
import { attempt, loadServices, loadStatus } from "./store.svelte";
import { isDatabase } from "./catalog";
import type { Service } from "./types";

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
