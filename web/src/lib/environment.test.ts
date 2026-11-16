import { describe, expect, it } from "vite-plus/test";
import {
  changeCount,
  changeList,
  cloneConfig,
  dirError,
  doneText,
  moveItem,
  nameError,
  newItemId,
  nowSummary,
  ownsAnything,
  pathCommands,
  pathDir,
  pathStatus,
  pathTitle,
  quickSetup,
  runtimeValue,
  runtimesWithoutPath,
  sameConfig,
  setAllEnabled,
  shadowText,
  shadowed,
  suggestions,
  tryCommand,
  varSourceLabel,
  varStatus,
  varValue,
} from "./environment";
import type { EnvConfig, EnvPath, EnvPathStatus, EnvReport, EnvVar, EnvVarStatus, Runtime } from "./types";

const rt = (id: string, kind: Runtime["kind"], version: string, binary: string, def = false): Runtime => ({
  id,
  kind,
  version,
  name: `${kind} ${version}`,
  binary,
  source: "installed",
  installedAt: "",
  default: def,
});
const node26 = rt("rt_n26", "node", "26.13.1", "D:\\z\\bin\\node-26\\node.exe", true);
const node22 = rt("rt_n22", "node", "22.12.0", "D:\\z\\bin\\node-22\\node.exe");
const java = rt("rt_j", "java", "21.0.5", "D:\\z\\bin\\jdk-21\\bin\\java.exe", true);
const rts = [node26, node22, java];
const SHIMS = "D:\\z\\data\\shims";

const shimsItem = (on = false): EnvPath => ({ id: "envp_shims", source: "shims", enabled: on });
const javaHome = (on = false): EnvVar => ({
  id: "envv_java_home",
  name: "JAVA_HOME",
  source: "default",
  kind: "java",
  enabled: on,
});
const cfg = (path: EnvPath[] = [shimsItem()], vars: EnvVar[] = [javaHome()]): EnvConfig => ({
  seeded: true,
  path,
  vars,
});
const report = (
  path: Partial<EnvPathStatus>[] = [],
  vars: Partial<EnvVarStatus>[] = [],
  supported = true,
): EnvReport => ({
  supported,
  shimsDir: SHIMS,
  path: path.map((p) => ({ id: "", dir: "", present: false, owned: false, ...p })),
  vars: vars.map((v) => ({ id: "", name: "", value: "", hasCurrent: false, owned: false, ...v })),
});

describe("config helpers", () => {
  it("clones deeply and compares by content", () => {
    const a = cfg();
    const b = cloneConfig(a);
    expect(sameConfig(a, b)).toBe(true);
    b.path[0].enabled = true;
    expect(a.path[0].enabled).toBe(false);
    expect(sameConfig(a, b)).toBe(false);
  });
  it("turns one part on or off and leaves the other alone", () => {
    const on = setAllEnabled(cfg(), "path", true);
    expect(on.path[0].enabled).toBe(true);
    expect(on.vars[0].enabled).toBe(false);
  });
  it("moves items within range only", () => {
    const list = [{ id: "a" }, { id: "b" }, { id: "c" }];
    expect(moveItem(list, "b", -1).map((x) => x.id)).toEqual(["b", "a", "c"]);
    expect(moveItem(list, "c", 1)).toBe(list);
    expect(moveItem(list, "nope", 1)).toBe(list);
  });
  it("makes ids the daemon accepts", () => {
    expect(newItemId("envp_")).toMatch(/^envp_[0-9a-f]{8}$/);
    expect(newItemId("envv_")).not.toBe(newItemId("envv_"));
  });
});

describe("what an item is", () => {
  it("names and locates folders", () => {
    expect(pathTitle(shimsItem(), rts)).toBe("Default versions");
    const one: EnvPath = { id: "p", source: "runtime", runtimeId: "rt_n22", enabled: true };
    expect(pathTitle(one, rts)).toBe("Node.js 22.12.0 only");
    expect(pathDir(one, undefined, rts, SHIMS)).toBe("D:\\z\\bin\\node-22");
    expect(pathDir(shimsItem(), undefined, rts, SHIMS)).toBe(SHIMS);
    expect(pathDir(one, { id: "p", dir: "X", present: false, owned: false }, rts, SHIMS)).toBe("X");
    expect(pathTitle({ id: "p", source: "runtime", runtimeId: "gone", enabled: true }, rts)).toBe(
      "A runtime that was removed",
    );
  });
  it("says which commands a folder gives, before and after the daemon knows", () => {
    expect(pathCommands({ id: "p", source: "runtime", runtimeId: "rt_n22", enabled: true }, undefined, rts)).toEqual([
      "node",
      "npm",
      "npx",
    ]);
    expect(pathCommands(shimsItem(), undefined, rts)).toEqual(["jar", "java", "javac", "node", "npm", "npx"]);
    expect(pathCommands(shimsItem(), { id: "s", dir: "", present: true, owned: true, commands: ["php"] }, rts)).toEqual(
      ["php"],
    );
  });
  it("works out variable values like the daemon", () => {
    expect(runtimeValue(java)).toBe("D:\\z\\bin\\jdk-21");
    expect(runtimeValue(node26)).toBe("D:\\z\\bin\\node-26");
    expect(varValue(javaHome(), undefined, rts)).toBe("D:\\z\\bin\\jdk-21");
    expect(
      varValue({ id: "v", name: "N", source: "runtime", runtimeId: "rt_n22", enabled: true }, undefined, rts),
    ).toBe("D:\\z\\bin\\node-22");
    expect(varValue({ id: "v", name: "X", source: "custom", value: "1", enabled: true }, undefined, rts)).toBe("1");
    expect(varValue({ id: "v", name: "G", source: "default", kind: "go", enabled: true }, undefined, rts)).toBe("");
    expect(varSourceLabel(javaHome(), rts)).toBe("Follows the default Java version");
    expect(varSourceLabel({ id: "v", name: "N", source: "runtime", runtimeId: "rt_n22", enabled: true }, rts)).toBe(
      "Always Node.js 22.12.0",
    );
  });
});

describe("pathStatus: one line per row", () => {
  const ctx = (draftList: EnvPath[], savedList: EnvPath[], supported = true) => ({ supported, draftList, savedList });
  const st = (o: Partial<EnvPathStatus> = {}): EnvPathStatus => ({
    id: "envp_shims",
    dir: SHIMS,
    present: false,
    owned: false,
    ...o,
  });
  it("tells staged from applied", () => {
    const on = shimsItem(true);
    const off = shimsItem(false);
    expect(pathStatus(on, off, st(), ctx([on], [off]))).toMatchObject({
      tone: "pending",
      text: expect.stringMatching(/added to your PATH when you apply/),
    });
    expect(pathStatus(off, on, st({ present: true, owned: true }), ctx([off], [on]))).toMatchObject({
      tone: "pending",
      text: expect.stringMatching(/removed/),
    });
    expect(pathStatus(on, on, st({ present: true, owned: true }), ctx([on], [on]))).toMatchObject({
      tone: "ok",
      text: "On your PATH",
    });
    expect(pathStatus(off, off, st(), ctx([off], [off]))).toMatchObject({ tone: "muted", text: "Off" });
    expect(pathStatus(on, undefined, undefined, ctx([on], []))).toMatchObject({ tone: "pending" });
  });
  it("never claims to remove a folder the user had", () => {
    const on = shimsItem(true);
    const off = shimsItem(false);
    expect(pathStatus(off, on, st({ present: true, owned: false }), ctx([off], [on])).text).toMatch(
      /Stays on your PATH/,
    );
    expect(pathStatus(on, on, st({ present: true }), ctx([on], [on])).text).toBe("On your PATH (it was already there)");
  });
  it("reports a new place, problems and drift with a way out", () => {
    const a: EnvPath = { id: "a", source: "runtime", runtimeId: "rt_n26", enabled: true };
    const b: EnvPath = { id: "b", source: "runtime", runtimeId: "rt_n22", enabled: true };
    expect(pathStatus(b, b, st({ id: "b", present: true, owned: true }), ctx([b, a], [a, b])).text).toMatch(/place 1/);
    // a new folder above is not a move of this one
    const n: EnvPath = { id: "n", source: "shims", enabled: true };
    expect(pathStatus(a, a, st({ id: "a", present: true, owned: true }), ctx([n, a], [a])).text).toBe("On your PATH");
    const gone = pathStatus(a, a, st({ id: "a", problem: "That runtime was removed." }), ctx([a], [a]));
    expect(gone).toMatchObject({ tone: "warn", fix: { href: "#/runtimes" } });
    expect(pathStatus(a, a, st({ id: "a" }), ctx([a], [a])).fix?.reapply).toBe(true);
    expect(pathStatus(a, a, st(), ctx([a], [a], false)).text).toMatch(/Windows only/);
  });
});

describe("varStatus: one line per row", () => {
  const st = (o: Partial<EnvVarStatus> = {}): EnvVarStatus => ({
    id: "envv_java_home",
    name: "JAVA_HOME",
    value: "D:\\z\\bin\\jdk-21",
    hasCurrent: false,
    owned: false,
    ...o,
  });
  const v = "D:\\z\\bin\\jdk-21";
  it("tells staged from applied", () => {
    expect(varStatus(javaHome(true), javaHome(false), st(), v, true)).toMatchObject({
      tone: "pending",
      text: `Will be set to ${v} when you apply.`,
    });
    expect(
      varStatus(javaHome(false), javaHome(true), st({ hasCurrent: true, current: v, owned: true }), v, true).text,
    ).toMatch(/removed when you apply/);
    expect(
      varStatus(
        javaHome(true),
        javaHome(true),
        st({ hasCurrent: true, current: v.toLowerCase(), owned: true }),
        v,
        true,
      ),
    ).toMatchObject({ tone: "ok", text: "Set" });
    expect(varStatus(javaHome(false), javaHome(false), st(), v, true).text).toBe("Off");
  });
  it("says plainly when the user's own value is kept, even before applying", () => {
    const mine = st({ hasCurrent: true, current: "D:\\my\\jdk" });
    for (const saved of [javaHome(true), javaHome(false)]) {
      const s = varStatus(javaHome(true), saved, mine, v, true);
      expect(s.tone).toBe("warn");
      expect(s.text).toContain("You already have JAVA_HOME = D:\\my\\jdk");
    }
    expect(varStatus(javaHome(false), javaHome(false), mine, v, true).text).toContain("You have your own JAVA_HOME");
  });
  it("offers a fix for a missing runtime and for drift", () => {
    const goroot: EnvVar = { id: "g", name: "GOROOT", source: "default", kind: "go", enabled: true };
    expect(varStatus(goroot, goroot, undefined, "", true)).toMatchObject({
      tone: "warn",
      fix: { label: "Install Go", href: "#/runtimes" },
    });
    expect(varStatus(javaHome(true), javaHome(true), st(), v, true).fix?.reapply).toBe(true);
    expect(
      varStatus(javaHome(true), javaHome(true), st({ hasCurrent: true, current: "old", owned: true }), v, true).text,
    ).toMatch(/Out of date/);
  });
  it("shows an edit as a change", () => {
    const edited = { ...javaHome(true), source: "runtime" as const, kind: undefined, runtimeId: "rt_j" };
    expect(varStatus(edited, javaHome(true), st({ hasCurrent: true, current: v, owned: true }), v, true).text).toMatch(
      /Will change to/,
    );
  });
});

describe("shadowed commands", () => {
  it("names the folder that wins", () => {
    const a: EnvPath = { id: "a", source: "runtime", runtimeId: "rt_n26", enabled: true };
    const b: EnvPath = { id: "b", source: "runtime", runtimeId: "rt_n22", enabled: true };
    const off: EnvPath = { id: "c", source: "shims", enabled: false };
    const res = shadowed(
      [a, off, b],
      (p) => pathCommands(p, undefined, rts),
      (p) => pathTitle(p, rts),
    );
    expect(res.has("a")).toBe(false);
    expect(res.has("c")).toBe(false);
    expect(shadowText(res.get("b")!)).toBe("node, npm and npx come from Node.js 26.13.1 only, higher on the list.");
    expect(shadowText([{ command: "go", by: "X" }])).toBe("go comes from X, higher on the list.");
  });
});

describe("changeList: what Apply does", () => {
  it("lists additions, removals and keeps, in plain words", () => {
    const saved = cfg([shimsItem(false)], [javaHome(false)]);
    const draft = cfg([shimsItem(true)], [javaHome(true)]);
    const rep = report(
      [{ id: "envp_shims", dir: SHIMS }],
      [{ id: "envv_java_home", name: "JAVA_HOME", value: "D:\\z\\bin\\jdk-21" }],
    );
    const ch = changeList(draft, saved, rep, rts);
    expect(ch.map((c) => c.text)).toEqual([`Add ${SHIMS} to your PATH`, "Set JAVA_HOME = D:\\z\\bin\\jdk-21"]);
    expect(changeCount(ch)).toBe(2);

    const mine = report(
      [{ id: "envp_shims", dir: SHIMS }],
      [{ id: "envv_java_home", name: "JAVA_HOME", hasCurrent: true, current: "D:\\my" }],
    );
    expect(changeList(draft, saved, mine, rts).map((c) => c.kind)).toEqual(["add", "keep"]);

    const back = changeList(
      saved,
      draft,
      report(
        [{ id: "envp_shims", dir: SHIMS, present: true, owned: true }],
        [{ id: "envv_java_home", name: "JAVA_HOME", hasCurrent: true, owned: true }],
      ),
      rts,
    );
    expect(back.map((c) => c.text)).toEqual([`Remove ${SHIMS} from your PATH`, "Remove JAVA_HOME"]);
  });
  it("reports a new order and list-only edits", () => {
    const a: EnvPath = { id: "a", source: "runtime", runtimeId: "rt_n26", enabled: true };
    const b: EnvPath = { id: "b", source: "runtime", runtimeId: "rt_n22", enabled: true };
    const moved = changeList(cfg([b, a], []), cfg([a, b], []), report(), rts);
    expect(moved).toEqual([
      { kind: "move", text: "Reorder your PATH: Node.js 22.12.0 only, then Node.js 26.13.1 only" },
    ]);
    const listOnly = changeList(
      cfg([shimsItem(), { id: "x", source: "custom", dir: "C:\\t", enabled: false }], []),
      cfg([shimsItem()], []),
      report(),
      rts,
    );
    expect(listOnly.map((c) => c.kind)).toEqual(["list"]);
    expect(changeCount(listOnly)).toBe(1);
    expect(changeList(cfg(), cfg(), report(), rts)).toEqual([]);
  });
  it("leaves out a variable whose runtime is missing, and words results in the past", () => {
    const goroot: EnvVar = { id: "g", name: "GOROOT", source: "default", kind: "go", enabled: true };
    const ch = changeList(
      cfg([], [goroot]),
      cfg([], [{ ...goroot, enabled: false }]),
      report([], [{ id: "g", name: "GOROOT", hasCurrent: true, current: "D:\\my" }]),
      rts,
    );
    expect(ch.map((c) => c.kind)).toEqual(["list"]);
    expect(doneText({ kind: "add", text: "Add X to your PATH" })).toBe("Added X to your PATH");
    expect(doneText({ kind: "keep", text: "Keep your own GOROOT (not changed)" })).toBe(
      "Kept your own GOROOT (not changed)",
    );
    expect(doneText({ kind: "unset", text: "Remove JAVA_HOME" })).toBe("Removed JAVA_HOME");
  });
  it("does not remove what Zero2Dev does not own", () => {
    const back = changeList(
      cfg([shimsItem(false)], []),
      cfg([shimsItem(true)], []),
      report([{ id: "envp_shims", dir: SHIMS, present: true, owned: false }]),
      rts,
    );
    expect(back.map((c) => c.kind)).toEqual(["list"]);
  });
});

describe("summaries and shortcuts", () => {
  it("summarises what Zero2Dev adds now", () => {
    const saved = cfg([shimsItem(true)], [javaHome(true)]);
    expect(
      nowSummary(
        report(
          [{ id: "envp_shims", present: true, owned: true }],
          [{ id: "envv_java_home", hasCurrent: true, owned: true }],
        ),
        saved,
      ),
    ).toBe("Right now Zero2Dev adds 1 folder and 1 variable to your Windows environment.");
    expect(nowSummary(report(), cfg())).toMatch(/adds nothing/);
    expect(nowSummary(report([], [], false), cfg())).toMatch(/not Windows/);
    expect(ownsAnything(report([{ id: "x", present: true, owned: true }]))).toBe(true);
    expect(ownsAnything(report([{ id: "x", present: true }]))).toBe(false);
  });
  it("sets up the default versions in one step", () => {
    const out = quickSetup(cfg([], []), rts);
    expect(out.path).toMatchObject([{ source: "shims", enabled: true }]);
    expect(out.vars.map((v) => v.name)).toEqual(["JAVA_HOME"]); // no Go installed
    const again = quickSetup(cfg(), rts);
    expect(again.path).toHaveLength(1);
    expect(again.vars).toHaveLength(1);
    expect(again.vars[0].enabled).toBe(true);
  });
  it("suggests variables for installed languages only", () => {
    expect(suggestions(rts, [javaHome()]).map((s) => s.name)).toEqual(["NODE_HOME"]);
    expect(suggestions([], [])).toEqual([]);
  });
  it("picks a command to try in a new terminal", () => {
    expect(
      tryCommand(cfg([shimsItem(true)], []), report([{ id: "envp_shims", commands: ["java", "node"] }]), rts),
    ).toBe("node -v");
    expect(tryCommand(cfg([], [javaHome(true)]), report(), rts)).toBe("echo %JAVA_HOME%");
    expect(tryCommand(cfg([], []), report(), rts)).toBe("");
  });
});

describe("input checks", () => {
  it("checks variable names", () => {
    expect(nameError("", [])).toBe("Enter a name.");
    expect(nameError("1A", [])).toMatch(/digit/);
    expect(nameError("path", [])).toMatch(/PATH/);
    expect(nameError("java_home", ["JAVA_HOME"])).toMatch(/already/);
    expect(nameError(" MY_VAR ", [])).toBe("");
  });
  it("checks folders", () => {
    expect(dirError("tools\\bin", [])).toMatch(/full path/);
    expect(dirError("C:\\a;b", [])).toMatch(/cannot go on PATH/);
    expect(dirError("c:/Tools/", ["C:\\tools"])).toMatch(/already/);
    expect(dirError("\\\\server\\share\\bin", [])).toBe("");
  });
  it("lists the runtimes without a folder item", () => {
    expect(
      runtimesWithoutPath(rts, [{ id: "a", source: "runtime", runtimeId: "rt_n26", enabled: false }]).map((r) => r.id),
    ).toEqual(["rt_n22", "rt_j"]);
  });
});
