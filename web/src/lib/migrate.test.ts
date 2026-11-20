import { describe, expect, it } from "vite-plus/test";
import {
  adoptHref,
  adviceFor,
  backupTotal,
  batchBusy,
  batchSummary,
  blockerAction,
  buildBatch,
  canRevert,
  cleanupText,
  dbTargets,
  defaultChecked,
  defaultMode,
  defaultRevert,
  filterHistory,
  fmtSize,
  freeSpace,
  groupSources,
  joinWords,
  nextWaiting,
  noCleanupReason,
  noteFor,
  pickedItems,
  preselect,
  recommended,
  requestItems,
  revertText,
  reviewText,
  selectable,
  sourceStatus,
  sourceSummary,
  stateLabel,
  summary,
  targetText,
  tookText,
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
  targetFreeBytes: 0,
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
  finishedAt: null,
  backupBytes: 0,
  canCleanup: false,
  originalsBytes: 0,
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
    expect(sourceSummary(src({ id: "x", items: [item("A", 1, false)] }))).toMatch(/nothing to import/);
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
    // the daemon's own wording (internal/migrate)
    expect(blockerAction("Add a MariaDB, MySQL or PostgreSQL service in Zero2Dev first")).toEqual({
      type: "link",
      href: "#/services/new",
      label: "New service",
    });
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
  it("notes an installed editor until it was imported", () => {
    expect(noteFor("vscode", [vscode], [])).toEqual({
      text: "Your installed VS Code settings can be imported.",
      href: "#/migrate?source=vscode",
      link: "Import them",
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

// ---------- 0.0.5: guidance, status, free space, batches, history ----------

const npmCache = src({ id: "npm-cache", items: [item("Download cache", 1_300_000_000)] });
const goCache = src({
  id: "go-cache",
  items: [item("Module cache", 900_000_000), item("Build cache", 1_100_000_000)],
});
const notepad = src({ id: "notepadpp", items: [item("Settings", 1_600_000)] });

describe("what is worth importing", () => {
  it("recommends settings and extensions, skips the npm cache, leaves the Go cache optional", () => {
    expect(adviceFor(vscode, vscode.items[0]).level).toBe("recommended");
    expect(adviceFor(vscode, vscode.items[1]).reason).toMatch(/exact versions/);
    expect(adviceFor(npmCache).level).toBe("skip");
    expect(adviceFor(goCache).level).toBe("optional");
    expect(adviceFor(src({ id: "composer" })).level).toBe("recommended");
    expect(adviceFor(src({ id: "runtime:rt_1", program: "runtime" })).level).toBe("optional");
  });
  it("ticks the recommended items, or all when there is one item or none is recommended", () => {
    expect(defaultChecked(vscode)).toEqual(["Settings", "Extensions"]);
    expect(defaultChecked(goCache)).toEqual(["Module cache", "Build cache"]);
    expect(defaultChecked(npmCache)).toEqual(["Download cache"]);
    expect(defaultChecked(src({ id: "vscode", items: [item("Settings", 1), item("Gone", 1, false)] }))).toEqual([
      "Settings",
    ]);
  });
  it("lists only what can start now", () => {
    const blocked = src({
      id: "dbeaver",
      blockers: ["Install DBeaver in Zero2Dev first"],
      items: [item("Workspace", 5)],
    });
    const list = recommended(
      [vscode, npmCache, blocked, notepad],
      [mig({ source: "notepadpp", program: "notepadpp" })],
    );
    expect(list.map((r) => r.source.id)).toEqual(["vscode"]);
    expect(list[0].bytes).toBe(414_048_000);
  });
});

describe("status of a source", () => {
  it("shows copied, moved, adopted, not ready or not imported", () => {
    const blocked = src({ id: "dbeaver", blockers: ["Close DBeaver first"] });
    expect(sourceStatus(vscode, [])).toMatchObject({ key: "new", text: "Not imported" });
    expect(sourceStatus(blocked, [])).toMatchObject({ key: "blocked", text: "Not ready", tone: "idle" });
    expect(sourceStatus(vscode, [mig({})])).toMatchObject({ key: "copied", text: "Copied" });
    expect(sourceStatus(vscode, [mig({ mode: "move" })]).key).toBe("moved");
    const rtSrc = src({ id: "runtime:rt_1", program: "runtime" });
    expect(sourceStatus(rtSrc, [mig({ source: rtSrc.id, program: "runtime" })]).key).toBe("adopted");
    // reverted: it can be imported again
    expect(sourceStatus(vscode, [mig({ state: "reverted" })]).key).toBe("new");
  });
  it("can be ticked for a batch only when it is found, ready and not imported", () => {
    expect(selectable(vscode, [])).toBe(true);
    expect(selectable(vscode, [mig({})])).toBe(false);
    expect(selectable(src({ id: "database" }), [])).toBe(false);
    expect(selectable(src({ id: "x", blockers: ["Close X first"] }), [])).toBe(false);
    expect(selectable(src({ id: "x", found: false }), [])).toBe(false);
  });
});

describe("summary", () => {
  it("counts what was found, what can come in, what is in and the backups", () => {
    const s = summary(
      [vscode, notepad, src({ id: "vscodium", found: false }), src({ id: "database" })],
      [
        mig({ source: "notepadpp", program: "notepadpp", backupBytes: 3_000_000 }),
        mig({ id: "mig_2", state: "reverted", backupBytes: 1_000_000 }),
      ],
    );
    expect(s).toEqual({ found: 2, bringable: 414_048_000, broughtIn: 1, backups: 4_000_000 });
    expect(backupTotal([mig({ backupBytes: -5 })])).toBe(0);
  });
});

describe("free space", () => {
  const withFree = (n: number) => src({ id: "vscode", targetFreeBytes: n });
  it("is unknown without a number", () => {
    expect(freeSpace(withFree(0), 5_000_000).state).toBe("unknown");
    expect(freeSpace(withFree(10), 0).state).toBe("unknown");
  });
  it("tells ok, tight and short", () => {
    expect(freeSpace(withFree(38_000_000_000), 414_000_000)).toMatchObject({
      state: "ok",
      text: "Needs 414 MB; 38 GB is free on Zero2Dev's drive.",
    });
    expect(freeSpace(withFree(450_000_000), 414_000_000).state).toBe("tight");
    const short = freeSpace(withFree(200_000_000), 414_000_000);
    expect(short.state).toBe("short");
    expect(short.text).toMatch(/Free some space first/);
  });
});

describe("several at once", () => {
  it("builds one entry per ready source with its recommended items", () => {
    const b = buildBatch(
      [vscode, goCache, npmCache, src({ id: "database" })],
      [],
      ["vscode", "go-cache", "npm-cache", "database", "nope"],
      false,
    );
    expect(b.map((e) => [e.sourceId, e.mode, e.items])).toEqual([
      ["vscode", "copy", []],
      ["go-cache", "copy", []],
      ["npm-cache", "copy", []],
    ]);
    expect(b[0].bytes).toBe(414_048_000);
  });
  it("moves only where Move is allowed, and skips what is imported already", () => {
    const rtSrc = src({ id: "runtime:rt_1", program: "runtime", modes: ["copy"], items: [item("Program files", 5)] });
    const b = buildBatch(
      [vscode, rtSrc, notepad],
      [mig({ source: "notepadpp", program: "notepadpp" })],
      ["vscode", "runtime:rt_1", "notepadpp"],
      true,
    );
    expect(b.map((e) => [e.sourceId, e.mode])).toEqual([
      ["vscode", "move"],
      ["runtime:rt_1", "copy"],
    ]);
  });
  it("runs one after another and says how it went", () => {
    const b = buildBatch([vscode, notepad], [], ["vscode", "notepadpp"], false);
    expect(nextWaiting(b)?.sourceId).toBe("vscode");
    expect(batchBusy(b)).toBe(true);
    b[0].state = "done";
    b[1].state = "failed";
    expect(nextWaiting(b)).toBeUndefined();
    expect(batchBusy(b)).toBe(false);
    expect(batchSummary(b)).toBe(
      "1 of 2 were imported. Notepad++ was not: what was there before is kept.".replace("Notepad++", "notepadpp"),
    );
    b[1].state = "done";
    expect(batchSummary(b)).toBe("All 2 are in Zero2Dev now.");
  });
});

describe("history", () => {
  const list = [
    mig({ id: "mig_1" }),
    mig({ id: "mig_2", state: "reverted", program: "notepadpp" }),
    mig({ id: "mig_3", state: "failed", program: "composer" }),
  ];
  it("filters by state and program", () => {
    expect(filterHistory(list, "all").length).toBe(3);
    expect(filterHistory(list, "active").map((m) => m.id)).toEqual(["mig_1"]);
    expect(filterHistory(list, "reverted").map((m) => m.id)).toEqual(["mig_2"]);
    expect(filterHistory(list, "failed", "composer").map((m) => m.id)).toEqual(["mig_3"]);
    expect(filterHistory(list, "all", "notepadpp").length).toBe(1);
  });
  it("says how long it took", () => {
    expect(tookText(mig({}))).toBe("");
    expect(tookText(mig({ finishedAt: "2026-11-20T10:00:00.400Z" }))).toBe("took under a second");
    expect(tookText(mig({ finishedAt: "2026-11-20T10:00:12Z" }))).toBe("took 12 s");
    expect(tookText(mig({ finishedAt: "2026-11-20T10:03:05Z" }))).toBe("took 3 min 5 s");
  });
  it("explains clean-up and why a runtime has none", () => {
    expect(cleanupText(mig({ originalsBytes: 414_048_000 }))).toMatch(/\(414 MB\).*checks that its copy is complete/);
    expect(noCleanupReason(mig({ program: "runtime" }))).toMatch(/installer/);
    expect(noCleanupReason(mig({ program: "database" }))).toMatch(/server/);
    expect(noCleanupReason(mig({}))).toBe("");
    expect(noCleanupReason(mig({ program: "runtime", state: "reverted" }))).toBe("");
  });
});
