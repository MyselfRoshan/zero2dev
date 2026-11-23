import { describe, expect, it } from "vite-plus/test";
import { ApiError } from "./api";
import {
  PROJECT_FILE,
  exportPathError,
  exportSummary,
  importName,
  importState,
  installLine,
  isExistsError,
  planCounts,
  planSource,
  projectFolder,
  suggestExportPath,
} from "./projectfile";
import type { ProjectImportPlan } from "./types";

const plan = (over: Partial<ProjectImportPlan> = {}): ProjectImportPlan => ({
  path: "D:\\work\\shop\\zero2dev.json",
  dir: "D:\\work\\shop",
  name: "Shop",
  services: ["Shop API (PHP, port 8000)"],
  sites: ["https://shop.localhost -> Shop API"],
  installs: [],
  changes: [],
  problems: [],
  warnings: [],
  ...over,
});

describe("suggested export path", () => {
  it("is the project folder plus zero2dev.json", () => {
    expect(suggestExportPath("D:\\work\\shop")).toBe(`D:\\work\\shop\\${PROJECT_FILE}`);
    expect(suggestExportPath("D:/work/shop/")).toBe("D:\\work\\shop\\zero2dev.json");
    expect(suggestExportPath("")).toBe("");
  });
  it("takes the folder of the first service with an absolute one", () => {
    const services = [
      { id: "a", cwd: "" },
      { id: "b", cwd: "relative" },
      { id: "c", cwd: "D:\\work\\shop" },
    ];
    expect(projectFolder({ serviceIds: ["a", "b", "c"] }, services)).toBe("D:\\work\\shop");
    expect(projectFolder({ serviceIds: ["a"] }, services)).toBe("");
  });
  it("asks for an absolute path", () => {
    expect(exportPathError("")).toMatch(/Choose/);
    expect(exportPathError("shop.json")).toMatch(/absolute/);
    expect(exportPathError("D:\\work\\zero2dev.json")).toBe("");
  });
});

describe("export answers", () => {
  it("knows the exists conflict", () => {
    expect(isExistsError(new ApiError(409, "conflict", "D:\\work\\zero2dev.json exists"))).toBe(true);
    expect(isExistsError(new ApiError(409, "conflict", "something else"))).toBe(false);
    expect(isExistsError(new ApiError(404, "not_found", "exists"))).toBe(false);
    expect(isExistsError(new Error("exists"))).toBe(false);
  });
  it("lists the path and the warnings", () => {
    const s = exportSummary({ path: "D:\\w\\zero2dev.json", warnings: ["  APP_KEY left out ", ""] });
    expect(s.saved).toBe("Saved to D:\\w\\zero2dev.json.");
    expect(s.warnings).toEqual(["APP_KEY left out"]);
  });
});

describe("import plan words", () => {
  it("counts what the plan holds", () => {
    expect(planCounts(plan())).toBe("1 service, 1 site");
    expect(
      planCounts(
        plan({
          services: ["a", "b"],
          sites: [],
          installs: [{ kind: "php", version: "8.4.1", catalogId: "x", name: "PHP", reason: "r" }],
        }),
      ),
    ).toBe("2 services, 0 sites, 1 runtime to install");
  });
  it("titles an install without repeating the version", () => {
    expect(installLine({ name: "PHP", version: "8.4.1" })).toBe("PHP 8.4.1");
    expect(installLine({ name: "PHP 8.4.1", version: "8.4.1" })).toBe("PHP 8.4.1");
  });
  it("names the project and its source", () => {
    expect(importName(plan(), "  ")).toBe("Shop");
    expect(importName(plan(), " Mine ")).toBe("Mine");
    expect(planSource(plan())).toBe("zero2dev.json");
  });
});

describe("importState", () => {
  it("needs a path and a checked plan", () => {
    expect(importState("", null, "idle", false).ok).toBe(false);
    expect(importState("D:\\x", null, "idle", false).reason).toMatch(/Check/);
    expect(importState("D:\\x", null, "loading", false).reason).toMatch(/Checking/);
    expect(importState("D:\\x", plan(), "ready", false)).toEqual({ ok: true, reason: "" });
  });
  it("is blocked by a problem", () => {
    const r = importState("D:\\x", plan({ problems: ["A service kind is unknown"] }), "ready", false);
    expect(r.ok).toBe(false);
    expect(r.reason).toMatch(/problems/);
  });
  it("waits while starting", () => {
    expect(importState("D:\\x", plan(), "ready", true).ok).toBe(false);
  });
});
