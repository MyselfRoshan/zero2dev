// Mock of the 0.0.5 Bring in routes (docs/API.md, the 0.0.5 amendment, "Bring in (migrations)").
//   export async function handle(ctx, m, p, body, res, url) -> true when it answered the request.
// ctx: { state, emit, send, fail, id, now, sleep, procs, setState, addLog, startProc, stopProc, fakeJob }
//
// Sources are made up but plausible: VS Code (settings + extensions), VSCodium not found,
// Notepad++, HeidiSQL (registry), DBeaver, one source per registered runtime, npm/Go caches,
// Composer not found, and "database" (a server the user names). Blockers follow the mock's
// state: "Install <program> in Zero2Dev first" until that runtime is installed, and
// "Close <program> first" while the e2e marks the program as running through the mock-only
// route POST /api/mock/migrations/running { program, running }.

const HOME = "C:\\Users\\me";
const ROAMING = `${HOME}\\AppData\\Roaming`;
const LOCAL = `${HOME}\\AppData\\Local`;
const MB = 1_000_000;

const PROGRAMS = {
  vscode: {
    label: "VS Code",
    found: true,
    items: [
      ["Settings", "dir", `${ROAMING}\\Code\\User`, "code\\user-data\\User", 2_048_000],
      ["Extensions", "dir", `${HOME}\\.vscode\\extensions`, "code\\extensions", 412 * MB],
    ],
    note: "Logins kept by Windows (Settings Sync) are not moved. Sign in again in Zero2Dev's VS Code.",
  },
  vscodium: {
    label: "VSCodium",
    found: false,
    items: [
      ["Settings", "dir", `${ROAMING}\\VSCodium\\User`, "codium\\user-data\\User", 0],
      ["Extensions", "dir", `${HOME}\\.vscode-oss\\extensions`, "codium\\extensions", 0],
    ],
    note: "",
  },
  notepadpp: {
    label: "Notepad++",
    found: true,
    items: [
      ["Settings and shortcuts", "dir", `${ROAMING}\\Notepad++`, "notepadpp", 1_200_000],
      ["Themes", "dir", `${ROAMING}\\Notepad++\\themes`, "notepadpp\\themes", 340_000],
      ["Plugin settings", "dir", `${ROAMING}\\Notepad++\\plugins\\config`, "notepadpp\\plugins\\config", 90_000],
    ],
    note: "",
  },
  heidisql: {
    label: "HeidiSQL",
    found: true,
    items: [
      ["Sessions and settings", "registry", "HKCU\\Software\\HeidiSQL", "heidisql\\portable_settings.txt", 48_000],
    ],
    note: "Move also deletes the registry key, after saving it to a .reg file. Saved passwords come along.",
  },
  dbeaver: {
    label: "DBeaver",
    found: true,
    items: [["Workspace and connections", "dir", `${ROAMING}\\DBeaverData\\workspace6`, "dbeaver\\workspace", 38 * MB]],
    note: "",
  },
  "npm-global": {
    label: "Global npm packages",
    found: true,
    items: [["Global packages", "dir", `${ROAMING}\\npm`, "node\\global", 64 * MB]],
    note: "Zero2Dev's Node.js then installs global packages there (npm_config_prefix).",
  },
  "npm-cache": {
    label: "npm download cache",
    found: true,
    items: [["Cache", "dir", `${LOCAL}\\npm-cache`, "node\\npm-cache", 1_300 * MB]],
    note: "A cache only saves downloads. Leaving it behind loses nothing.",
  },
  "go-cache": {
    label: "Go module and build cache",
    found: true,
    items: [
      ["Module cache", "dir", `${HOME}\\go\\pkg\\mod`, "go\\pkg\\mod", 820 * MB],
      ["Build cache", "dir", `${LOCAL}\\go-build`, "go\\cache", 260 * MB],
    ],
    note: "Zero2Dev's Go then uses these folders (GOMODCACHE and GOCACHE).",
  },
  composer: {
    label: "Composer home",
    found: false,
    items: [["Composer home", "dir", `${ROAMING}\\Composer`, "composer", 0]],
    note: "",
  },
};
const EDITORS = ["vscode", "vscodium", "notepadpp", "heidisql", "dbeaver"];
const PORTABLE_KEY = { "npm-global": "npmGlobal", composer: "composer", "go-cache": "goCache" };
const RUNTIME_SIZES = { node: 98 * MB, go: 250 * MB, php: 44 * MB, java: 310 * MB, nginx: 9 * MB, caddy: 42 * MB };
const RUNTIME_REASON =
  "A runtime's files belong to the program's own installer. Moving them would break its uninstaller and needs administrator rights.";
const DB_REASON = "Zero2Dev does not remove databases from a server it does not own.";

// The databases of the fake servers, by family.
const REMOTE = {
  mysql: [
    { name: "shop", sizeBytes: 48 * MB },
    { name: "blog", sizeBytes: 6 * MB },
    { name: "crm", sizeBytes: 120 * MB },
  ],
  postgres: [
    { name: "inventory", sizeBytes: 210 * MB },
    { name: "analytics", sizeBytes: 1_100 * MB },
  ],
};

const history = []; // Migration[], newest first
const running = new Set(); // programs the e2e marks as running
const busy = new Map(); // source id -> job id while a job works on it
const adopted = new Map(); // runtimeId -> the registered binary it came from

const dataDir = (ctx) => ctx.state.settings.homeDir;
const exeDir = (ctx) => ctx.state.settings.homeDir.replace(/\\data$/, "");
const dirName = (f) => f.replace(/[\\/][^\\/]*$/, "");
const installedIn = (ctx, kind) => ctx.state.runtimes.some((r) => r.kind === kind && r.source === "installed");
const active = (source) => history.find((m) => m.source === source && m.state === "done");

function programSource(ctx, program) {
  const d = PROGRAMS[program];
  const editor = EDITORS.includes(program);
  const blockers = [];
  if (d.found && editor && !installedIn(ctx, program)) blockers.push(`Install ${d.label} in Zero2Dev first`);
  if (d.found && running.has(program)) blockers.push(`Close ${d.label} first`);
  const cur = active(program);
  return {
    id: program,
    program,
    label: d.label,
    found: d.found,
    items: d.items.map(([label, kind, from, to, sizeBytes]) => ({
      label,
      kind,
      from,
      to: `${dataDir(ctx)}\\${to}`,
      sizeBytes,
      // A moved item is no longer at its old place.
      exists: d.found && !(cur?.mode === "move" && cur.items.some((i) => i.label === label)),
    })),
    modes: ["copy", "move"],
    copyOnlyReason: "",
    blockers,
    // Zero2Dev's own Notepad++ and DBeaver write a default config on first start; a copy fills it.
    targetHasData: !!cur || (editor && installedIn(ctx, program) && ["notepadpp", "dbeaver"].includes(program)),
    note: d.note,
  };
}

function runtimeSource(ctx, rt) {
  return {
    id: `runtime:${rt.id}`,
    program: "runtime",
    label: rt.name,
    found: true,
    items: [
      {
        label: "Program files",
        kind: "runtime",
        from: rt.kind === "java" || rt.kind === "go" ? dirName(dirName(rt.binary)) : dirName(rt.binary),
        to: `${exeDir(ctx)}\\adopted\\${rt.kind}-${rt.version}`,
        sizeBytes: RUNTIME_SIZES[rt.kind] ?? 20 * MB,
        exists: true,
      },
    ],
    modes: ["copy"],
    copyOnlyReason: RUNTIME_REASON,
    blockers: [],
    targetHasData: false,
    runtimeId: rt.id,
    note: "Services that use it keep working: the same runtime then runs from Zero2Dev's folder.",
  };
}

const dbServices = (ctx) => ctx.state.services.filter((s) => ["mariadb", "mysql", "postgres"].includes(s.kind));

function databaseSource(ctx) {
  return {
    id: "database",
    program: "database",
    label: "Databases from another server",
    found: true,
    items: [],
    modes: ["copy"],
    copyOnlyReason: DB_REASON,
    blockers: dbServices(ctx).length ? [] : ["Create a MariaDB, MySQL or PostgreSQL service in Zero2Dev first"],
    targetHasData: false,
    note: "Each database is copied with a dump and an import. The users and passwords of that server are not copied.",
  };
}

function sources(ctx) {
  const out = [];
  for (const p of EDITORS) out.push(programSource(ctx, p));
  for (const rt of ctx.state.runtimes) if (rt.source === "registered") out.push(runtimeSource(ctx, rt));
  for (const p of ["npm-global", "npm-cache", "go-cache", "composer"]) out.push(programSource(ctx, p));
  out.push(databaseSource(ctx));
  return out;
}

const sum = (items) => items.reduce((n, i) => n + i.sizeBytes, 0);
const size = (b) =>
  b >= 1e9 ? `${(b / 1e9).toFixed(1)} GB` : b >= 1e6 ? `${Math.round(b / 1e6)} MB` : `${Math.round(b / 1e3)} KB`;
const words = (a) => (a.length <= 1 ? a.join("") : `${a.slice(0, -1).join(", ")} and ${a[a.length - 1]}`);
const fam = (k) => (k === "postgres" ? "postgres" : k === "mariadb" || k === "mysql" ? "mysql" : k);

function checkConn(b) {
  if (!b || typeof b !== "object") return "body is required";
  if (!["mariadb", "mysql", "postgres"].includes(b.kind)) return "kind must be mariadb, mysql or postgres";
  if (!String(b.host ?? "").trim()) return "host is required";
  if (!Number.isInteger(b.port) || b.port < 1 || b.port > 65535) return "port must be 1-65535";
  if (!String(b.user ?? "").trim()) return "user is required";
  return "";
}
// The fake servers: any host answers on 3306 (MySQL/MariaDB) and 5432 (PostgreSQL);
// the host "unreachable" and the password "wrong" fail like a real server would.
function connect(b) {
  const family = fam(b.kind);
  const port = family === "postgres" ? 5432 : 3306;
  if (b.host === "unreachable" || b.port !== port) return { error: `could not connect to ${b.host}:${b.port}` };
  if (b.password === "wrong")
    return {
      error:
        family === "postgres"
          ? `password authentication failed for user "${b.user}"`
          : `Access denied for user '${b.user}'@'${b.host}' (using password: YES)`,
    };
  return { databases: REMOTE[family] };
}

function revertOptions(src, mode) {
  if (src.program === "runtime" || src.program === "database") return ["discard"];
  return mode === "move" ? ["move-back"] : ["discard", "copy-back"];
}

const busyError = (ctx, res, label, jobId) =>
  ctx.send(res, 409, {
    error: { code: "conflict", message: `${label} is busy; try again when it is done`, details: { jobId } },
  });

function startMigration(ctx, body, res) {
  const { send, fail } = ctx;
  if (!body || typeof body !== "object") return fail(res, 400, "bad_request", "body is required");
  const src = sources(ctx).find((s) => s.id === body.source);
  if (!src) return fail(res, 400, "bad_request", `unknown source ${body.source}`);
  if (!["copy", "move"].includes(body.mode)) return fail(res, 400, "bad_request", "mode must be copy or move");
  if (!src.modes.includes(body.mode))
    return fail(res, 400, "bad_request", `${src.label} can only be copied: ${src.copyOnlyReason}`);
  if (!src.found) return fail(res, 400, "bad_request", `${src.label} was not found on this PC`);
  if (busy.has(src.id)) return busyError(ctx, res, src.label, busy.get(src.id));
  if (src.blockers.length) return fail(res, 409, "conflict", src.blockers[0]);
  const present = src.items.filter((i) => i.exists);
  const wanted = Array.isArray(body.items) ? body.items : [];
  const unknown = wanted.find((l) => !present.some((i) => i.label === l));
  if (unknown) return fail(res, 400, "bad_request", `unknown item ${unknown}`);
  const items = wanted.length ? present.filter((i) => wanted.includes(i.label)) : present;
  if (src.program !== "database" && !items.length) return fail(res, 400, "bad_request", "nothing to bring in");
  if (src.program !== "database" && active(src.id))
    return fail(res, 409, "conflict", `${src.label} was brought in already; revert that first`);

  let db = null;
  let target = null;
  let dbItems = [];
  if (src.program === "database") {
    const d = body.database;
    const err = checkConn(d);
    if (err) return fail(res, 400, "bad_request", err);
    if (!Array.isArray(d.databases) || !d.databases.length)
      return fail(res, 400, "bad_request", "pick at least one database");
    target = ctx.state.services.find((s) => s.id === d.targetServiceId);
    if (!target) return fail(res, 400, "bad_request", `service ${d.targetServiceId} not found`);
    if (fam(target.kind) !== fam(d.kind))
      return fail(
        res,
        400,
        "bad_request",
        `a ${d.kind === "postgres" ? "PostgreSQL" : "MySQL or MariaDB"} database cannot go into ${target.name}`,
      );
    const r = connect(d);
    if (r.error) return fail(res, 400, "bad_request", r.error);
    const missing = d.databases.find((n) => !r.databases.some((x) => x.name === n));
    if (missing) return fail(res, 400, "bad_request", `database ${missing} not found on ${d.host}:${d.port}`);
    if (target.state !== "running") return fail(res, 409, "conflict", `start ${target.name} first`);
    db = d;
    dbItems = r.databases
      .filter((x) => d.databases.includes(x.name))
      .map((x) => ({
        label: x.name,
        kind: "databases",
        from: `${d.host}:${d.port}/${x.name}`,
        to: `${target.name}/${x.name}`,
        sizeBytes: x.sizeBytes,
        exists: true,
      }));
  }

  const migId = ctx.id("mig");
  const steps =
    src.program === "runtime"
      ? ["Copying", "Checking the copy", "Switching the runtime"]
      : src.program === "database"
        ? db.databases.flatMap((n) => [`Exporting ${n}`, `Importing ${n}`])
        : body.mode === "move"
          ? ["Saving what is there", "Moving", "Checking the copy", "Removing the originals"]
          : ["Saving what is there", "Copying", "Checking the copy"];
  const job = ctx.fakeJob({
    type: "migrate",
    fields: { migrationId: migId, ...(target ? { serviceId: target.id } : {}), runtimeId: src.runtimeId ?? "" },
    steps,
    ms: 300,
    work: () => {
      busy.delete(src.id);
      const took = src.program === "database" ? dbItems : items;
      const verb = body.mode === "move" ? "Moved" : "Copied";
      const rec = {
        id: migId,
        source: src.id,
        program: src.program,
        label: src.label,
        mode: body.mode,
        createdAt: ctx.now(),
        state: "done",
        revertedAt: null,
        items: took,
        revertOptions: revertOptions(src, body.mode),
        description:
          src.program === "database"
            ? `Copied ${words(db.databases)} (${size(sum(took))}) from ${db.host}:${db.port} into ${target.name}.`
            : src.program === "runtime"
              ? `Adopted ${src.label} (${size(sum(took))}) into Zero2Dev's folder.`
              : `${verb} ${words(took.map((i) => i.label.toLowerCase()))} (${size(sum(took))}) of ${src.label} into Zero2Dev.`,
      };
      if (db) Object.assign(rec, { databases: db.databases, serviceId: target.id });
      if (src.program === "runtime") {
        const rt = ctx.state.runtimes.find((r) => r.id === src.runtimeId);
        if (rt) {
          adopted.set(rt.id, rt.binary);
          rt.binary = `${took[0].to}\\${rt.binary.slice(took[0].from.length + 1)}`;
          rt.source = "installed";
          rec.runtimeId = rt.id;
          ctx.emit("runtimes.changed");
        }
      }
      if (PORTABLE_KEY[src.program]) {
        const cur = { npmGlobal: false, composer: false, goCache: false, ...ctx.state.settings.portable };
        ctx.state.settings.portable = { ...cur, [PORTABLE_KEY[src.program]]: true };
      }
      history.unshift(rec);
      ctx.emit("migrations.changed", {});
      return { migrationId: migId };
    },
  });
  busy.set(src.id, job.id);
  return send(res, 202, job);
}

function revert(ctx, mig, body, res) {
  const { send, fail } = ctx;
  const how = body?.how;
  if (!["discard", "copy-back", "move-back"].includes(how))
    return fail(res, 400, "bad_request", "how must be discard, copy-back or move-back");
  if (mig.state !== "done") return fail(res, 409, "conflict", `${mig.label} was ${mig.state} already`);
  if (!mig.revertOptions.includes(how))
    return fail(
      res,
      400,
      "bad_request",
      `this migration cannot be undone with ${how}; use ${mig.revertOptions.join(" or ")}`,
    );
  if (busy.has(mig.source)) return busyError(ctx, res, mig.label, busy.get(mig.source));
  if (running.has(mig.program)) return fail(res, 409, "conflict", `Close ${mig.label} first`);
  const steps =
    how === "move-back"
      ? ["Saving what is there", "Moving back"]
      : how === "copy-back"
        ? ["Saving the originals", "Copying back"]
        : mig.program === "database"
          ? (mig.databases ?? []).map((n) => `Dropping ${n}`)
          : ["Removing the copy", "Bringing back what was there"];
  const job = ctx.fakeJob({
    type: "migrate-revert",
    fields: {
      migrationId: mig.id,
      ...(mig.serviceId ? { serviceId: mig.serviceId } : {}),
      runtimeId: mig.runtimeId ?? "",
    },
    steps,
    ms: 300,
    work: () => {
      busy.delete(mig.source);
      mig.state = "reverted";
      mig.revertedAt = ctx.now();
      if (mig.program === "runtime" && mig.runtimeId) {
        const rt = ctx.state.runtimes.find((r) => r.id === mig.runtimeId);
        if (rt && adopted.has(rt.id)) {
          rt.binary = adopted.get(rt.id);
          rt.source = "registered";
          adopted.delete(rt.id);
          ctx.emit("runtimes.changed");
        }
      }
      if (PORTABLE_KEY[mig.program] && ctx.state.settings.portable)
        ctx.state.settings.portable = { ...ctx.state.settings.portable, [PORTABLE_KEY[mig.program]]: false };
      ctx.emit("migrations.changed", {});
      return { migrationId: mig.id };
    },
  });
  busy.set(mig.source, job.id);
  return send(res, 202, job);
}

export async function handle(ctx, m, p, body, res) {
  const { send, fail } = ctx;
  if (m === "GET" && p === "/api/migrations/sources") {
    await ctx.sleep(150); // the daemon measures folders
    send(res, 200, sources(ctx));
    return true;
  }
  if (m === "POST" && p === "/api/migrations/databases") {
    const err = checkConn(body);
    if (err) {
      fail(res, 400, "bad_request", err);
      return true;
    }
    await ctx.sleep(200);
    const r = connect(body);
    if (r.error) fail(res, 400, "bad_request", r.error);
    else send(res, 200, { databases: r.databases });
    return true;
  }
  if (p === "/api/migrations" && (m === "GET" || m === "POST")) {
    if (m === "GET") send(res, 200, history);
    else startMigration(ctx, body, res);
    return true;
  }
  const mm = p.match(/^\/api\/migrations\/([^/]+)\/revert$/);
  if (mm && m === "POST") {
    const mig = history.find((x) => x.id === decodeURIComponent(mm[1]));
    if (!mig) fail(res, 404, "not_found", `migration ${mm[1]} not found`);
    else revert(ctx, mig, body, res);
    return true;
  }
  // Mock only (used by the e2e): mark an installed program as running, so its source shows
  // "Close <program> first".
  if (m === "POST" && p === "/api/mock/migrations/running") {
    if (body?.running) running.add(body.program);
    else running.delete(body?.program);
    send(res, 204);
    return true;
  }
  return false;
}
