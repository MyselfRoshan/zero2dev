import { describe, expect, it } from "vite-plus/test";
import type { ServiceInput, SiteInput } from "./types";
import {
  envRowsToMap,
  isAbsolutePath,
  parsePort,
  parseSuffixes,
  isValidChecksum,
  validateDomain,
  validateImport,
  validateProxyUrl,
  validateRegister,
  validateService,
  validateSettings,
  validateSite,
} from "./validate";

const svc = (o: Partial<ServiceInput> = {}): ServiceInput => ({
  name: "API",
  kind: "node",
  command: "",
  args: ["server.js"],
  settings: {},
  customArgs: false,
  cwd: "C:\\work\\api",
  env: {},
  port: 3000,
  healthPath: "",
  runtimeId: "",
  autostart: false,
  ...o,
});
const site = (o: Partial<SiteInput> = {}): SiteInput => ({
  name: "Shop",
  domain: "shop.localhost",
  type: "proxy",
  root: "",
  serviceId: "svc_1",
  upstreamPort: 0,
  https: false,
  enabled: true,
  ...o,
});

describe("paths and ports", () => {
  it("recognises absolute paths", () => {
    expect(isAbsolutePath("C:\\work")).toBe(true);
    expect(isAbsolutePath("c:/work")).toBe(true);
    expect(isAbsolutePath("\\\\server\\share")).toBe(true);
    expect(isAbsolutePath("/home/me")).toBe(true);
    expect(isAbsolutePath("work\\app")).toBe(false);
    expect(isAbsolutePath("./app")).toBe(false);
    expect(isAbsolutePath("")).toBe(false);
  });
  it("parses port text", () => {
    expect(parsePort("")).toBe(0);
    expect(parsePort(" 8080 ")).toBe(8080);
    expect(parsePort("80a")).toBeNaN();
    expect(parsePort("-1")).toBeNaN();
  });
});

describe("validateService", () => {
  it("accepts a valid node service", () => {
    expect(validateService(svc())).toEqual({});
  });
  it("checks name length", () => {
    expect(validateService(svc({ name: " " })).name).toBeTruthy();
    expect(validateService(svc({ name: "x".repeat(65) })).name).toBeTruthy();
    expect(validateService(svc({ name: "x".repeat(64) })).name).toBeUndefined();
  });
  it("requires a command for custom services only", () => {
    expect(validateService(svc({ kind: "custom", args: [] })).command).toBeTruthy();
    expect(validateService(svc({ kind: "custom", command: "app.exe", args: [] }))).toEqual({});
    expect(validateService(svc({ kind: "php", args: [] }))).toEqual({});
  });
  it("leaves required settings (such as Node's script) to the settings", () => {
    expect(validateService(svc({ args: [] })).args).toBeUndefined();
  });
  it("requires an absolute folder where the type has one", () => {
    const db = { folderRequired: false, portRequired: true };
    expect(validateService(svc({ cwd: "" })).cwd).toBeTruthy();
    expect(validateService(svc({ cwd: "relative\\dir" })).cwd).toBeTruthy();
    expect(validateService(svc({ kind: "mariadb", cwd: "" }), db).cwd).toBeUndefined();
    expect(validateService(svc({ kind: "mariadb", cwd: "rel" }), db).cwd).toBeTruthy();
  });
  it("requires a port where the type needs one", () => {
    const db = { folderRequired: false, portRequired: true };
    expect(validateService(svc({ kind: "mariadb", cwd: "", port: 0 }), db).port).toBeTruthy();
    expect(validateService(svc({ kind: "mariadb", cwd: "", port: 3306 }), db)).toEqual({});
  });
  it("bounds the port", () => {
    expect(validateService(svc({ port: 0 })).port).toBeUndefined();
    expect(validateService(svc({ port: 65535 })).port).toBeUndefined();
    expect(validateService(svc({ port: 65536 })).port).toBeTruthy();
    expect(validateService(svc({ port: -1 })).port).toBeTruthy();
    expect(validateService(svc({ port: 1.5 })).port).toBeTruthy();
  });
  it("health path must start with / and needs a port", () => {
    expect(validateService(svc({ healthPath: "health" })).healthPath).toBeTruthy();
    expect(validateService(svc({ healthPath: "/health", port: 0 })).healthPath).toBeTruthy();
    expect(validateService(svc({ healthPath: "/health" }))).toEqual({});
  });
});

describe("envRowsToMap", () => {
  it("builds a map and skips untouched rows", () => {
    expect(
      envRowsToMap([
        { key: "A", value: "1" },
        { key: "", value: "" },
      ]).env,
    ).toEqual({ A: "1" });
  });
  it("reports duplicates, empty names and =", () => {
    expect(
      envRowsToMap([
        { key: "A", value: "1" },
        { key: "A", value: "2" },
      ]).error,
    ).toMatch(/twice/);
    expect(envRowsToMap([{ key: "", value: "x" }]).error).toMatch(/empty/);
    expect(envRowsToMap([{ key: "A=B", value: "x" }]).error).toMatch(/=/);
  });
});

describe("domains and sites", () => {
  const sfx = parseSuffixes("localhost");
  it("parses suffix lists", () => {
    expect(parseSuffixes(".localhost, test")).toEqual(["localhost", "test"]);
  });
  it("requires the configured suffix and a lowercase hostname", () => {
    expect(validateDomain("shop.localhost", sfx)).toBeUndefined();
    expect(validateDomain("a.b.localhost", sfx)).toBeUndefined();
    expect(validateDomain("shop.test", sfx)).toMatch(/\.localhost/);
    expect(validateDomain("shop.test", ["localhost", "test"])).toBeUndefined();
    expect(validateDomain("Shop.localhost", sfx)).toMatch(/lowercase/);
    expect(validateDomain("localhost", sfx)).toBeTruthy();
    expect(validateDomain("sh op.localhost", sfx)).toBeTruthy();
    expect(validateDomain("-x.localhost", sfx)).toBeTruthy();
    expect(validateDomain("", sfx)).toBeTruthy();
  });
  it("validates per type", () => {
    expect(validateSite(site(), sfx)).toEqual({});
    expect(validateSite(site({ type: "static", serviceId: "" }), sfx).root).toBeTruthy();
    expect(validateSite(site({ type: "static", serviceId: "", root: "C:\\www" }), sfx)).toEqual({});
    expect(validateSite(site({ serviceId: "", upstreamPort: 0 }), sfx).serviceId).toBeTruthy();
    expect(validateSite(site({ serviceId: "", upstreamPort: 3000 }), sfx)).toEqual({});
    expect(validateSite(site({ serviceId: "", upstreamPort: 70000 }), sfx).upstreamPort).toBeTruthy();
    expect(validateSite(site({ type: "php", serviceId: "" }), sfx).serviceId).toBeTruthy();
  });
});

describe("register and settings", () => {
  it("validates register", () => {
    expect(validateRegister({ kind: "php", binary: "C:\\php\\php.exe" })).toEqual({});
    expect(validateRegister({ kind: "php", binary: "php" }).binary).toBeTruthy();
    expect(validateRegister({ kind: "", binary: "" })).toHaveProperty("kind");
  });
  it("validates settings", () => {
    const ok = { domainSuffix: "localhost", proxyHttpPort: 8080, proxyHttpsPort: 8443 };
    expect(validateSettings(ok)).toEqual({});
    expect(validateSettings({ ...ok, domainSuffix: " " }).domainSuffix).toBeTruthy();
    expect(validateSettings({ ...ok, proxyHttpPort: 0 }).proxyHttpPort).toBeTruthy();
    expect(validateSettings({ ...ok, proxyHttpsPort: 8080 }).proxyHttpsPort).toMatch(/differ/);
  });
});

describe("sha256", () => {
  it("accepts 64 (SHA-256) or 128 (SHA-512) hex characters only", () => {
    expect(isValidChecksum("a".repeat(32))).toBe(true); // MD5
    expect(isValidChecksum("a".repeat(31))).toBe(false);
    expect(isValidChecksum("a".repeat(64))).toBe(true);
    expect(isValidChecksum("a".repeat(128))).toBe(true);
    expect(isValidChecksum("a".repeat(100))).toBe(false);
    expect(isValidChecksum(" " + "AB12".repeat(16) + " ")).toBe(true);
    expect(isValidChecksum("a".repeat(63))).toBe(false);
    expect(isValidChecksum("g".repeat(64))).toBe(false);
    expect(isValidChecksum("")).toBe(false);
  });
});

describe("validateImport", () => {
  const hash = "0".repeat(64);
  it("requires an absolute path", () => {
    expect(validateImport({ filePath: "", sha256: "" }).filePath).toBeTruthy();
    expect(validateImport({ filePath: "Downloads\\x.zip", sha256: "" }).filePath).toMatch(/absolute/);
    expect(validateImport({ filePath: "C:\\Users\\me\\Downloads\\x.zip", sha256: "" })).toEqual({});
  });
  it("sha256 is optional but must be valid when filled", () => {
    expect(validateImport({ filePath: "/tmp/x.zip", sha256: hash })).toEqual({});
    expect(validateImport({ filePath: "/tmp/x.zip", sha256: "abc" }).sha256).toMatch(/64/);
  });
});

describe("validateProxyUrl", () => {
  it("allows empty and http(s) URLs", () => {
    expect(validateProxyUrl("")).toBeUndefined();
    expect(validateProxyUrl("http://proxy.corp:8080")).toBeUndefined();
    expect(validateProxyUrl("https://user:pa55@proxy.corp:3128")).toBeUndefined();
    expect(validateProxyUrl("http://127.0.0.1:8888/")).toBeUndefined();
  });
  it("rejects other forms", () => {
    expect(validateProxyUrl("proxy.corp:8080")).toBeTruthy();
    expect(validateProxyUrl("socks5://proxy:1080")).toMatch(/http/);
    expect(validateProxyUrl("ftp://x")).toBeTruthy();
    expect(validateProxyUrl("http://")).toBeTruthy();
    expect(validateProxyUrl("http://proxy:8080/path")).toBeTruthy();
    expect(validateProxyUrl("not a url")).toBeTruthy();
  });
  it("is part of settings validation", () => {
    const ok = { domainSuffix: "localhost", proxyHttpPort: 8080, proxyHttpsPort: 8443 };
    expect(validateSettings({ ...ok, downloadProxy: "nope" }).downloadProxy).toBeTruthy();
    expect(validateSettings({ ...ok, downloadProxy: "http://p:1" })).toEqual({});
  });
});
