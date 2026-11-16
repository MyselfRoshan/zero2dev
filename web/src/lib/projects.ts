// Pure helpers behind the Projects page and the Dashboard's projects section: how many of a
// project's services run, how many of its sites are on, and what its buttons may do.
import type { Project, Service, Site } from "./types";

export type ProjectState = "empty" | "stopped" | "partial" | "running";

export interface ProjectStatus {
  /** Services of the project that still exist (a deleted member is ignored). */
  services: number;
  /** Of those, running. */
  running: number;
  /** Of those, running or starting: what Stop would act on. */
  active: number;
  failed: number;
  sites: number;
  sitesOn: number;
  state: ProjectState;
  /** Start has something to do: a service is not active, or a site is off. */
  canStart: boolean;
  /** Stop has something to do: a service is running or starting. */
  canStop: boolean;
}

export function projectStatus(
  p: Pick<Project, "serviceIds" | "siteIds">,
  services: Service[],
  sites: Site[],
): ProjectStatus {
  const ids = new Set(p.serviceIds);
  const members = services.filter((s) => ids.has(s.id));
  const siteIds = new Set(p.siteIds);
  const own = sites.filter((s) => siteIds.has(s.id));
  const running = members.filter((s) => s.state === "running").length;
  const active = members.filter((s) => s.state === "running" || s.state === "starting").length;
  const failed = members.filter((s) => s.state === "failed").length;
  const sitesOn = own.filter((s) => s.enabled).length;
  let state: ProjectState = "stopped";
  if (members.length === 0) state = "empty";
  else if (running === members.length) state = "running";
  else if (active > 0) state = "partial";
  return {
    services: members.length,
    running,
    active,
    failed,
    sites: own.length,
    sitesOn,
    state,
    canStart: active < members.length || sitesOn < own.length,
    canStop: active > 0,
  };
}

const plural = (n: number, one: string, many = `${one}s`) => `${n} ${n === 1 ? one : many}`;

/** "2 of 3 services running" / "No services". */
export function servicesLine(st: ProjectStatus): string {
  if (st.services === 0) return "No services";
  return `${st.running} of ${plural(st.services, "service")} running`;
}

/** "2 of 2 sites on" / "No sites". */
export function sitesLine(st: ProjectStatus): string {
  if (st.sites === 0) return "No sites";
  return `${st.sitesOn} of ${plural(st.sites, "site")} on`;
}

/** The word shown next to a project's dot: always paired with a colour, never colour alone. */
export function stateLabel(state: ProjectState): string {
  return { empty: "empty", stopped: "stopped", partial: "partly running", running: "running" }[state];
}

/** What deleting a project does, in plain words. */
export function deleteProjectMessage(name: string): string {
  return `${name} is removed from the list. Its services and sites are not touched and keep running if they are.`;
}

/** Adds the id to the list, or removes it when it is there (a checkbox group). */
export function toggleId(list: string[], id: string): string[] {
  return list.includes(id) ? list.filter((x) => x !== id) : [...list, id];
}

/** Problems with the form fields, keyed by field; empty when it can be saved. */
export function validateProjectForm(name: string): { name?: string } {
  const n = Array.from(name.trim()).length;
  if (n === 0) return { name: "Enter a name." };
  if (n > 64) return { name: "Use 64 characters or fewer." };
  return {};
}
