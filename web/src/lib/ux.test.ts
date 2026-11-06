import { describe, expect, it } from "vite-plus/test";
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
      rt({ id: "b", binary: "C:\\bin\\node-22\\node.exe", default: true }),
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
  });
  it("tells that database files are kept on delete", () => {
    expect(deleteServiceMessage({ kind: "postgres", id: "svc_1" }, false, "D:/z/data/databases/svc_1")).toContain(
      "kept in D:/z/data/databases/svc_1",
    );
    expect(deleteServiceMessage({ kind: "node", id: "svc_1" }, true, "x")).toMatch(/running.*cannot be undone/);
  });
});
