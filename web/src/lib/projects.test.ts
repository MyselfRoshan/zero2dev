import { describe, expect, it } from "vite-plus/test";
import {
  deleteProjectMessage,
  projectStatus,
  servicesLine,
  sitesLine,
  stateLabel,
  toggleId,
  validateProjectForm,
} from "./projects";
import type { Service, Site } from "./types";

const svc = (id: string, state: Service["state"]) => ({ id, name: id, state }) as Service;
const site = (id: string, enabled: boolean) => ({ id, name: id, enabled }) as Site;

describe("projectStatus", () => {
  const services = [
    svc("a", "running"),
    svc("b", "stopped"),
    svc("c", "starting"),
    svc("d", "failed"),
    svc("x", "running"),
  ];
  const sites = [site("s1", true), site("s2", false)];

  it("counts running, active and failed members only", () => {
    const st = projectStatus({ serviceIds: ["a", "b", "c", "d"], siteIds: ["s1", "s2"] }, services, sites);
    expect(st).toMatchObject({ services: 4, running: 1, active: 2, failed: 1, sites: 2, sitesOn: 1, state: "partial" });
    expect(st.canStart).toBe(true);
    expect(st.canStop).toBe(true);
  });

  it("ignores members that were deleted", () => {
    const st = projectStatus({ serviceIds: ["a", "gone"], siteIds: ["gone"] }, services, sites);
    expect(st).toMatchObject({ services: 1, running: 1, sites: 0, sitesOn: 0, state: "running" });
    expect(st.canStart).toBe(false);
  });

  it("is running only when every member runs", () => {
    expect(projectStatus({ serviceIds: ["a", "x"], siteIds: [] }, services, sites).state).toBe("running");
    expect(projectStatus({ serviceIds: ["a", "b"], siteIds: [] }, services, sites).state).toBe("partial");
  });

  it("is stopped when nothing is active, and empty without services", () => {
    const stopped = projectStatus({ serviceIds: ["b", "d"], siteIds: [] }, services, sites);
    expect(stopped.state).toBe("stopped");
    expect(stopped.canStop).toBe(false);
    expect(projectStatus({ serviceIds: [], siteIds: ["s1"] }, services, sites).state).toBe("empty");
  });

  it("offers Start for a site that is off even when every service runs", () => {
    const st = projectStatus({ serviceIds: ["a"], siteIds: ["s2"] }, services, sites);
    expect(st.state).toBe("running");
    expect(st.canStart).toBe(true);
    expect(st.canStop).toBe(true);
  });

  it("has nothing to start for an empty project", () => {
    const st = projectStatus({ serviceIds: [], siteIds: [] }, services, sites);
    expect(st.canStart).toBe(false);
    expect(st.canStop).toBe(false);
  });
});

describe("labels", () => {
  it("words the counts", () => {
    const st = projectStatus(
      { serviceIds: ["a", "b"], siteIds: ["s1"] },
      [svc("a", "running"), svc("b", "stopped")],
      [site("s1", true)],
    );
    expect(servicesLine(st)).toBe("1 of 2 services running");
    expect(sitesLine(st)).toBe("1 of 1 site on");
    const none = projectStatus({ serviceIds: [], siteIds: [] }, [], []);
    expect(servicesLine(none)).toBe("No services");
    expect(sitesLine(none)).toBe("No sites");
    expect(projectStatus({ serviceIds: ["a"], siteIds: [] }, [svc("a", "running")], []).state).toBe("running");
    expect(servicesLine(projectStatus({ serviceIds: ["a"], siteIds: [] }, [svc("a", "running")], []))).toBe(
      "1 of 1 service running",
    );
  });

  it("names every state", () => {
    expect(["empty", "stopped", "partial", "running"].map((s) => stateLabel(s as never))).toEqual([
      "empty",
      "stopped",
      "partly running",
      "running",
    ]);
  });

  it("explains that deleting keeps services and sites", () => {
    expect(deleteProjectMessage("Shop")).toContain("not touched");
  });
});

describe("toggleId", () => {
  it("adds and removes without mutating", () => {
    const l = ["a"];
    expect(toggleId(l, "b")).toEqual(["a", "b"]);
    expect(toggleId(l, "a")).toEqual([]);
    expect(l).toEqual(["a"]);
  });
});

describe("validateProjectForm", () => {
  it("needs a name of 1 to 64 characters", () => {
    expect(validateProjectForm("  ").name).toBeTruthy();
    expect(validateProjectForm("Shop")).toEqual({});
    expect(validateProjectForm("x".repeat(64))).toEqual({});
    expect(validateProjectForm("x".repeat(65)).name).toBeTruthy();
  });
});
