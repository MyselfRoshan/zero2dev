// Mock of the 0.0.5 databases routes (docs/API.md, the 0.0.5 amendment, "Databases" and "File dialog").
//   export async function handle(ctx, m, p, body, res, url) -> true when it answered the request.
// ctx: { state, emit, send, fail, id, now, sleep, procs, setState, addLog, startProc, stopProc, fakeJob }
//
// Each database service gets its own databases (name -> size) and users, kept here. The password
// is the service's adminPassword, like the daemon. Imports and exports are fake jobs. A few
// file names steer the outcome, for the e2e test and for trying the UI:
//   a path containing "missing" or "nonexistent" does not exist (400);
//   an import of a path containing "bad" fails with a SQL error;
//   an export to a path containing "existing", or to a file exported before, exists (409).

const DB_KINDS = ["mariadb", "mysql", "postgres", "redis"];
const SYSTEM = ["mysql", "information_schema", "performance_schema", "sys", "postgres", "template0", "template1"];
const NAME_RE = /^[A-Za-z0-9_]{1,64}$/;

const servers = new Map(); // serviceId -> { databases: Map<name, size>, users: Set<string> }
const written = new Set(); // files the mock's exports wrote (lower case)
const busy = new Map(); // serviceId -> id of its running import or export
let dialogOpen = false;

const isAbs = (p) => /^[A-Za-z]:[\\/]/.test(p) || p.startsWith("/") || p.startsWith("\\\\");
const adminOf = (kind) => (kind === "postgres" ? "postgres" : kind === "redis" ? "" : "root");
function server(svc) {
  let s = servers.get(svc.id);
  if (!s) {
    s = { databases: new Map(), users: new Set(adminOf(svc.kind) ? [adminOf(svc.kind)] : []) };
    servers.set(svc.id, s);
  }
  return s;
}
const dataDir = (ctx, svc) => `${ctx.state.settings.homeDir}\\${svc.kind}\\${svc.id}`;
function info(ctx, svc) {
  const running = svc.state === "running";
  const sql = svc.kind !== "redis";
  const s = server(svc);
  const list = [...s.databases].map(([name, sizeBytes]) => ({ name, sizeBytes }));
  return {
    serviceId: svc.id,
    kind: svc.kind,
    running,
    user: adminOf(svc.kind),
    hasPassword: !!svc.adminPassword,
    databases: running && sql ? list.sort((a, b) => a.name.localeCompare(b.name)) : [],
    users: running && sql ? [...s.users].sort((a, b) => a.localeCompare(b)) : [],
    supports: { databases: sql, dumps: sql, password: true, reset: true },
    dataDir: dataDir(ctx, svc),
  };
}
const nameError = (what, n) =>
  typeof n !== "string" || !NAME_RE.test(n) ? `${what} name must be 1-64 letters, digits or _` : "";
const systemError = (n) => (SYSTEM.includes(String(n).toLowerCase()) ? `${n} is a system database` : "");
const conflict = (ctx, res, message, details) =>
  ctx.send(res, 409, { error: { code: "conflict", message, ...(details ? { details } : {}) } });
const unknownField = (body, allowed) => Object.keys(body ?? {}).find((k) => !allowed.includes(k));

async function dialogFile(ctx, body, res) {
  const b = body ?? {};
  const bad = (msg) => ctx.fail(res, 400, "bad_request", msg);
  const field = unknownField(b, ["title", "start", "save", "defaultName", "filters"]);
  if (field) return bad(`unknown field ${field}`);
  if (b.title !== undefined && (typeof b.title !== "string" || b.title.length > 100 || /[\r\n]/.test(b.title)))
    return bad("title must be at most 100 characters on one line");
  if (b.filters !== undefined) {
    if (!Array.isArray(b.filters) || b.filters.length > 10) return bad("at most 10 filters");
    if (b.filters.some((f) => !f || !f.name || !f.pattern)) return bad("a filter needs a name and a pattern");
  }
  if (b.defaultName !== undefined && /[\\/:\r\n]/.test(String(b.defaultName)))
    return bad("defaultName must be a file name without a folder");
  if (dialogOpen) return conflict(ctx, res, "another dialog is open");
  // The real daemon shows the Windows Open / Save as dialog; the mock answers after a short pause,
  // like the folder dialog: a file in the start folder (or a fixed one), or "cancelled" when asked.
  dialogOpen = true;
  try {
    await ctx.sleep(400);
  } finally {
    dialogOpen = false;
  }
  const start = typeof b.start === "string" ? b.start.replace(/[\\/]+$/, "") : "";
  if (/cancel/i.test(start) || /cancel/i.test(b.title || "")) return ctx.send(res, 200, { path: "", cancelled: true });
  const dir = start || (b.save ? "C:\\Users\\me\\Documents" : "C:\\Users\\me\\Downloads");
  const name = b.save ? b.defaultName || "dump.sql" : "shop.sql";
  return ctx.send(res, 200, { path: `${dir}\\${name}`, cancelled: false });
}

const ROUTES = ["GET ", "POST databases", "POST users", "POST import", "POST export", "POST reset", "PUT password"];

export async function handle(ctx, m, p, body, res) {
  if (m === "POST" && p === "/api/dialog/file") {
    await dialogFile(ctx, body, res);
    return true;
  }
  const mm = p.match(/^\/api\/services\/([^/]+)\/database(?:\/(databases|users|import|export|reset|password))?$/);
  if (!mm) return false;
  const [, sid, action = ""] = mm;
  const route = `${m} ${action}`;
  if (!ROUTES.includes(route)) return false;
  const reply = (status, b) => (ctx.send(res, status, b), true);
  const bad = (msg) => (ctx.fail(res, 400, "bad_request", msg), true);
  const busyWith = (msg, details) => (conflict(ctx, res, msg, details), true);

  const svc = ctx.state.services.find((s) => s.id === sid);
  if (!svc) return (ctx.fail(res, 404, "not_found", `service ${sid} not found`), true);
  if (!DB_KINDS.includes(svc.kind)) return bad(`${svc.name} is not a database`);
  svc.adminPassword ??= "";
  const sql = svc.kind !== "redis";
  const s = server(svc);
  // The checks every route shares, in the daemon's order. Each returns true once it has answered.
  const needSql = () => !sql && bad(`${svc.name} is Redis: it has no databases, users or dumps`);
  const needRunning = () => svc.state !== "running" && busyWith(`start ${svc.name} first`);
  const noJob = () => busy.has(svc.id) && busyWith(`${svc.name} is busy with another job`, { jobId: busy.get(svc.id) });

  if (route === "GET ") return reply(200, info(ctx, svc));

  if (route === "POST databases") {
    if (needSql() || needRunning()) return true;
    const name = body?.name;
    const err = nameError("database", name) || systemError(name);
    if (err) return bad(err);
    if (s.databases.has(name)) return busyWith(`database ${name} exists`);
    s.databases.set(name, 0);
    ctx.addLog(svc, "system", `CREATE DATABASE \`${name}\``);
    return reply(201, { name });
  }

  if (route === "POST users") {
    if (needSql() || needRunning()) return true;
    const field = unknownField(body, ["user", "password", "database"]);
    if (field) return bad(`unknown field ${field}`);
    const { user, password, database = "" } = body ?? {};
    const err = nameError("user", user);
    if (err) return bad(err);
    if (typeof password !== "string" || /[\r\n]/.test(password)) return bad("password must be one line of text");
    if (database && !s.databases.has(database)) return bad(`database ${database} does not exist`);
    if (s.users.has(user)) return busyWith(`user ${user} exists`);
    s.users.add(user);
    ctx.addLog(svc, "system", `CREATE USER '${user}'${database ? `; GRANT ALL ON \`${database}\`.*` : ""}`);
    return reply(201, { user, database });
  }

  if (route === "POST import") {
    if (needSql() || needRunning() || noJob()) return true;
    const field = unknownField(body, ["database", "filePath", "create"]);
    if (field) return bad(`unknown field ${field}`);
    const { database, filePath = "", create = false } = body ?? {};
    const err = nameError("database", database) || systemError(database);
    if (err) return bad(err);
    if (typeof filePath !== "string" || !isAbs(filePath)) return bad("filePath must be an absolute path");
    if (/missing|nonexistent/i.test(filePath)) return bad(`${filePath} does not exist`);
    if (!s.databases.has(database) && !create) return bad(`database ${database} does not exist`);
    if (!s.databases.has(database)) s.databases.set(database, 0);
    ctx.addLog(svc, "system", `importing ${filePath} into ${database}`);
    const job = ctx.fakeJob({
      type: "db-import",
      fields: { serviceId: svc.id },
      steps: ["Importing", "Importing", "Importing", "Importing"],
      ms: 350,
      work: async () => {
        busy.delete(svc.id);
        if (/bad/i.test(filePath)) {
          ctx.addLog(svc, "system", "import failed");
          throw new Error("ERROR 1064 (42000) at line 12: You have an error in your SQL syntax");
        }
        const grown = (s.databases.get(database) ?? 0) + 2_400_000 + Math.floor(Math.random() * 400_000);
        s.databases.set(database, grown);
        ctx.addLog(svc, "system", `imported ${filePath} into ${database}`);
        return {};
      },
    });
    busy.set(svc.id, job.id);
    return reply(202, job);
  }

  if (route === "POST export") {
    if (needSql() || needRunning() || noJob()) return true;
    const field = unknownField(body, ["database", "filePath", "overwrite"]);
    if (field) return bad(`unknown field ${field}`);
    const { database, filePath = "", overwrite = false } = body ?? {};
    const err = nameError("database", database) || systemError(database);
    if (err) return bad(err);
    if (typeof filePath !== "string" || !isAbs(filePath)) return bad("filePath must be an absolute path");
    if (!s.databases.has(database)) return bad(`database ${database} does not exist`);
    if (!overwrite && (written.has(filePath.toLowerCase()) || /existing/i.test(filePath)))
      return busyWith(`${filePath} exists`);
    ctx.addLog(svc, "system", `exporting ${database} to ${filePath}`);
    const job = ctx.fakeJob({
      type: "db-export",
      fields: { serviceId: svc.id },
      steps: ["Exporting", "Exporting", "Exporting"],
      ms: 300,
      work: async () => {
        busy.delete(svc.id);
        written.add(filePath.toLowerCase());
        ctx.addLog(svc, "system", `exported ${database} to ${filePath}`);
        return { path: filePath };
      },
    });
    busy.set(svc.id, job.id);
    return reply(202, job);
  }

  if (route === "POST reset") {
    if (["running", "starting", "stopping"].includes(svc.state)) return busyWith(`stop ${svc.name} first`);
    if (noJob()) return true;
    servers.delete(svc.id);
    svc.adminPassword = "";
    ctx.addLog(svc, "system", `reset: deleted ${dataDir(ctx, svc)}`);
    ctx.emit("services.changed");
    return reply(200, svc);
  }

  // PUT password
  if (needRunning()) return true;
  const field = unknownField(body, ["password"]);
  if (field) return bad(`unknown field ${field}`);
  const pw = body?.password;
  if (typeof pw !== "string" || /[\r\n]/.test(pw)) return bad("password must be one line of text");
  if (pw.length > 128) return bad("password must be at most 128 characters");
  svc.adminPassword = pw;
  ctx.addLog(svc, "system", pw ? "administrator password set" : "administrator password removed");
  ctx.emit("services.changed");
  return reply(200, svc);
}
