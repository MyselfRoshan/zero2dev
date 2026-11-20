import { describe, expect, it } from "vite-plus/test";
import { ApiError } from "./api";
import {
  actionState,
  adminLabel,
  baseName,
  databaseNameError,
  dirOf,
  dumpPathError,
  exportFileName,
  fmtSize,
  generatePassword,
  isDbService,
  isFileExists,
  isGzipPath,
  isSystemDatabase,
  jobNoun,
  jobText,
  passwordError,
  resetMessage,
  stateNote,
  supportsFor,
  userDatabases,
  userNameError,
} from "./databases";

describe("names (mirroring the daemon's rules)", () => {
  it("accepts 1-64 letters, digits and _", () => {
    expect(databaseNameError("shop")).toBe("");
    expect(databaseNameError("Shop_2")).toBe("");
    expect(databaseNameError("a".repeat(64))).toBe("");
    expect(databaseNameError("  shop  ")).toBe("");
  });
  it("rejects empty, long and other characters", () => {
    expect(databaseNameError("")).toBe("Enter a database name.");
    expect(databaseNameError("a".repeat(65))).toMatch(/1-64 letters, digits or _/);
    expect(databaseNameError("shop-db")).toMatch(/1-64/);
    expect(databaseNameError("shop db")).toMatch(/1-64/);
    expect(databaseNameError("sh;op")).toMatch(/1-64/);
    expect(databaseNameError("café")).toMatch(/1-64/);
    expect(userNameError("")).toBe("Enter a user name.");
    expect(userNameError("app-user")).toMatch(/^User name must be 1-64/);
    expect(userNameError("app_user")).toBe("");
  });
  it("refuses the system databases, in any case", () => {
    for (const n of ["mysql", "information_schema", "performance_schema", "sys", "postgres", "template0", "template1"])
      expect(databaseNameError(n)).toMatch(/system database/);
    expect(isSystemDatabase("MySQL")).toBe(true);
    expect(isSystemDatabase("shop")).toBe(false);
  });
  it("checks passwords", () => {
    expect(passwordError("")).toBe("Enter a password.");
    expect(passwordError("a\nb")).toMatch(/line break/);
    expect(passwordError("x".repeat(129))).toMatch(/128/);
    expect(passwordError("s3cret!")).toBe("");
  });
});

describe("dump files", () => {
  it("needs an absolute file path", () => {
    expect(dumpPathError("")).toMatch(/Browse/);
    expect(dumpPathError("shop.sql")).toMatch(/absolute/);
    expect(dumpPathError("C:\\dumps\\")).toMatch(/folder/);
    expect(dumpPathError("C:\\dumps\\shop.sql")).toBe("");
    expect(dumpPathError("\\\\server\\share\\shop.sql.gz")).toBe("");
  });
  it("splits paths and names exports", () => {
    expect(dirOf("C:\\dumps\\shop.sql")).toBe("C:\\dumps");
    expect(dirOf("shop.sql")).toBe("");
    expect(baseName("C:\\dumps\\shop.sql")).toBe("shop.sql");
    expect(baseName("D:/x/y.sql.gz")).toBe("y.sql.gz");
    expect(exportFileName("shop", false, new Date(2026, 10, 8))).toBe("shop-2026-11-08.sql");
    expect(exportFileName("shop", true, new Date(2026, 0, 31))).toBe("shop-2026-01-31.sql.gz");
    expect(isGzipPath("C:\\a\\b.SQL.GZ")).toBe(true);
    expect(isGzipPath("C:\\a\\b.sql")).toBe(false);
  });
  it("formats sizes", () => {
    expect(fmtSize(0)).toBe("-");
    expect(fmtSize(512)).toBe("512 B");
    expect(fmtSize(1536)).toBe("1.5 KB");
    expect(fmtSize(1048576)).toBe("1.0 MB");
    expect(fmtSize(73400320)).toBe("70 MB");
    expect(fmtSize(3 * 1024 ** 3)).toBe("3.0 GB");
  });
});

describe("lists and labels", () => {
  it("leaves the system databases out and sorts", () => {
    expect(
      userDatabases({
        databases: [
          { name: "shop", sizeBytes: 1 },
          { name: "mysql", sizeBytes: 2 },
          { name: "blog", sizeBytes: 3 },
          { name: "information_schema", sizeBytes: 0 },
        ],
      }).map((d) => d.name),
    ).toEqual(["blog", "shop"]);
    expect(userDatabases(null)).toEqual([]);
  });
  it("names the administrator", () => {
    expect(adminLabel("mariadb")).toBe("root");
    expect(adminLabel("postgres")).toBe("postgres");
    expect(adminLabel("redis")).toBe("Redis");
    expect(adminLabel("mysql", "admin")).toBe("admin");
  });
  it("knows what each kind supports", () => {
    expect(isDbService("postgres")).toBe(true);
    expect(isDbService("node")).toBe(false);
    expect(supportsFor("mariadb")).toEqual({ databases: true, dumps: true, password: true, reset: true });
    expect(supportsFor("redis")).toEqual({ databases: false, dumps: false, password: true, reset: true });
    expect(supportsFor("node").password).toBe(false);
  });
});

describe("which action is allowed when", () => {
  const svc = (state: "running" | "stopped" | "starting" | "stopping" | "failed", kind = "mariadb") => ({
    name: "Shop DB",
    kind: kind as "mariadb",
    state,
  });
  it("needs a running server for everything but reset", () => {
    for (const a of ["create", "user", "import", "export", "password"] as const) {
      expect(actionState(a, svc("running")).allowed).toBe(true);
      const s = actionState(a, svc("stopped"));
      expect(s.allowed).toBe(false);
      expect(s.reason).toMatch(/^Start Shop DB first: /);
    }
    expect(actionState("create", svc("failed")).reason).toMatch(/^Start Shop DB first/);
  });
  it("needs a stopped server for reset", () => {
    expect(actionState("reset", svc("stopped")).allowed).toBe(true);
    expect(actionState("reset", svc("failed")).allowed).toBe(true);
    expect(actionState("reset", svc("running")).reason).toMatch(/^Stop Shop DB first: /);
  });
  it("waits while the server changes state or a job runs", () => {
    expect(actionState("import", svc("starting")).reason).toBe("Wait until Shop DB has started.");
    expect(actionState("reset", svc("stopping")).reason).toBe("Wait until Shop DB has stopped.");
    expect(actionState("export", svc("running"), jobNoun("db-import")).reason).toBe(
      "Wait until an import on Shop DB has finished.",
    );
    expect(jobNoun("db-export")).toBe("an export");
  });
  it("follows what the server supports (Redis: password and reset only)", () => {
    expect(actionState("create", svc("running", "redis")).reason).toBe("Redis does not support this.");
    expect(actionState("password", svc("running", "redis")).allowed).toBe(true);
    expect(
      actionState("import", svc("running"), "", { databases: true, dumps: false, password: true, reset: true }).allowed,
    ).toBe(false);
  });
  it("explains the state in one line", () => {
    expect(stateNote(svc("running"))).toBe("");
    expect(stateNote(svc("stopped"))).toMatch(/not running.*Reset works only while it is stopped/);
    expect(stateNote(svc("stopped", "redis"))).toMatch(/password/);
    expect(stateNote(svc("starting"))).toMatch(/starting/);
  });
});

describe("job text", () => {
  const imp = { type: "db-import" as const, database: "shop", file: "C:\\dumps\\shop.sql" };
  const exp = { type: "db-export" as const, database: "shop", file: "C:\\dumps\\shop.sql.gz" };
  it("shows the step and percent while running", () => {
    expect(jobText(imp, undefined)).toBe("Importing shop.sql into shop...");
    expect(jobText(imp, { state: "running", percent: 42.4, step: "Importing", error: "" })).toBe(
      "Importing shop.sql into shop... 42%",
    );
    expect(jobText(exp, { state: "running", percent: 0, step: "", error: "" })).toBe(
      "Exporting shop to shop.sql.gz...",
    );
  });
  it("says what happened at the end", () => {
    expect(jobText(imp, { state: "done", percent: 100, step: "", error: "" })).toBe("Imported shop.sql into shop.");
    expect(jobText(exp, { state: "done", percent: 100, step: "", error: "", path: "D:\\out\\shop.sql.gz" })).toBe(
      "Exported shop to D:\\out\\shop.sql.gz.",
    );
    expect(jobText(imp, { state: "failed", percent: 30, step: "", error: "ERROR 1064 at line 3" })).toBe(
      "The import of shop.sql into shop failed: ERROR 1064 at line 3",
    );
  });
  it("tells a file that exists from other conflicts", () => {
    expect(isFileExists(new ApiError(409, "conflict", "C:\\dumps\\shop.sql exists"))).toBe(true);
    expect(isFileExists(new ApiError(409, "conflict", "start Shop DB first"))).toBe(false);
    expect(isFileExists(new ApiError(409, "conflict", "x exists", { jobId: "job_1" }))).toBe(false);
    expect(isFileExists(new ApiError(400, "bad_request", "x exists"))).toBe(false);
  });
});

describe("passwords and reset", () => {
  it("generates passwords that need no escaping", () => {
    let i = 0;
    const p = generatePassword(20, (n) => i++ % n);
    expect(p).toHaveLength(20);
    expect(p).toMatch(/^[A-Za-z0-9]+$/);
    expect(generatePassword()).toMatch(/^[A-Za-z0-9]{20}$/);
  });
  it("says exactly what a reset deletes", () => {
    const m = resetMessage({ name: "Shop DB", kind: "mariadb" }, "D:\\z\\data\\mariadb\\svc_1", ["blog", "shop"]);
    expect(m).toContain("D:\\z\\data\\mariadb\\svc_1");
    expect(m).toContain("every database (blog, shop), every user and the administrator password");
    expect(m).toContain("cannot be undone");
    expect(resetMessage({ name: "Cache", kind: "redis" }, "", [])).toContain("every key it stores");
  });
});
