import { describe, expect, it } from "vite-plus/test";
import {
  PARENT_KEY,
  buildRequest,
  canPlan,
  createState,
  createdOf,
  currentStep,
  databaseChoices,
  defaultDatabase,
  defaultDomain,
  failureParts,
  frameworksOf,
  installTitle,
  joinProjectPath,
  languagesOf,
  loadParent,
  needsLine,
  outline,
  problemAction,
  projectDomainError,
  projectPortError,
  saveParent,
  slugify,
  stepRows,
  type CreateForm,
} from "./newproject";
import type { Framework, NewProjectPlan } from "./types";

const laravel: Framework = {
  id: "laravel",
  label: "Laravel",
  language: "php",
  summary: "The PHP framework for web artisans.",
  runtime: { kind: "php", min: "8.2" },
  composer: true,
  database: { kinds: ["mariadb", "mysql", "postgres"], default: "mariadb", optional: true },
  steps: [{ type: "run", label: "Creating the Laravel project", program: "composer" }],
  services: [{ name: "{name}", kind: "php", port: 8000, usesDatabase: true }],
  site: { type: "php", root: "public", https: true },
};
const html: Framework = {
  id: "html",
  label: "HTML",
  language: "static",
  summary: "A folder with an index.html.",
  runtime: { kind: "" },
  steps: [{ type: "files", label: "Writing index.html" }],
  services: [],
  site: { type: "static", https: false },
};
const nextjs: Framework = {
  ...laravel,
  id: "nextjs",
  label: "Next.js",
  language: "javascript",
  runtime: { kind: "node", min: "20" },
  composer: false,
  database: undefined,
};
const wordpress: Framework = {
  ...laravel,
  id: "wordpress",
  database: { kinds: ["mariadb", "mysql"], default: "mysql", optional: false },
};

const form = (over: Partial<CreateForm> = {}): CreateForm => ({
  framework: "laravel",
  name: "My Shop",
  parent: "D:\\www",
  database: "mariadb",
  domain: "",
  port: "",
  openEditor: true,
  ...over,
});
const plan = (over: Partial<NewProjectPlan> = {}): NewProjectPlan => ({
  dir: "D:\\www\\my-shop",
  slug: "my-shop",
  domain: "my-shop.localhost",
  port: 8000,
  installs: [],
  database: null,
  steps: [],
  creates: [],
  problems: [],
  warnings: [],
  ...over,
});

describe("slugify", () => {
  it("makes lower-case letters, digits, - and _", () => {
    expect(slugify("My Shop")).toBe("my-shop");
    expect(slugify("  Hello   World  ")).toBe("hello-world");
    expect(slugify("api_v2")).toBe("api_v2");
  });
  it("strips leading dots and odd characters", () => {
    expect(slugify(".hidden")).toBe("hidden");
    expect(slugify("..\\evil/name")).toBe("evilname");
    expect(slugify("a.b.c")).toBe("abc");
    expect(slugify("--x--")).toBe("x");
  });
  it("drops accents and gives empty for nothing usable", () => {
    expect(slugify("Café Près")).toBe("cafe-pres");
    expect(slugify("!!!")).toBe("");
    expect(slugify("")).toBe("");
  });
});

describe("paths and domain", () => {
  it("joins with backslashes", () => {
    expect(joinProjectPath("D:\\www", "shop")).toBe("D:\\www\\shop");
    expect(joinProjectPath("D:\\www\\", "shop")).toBe("D:\\www\\shop");
    expect(joinProjectPath("D:/www/", "shop")).toBe("D:\\www\\shop");
    expect(joinProjectPath("", "shop")).toBe("");
    expect(joinProjectPath("D:\\www", "")).toBe("");
  });
  it("suggests the domain from the slug", () => {
    expect(defaultDomain("my-shop", "localhost")).toBe("my-shop.localhost");
    expect(defaultDomain("", "localhost")).toBe("");
  });
  it("checks a typed domain like the site form; empty is the default", () => {
    expect(projectDomainError("", ["localhost"])).toBeUndefined();
    expect(projectDomainError("shop.localhost", ["localhost"])).toBeUndefined();
    expect(projectDomainError("Shop.localhost", ["localhost"])).toMatch(/lowercase/);
    expect(projectDomainError("shop.dev", ["localhost"])).toMatch(/must end with \.localhost/);
  });
  it("checks the port", () => {
    expect(projectPortError("")).toBeUndefined();
    expect(projectPortError("8000")).toBeUndefined();
    expect(projectPortError("abc")).toBeDefined();
    expect(projectPortError("70000")).toBeDefined();
  });
});

describe("languages and frameworks", () => {
  const all = [nextjs, laravel, html];
  it("lists the languages that exist, in the usual order", () => {
    expect(languagesOf(all).map((l) => l.id)).toEqual(["php", "javascript", "static"]);
    expect(languagesOf([]).length).toBe(0);
  });
  it("groups frameworks by language", () => {
    expect(frameworksOf(all, "php").map((f) => f.id)).toEqual(["laravel"]);
    expect(frameworksOf(all, "go")).toEqual([]);
  });
  it("says what a framework needs", () => {
    expect(needsLine(laravel)).toBe("PHP 8.2 or newer, Composer, optional database (MariaDB, MySQL or PostgreSQL)");
    expect(needsLine(wordpress)).toContain("a database (MariaDB or MySQL)");
    expect(needsLine(nextjs)).toBe("Node.js 20 or newer");
    expect(needsLine(html)).toBe("Nothing to install");
  });
  it("offers the database kinds with None when optional", () => {
    expect(databaseChoices(laravel).map((c) => c.label)).toEqual(["None", "MariaDB", "MySQL", "PostgreSQL"]);
    expect(databaseChoices(wordpress).map((c) => c.value)).toEqual(["mariadb", "mysql"]);
    expect(databaseChoices(html)).toEqual([]);
    expect(defaultDatabase(laravel)).toBe("mariadb");
    expect(defaultDatabase(wordpress)).toBe("mysql");
    expect(defaultDatabase(html)).toBe("");
  });
  it("outlines a framework before the plan arrives", () => {
    const o = outline(laravel);
    expect(o.steps).toEqual(["Creating the Laravel project"]);
    expect(o.creates[0]).toContain("port 8000");
    expect(o.creates.at(-1)).toMatch(/project/);
  });
});

describe("request and Create state", () => {
  it("builds the request", () => {
    expect(buildRequest(laravel, form({ name: "  My Shop ", port: "8100", domain: " shop.localhost " }))).toEqual({
      framework: "laravel",
      name: "My Shop",
      parent: "D:\\www",
      databaseKind: "mariadb",
      domain: "shop.localhost",
      port: 8100,
      openEditor: true,
    });
  });
  it("sends no database for a framework without one, or for None", () => {
    expect(buildRequest(html, form({ framework: "html", database: "mariadb" })).databaseKind).toBe("");
    expect(buildRequest(laravel, form({ database: "" })).databaseKind).toBe("");
    expect(buildRequest(laravel, form()).port).toBe(0);
  });
  it("asks for a plan only when the daemon would not answer 400", () => {
    expect(canPlan(form(), ["localhost"])).toBe(true);
    expect(canPlan(form({ name: "" }), ["localhost"])).toBe(false);
    expect(canPlan(form({ name: "!!!" }), ["localhost"])).toBe(false);
    expect(canPlan(form({ parent: "" }), ["localhost"])).toBe(false);
    expect(canPlan(form({ parent: "www" }), ["localhost"])).toBe(false);
    expect(canPlan(form({ domain: "x.dev" }), ["localhost"])).toBe(false);
  });
  it("gives the reason Create is off", () => {
    const s = ["localhost"];
    expect(createState(form({ name: "" }), s, null, "idle").reason).toBe("Enter a name.");
    expect(createState(form({ parent: "" }), s, null, "idle").reason).toMatch(/folder/);
    expect(createState(form(), s, null, "loading").reason).toBe("Checking the plan...");
    expect(createState(form(), s, plan(), "error").ok).toBe(false);
    expect(createState(form(), s, plan({ problems: ["The folder exists"] }), "ready").reason).toMatch(/problem/);
    expect(createState(form(), s, plan({ problems: ["a", "b"] }), "ready").reason).toMatch(/2 problems/);
    expect(createState(form(), s, plan(), "ready", true).ok).toBe(false);
    expect(createState(form(), s, plan({ warnings: ["careful"] }), "ready")).toEqual({ ok: true, reason: "" });
  });
});

describe("plan problems", () => {
  it("finds an action where one can be guessed", () => {
    expect(problemAction("The domain shop.localhost is used by the site Shop")).toMatchObject({
      kind: "focus",
      field: "domain",
    });
    expect(problemAction("Port 8000 is used by Docs site")).toMatchObject({ field: "port" });
    expect(problemAction("The folder D:\\www\\taken exists and is not empty")).toMatchObject({ field: "name" });
    expect(problemAction("The parent folder D:\\nope does not exist")).toMatchObject({ field: "parent" });
    expect(problemAction("No PHP version fits Laravel (8.2 or newer)")).toMatchObject({
      kind: "link",
      href: "#/runtimes",
    });
    expect(problemAction("Something else")).toBeUndefined();
  });
  it("names an install", () => {
    expect(installTitle({ name: "PHP", version: "8.4.1" })).toBe("PHP 8.4.1");
    expect(installTitle({ name: "PHP 8.4.1", version: "8.4.1" })).toBe("PHP 8.4.1");
  });
});

describe("job steps", () => {
  const steps = ["Installing PHP 8.4.1", "Installing Composer 2.11.1", "Creating the Laravel project", "Writing .env"];
  const job = (state: "running" | "done" | "failed", step: string, percent: number) => ({ state, step, percent });
  it("finds the current step by name, by prefix, else by percent", () => {
    expect(currentStep(steps, { step: "Creating the Laravel project", percent: 10 })).toBe(2);
    expect(currentStep(steps, { step: "Installing PHP 8.4.1 (12 of 30 MB)", percent: 10 })).toBe(0);
    expect(currentStep(steps, { step: "Something unlisted", percent: 60 })).toBe(2);
    expect(currentStep(steps, { step: "", percent: 0 })).toBe(0);
    expect(currentStep([], { step: "x", percent: 0 })).toBe(-1);
  });
  it("derives the state of every step", () => {
    expect(stepRows(steps, null).map((r) => r.state)).toEqual(["waiting", "waiting", "waiting", "waiting"]);
    const run = stepRows(steps, job("running", "Installing Composer 2.11.1", 33));
    expect(run.map((r) => r.state)).toEqual(["done", "running", "waiting", "waiting"]);
    expect(run[1].percent).toBe(33);
    expect(stepRows(steps, job("failed", "Creating the Laravel project", 55)).map((r) => r.state)).toEqual([
      "done",
      "done",
      "failed",
      "waiting",
    ]);
    expect(stepRows(steps, job("done", "Writing .env", 100)).every((r) => r.state === "done")).toBe(true);
  });
});

describe("failure and result", () => {
  it("finds the kept folder", () => {
    const e = "composer exited with code 1. The folder D:\\www\\fail-me was kept.";
    expect(failureParts(e)).toEqual({ message: e, kept: "D:\\www\\fail-me" });
    expect(failureParts("boom").kept).toBe("");
    expect(failureParts("").message).toBe("Unknown error.");
  });
  it("collects what the project holds", () => {
    const projects = [{ id: "p1", name: "Shop", serviceIds: ["s1", "gone"], siteIds: ["t1"] }];
    const services = [{ id: "s1", name: "Shop" }] as never[];
    const sites = [{ id: "t1", domain: "shop.localhost" }] as never[];
    const r = createdOf("p1", projects, services, sites);
    expect(r.services.length).toBe(1);
    expect(r.sites.length).toBe(1);
    expect(createdOf("nope", projects, services, sites).project).toBeUndefined();
  });
});

describe("the remembered folder", () => {
  const mem = (initial?: string) => {
    let v = initial ?? null;
    return { getItem: () => v, setItem: (_: string, x: string) => void (v = x) };
  };
  it("saves and loads, ignoring empty and broken storage", () => {
    const st = mem();
    saveParent("  D:\\www ", st);
    expect(loadParent(st)).toBe("D:\\www");
    saveParent("  ", st);
    expect(loadParent(st)).toBe("D:\\www");
    const broken = {
      getItem: () => {
        throw new Error("blocked");
      },
      setItem: () => {
        throw new Error("blocked");
      },
    };
    expect(loadParent(broken)).toBe("");
    expect(() => saveParent("D:\\x", broken)).not.toThrow();
    expect(PARENT_KEY).toContain("newproject");
  });
});
