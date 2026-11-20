import { describe, expect, it } from "vite-plus/test";
import {
  adoptHref,
  blockerAction,
  canRevert,
  dbTargets,
  defaultMode,
  defaultRevert,
  fmtSize,
  groupSources,
  joinWords,
  noteFor,
  pickedItems,
  preselect,
  requestItems,
  revertText,
  reviewText,
  sourceSummary,
  stateLabel,
  targetText,
  totalSize,
  validateDbCopy,
  validateDbForm,
} from "./migrate";
import type { MigrateSource, Migration, Runtime, Service } from "./types";

const src = (o: Partial<MigrateSource> & { id: string }): MigrateSource => ({
  program: o.id as MigrateSource["program"],
  label: o.id,
  found: true,
  items: [],
  modes: ["copy", "move"],
  copyOnlyReason: "",
  blockers: [],
  targetHasData: false,
  note: "",
  ...o,
});
const item = (label: string, sizeBytes: number, exists = true) => ({
  label,
  kind: "dir" as const,
  from: `C:\\from\\${label}`,
  to: `D:\\to\\${label}`,
  sizeBytes,
  exists,
});
const vscode = src({
  id: "vscode",
  label: "VS Code",
  items: [item("Settings", 2_048_000), item("Extensions", 412_000_000)],
});
const mig = (o: Partial<Migration>): Migration => ({
  id: "mig_1",
  source: "vscode",
  program: "vscode",
  label: "VS Code",
  mode: "copy",
  createdAt: "2026-11-20T10:00:00Z",
  state: "done",
  revertedAt: null,
  items: [],
  revertOptions: ["discard", "copy-back"],
  description: "",
  ...o,
});
const rt = (id: string, kind: Runtime["kind"], source: Runtime["source"] = "registered"): Runtime => ({
  id,
  kind,
  version: "1",
  name: kind,
  binary: "C:\\x.exe",
  source,
  installedAt: "",
});
const svc = (id: string, kind: Service["kind"], port: number, name = id) => ({ id, kind, port, name }) as Service;

describe("sizes", () => {
  it("formats bytes in decimal units", () => {
    expect(fmtSize(0)).toBe("0 bytes");
    expect(fmtSize(512)).toBe("512 bytes");
    expect(fmtSize(2_048_000)).toBe("2.0 MB");
    expect(fmtSize(412_000_000)).toBe("412 MB");
    expect(fmtSize(1_300_000_000)).toBe("1.3 GB");
  });
  it("adds sizes, ignoring unknown ones", () => {
    expect(totalSize([{ sizeBytes: 5 }, { sizeBytes: -1 }, { sizeBytes: 7 }])).toBe(12);
  });
});

describe("grouping", () => {
  it("groups found and missing sources in the order of the groups", () => {
    const g = groupSources([
      src({ id: "database" }),
      src({ id: "notepadpp" }),
      src({ id: "vscodium", found: false }),
      vscode,
      src({ id: "runtime:rt_1", program: "runtime" }),
    ]);
    expect(g.map((x) => x.group.id)).toEqual(["tools", "runtimes", "databases"]);
    expect(g[0].found.map((s) => s.id)).toEqual(["vscode", "notepadpp"]);
    expect(g[0].missing.map((s) => s.id)).toEqual(["vscodium"]);
  });
  it("preselects by id or by runtime id", () => {
    const list = [vscode, src({ id: "runtime:rt_1", program: "runtime", runtimeId: "rt_1" })];
    expect(preselect(list, "vscode")).toBe("vscode");
    expect(preselect(list, "runtime:rt_1")).toBe("runtime:rt_1");
    expect(preselect(list, "runtime:rt_9")).toBe("");
    expect(preselect(list, "")).toBe("");
    expect(adoptHref("rt_1")).toBe("#/migrate?source=runtime%3Art_1");
  });
});

describe("plan", () => {
  it("summarises what a source holds", () => {
    expect(sourceSummary(vscode)).toBe("Settings and Extensions, 414 MB");
    expect(sourceSummary(src({ id: "x", found: false }))).toBe("Not found on this PC");
    expect(sourceSummary(src({ id: "x", items: [item("A", 1, false)] }))).toMatch(/nothing to bring in/);
  });
  it("sends [] when every item is checked", () => {
    expect(requestItems(vscode, ["Settings", "Extensions"])).toEqual([]);
    expect(requestItems(vscode, ["Settings"])).toEqual(["Settings"]);
    expect(pickedItems(vscode, []).length).toBe(2);
    expect(pickedItems(vscode, ["Extensions"]).map((i) => i.label)).toEqual(["Extensions"]);
  });
  it("starts with copy", () => {
    expect(defaultMode(vscode)).toBe("copy");
    expect(defaultMode(src({ id: "x", modes: ["move"] }))).toBe("move");
  });
  it("says what will happen before it happens", () => {
    expect(reviewText(vscode, "copy", vscode.items)).toBe(
      "This copies 414 MB (settings and extensions) of VS Code into Zero2Dev's folder. The originals stay where they are.",
    );
    expect(reviewText(vscode, "move", vscode.items.slice(0, 1))).toMatch(
      /^This moves 2.0 MB \(settings\) of VS Code.*removed/,
    );
    const r = src({ id: "runtime:rt_1", program: "runtime", label: "Node.js 20.11.1" });
    expect(reviewText(r, "copy", [item("Program files", 98_000_000)])).toMatch(
      /^This copies Node.js 20.11.1 \((98 MB)\)/,
    );
    const db = src({ id: "database", label: "Databases" });
    expect(
      reviewText(db, "copy", [], {
        databases: [
          { name: "shop", sizeBytes: 1000 },
          { name: "blog", sizeBytes: 0 },
        ],
        host: "127.0.0.1",
        port: 3306,
        target: "Shop DB",
      }),
    ).toBe(
      "This copies 2 databases (1.0 KB) from 127.0.0.1:3306 into Shop DB. The server you copy from is not changed.",
    );
  });
  it("warns that the target is saved first", () => {
    expect(targetText(vscode)).toBe("");
    expect(targetText({ ...vscode, targetHasData: true })).toMatch(/saved first/);
  });
});

describe("blockers", () => {
  it("turns blockers into actions", () => {
    expect(blockerAction("Install VS Code in Zero2Dev first")).toEqual({
      type: "link",
      href: "#/runtimes",
      label: "Open Runtimes",
    });
    expect(blockerAction("Close VS Code first")).toEqual({ type: "recheck", label: "Check again" });
    expect(blockerAction("Create a MariaDB, MySQL or PostgreSQL service in Zero2Dev first").type).toBe("link");
    expect(blockerAction("Something else")).toEqual({ type: "none" });
  });
});

describe("history", () => {
  it("labels states", () => {
    expect(stateLabel(mig({})).text).toBe("Copied");
    expect(stateLabel(mig({ mode: "move" })).text).toBe("Moved");
    expect(stateLabel(mig({ program: "runtime" })).text).toBe("Adopted");
    expect(stateLabel(mig({ state: "reverted" }))).toEqual({ tone: "idle", text: "Reverted" });
    expect(stateLabel(mig({ state: "failed" })).tone).toBe("bad");
  });
  it("explains each revert choice in plain words", () => {
    expect(revertText("discard", mig({}))).toMatch(/Removes the copy.*not touched/);
    expect(revertText("copy-back", mig({}))).toMatch(/saved first/);
    expect(revertText("move-back", mig({ mode: "move" }))).toMatch(/Puts the moved items back/);
    expect(revertText("discard", mig({ program: "runtime" }))).toMatch(/old place again/);
    expect(revertText("discard", mig({ program: "database", databases: ["shop", "blog"] }), "Shop DB")).toBe(
      "Drops shop and blog from Shop DB. The server you copied from was never changed.",
    );
  });
  it("preselects the revert that loses nothing", () => {
    expect(defaultRevert(mig({}))).toBe("discard");
    expect(defaultRevert(mig({ revertOptions: ["move-back"] }))).toBe("move-back");
    expect(defaultRevert(mig({ revertOptions: [] }))).toBeUndefined();
    expect(canRevert(mig({}))).toBe(true);
    expect(canRevert(mig({ state: "reverted" }))).toBe(false);
  });
});

describe("entry points", () => {
  it("notes an installed editor until it was brought in", () => {
    expect(noteFor("vscode", [vscode], [])).toEqual({
      text: "Your installed VS Code settings can be brought in.",
      href: "#/migrate?source=vscode",
      link: "Bring them in",
    });
    expect(noteFor("vscode", [vscode], [mig({})])).toBeNull();
    expect(noteFor("vscode", [vscode], [mig({ state: "reverted" })])).not.toBeNull();
    expect(noteFor("vscodium", [src({ id: "vscodium", found: false })], [])).toBeNull();
  });
  it("offers to adopt registered runtimes of the card's kind", () => {
    const s = src({ id: "runtime:rt_1", program: "runtime", runtimeId: "rt_1", label: "Node.js 20.11.1" });
    const n = noteFor("node", [s], [], [rt("rt_1", "node")]);
    expect(n?.href).toBe("#/migrate?source=runtime%3Art_1");
    expect(n?.link).toBe("Adopt into Zero2Dev");
    expect(noteFor("php", [s], [], [rt("rt_1", "node")])).toBeNull();
    expect(noteFor("node", [s], [], [rt("rt_1", "node", "installed")])).toBeNull();
  });
});

describe("database form", () => {
  const ok = { kind: "mysql" as const, host: "127.0.0.1", port: "3306", user: "root" };
  it("accepts a usual connection", () => {
    expect(validateDbForm(ok)).toEqual({});
  });
  it("names each problem", () => {
    const e = validateDbForm({ kind: "mysql", host: " ", port: "70000", user: "" });
    expect(Object.keys(e).sort()).toEqual(["host", "port", "user"]);
    expect(validateDbForm({ ...ok, port: "33a" }).port).toMatch(/1 to 65535/);
    expect(validateDbForm({ ...ok, host: "a b" }).host).toBeTruthy();
  });
  it("refuses a Zero2Dev service as the source", () => {
    expect(validateDbForm({ ...ok, port: "3307" }, [svc("s1", "mariadb", 3307, "Shop DB")]).port).toMatch(
      /Shop DB, a Zero2Dev service/,
    );
    expect(validateDbForm({ ...ok, host: "db.lan", port: "3307" }, [svc("s1", "mariadb", 3307)])).toEqual({});
  });
  it("picks targets of the same family and needs a pick", () => {
    const list = [svc("a", "mariadb", 1), svc("b", "postgres", 2), svc("c", "mysql", 3), svc("d", "redis", 4)];
    expect(dbTargets(list, "mysql").map((s) => s.id)).toEqual(["a", "c"]);
    expect(dbTargets(list, "postgres").map((s) => s.id)).toEqual(["b"]);
    expect(validateDbCopy([], "", list)).toEqual({ databases: expect.any(String), target: expect.any(String) });
    expect(validateDbCopy(["shop"], "a", list)).toEqual({});
  });
  it("joins words", () => {
    expect(joinWords(["a"])).toBe("a");
    expect(joinWords(["a", "b", "c"])).toBe("a, b and c");
  });
});
