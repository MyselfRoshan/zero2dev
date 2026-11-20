import { describe, expect, it } from "vite-plus/test";
import {
  catalogEntryFor,
  clonePreview,
  defaultFollowers,
  defaultSnapshotName,
  fmtBytes,
  joinNames,
  nameFromReason,
  ownerServices,
  restoreChanges,
  restoreReadiness,
  sharedFolders,
  snapshotCounts,
  snapshotRuntimes,
  suffixDomain,
  validateDomainSuffix,
  validateSnapshotName,
} from "./snapshots";
import type { CatalogEntry, MissingRuntime, Project, Runtime, Service, Site, Snapshot } from "./types";

const svc = (id: string, o: Partial<Service> = {}) =>
  ({ id, name: id, kind: "node", runtimeId: "", state: "stopped", cwd: "", ...o }) as Service;
const prj = (id: string, serviceIds: string[], siteIds: string[] = []): Project => ({
  id,
  name: id,
  serviceIds,
  siteIds,
});
const rt = (id: string, kind: string, version: string, def = false) => ({ id, kind, version, default: def }) as Runtime;
const site = (id: string, domain: string) => ({ id, name: id, domain }) as Site;

const snap: Snapshot = {
  format: 1,
  id: "snp_1",
  name: "First",
  scope: "project",
  ownerId: "prj_1",
  createdAt: "2026-11-18T10:00:00Z",
  version: "0.0.5",
  sizeBytes: 1024,
  project: prj("prj_1", ["api", "db"], ["site_1"]),
  sites: [site("site_1", "shop.localhost")],
  services: [
    {
      service: svc("api", { cwd: "C:\\work\\shop" }),
      runtime: { kind: "node", version: "26.13.1", catalogId: "node-26.13.1", binary: "", followedDefault: true },
      hasData: false,
      phpIni: false,
    },
    {
      service: svc("db", { kind: "mariadb", cwd: "C:\\work\\shop" }),
      runtime: { kind: "mariadb", version: "11.4.4", catalogId: "mariadb-11.4.4", binary: "", followedDefault: true },
      hasData: true,
      phpIni: false,
    },
  ],
  warnings: [],
};

describe("fmtBytes", () => {
  it("scales", () => {
    expect(fmtBytes(0)).toBe("0 B");
    expect(fmtBytes(512)).toBe("512 B");
    expect(fmtBytes(1536)).toBe("1.5 KB");
    expect(fmtBytes(73400320)).toBe("70 MB");
    expect(fmtBytes(1.25 * 1024 ** 3)).toBe("1.3 GB");
    expect(fmtBytes(NaN)).toBe("0 B");
  });
});

describe("summaries", () => {
  it("lists runtimes once, sorted, and counts", () => {
    expect(snapshotRuntimes(snap)).toEqual(["MariaDB 11.4.4", "Node.js 26.13.1"]);
    expect(snapshotCounts(snap)).toBe("2 services, 1 site");
    expect(snapshotCounts({ services: [snap.services[0]], sites: [] })).toBe("1 service");
    expect(sharedFolders(snap)).toEqual(["C:\\work\\shop"]);
  });
  it("joins names", () => {
    expect(joinNames([])).toBe("");
    expect(joinNames(["a"])).toBe("a");
    expect(joinNames(["a", "b", "c"])).toBe("a, b and c");
  });
});

describe("names", () => {
  it("validates", () => {
    expect(validateSnapshotName("  ")).toMatch(/name/);
    expect(validateSnapshotName("x".repeat(65))).toMatch(/64/);
    expect(validateSnapshotName("Before upgrade")).toBe("");
  });
  it("suggests", () => {
    expect(defaultSnapshotName(new Date(2026, 10, 8, 9, 5))).toBe("Snapshot 2026-11-08 09:05");
    expect(nameFromReason("before importing shop.sql")).toBe("Before importing shop.sql");
    expect(nameFromReason("x".repeat(80)).length).toBeLessThanOrEqual(64);
  });
});

describe("ownerServices", () => {
  it("finds the members in project order, or the one service", () => {
    const all = [svc("a"), svc("b"), svc("c")];
    expect(ownerServices("project", "p", all, [prj("p", ["c", "a", "gone"])]).map((s) => s.id)).toEqual(["c", "a"]);
    expect(ownerServices("service", "b", all, []).map((s) => s.id)).toEqual(["b"]);
    expect(ownerServices("project", "nope", all, [])).toEqual([]);
  });
});

describe("defaultFollowers", () => {
  it("groups the services of the kind without their own version by project", () => {
    const all = [
      svc("api"),
      svc("pinned", { runtimeId: "rt_1" }),
      svc("worker"),
      svc("php", { kind: "php" }),
      svc("loose"),
    ];
    const f = defaultFollowers("node", all, [
      prj("shop", ["api", "pinned", "php"]),
      prj("jobs", ["worker"]),
      prj("none", ["pinned"]),
    ]);
    expect(f.projects.map((g) => [g.project.id, g.services.map((s) => s.id)])).toEqual([
      ["shop", ["api"]],
      ["jobs", ["worker"]],
    ]);
    expect(f.loose.map((s) => s.id)).toEqual(["loose"]);
    expect(f.services).toHaveLength(3);
    expect(defaultFollowers("go", all, []).services).toEqual([]);
  });
});

describe("clone domains", () => {
  it("adds the suffix to the first label and keeps it unique", () => {
    expect(suffixDomain("shop.localhost", "-copy", [])).toBe("shop-copy.localhost");
    expect(suffixDomain("shop.localhost", "-copy", ["SHOP-copy.localhost"])).toBe("shop-copy-2.localhost");
    expect(suffixDomain("shop", "-copy", ["shop-copy", "shop-copy-2"])).toBe("shop-copy-3");
    expect(clonePreview([{ domain: "a.localhost" }, { domain: "a.localhost" }], "-copy", ["a-copy.localhost"])).toEqual(
      [
        { from: "a.localhost", to: "a-copy-2.localhost" },
        { from: "a.localhost", to: "a-copy-3.localhost" },
      ],
    );
  });
  it("validates the suffix", () => {
    expect(validateDomainSuffix("")).not.toBe("");
    expect(validateDomainSuffix("-copy")).toBe("");
    expect(validateDomainSuffix("a.b")).not.toBe("");
  });
});

describe("restore", () => {
  const runtimes = [rt("n1", "node", "26.14.0", true), rt("m1", "mariadb", "11.4.4", true)];
  it("says what changes", () => {
    const now = [svc("api", { port: 1 } as Partial<Service>), svc("new")];
    const lines = restoreChanges(
      snap,
      now,
      [site("site_1", "other.localhost")],
      runtimes,
      prj("prj_1", ["api", "new"]),
    );
    expect(lines).toContain("api gets its settings back.");
    expect(lines).toContain("api runs Node.js 26.13.1 again (now Node.js 26.14.0).");
    expect(lines).toContain("db comes back (it was deleted), with its settings and data.");
    expect(lines).toContain("The site other.localhost is shop.localhost again.");
    expect(lines).toContain("new was added since; taken out of the project, not deleted.");
    expect(restoreChanges(snap, [], [], [], undefined)).toContain("The project prj_1 comes back (it was deleted).");
  });
  it("is ready when nothing is missing and nothing runs, or restart is allowed", () => {
    const missing: MissingRuntime[] = [
      { kind: "node", version: "26.13.1", catalogId: "node-26.13.1", installable: true, services: ["api"] },
    ];
    const running = [svc("api", { state: "running" })];
    expect(restoreReadiness(snap, missing, running, true)).toMatchObject({
      ready: false,
      reason: "Install Node.js 26.13.1 first.",
    });
    expect(restoreReadiness(snap, [], running, false)).toMatchObject({ ready: false, reason: "api must stop first." });
    expect(restoreReadiness(snap, [], running, true).ready).toBe(true);
    expect(restoreReadiness(snap, [], [svc("api", { state: "failed" })], false).ready).toBe(true);
  });
  it("finds the catalog entry of an installable runtime", () => {
    const cat = [
      { id: "node-26.13.1", kind: "node", version: "26.13.1", supported: true },
      { id: "x", kind: "php", version: "8.4.1", supported: true },
    ] as CatalogEntry[];
    const m = (o: Partial<MissingRuntime>): MissingRuntime => ({
      kind: "node",
      version: "26.13.1",
      catalogId: "node-26.13.1",
      installable: true,
      services: [],
      ...o,
    });
    expect(catalogEntryFor(m({}), cat)?.id).toBe("node-26.13.1");
    expect(catalogEntryFor(m({ kind: "php", version: "8.4.1", catalogId: "" }), cat)?.id).toBe("x");
    expect(catalogEntryFor(m({ installable: false }), cat)).toBeUndefined();
  });
});
