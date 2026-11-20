import { describe, expect, it } from "vite-plus/test";
import { sitesBulkMessage, startAllMessage, stopAllMessage } from "./ux";
import type { Runtime, Service } from "./types";
import {
  connectionInfo,
  deleteServiceMessage,
  dbToolsFor,
  editorsFor,
  serviceKindLabel,
  serviceRuntime,
  setupSteps,
  siteTargetOf,
  siteTypeFor,
  suggestDomain,
} from "./ux";

const svc = (over: Partial<Service>): Service => ({
  id: "svc_1",
  name: "x",
  kind: "node",
  command: "",
  args: [],
  settings: {},
  customArgs: false,
  cwd: "C:\\work\\app",
  env: {},
  port: 3000,
  healthPath: "",
  runtimeId: "",
  autostart: false,
  dependsOn: [],
  databaseId: "",
  envFile: "",
  restartOnCrash: false,
  restartLimit: 5,
  restarts: 0,
  state: "stopped",
  pid: 0,
  startedAt: null,
  lastError: "",
  ...over,
});
const rt = (over: Partial<Runtime>): Runtime =>
  ({
    id: "rt_1",
    kind: "node",
    version: "22",
    name: "",
    binary: "C:\\bin\\node\\node.exe",
    source: "installed",
    default: false,
    installedAt: "",
    ...over,
  }) as Runtime;

describe("setupSteps", () => {
  it("walks runtime -> service -> site", () => {
    expect(setupSteps({ runtimes: 0, services: 0, sites: 0 }).next?.id).toBe("runtime");
    expect(setupSteps({ runtimes: 1, services: 0, sites: 0 }).next?.id).toBe("service");
    const s = setupSteps({ runtimes: 1, services: 2, sites: 0 });
    expect(s.next?.id).toBe("site");
    expect(s.next?.optional).toBe(true);
    expect(setupSteps({ runtimes: 1, services: 1, sites: 1 }).complete).toBe(true);
  });
});

describe("services", () => {
  it("labels kinds for people", () => {
    expect(serviceKindLabel("node")).toBe("Node.js");
    expect(serviceKindLabel("custom")).toBe("Custom command");
  });

  it("finds the runtime a service uses", () => {
    const runtimes = [
      rt({ id: "a", binary: "C:\\bin\\node-20\\node.exe" }),
      rt({ id: "b", binary: "C:\\bin\\node-26\\node.exe", default: true }),
    ];
    expect(serviceRuntime(svc({}), runtimes)?.id).toBe("b");
    expect(serviceRuntime(svc({ runtimeId: "a" }), runtimes)?.id).toBe("a");
    expect(serviceRuntime(svc({ kind: "go" }), runtimes)).toBeUndefined();
  });
});

describe("sites", () => {
  it("maps between the form's target and the API type", () => {
    expect(siteTargetOf({ type: "static", serviceId: "" })).toBe("folder");
    expect(siteTargetOf({ type: "proxy", serviceId: "svc_1" })).toBe("service");
    expect(siteTargetOf({ type: "php", serviceId: "svc_1" })).toBe("service");
    expect(siteTargetOf({ type: "proxy", serviceId: "" })).toBe("port");
    expect(siteTypeFor("folder")).toBe("static");
    expect(siteTypeFor("port")).toBe("proxy");
    expect(siteTypeFor("service", "node")).toBe("proxy");
    expect(siteTypeFor("service", "php")).toBe("php");
  });

  it("suggests a domain from the name", () => {
    expect(suggestDomain("My Shop!", "localhost")).toBe("my-shop.localhost");
    expect(suggestDomain("  Café API ", "test")).toBe("cafe-api.test");
    expect(suggestDomain("!!!", "localhost")).toBe("");
  });
});

describe("open in", () => {
  const r = (id: string, kind: string, def = false) => ({ ...rt({ id, kind: kind as Runtime["kind"] }), default: def });
  it("offers one editor per kind, the default version first choice", () => {
    const list = [r("a", "notepadpp"), r("b", "vscode"), r("c", "vscode", true), r("d", "node")];
    expect(editorsFor(list).map((x) => x.id)).toEqual(["c", "a"]);
  });
  it("offers database tools only for databases they can open", () => {
    const list = [r("h", "heidisql"), r("d", "dbeaver"), r("c", "vscode")];
    expect(dbToolsFor(list, "postgres").map((x) => x.id)).toEqual(["h", "d"]);
    expect(dbToolsFor(list, "redis")).toEqual([]);
    expect(dbToolsFor(list, "node")).toEqual([]);
  });
});

describe("databases", () => {
  it("builds connection details for local databases", () => {
    expect(connectionInfo({ kind: "mariadb", port: 3307 })?.url).toBe("mysql://root@127.0.0.1:3307");
    expect(connectionInfo({ kind: "postgres", port: 5432 })).toMatchObject({
      user: "postgres",
      url: "postgresql://postgres@127.0.0.1:5432/postgres",
    });
    expect(connectionInfo({ kind: "redis", port: 6379 })).toMatchObject({ user: "", url: "redis://127.0.0.1:6379" });
    expect(connectionInfo({ kind: "node", port: 3000 })).toBeNull();
    expect(connectionInfo({ kind: "mysql", port: 0 })).toBeNull();
    expect(connectionInfo({ kind: "mariadb", port: 3307 })).toMatchObject({
      password: "",
      maskedUrl: "mysql://root@127.0.0.1:3307",
    });
    expect(connectionInfo({ kind: "mariadb", port: 3307 })?.hint).toMatch(/^No password/);
  });
  it("puts the administrator password in the URL, escaped, and masks it for the screen", () => {
    const c = connectionInfo({ kind: "mariadb", port: 3307, adminPassword: "p@ss/w:rd" });
    expect(c).toMatchObject({
      user: "root",
      password: "p@ss/w:rd",
      url: "mysql://root:p%40ss%2Fw%3Ard@127.0.0.1:3307",
      maskedUrl: "mysql://root:********@127.0.0.1:3307",
    });
    expect(c?.hint).toMatch(/^Password set: apps get it in DB_PASSWORD/);
    expect(connectionInfo({ kind: "postgres", port: 5432, adminPassword: "pw" })).toMatchObject({
      url: "postgresql://postgres:pw@127.0.0.1:5432/postgres",
    });
    expect(connectionInfo({ kind: "postgres", port: 5432, adminPassword: "pw" })?.hint).toContain("without it");
    expect(connectionInfo({ kind: "redis", port: 6379, adminPassword: "pw" })).toMatchObject({
      url: "redis://:pw@127.0.0.1:6379",
      maskedUrl: "redis://:********@127.0.0.1:6379",
    });
    expect(connectionInfo({ kind: "redis", port: 6379, adminPassword: "" })?.url).toBe("redis://127.0.0.1:6379");
  });
  it("tells that database files are kept on delete", () => {
    expect(deleteServiceMessage({ kind: "postgres", id: "svc_1" }, false, "D:/z/data/databases/svc_1")).toContain(
      "kept in D:/z/data/databases/svc_1",
    );
    expect(deleteServiceMessage({ kind: "node", id: "svc_1" }, true, "x")).toMatch(/running.*cannot be undone/);
  });
});

describe("runtime view preference", () => {
  it("handles a throwing localStorage getter", async () => {
    const { getRuntimeView, setRuntimeView } = await import("./ux");
    const original = Object.getOwnPropertyDescriptor(globalThis, "localStorage");
    Object.defineProperty(globalThis, "localStorage", {
      configurable: true,
      get() {
        throw new Error("blocked");
      },
    });
    try {
      expect(getRuntimeView()).toBe("list");
      expect(() => setRuntimeView("grid")).not.toThrow();
    } finally {
      if (original) Object.defineProperty(globalThis, "localStorage", original);
      else Reflect.deleteProperty(globalThis, "localStorage");
    }
  });
  it("remembers list or grid, defaulting to list", async () => {
    const { getRuntimeView, setRuntimeView } = await import("./ux");
    const mem = new Map<string, string>();
    const st = { getItem: (k: string) => mem.get(k) ?? null, setItem: (k: string, v: string) => void mem.set(k, v) };
    expect(getRuntimeView(st)).toBe("list");
    setRuntimeView("grid", st);
    expect(getRuntimeView(st)).toBe("grid");
    const broken = {
      getItem: () => {
        throw new Error("blocked");
      },
      setItem: () => {
        throw new Error("blocked");
      },
    };
    expect(getRuntimeView(broken)).toBe("list");
    expect(() => setRuntimeView("grid", broken)).not.toThrow();
  });
});

describe("stopAllMessage", () => {
  it("says so when nothing ran", () => {
    expect(stopAllMessage({ stopped: [], failed: [] })).toEqual({ kind: "info", text: "No services were running." });
  });
  it("counts what stopped", () => {
    expect(stopAllMessage({ stopped: ["a"], failed: [] }).text).toBe("Stopped 1 service.");
    expect(stopAllMessage({ stopped: ["a", "b"], failed: [] })).toEqual({
      kind: "success",
      text: "Stopped 2 services.",
    });
  });
  it("names the services that did not stop, at most three", () => {
    const f = (name: string) => ({ id: `svc_${name}`, name, error: "did not stop in time" });
    const m = stopAllMessage({ stopped: ["x"], failed: ["a", "b", "c", "d", "e"].map(f) });
    expect(m.kind).toBe("error");
    expect(m.text).toBe("1 stopped. Could not stop: a, b, c, 2 more. did not stop in time");
  });
});

describe("startAllMessage", () => {
  it("says so when everything already runs", () => {
    expect(startAllMessage({ started: [], failed: [] })).toEqual({
      kind: "info",
      text: "Every service is already running.",
    });
  });
  it("counts what started", () => {
    expect(startAllMessage({ started: ["a"], failed: [] }).text).toBe("Started 1 service.");
    expect(startAllMessage({ started: ["a", "b", "c"], failed: [] })).toEqual({
      kind: "success",
      text: "Started 3 services.",
    });
  });
  it("names the services that did not start, at most three", () => {
    const f = (name: string) => ({ id: `svc_${name}`, name, error: "port 5432 is already in use" });
    const m = startAllMessage({ started: ["x"], failed: ["db", "api", "web", "w2", "w3"].map(f) });
    expect(m.kind).toBe("error");
    expect(m.text).toBe("1 started. Could not start: db, api, web, 2 more. port 5432 is already in use");
  });
});

describe("sitesBulkMessage", () => {
  it("says so when nothing changed", () => {
    expect(sitesBulkMessage({ changed: 0 }, true)).toEqual({ kind: "info", text: "All sites are already enabled." });
    expect(sitesBulkMessage({ changed: 0 }, false).text).toBe("All sites are already disabled.");
  });
  it("counts the sites that changed", () => {
    expect(sitesBulkMessage({ changed: 1 }, true)).toEqual({ kind: "success", text: "Enabled 1 site." });
    expect(sitesBulkMessage({ changed: 4 }, false)).toEqual({ kind: "success", text: "Disabled 4 sites." });
  });
});
