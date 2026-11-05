import { describe, expect, it } from "vite-plus/test";
import type { Runtime, Service } from "./types";
import {
  commandLine,
  defaultArgsText,
  serviceKindLabel,
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
    expect(defaultArgsText("go")).toMatch(/go run \./);
  });

  it("shows the real command line", () => {
    const runtimes = [
      rt({ id: "a", binary: "C:\\bin\\node-20\\node.exe" }),
      rt({ id: "b", binary: "C:\\bin\\node-22\\node.exe", default: true }),
    ];
    expect(commandLine(svc({ args: ["server.js", "--port", "{port}"] }), runtimes)).toBe(
      "node.exe server.js --port 3000",
    );
    // an explicit runtime wins over the default
    expect(commandLine(svc({ runtimeId: "a", args: ["x.js"] }), runtimes)).toBe("node.exe x.js");
    // no runtime registered: the kind's name on PATH
    expect(commandLine(svc({ kind: "go" }), [])).toBe("go run .");
    expect(commandLine(svc({ kind: "php", port: 8000, cwd: "C:\\my site" }), [])).toBe(
      'php -S 127.0.0.1:8000 -t "C:\\my site"',
    );
    expect(commandLine(svc({ kind: "custom", command: "C:\\tools\\app.exe", args: ["--port={port}"] }), runtimes)).toBe(
      "C:\\tools\\app.exe --port=3000",
    );
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
