import { describe, expect, it } from "vite-plus/test";
import { importBody, installBody } from "./api";
import {
  CARD_KINDS,
  RUNTIME_GROUPS,
  addedMessage,
  availability,
  compareVersions,
  defaultSelection,
  entryTags,
  noDownloadText,
  optionGroups,
  optionLabel,
  sortNewestFirst,
  sourceErrors,
  supportedForKind,
  updatesAvailable,
  canMakeDefault,
  defaultOptionLabel,
  runtimeOptionLabel,
  runtimesOfKind,
  serviceRuntimeLabel,
  sourceLabel,
  defaultRuntime,
} from "./catalog";
import { RUNTIME_KINDS, type CatalogEntry, type Runtime } from "./types";

const e = (o: Partial<CatalogEntry>): CatalogEntry => ({
  id: "php-8.3.12",
  kind: "php",
  version: "8.3.12",
  name: "PHP 8.3.12",
  url: "",
  checksum: { type: "sha256", value: "00" },
  binaryRelPath: "php.exe",
  stripComponents: 0,
  os: "windows",
  arch: "amd64",
  homepage: "",
  fileName: "",
  installed: false,
  supported: true,
  line: "8.3",
  channel: "stable",
  source: "builtin",
  latest: false,
  unverified: false,
  ...o,
});
const rt = (o: Partial<Runtime> = {}): Runtime => ({
  id: "rt_1",
  kind: "php",
  version: "8.3.12",
  name: "PHP 8.3.12",
  binary: "C:\\z\\php.exe",
  source: "installed",
  installedAt: "",
  catalogId: "php-8.3.12",
  default: false,
  ...o,
});

const catalog = [
  e({ id: "php-8.4.1", version: "8.4.1", line: "8.4", latest: true }),
  e({ id: "php-8.3.13", version: "8.3.13", line: "8.3", latest: true }),
  e({ id: "php-8.3.12", version: "8.3.12", line: "8.3" }),
  e({ id: "php-7.1.33", version: "7.1.33", line: "7.1", channel: "", latest: true, unverified: true }),
  e({ id: "php-arm", version: "8.4.1", line: "8.4", supported: false, arch: "arm64" }),
  e({
    id: "nginx-1.27.2",
    kind: "nginx",
    version: "1.27.2",
    line: "1.27",
    channel: "mainline",
    latest: true,
    unverified: true,
  }),
  e({ id: "java-21.0.5", kind: "java", version: "21.0.5", line: "21", channel: "lts", latest: true }),
  e({ id: "node-22.11.0", kind: "node", version: "22.11.0", line: "22", channel: "lts", latest: true }),
];

describe("compareVersions / sorting", () => {
  it("compares numerically, not as text", () => {
    expect(compareVersions("8.10.0", "8.9.9")).toBeGreaterThan(0);
    expect(compareVersions("21.0.5", "21.0.10")).toBeLessThan(0);
    expect(compareVersions("1.26", "1.26.0")).toBe(0);
    expect(compareVersions("17.0.13+11", "17.0.13+9")).toBeGreaterThan(0);
  });
  it("sorts newest first", () => {
    expect(sortNewestFirst(catalog.filter((c) => c.kind === "php")).map((c) => c.version)).toEqual([
      "8.4.1",
      "8.4.1",
      "8.3.13",
      "8.3.12",
      "7.1.33",
    ]);
  });
  it("limits to supported entries for the picker", () => {
    expect(supportedForKind(catalog, "php").map((c) => c.id)).toEqual([
      "php-8.4.1",
      "php-8.3.13",
      "php-8.3.12",
      "php-7.1.33",
    ]);
  });
});

describe("tags and groups", () => {
  it("builds tags", () => {
    expect(entryTags(catalog[0], [])).toEqual(["latest", "stable"]);
    expect(entryTags(catalog[2], [rt()])).toEqual(["stable", "installed"]);
    expect(entryTags(catalog[3], [])).toEqual(["latest", "unverified"]);
    expect(entryTags(catalog[5], [])).toEqual(["latest", "mainline", "unverified"]);
    expect(entryTags(catalog[6], [])).toEqual(["latest", "LTS"]);
  });
  it("treats an entry as installed when the daemon says so or a runtime came from it", () => {
    expect(entryTags(e({ installed: true }), [])).toContain("installed");
    expect(entryTags(catalog[2], [rt({ catalogId: undefined })])).not.toContain("installed");
  });
  it("formats option labels", () => {
    expect(optionLabel(catalog[2], [])).toBe("8.3.12 (stable)");
    expect(optionLabel(e({ channel: "" }), [])).toBe("8.3.12");
  });
  it("groups every kind by line, newest line first", () => {
    const g = optionGroups(supportedForKind(catalog, "php"), "php");
    expect(g.map((x) => x.label)).toEqual(["PHP 8.4", "PHP 8.3", "PHP 7.1"]);
    expect(g[1].entries.map((x) => x.version)).toEqual(["8.3.13", "8.3.12"]);
    expect(optionGroups([catalog[5]], "nginx").map((x) => x.label)).toEqual(["nginx 1.27"]);
    expect(optionGroups([e({ line: "" })], "php")[0].label).toBe("");
  });
});

describe("updatesAvailable (never removes anything)", () => {
  it("offers the latest of the line of each catalog-installed runtime", () => {
    expect(updatesAvailable(catalog, [rt()]).map((c) => c.id)).toEqual(["php-8.3.13"]);
  });
  it("covers several installed versions side by side, de-duplicated", () => {
    const rts = [
      rt(),
      rt({ id: "rt_2", version: "7.1.33", catalogId: "php-7.1.33" }),
      rt({ id: "rt_3", catalogId: "php-8.3.12" }),
    ];
    expect(updatesAvailable(catalog, rts).map((c) => c.id)).toEqual(["php-8.3.13"]);
  });
  it("ignores up-to-date, other-line, registered and unknown runtimes", () => {
    expect(updatesAvailable(catalog, [rt({ catalogId: "php-8.3.13", version: "8.3.13" })])).toEqual([]);
    expect(updatesAvailable(catalog, [rt({ catalogId: "gone" })])).toEqual([]);
    expect(updatesAvailable(catalog, [rt({ catalogId: undefined, source: "registered" })])).toEqual([]);
    expect(updatesAvailable(catalog, [])).toEqual([]);
  });
  it("skips an unsupported build", () => {
    const c = catalog.map((x) => (x.id === "php-8.3.13" ? { ...x, supported: false } : x));
    expect(updatesAvailable(c, [rt()])).toEqual([]);
  });
});

describe("picker selection and availability", () => {
  const list = supportedForKind(catalog, "php");
  it("preselects the newest version that is not installed yet", () => {
    expect(defaultSelection(list, [])).toBe("php-8.4.1");
    expect(defaultSelection(list, [rt({ catalogId: "php-8.4.1", version: "8.4.1" })])).toBe("php-8.3.13");
    expect(defaultSelection([], [])).toBe("");
  });
  it("keeps a valid earlier choice", () => {
    expect(defaultSelection(list, [], "php-7.1.33")).toBe("php-7.1.33");
    expect(defaultSelection(list, [], "gone")).toBe("php-8.4.1");
  });
  it("tells check vs unsupported vs ok", () => {
    expect(availability(catalog, "java")).toBe("ok");
    expect(availability([], "go")).toBe("check");
    expect(availability([e({ kind: "nginx", supported: false })], "nginx")).toBe("unsupported");
    expect(noDownloadText("linux", "amd64")).toBe(
      "No download for linux/amd64. Use Detect on this PC or Register by path.",
    );
  });
});

describe("default runtime handling", () => {
  const a = rt({ id: "a", version: "8.4.1", default: true, catalogId: "php-8.4.1" });
  const b = rt({ id: "b", version: "7.1.33", catalogId: "php-7.1.33" });
  const c = rt({ id: "c", version: "8.3.12", source: "registered", catalogId: undefined });
  it("lists newest first with the default pinned on top", () => {
    expect(runtimesOfKind([b, c, a], "php").map((r) => r.id)).toEqual(["a", "c", "b"]);
    expect(runtimesOfKind([b, c, { ...b, id: "n", kind: "node" }], "php").map((r) => r.id)).toEqual(["c", "b"]);
    expect(runtimesOfKind([b, { ...c, default: true }], "php")[0].id).toBe("c");
  });
  it("offers Make default only for a non-default when there is more than one", () => {
    expect(canMakeDefault(b, [a, b])).toBe(true);
    expect(canMakeDefault(a, [a, b])).toBe(false);
    expect(canMakeDefault(b, [b])).toBe(false);
    expect(canMakeDefault(b, [b, { ...a, kind: "node" }])).toBe(false);
  });
  it("labels the source", () => {
    expect(sourceLabel(a)).toBe("installed");
    expect(sourceLabel(c)).toBe("registered by path");
  });
  it("finds the default", () => {
    expect(defaultRuntime([a, b], "php")?.id).toBe("a");
    expect(defaultRuntime([b], "php")).toBeUndefined();
  });
});

describe("service form labels", () => {
  const a = rt({ id: "a", version: "8.4.1", default: true });
  const b = rt({ id: "b", version: "7.1.33" });
  it("labels runtime options with the default suffix", () => {
    expect(runtimeOptionLabel(a)).toBe("PHP 8.4.1 (default)");
    expect(runtimeOptionLabel(b)).toBe("PHP 7.1.33");
    expect(runtimeOptionLabel({ ...b, kind: "nginx", version: "1.26.2" })).toBe("nginx 1.26.2");
  });
  it("names the default in the empty option, or PATH", () => {
    expect(defaultOptionLabel([a, b], "php")).toBe("Default (PHP 8.4.1)");
    expect(defaultOptionLabel([b], "php")).toBe("Default (from PATH)");
    expect(defaultOptionLabel([], "go")).toBe("Default (from PATH)");
  });
});

describe("messages", () => {
  it("formats source errors and refresh results", () => {
    expect(
      sourceErrors([
        {
          kind: "java",
          name: "api.adoptium.net",
          ok: false,
          count: 0,
          error: "could not reach api.adoptium.net",
          checkedAt: "",
        },
        { kind: "php", name: "windows.php.net", ok: true, count: 4, error: "", checkedAt: "" },
        { kind: "nginx", name: "nginx.org", ok: false, count: 0, error: "", checkedAt: "" },
      ]),
    ).toEqual(["Java: could not reach api.adoptium.net", "nginx: could not reach nginx.org"]);
    expect(addedMessage([])).toBe("Already up to date");
    expect(addedMessage(["a"])).toBe("1 new version found");
    expect(addedMessage(["a", "b"])).toBe("2 new versions found");
  });
});

describe("API payloads send only contract fields", () => {
  it("install", () => {
    expect(installBody("php-8.4.1")).toEqual({ catalogId: "php-8.4.1" });
    expect(installBody("php-7.1.33", { acceptUnverified: true })).toEqual({
      catalogId: "php-7.1.33",
      acceptUnverified: true,
    });
    expect(installBody("x", { acceptUnverified: false })).toEqual({ catalogId: "x" });
    expect(installBody("x", { replace: true } as never)).toEqual({ catalogId: "x" });
  });
  it("import", () => {
    expect(importBody({ catalogId: "c", filePath: "C:\\a.zip", sha256: "  " })).toEqual({
      catalogId: "c",
      filePath: "C:\\a.zip",
    });
    expect(importBody({ catalogId: "c", filePath: "C:\\a.zip", sha256: "ab", acceptUnverified: true })).toEqual({
      catalogId: "c",
      filePath: "C:\\a.zip",
      sha256: "ab",
      acceptUnverified: true,
    });
    expect(importBody({ catalogId: "c", filePath: "p", replace: true } as never)).toEqual({
      catalogId: "c",
      filePath: "p",
    });
  });
});

describe("serviceRuntimeLabel", () => {
  it("names the pinned runtime only", () => {
    const r = rt({ id: "b", version: "7.1.33" });
    expect(serviceRuntimeLabel("b", [r])).toBe("PHP 7.1.33");
    expect(serviceRuntimeLabel("", [r])).toBe("");
    expect(serviceRuntimeLabel(undefined, [r])).toBe("");
    expect(serviceRuntimeLabel("gone", [r])).toBe("");
  });
});

describe("runtime groups", () => {
  it("puts every kind in exactly one category", () => {
    const listed = RUNTIME_GROUPS.flatMap((g) => g.kinds);
    expect(new Set(listed).size).toBe(listed.length);
    expect([...listed].sort()).toEqual(RUNTIME_KINDS.filter((k) => k !== "other").sort());
    expect(CARD_KINDS).toEqual(listed);
  });
  it("keeps the web servers apart from the languages", () => {
    const by = Object.fromEntries(RUNTIME_GROUPS.map((g) => [g.id, g.kinds]));
    expect(by.servers).toEqual(["caddy", "nginx"]);
    expect(by.languages).not.toContain("caddy");
  });
});
