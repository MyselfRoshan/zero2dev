import { describe, expect, it } from "vite-plus/test";
// The same file the daemon embeds and serves at GET /api/kinds.
import profilesJson from "../../../internal/kinds/profiles.json";
import {
  changedKeys,
  checkOption,
  commandText,
  creatableKinds,
  defaultsFor,
  effective,
  friendlyArgs,
  groupedOptions,
  isChanged,
  isVisible,
  panelNotes,
  profileFor,
  resetKeys,
  settingErrors,
  settingsForSave,
  splitCommon,
  versionAtLeast,
  writesLabel,
  type KindProfile,
} from "./kinds";
import { SERVICE_KINDS } from "./types";

const profiles = profilesJson as unknown as KindProfile[];
const get = (k: string) => profileFor(profiles, k)!;

describe("profiles", () => {
  it("cover every service kind", () => {
    for (const k of SERVICE_KINDS) expect(profileFor(profiles, k), k).toBeDefined();
  });
  it("offer the web servers only when editing (Zero2Dev creates them)", () => {
    const c = creatableKinds(profiles);
    expect(c).toContain("node");
    expect(c).toContain("mariadb");
    expect(c).not.toContain("caddy");
    expect(c).not.toContain("nginx");
  });
  it("hide the project folder for databases only", () => {
    expect(get("mariadb").folder.use).toBe("hidden");
    expect(get("redis").folder.use).toBe("hidden");
    expect(get("node").folder.use).toBe("required");
    expect(get("custom").folder.label).toBe("Working folder");
    expect(get("mariadb").health).toBe(false);
  });
  it("defaults pass their own checks", () => {
    for (const p of profiles) {
      for (const o of p.options) expect(checkOption(o, o.default), `${p.kind}.${o.key}`).toBe("");
    }
  });
});

describe("visibility", () => {
  it("follows show conditions, including nested ones", () => {
    const node = get("node");
    const opt = (k: string) => node.options.find((o) => o.key === k)!;
    const s = effective(node, {});
    expect(isVisible(node, opt("script"), s)).toBe(true);
    expect(isVisible(node, opt("npmScript"), s)).toBe(false);
    expect(isVisible(node, opt("inspectPort"), s)).toBe(false);
    expect(isVisible(node, opt("inspectPort"), { ...s, inspect: "true" })).toBe(true);
    // inspectPort needs run=script too
    expect(isVisible(node, opt("inspectPort"), { ...s, inspect: "true", run: "npm" })).toBe(false);
  });
  it("shows Xdebug settings once Xdebug is ticked", () => {
    const php = get("php");
    const mode = php.options.find((o) => o.key === "xdebugMode")!;
    expect(isVisible(php, mode, effective(php, {}))).toBe(false);
    expect(isVisible(php, mode, effective(php, { extensions: "curl,xdebug" }))).toBe(true);
  });
  it("shows the eviction policy only with a memory limit", () => {
    const r = get("redis");
    const pol = r.options.find((o) => o.key === "maxMemoryPolicy")!;
    expect(isVisible(r, pol, effective(r, {}))).toBe(false);
    expect(isVisible(r, pol, effective(r, { maxMemory: "256mb" }))).toBe(true);
  });
  it("groups visible options in order", () => {
    const names = groupedOptions(get("mariadb"), effective(get("mariadb"), {})).map((g) => g.name);
    expect(names).toEqual(["Text", "Behaviour", "Performance", "Logging"]);
  });
});

describe("values", () => {
  it("fills defaults and keeps only known keys for saving", () => {
    const p = get("postgres");
    expect(effective(p, { maxConnections: "20", stale: "x" }).maxConnections).toBe("20");
    expect(effective(p, {}).maxConnections).toBe(defaultsFor(p).maxConnections);
    expect(settingsForSave(p, { maxConnections: "20", stale: "x" })).toEqual({ maxConnections: "20" });
  });
  it("reports required and invalid settings with their label", () => {
    expect(settingErrors(get("node"), {})).toEqual({ script: "Script is required" });
    expect(settingErrors(get("node"), { script: "a.js" })).toEqual({});
    expect(settingErrors(get("node"), { run: "npm" })).toEqual({}); // npmScript defaults to dev
    expect(settingErrors(get("mariadb"), { maxConnections: "0" }).maxConnections).toMatch(
      /Max connections must be between/,
    );
    expect(settingErrors(get("php"), { memoryLimit: "lots" }).memoryLimit).toMatch(/not valid/);
  });
  it("compares versions like the daemon", () => {
    expect(versionAtLeast("20.11.1", "20.6")).toBe(true);
    expect(versionAtLeast("18.9.0", "18.11")).toBe(false);
    expect(versionAtLeast("", "20.6")).toBe(true);
    expect(versionAtLeast("v22.0.0", "18.11")).toBe(true);
  });
});

describe("labels", () => {
  it("says what an option writes", () => {
    const o = (k: string, key: string) => get(k).options.find((x) => x.key === key)!;
    expect(writesLabel(o("mariadb", "maxConnections"))).toBe("--max-connections");
    expect(writesLabel(o("php", "memoryLimit"))).toBe("php.ini memory_limit");
    expect(writesLabel(o("node", "nodeEnv"))).toBe("NODE_ENV");
    expect(writesLabel(o("postgres", "maxConnections"))).toBe("-c max_connections");
    expect(writesLabel(o("redis", "persistence"))).toBe("--appendonly, --save");
    expect(writesLabel(o("redis", "maxMemory"))).toBe("--maxmemory");
    expect(writesLabel(o("java", "jar"))).toBe("-jar");
    expect(writesLabel(o("mariadb", "sqlMode"))).toBe("--sql-mode");
    expect(writesLabel(o("node", "script"))).toBe("");
  });
  it("puts placeholders in words", () => {
    expect(friendlyArgs(["--datadir={data}", "--port={port}"])).toBe("--datadir=<service data folder> --port=<port>");
    expect(friendlyArgs(["--save", ""])).toBe('--save ""');
  });
  it("quotes command lines for display", () => {
    expect(commandText("C:\\Program Files\\php\\php.exe", ["-t", "C:\\my site", ""])).toBe(
      '"C:\\Program Files\\php\\php.exe" -t "C:\\my site" ""',
    );
  });
});

describe("common settings", () => {
  it("every type with settings has some up front and a summary", () => {
    for (const p of profiles) {
      expect(p.summary, p.kind).toBeTruthy();
      if (p.options.length)
        expect(
          p.options.some((o) => o.common),
          p.kind,
        ).toBe(true);
    }
  });
  it("keeps the rest under More settings, grouped", () => {
    const { common, more } = splitCommon(get("mariadb"), {});
    expect(common.map((o) => o.key)).toEqual(["charset", "sqlMode", "maxConnections"]);
    expect(more.map((g) => g.name)).toEqual(["Text", "Performance", "Logging"]);
  });
  it("brings changed and failing settings up front", () => {
    const p = get("mariadb");
    expect(splitCommon(p, { bufferPool: "1G" }).common.map((o) => o.key)).toContain("bufferPool");
    expect(splitCommon(p, {}, { collation: "bad" }).common.map((o) => o.key)).toContain("collation");
    // a setting saved with its default value is not "changed"
    expect(splitCommon(p, { bufferPool: "128M" }).common.map((o) => o.key)).not.toContain("bufferPool");
  });
  it("finds changed settings and resets them", () => {
    const p = get("php");
    const s = {
      memoryLimit: "512M",
      extensions: "zip,intl,curl,fileinfo,mbstring,openssl,mysqli,pdo_mysql,pdo_sqlite,sqlite3",
    };
    expect(changedKeys(p, s)).toEqual(["memoryLimit"]); // same extensions in another order
    expect(resetKeys(s, ["memoryLimit"])).toEqual({ extensions: s.extensions });
    expect(
      isChanged(
        p.options.find((o) => o.key === "displayErrors")!,
        "false",
      ),
    ).toBe(true);
  });
});

describe("panel notes", () => {
  it("says what to do in plain words", () => {
    const label = (k: string) => ({ mariadb: "MariaDB" })[k] ?? k;
    expect(panelNotes(["Script is required"], label)).toEqual([
      { level: "todo", text: "Fill in Script to complete the command line." },
    ]);
    expect(panelNotes(["no mariadb runtime registered and none found on PATH"], label)[0]).toMatchObject({
      level: "warn",
      text: expect.stringContaining("No MariaDB is installed yet"),
    });
    expect(panelNotes(["program x not found"], label)[0].text).toBe("Program x not found");
    expect(panelNotes(["no program set"], label)[0]).toMatchObject({
      level: "todo",
      text: expect.stringContaining("Program"),
    });
  });
});
