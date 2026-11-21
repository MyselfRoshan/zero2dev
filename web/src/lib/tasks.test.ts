import { describe, expect, it } from "vite-plus/test";
import type { KindProfile } from "./kinds";
import {
  customRequest,
  expandTemplate,
  extraScripts,
  isComposerMissing,
  offersTasks,
  packageManagerOf,
  parseCommandLine,
  presetCommand,
  programError,
  resultLine,
  scriptCommandText,
  scriptRequest,
  splitTaskError,
} from "./tasks";

describe("parseCommandLine", () => {
  it("splits on spaces", () => {
    expect(parseCommandLine("run build --watch").args).toEqual(["run", "build", "--watch"]);
    expect(parseCommandLine("  a   b\tc  ").args).toEqual(["a", "b", "c"]);
    expect(parseCommandLine("").args).toEqual([]);
    expect(parseCommandLine("   ").args).toEqual([]);
  });
  it("keeps quoted values together, with either quote", () => {
    expect(parseCommandLine('run build -- --mode "my mode"').args).toEqual(["run", "build", "--", "--mode", "my mode"]);
    expect(parseCommandLine("commit -m 'first try'").args).toEqual(["commit", "-m", "first try"]);
    expect(parseCommandLine(`say "it's fine"`).args).toEqual(["say", "it's fine"]);
  });
  it("joins quoted and plain parts of one word, and keeps an empty pair of quotes", () => {
    expect(parseCommandLine('a"b c"d').args).toEqual(["ab cd"]);
    expect(parseCommandLine('--name ""').args).toEqual(["--name", ""]);
  });
  it("treats a backslash as a plain character (Windows paths)", () => {
    expect(parseCommandLine('--out "C:\\my folder\\x.txt" C:\\a\\b').args).toEqual([
      "--out",
      "C:\\my folder\\x.txt",
      "C:\\a\\b",
    ]);
  });
  it("does not interpret shell characters", () => {
    expect(parseCommandLine("a | b && c > d").args).toEqual(["a", "|", "b", "&&", "c", ">", "d"]);
  });
  it("reports a quote that is not closed", () => {
    expect(parseCommandLine('a "b c').error).toMatch(/double quote/);
    expect(parseCommandLine("a 'b").error).toMatch(/single quote/);
  });
});

describe("programError", () => {
  it("accepts a plain name", () => {
    expect(programError("npm")).toBe("");
    expect(programError("  composer ")).toBe("");
  });
  it("refuses an empty name, a path, a space and a line break", () => {
    expect(programError("")).not.toBe("");
    expect(programError("C:\\tools\\npm")).toMatch(/without a folder/);
    expect(programError("bin/npm")).toMatch(/without a folder/);
    expect(programError("npm run")).toMatch(/no spaces/);
    expect(programError("npm\nrun")).toMatch(/one line/);
  });
});

describe("customRequest", () => {
  it("builds the request", () => {
    expect(customRequest(" npm ", 'run build -- --mode "a b"')).toEqual({
      request: { program: "npm", args: ["run", "build", "--", "--mode", "a b"] },
    });
    expect(customRequest("go", "").request).toEqual({ program: "go", args: [] });
  });
  it("returns the first error", () => {
    expect(customRequest("", "x").error).toMatch(/program/i);
    expect(customRequest("npm", '"x').error).toMatch(/quote/);
    expect(customRequest("npm", Array.from({ length: 201 }, () => "a").join(" ")).error).toMatch(/200/);
  });
});

const nodeProfile = {
  kind: "node",
  options: [{ key: "packageManager", default: "npm" }],
  tasks: [{ id: "npm-install", label: "Install", program: "{s.packageManager}", args: ["install"] }],
} as unknown as KindProfile;

describe("packageManagerOf and scripts", () => {
  it("uses the setting, then the profile's default, then npm", () => {
    expect(packageManagerOf(nodeProfile, { settings: { packageManager: "pnpm" } })).toBe("pnpm");
    expect(packageManagerOf(nodeProfile, { settings: {} })).toBe("npm");
    expect(packageManagerOf(undefined, { settings: {} })).toBe("npm");
  });
  it("turns a script into `<pm> run <name>`", () => {
    expect(scriptRequest("pnpm", { name: "build" })).toEqual({ program: "pnpm", args: ["run", "build"] });
    expect(scriptRequest("", { name: "dev" })).toEqual({ program: "npm", args: ["run", "dev"] });
    expect(scriptCommandText("yarn", { name: "lint" })).toBe("yarn run lint");
  });
  it("hides a script a preset already runs", () => {
    const list = {
      presets: [{ id: "npm-build", label: "Build", program: "{s.packageManager}", args: ["run", "build"] }],
      scripts: [
        { name: "build", command: "vite build" },
        { name: "lint", command: "eslint ." },
      ],
    };
    expect(extraScripts(list, "npm").map((s) => s.name)).toEqual(["lint"]);
    expect(extraScripts({ presets: [], scripts: list.scripts }, "npm")).toHaveLength(2);
  });
});

describe("presets", () => {
  it("expands settings and the port", () => {
    expect(expandTemplate("{s.packageManager}", { packageManager: "pnpm" }, 0)).toBe("pnpm");
    expect(expandTemplate("{s.unknown}", {}, 0)).toBe("");
    expect(expandTemplate("--port={port}", {}, 3000)).toBe("--port=3000");
    expect(expandTemplate("--port={port}", {}, 0)).toBe("--port=");
    expect(
      presetCommand({ program: "{s.packageManager}", args: ["run", "build"] }, { packageManager: "yarn" }, 0),
    ).toBe("yarn run build");
    expect(presetCommand({ program: "composer", args: [] }, {}, 0)).toBe("composer");
  });
  it("offers tasks for kinds with presets and for Node", () => {
    expect(offersTasks(nodeProfile)).toBe(true);
    expect(offersTasks({ kind: "node", options: [] } as unknown as KindProfile)).toBe(true);
    expect(offersTasks({ kind: "go", options: [], tasks: [{}] } as unknown as KindProfile)).toBe(true);
    expect(offersTasks({ kind: "redis", options: [] } as unknown as KindProfile)).toBe(false);
    expect(offersTasks(undefined)).toBe(false);
  });
});

describe("results", () => {
  it("splits the error of a failed task", () => {
    expect(splitTaskError("npm exited with code 1: npm ERR! a; npm ERR! b")).toEqual({
      headline: "npm exited with code 1",
      lines: ["npm ERR! a", "npm ERR! b"],
    });
    expect(splitTaskError("npm timed out after 10m0s")).toEqual({ headline: "npm timed out after 10m0s", lines: [] });
    expect(splitTaskError("").headline).toMatch(/failed/);
  });
  it("words the result line", () => {
    expect(resultLine({ state: "done", error: "", step: "npm install" })).toBe("npm install finished.");
    expect(resultLine({ state: "failed", error: "npm exited with code 1: x", step: "npm install" })).toBe(
      "npm exited with code 1",
    );
  });
  it("recognises the Composer conflict", () => {
    expect(isComposerMissing("Composer is not installed: install it on the Runtimes page")).toBe(true);
    expect(isComposerMissing("a task of Shop is already running")).toBe(false);
  });
});
