// Dependency-free mock of the Zero2Dev daemon (docs/API.md) for UI development and tests.
//   node mock/server.mjs                 -> http://127.0.0.1:7000  (token: devtoken)
//   MOCK_PORT=7100 MOCK_EMPTY=1 node ... -> different port, no seed data
// Host/Origin are NOT enforced. If ../internal/webui/dist exists it is served at / (same origin).
import http from "node:http";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { randomBytes } from "node:crypto";

const PORT = Number(process.env.MOCK_PORT || 7000);
const TOKEN = process.env.MOCK_TOKEN || "devtoken";
const EMPTY = process.env.MOCK_EMPTY === "1";
const DIST =
  process.env.MOCK_STATIC || path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../../internal/webui/dist");
const started = Date.now();
const id = (p) => `${p}_${randomBytes(4).toString("hex")}`;
const now = () => new Date().toISOString();

const state = {
  services: [],
  runtimes: [],
  sites: [],
  settings: {
    apiPort: PORT,
    homeDir: "D:\\zero2dev\\bin\\data",
    domainSuffix: "localhost",
    proxyHttpPort: 8080,
    proxyHttpsPort: 8443,
    openBrowserOnStart: true,
    trustLocalCA: false,
    downloadProxy: "",
  },
};
const EXE_DIR = "D:\\zero2dev\\bin";
const installerLog = [];
function ilog(msg) {
  installerLog.push(`${now()} ${msg}`);
  if (installerLog.length > 1000) installerLog.shift();
}
const logs = new Map(); // serviceId -> lines[]
const procs = new Map(); // serviceId -> { timer, n }
const jobs = new Map();
const tickets = new Map();
const clients = new Set();

const CATALOG = [
  {
    id: "node-22.11.0",
    kind: "node",
    version: "22.11.0",
    name: "Node.js 22.11.0",
    url: "https://nodejs.org/dist/v22.11.0/node-v22.11.0-win-x64.zip",
    checksum: { type: "shasums", url: "https://nodejs.org/dist/v22.11.0/SHASUMS256.txt" },
    binaryRelPath: "node.exe",
    stripComponents: 1,
    os: "windows",
    arch: "amd64",
  },
  {
    id: "node-20.18.0",
    kind: "node",
    version: "20.18.0",
    name: "Node.js 20.18.0",
    url: "https://nodejs.org/dist/v20.18.0/node-v20.18.0-win-x64.zip",
    checksum: { type: "shasums", url: "https://nodejs.org/dist/v20.18.0/SHASUMS256.txt" },
    binaryRelPath: "node.exe",
    stripComponents: 1,
    os: "windows",
    arch: "amd64",
  },
  {
    id: "go-1.23.3",
    kind: "go",
    version: "1.23.3",
    name: "Go 1.23.3",
    url: "https://go.dev/dl/go1.23.3.windows-amd64.zip",
    checksum: { type: "sha256", value: "00" },
    binaryRelPath: "bin/go.exe",
    stripComponents: 1,
    os: "windows",
    arch: "amd64",
  },
  {
    id: "caddy-2.8.4",
    kind: "caddy",
    version: "2.8.4",
    name: "Caddy 2.8.4",
    url: "https://github.com/caddyserver/caddy/releases/download/v2.8.4/caddy_2.8.4_windows_amd64.zip",
    checksum: {
      type: "shasums",
      url: "https://github.com/caddyserver/caddy/releases/download/v2.8.4/caddy_2.8.4_checksums.txt",
    },
    binaryRelPath: "caddy.exe",
    stripComponents: 0,
    os: "windows",
    arch: "amd64",
  },
  {
    id: "go-1.23.3-arm64",
    kind: "go",
    version: "1.23.3",
    name: "Go 1.23.3 (ARM64)",
    url: "https://go.dev/dl/go1.23.3.windows-arm64.zip",
    checksum: { type: "sha256", value: "00" },
    binaryRelPath: "bin/go.exe",
    stripComponents: 1,
    os: "windows",
    arch: "arm64",
  },
];
const THIS_PC = { os: "windows", arch: "amd64" };

const DETECT = [
  { kind: "node", version: "20.11.1", binary: "C:\\Program Files\\nodejs\\node.exe" },
  { kind: "go", version: "1.22.5", binary: "C:\\Program Files\\Go\\bin\\go.exe" },
  { kind: "php", version: "8.3.12", binary: "C:\\tools\\php\\php.exe" },
  { kind: "java", version: "21.0.4", binary: "C:\\Program Files\\Java\\jdk-21\\bin\\java.exe" },
];

function newService(input) {
  return { ...input, id: id("svc"), state: "stopped", pid: 0, startedAt: null, lastError: "" };
}
function siteView(s) {
  const port = s.https ? state.settings.proxyHttpsPort : state.settings.proxyHttpPort;
  return { ...s, url: `${s.https ? "https" : "http"}://${s.domain}:${port}` };
}

if (!EMPTY) {
  state.runtimes.push({
    id: id("rt"),
    kind: "node",
    version: "20.11.1",
    name: "Node.js 20.11.1",
    binary: DETECT[0].binary,
    source: "registered",
    installedAt: now(),
  });
  state.services.push(
    newService({
      name: "Docs site",
      kind: "node",
      command: "",
      args: ["server.js", "--port", "{port}"],
      cwd: "C:\\work\\docs",
      env: { NODE_ENV: "development" },
      port: 3000,
      healthPath: "/",
      runtimeId: "",
      autostart: false,
    }),
    newService({
      name: "Worker",
      kind: "custom",
      command: "C:\\tools\\worker.exe",
      args: [],
      cwd: "C:\\work\\worker",
      env: {},
      port: 0,
      healthPath: "",
      runtimeId: "",
      autostart: true,
    }),
  );
}

// ---------- helpers ----------
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
function send(res, status, body) {
  if (status === 204) {
    res.writeHead(204);
    return res.end();
  }
  const json = JSON.stringify(body);
  res.writeHead(status, { "Content-Type": "application/json", "Content-Length": Buffer.byteLength(json) });
  res.end(json);
}
const fail = (res, status, code, message) => send(res, status, { error: { code, message } });
async function readBody(req) {
  const chunks = [];
  for await (const c of req) chunks.push(c);
  if (!chunks.length) return undefined;
  return JSON.parse(Buffer.concat(chunks).toString("utf8"));
}
function emit(type, data = {}) {
  const frame = `event: ${type}\ndata: ${JSON.stringify(data)}\n\n`;
  for (const c of clients) c.write(frame);
}
function addLog(svc, stream, text) {
  const line = { ts: now(), stream, text };
  const arr = logs.get(svc.id) ?? [];
  arr.push(line);
  if (arr.length > 2000) arr.shift();
  logs.set(svc.id, arr);
  emit("log.line", { serviceId: svc.id, ...line });
}
const isAbs = (p) => /^[A-Za-z]:[\\/]/.test(p) || p.startsWith("/") || p.startsWith("\\\\");

function validateService(b) {
  const kinds = ["custom", "node", "php", "go", "java", "caddy"];
  const allowed = ["name", "kind", "command", "args", "cwd", "env", "port", "healthPath", "runtimeId", "autostart"];
  if (!b || typeof b !== "object") return "invalid JSON body";
  for (const k of Object.keys(b)) if (!allowed.includes(k)) return `unknown field ${k}`;
  if (typeof b.name !== "string" || b.name.trim().length < 1 || b.name.length > 64)
    return "name must be 1-64 characters";
  if (!kinds.includes(b.kind)) return "kind must be one of " + kinds.join(", ");
  if (b.kind === "custom" && !b.command) return "command is required for custom services";
  if ((b.kind === "node" || b.kind === "java") && !(b.args || []).length && !b.command)
    return `args required for ${b.kind} services`;
  if (typeof b.cwd !== "string" || !isAbs(b.cwd)) return "cwd must be an absolute path";
  if (b.cwd.includes("nonexistent")) return `cwd ${b.cwd} does not exist`;
  if (!Number.isInteger(b.port ?? 0) || (b.port ?? 0) < 0 || (b.port ?? 0) > 65535) return "port must be 0-65535";
  return null;
}
function normalizeService(b) {
  return {
    name: b.name.trim(),
    kind: b.kind,
    command: b.command || "",
    args: b.args || [],
    cwd: b.cwd,
    env: b.env || {},
    port: b.port || 0,
    healthPath: b.healthPath || "",
    runtimeId: b.runtimeId || "",
    autostart: !!b.autostart,
  };
}

function setState(svc, st, extra = {}) {
  Object.assign(svc, { state: st }, extra);
  emit("service.status", {
    id: svc.id,
    state: svc.state,
    pid: svc.pid,
    startedAt: svc.startedAt,
    lastError: svc.lastError,
  });
}

function startProc(svc) {
  svc.lastError = "";
  setState(svc, "starting", { pid: 0, startedAt: null, lastError: "" });
  addLog(svc, "system", `starting ${svc.command || svc.kind} ${svc.args.join(" ")}`.trim());
  const entry = { timer: null, n: 0, boot: null };
  procs.set(svc.id, entry);
  entry.boot = setTimeout(() => {
    if (svc.command === "fail" || svc.name.toLowerCase().includes("crash")) {
      addLog(svc, "stderr", "Error: listen EADDRINUSE: address already in use");
      clearInterval(entry.timer);
      procs.delete(svc.id);
      return setState(svc, "failed", { pid: 0, lastError: "exit status 1" });
    }
    setState(svc, "running", { pid: 4000 + Math.floor(Math.random() * 5000), startedAt: now() });
    for (const l of [
      "loading configuration",
      "connected to database (sqlite)",
      "migrations: 12 applied, 0 pending",
      "compiled 148 modules",
    ])
      addLog(svc, "stdout", l);
    addLog(svc, "stdout", svc.port ? `listening on http://127.0.0.1:${svc.port}` : "ready");
    entry.timer = setInterval(() => {
      entry.n++;
      if (entry.n % 7 === 0) addLog(svc, "stderr", `warn: slow response (${200 + entry.n}ms) GET /api/items`);
      else addLog(svc, "stdout", `GET /api/items 200 ${3 + (entry.n % 9)}ms`);
    }, 1000);
  }, 1200);
}
function stopProc(svc) {
  const entry = procs.get(svc.id);
  if (entry) {
    clearTimeout(entry.boot);
    clearInterval(entry.timer);
    procs.delete(svc.id);
  }
  setState(svc, "stopping");
  setTimeout(() => {
    addLog(svc, "system", "stopped");
    setState(svc, "stopped", { pid: 0, startedAt: null });
  }, 600);
}

const fileNameOf = (c) => c.url.split("/").pop();
const homepageOf = (c) =>
  c.kind === "node"
    ? `https://nodejs.org/dist/v${c.version}/`
    : c.kind === "caddy"
      ? `https://github.com/caddyserver/caddy/releases/tag/v${c.version}`
      : "https://go.dev/dl/";

// Scenarios for testing failed jobs:
//  - installing node-20.18.0 fails while downloading (blocked network)
//  - importing a file whose path contains "locked" fails at Registering (antivirus)
function runInstall(entry, opts = {}) {
  const isImport = !!opts.filePath;
  let failAt = null;
  if (!isImport && entry.id === "node-20.18.0")
    failAt = {
      step: "Downloading",
      pct: 28,
      error: `Downloading: Get "${entry.url}": dial tcp 104.20.22.46:443: i/o timeout (after 3 attempts)`,
    };
  if (isImport && /locked/i.test(opts.filePath))
    failAt = {
      step: "Registering",
      pct: 96,
      error: `Registering: rename D:\\zero2dev\\bin\\data\\tmp\\stage-1\\${entry.binaryRelPath} D:\\zero2dev\\bin\\data\\runtimes\\${entry.id}\\${entry.binaryRelPath}: Access is denied (antivirus may be scanning the file; try again)`,
    };
  ilog(
    `${isImport ? "import" : "install"} start ${entry.id}${isImport ? ` file=${opts.filePath}` : ` url=${entry.url}`}`,
  );
  const job = {
    id: id("job"),
    type: isImport ? "import" : "install",
    state: "running",
    percent: 0,
    step: isImport ? "Importing" : "Downloading",
    error: "",
    runtimeId: "",
  };
  jobs.set(job.id, job);
  const steps = [
    [isImport ? "Importing" : "Downloading", 0, 70],
    ["Verifying", 70, 78],
    ["Extracting", 78, 94],
    ["Registering", 94, 99],
  ];
  let si = 0;
  let pct = 0;
  const t = setInterval(() => {
    const [name, , hi] = steps[si];
    pct = Math.min(hi, pct + (name === "Downloading" || name === "Importing" ? 7 : 5));
    job.step = name;
    job.percent = pct;
    emit("job.progress", { id: job.id, percent: pct, step: name });
    ilog(`${name} ${pct}%`);
    if (failAt && pct >= failAt.pct) {
      clearInterval(t);
      ilog(`ERROR ${failAt.error}`);
      Object.assign(job, { state: "failed", error: failAt.error });
      emit("job.done", { id: job.id, state: "failed", error: failAt.error, runtimeId: "" });
      return;
    }
    if (pct >= hi) si++;
    if (si >= steps.length) {
      clearInterval(t);
      const rt = {
        id: id("rt"),
        kind: entry.kind,
        version: entry.version,
        name: entry.name,
        binary: `D:\\zero2dev\\bin\\data\\runtimes\\${entry.id}\\${entry.binaryRelPath.replace("/", "\\")}`,
        source: "installed",
        installedAt: now(),
      };
      state.runtimes.push(rt);
      ilog(`done ${entry.id} -> ${rt.binary}`);
      Object.assign(job, { state: "done", percent: 100, runtimeId: rt.id, step: "Registering" });
      emit("job.done", { id: job.id, state: "done", error: "", runtimeId: rt.id });
      emit("runtimes.changed");
      if (rt.kind === "caddy" && !state.services.some((s) => s.kind === "caddy")) {
        state.services.push(
          newService({
            name: "Caddy",
            kind: "caddy",
            command: "",
            args: [],
            cwd: "D:\\zero2dev\\bin\\data",
            env: {},
            port: 0,
            healthPath: "",
            runtimeId: "",
            autostart: false,
          }),
        );
        emit("services.changed");
      }
    }
  }, 250);
  return job;
}

// ---------- router ----------
async function api(req, res, url) {
  const p = url.pathname;
  const m = req.method;

  if (m === "GET" && p === "/api/events") {
    const t = tickets.get(url.searchParams.get("ticket"));
    tickets.delete(url.searchParams.get("ticket"));
    if (!t || t < Date.now()) return fail(res, 401, "unauthorized", "invalid or expired ticket");
    res.writeHead(200, { "Content-Type": "text/event-stream", "Cache-Control": "no-cache", Connection: "keep-alive" });
    res.write(": hello\n\n");
    clients.add(res);
    const ping = setInterval(() => res.write(": ping\n\n"), 15000);
    req.on("close", () => {
      clearInterval(ping);
      clients.delete(res);
    });
    return;
  }

  if (req.headers.authorization !== `Bearer ${TOKEN}`)
    return fail(res, 401, "unauthorized", "missing or invalid access token");
  let m2;
  const body = m === "POST" || m === "PUT" ? await readBody(req).catch(() => null) : undefined;
  if (body === null) return fail(res, 400, "bad_request", "invalid JSON");

  if (m === "POST" && p === "/api/events/ticket") {
    const ticket = randomBytes(16).toString("hex");
    tickets.set(ticket, Date.now() + 30000);
    return send(res, 200, { ticket });
  }
  if (m === "GET" && p === "/api/status") {
    return send(res, 200, {
      version: "0.0.1",
      uptimeSeconds: Math.floor((Date.now() - started) / 1000),
      os: THIS_PC.os,
      arch: THIS_PC.arch,
      homeDir: state.settings.homeDir,
      homeSource: process.env.MOCK_HOME_SOURCE || "exe",
      portable: (process.env.MOCK_HOME_SOURCE || "exe") === "exe",
      exeDir: EXE_DIR,
      counts: {
        services: state.services.length,
        running: state.services.filter((s) => s.state === "running").length,
        runtimes: state.runtimes.length,
        sites: state.sites.length,
      },
    });
  }

  // services
  if (p === "/api/services") {
    if (m === "GET")
      return send(
        res,
        200,
        [...state.services].sort((a, b) => a.name.localeCompare(b.name)),
      );
    if (m === "POST") {
      const err = validateService(body);
      if (err) return fail(res, 400, "bad_request", err);
      const svc = newService(normalizeService(body));
      state.services.push(svc);
      emit("services.changed");
      return send(res, 201, svc);
    }
  }
  if ((m2 = p.match(/^\/api\/services\/([^/]+)(?:\/(start|stop|restart|logs))?$/))) {
    const svc = state.services.find((s) => s.id === m2[1]);
    if (!svc) return fail(res, 404, "not_found", `service ${m2[1]} not found`);
    const action = m2[2];
    if (!action) {
      if (m === "GET") return send(res, 200, svc);
      if (m === "PUT") {
        if (svc.state !== "stopped" && svc.state !== "failed")
          return fail(res, 409, "conflict", "cannot update a running service; stop it first");
        const err = validateService(body);
        if (err) return fail(res, 400, "bad_request", err);
        Object.assign(svc, normalizeService(body));
        emit("services.changed");
        return send(res, 200, svc);
      }
      if (m === "DELETE") {
        if (procs.has(svc.id)) {
          clearTimeout(procs.get(svc.id).boot);
          clearInterval(procs.get(svc.id).timer);
          procs.delete(svc.id);
        }
        state.services = state.services.filter((s) => s !== svc);
        logs.delete(svc.id);
        emit("services.changed");
        return send(res, 204);
      }
    }
    if (action === "logs" && m === "GET") {
      const tail = Math.min(5000, Number(url.searchParams.get("tail") || 500));
      return send(res, 200, { lines: (logs.get(svc.id) ?? []).slice(-tail) });
    }
    if (m === "POST" && (action === "start" || action === "restart")) {
      if (action === "start" && (svc.state === "running" || svc.state === "starting"))
        return fail(res, 409, "conflict", "service is already running");
      if (action === "restart" && procs.has(svc.id)) {
        clearTimeout(procs.get(svc.id).boot);
        clearInterval(procs.get(svc.id).timer);
        procs.delete(svc.id);
      }
      if (svc.port > 0) {
        const other = state.services.find(
          (s) => s !== svc && s.port === svc.port && (s.state === "running" || s.state === "starting"),
        );
        if (other) return fail(res, 409, "conflict", `port ${svc.port} is already in use by ${other.name}`);
      }
      if (
        svc.kind !== "custom" &&
        !svc.command &&
        !state.runtimes.some((r) => r.kind === svc.kind) &&
        svc.kind !== "node"
      ) {
        return fail(res, 409, "conflict", `no ${svc.kind} runtime found: register or install one first`);
      }
      startProc(svc);
      return send(res, 202, svc);
    }
    if (m === "POST" && action === "stop") {
      if (svc.state === "stopped") return send(res, 202, svc);
      stopProc(svc);
      return send(res, 202, svc);
    }
  }

  // runtimes
  if (m === "GET" && p === "/api/runtimes") return send(res, 200, state.runtimes);
  if (m === "GET" && p === "/api/runtimes/detect") {
    await sleep(400);
    return send(res, 200, DETECT);
  }
  if (m === "POST" && p === "/api/runtimes/register") {
    if (!body || !body.kind || !body.binary) return fail(res, 400, "bad_request", "kind and binary are required");
    if (!isAbs(body.binary)) return fail(res, 400, "bad_request", "binary must be an absolute path");
    if (body.binary.includes("nonexistent")) return fail(res, 400, "bad_request", `${body.binary} does not exist`);
    const version = body.version || "1.0.0";
    const kind = ["node", "php", "go", "java", "caddy", "other"].includes(body.kind) ? body.kind : "other";
    const rt = {
      id: id("rt"),
      kind,
      version,
      name: `${kind[0].toUpperCase()}${kind.slice(1)} ${version}`,
      binary: body.binary,
      source: "registered",
      installedAt: now(),
    };
    state.runtimes.push(rt);
    emit("runtimes.changed");
    return send(res, 201, rt);
  }
  if (m === "GET" && p === "/api/installer/log") {
    const tail = Math.min(1000, Number(url.searchParams.get("tail") || 200));
    return send(res, 200, { lines: installerLog.slice(-tail) });
  }
  if (m === "POST" && p === "/api/runtimes/import") {
    const entry = CATALOG.find((c) => c.id === body?.catalogId);
    if (!entry) return fail(res, 404, "not_found", `catalog entry ${body?.catalogId} not found`);
    const fp = body.filePath || "";
    if (!isAbs(fp)) return fail(res, 400, "bad_request", "filePath must be an absolute path");
    if (fp.includes("nonexistent")) return fail(res, 400, "bad_request", `${fp} does not exist`);
    const base = fp.split(/[\\/]/).pop();
    if (base !== fileNameOf(entry))
      return fail(res, 400, "bad_request", `the file name should be ${fileNameOf(entry)}, got ${base}`);
    if (body.sha256 && !/^([0-9a-fA-F]{64}|[0-9a-fA-F]{128})$/.test(body.sha256))
      return fail(res, 400, "bad_request", "checksum must be a SHA-256 (64) or SHA-512 (128) hexadecimal digest");
    if (entry.os !== THIS_PC.os || entry.arch !== THIS_PC.arch)
      return fail(res, 409, "conflict", "this catalog entry is not supported on this machine");
    if (state.runtimes.some((r) => r.name === entry.name && r.source === "installed"))
      return fail(res, 409, "conflict", `${entry.name} is already installed`);
    if (!body.sha256 && /nochecksum/i.test(fp))
      return fail(
        res,
        400,
        "bad_request",
        `Cannot verify the file because the published checksum could not be fetched. Paste the SHA-256 or SHA-512 for ${fileNameOf(entry)} from ${entry.checksum.url} into the checksum field.`,
      );
    return send(res, 202, runInstall(entry, { filePath: fp }));
  }
  if (m === "POST" && p === "/api/runtimes/install") {
    const entry = CATALOG.find((c) => c.id === body?.catalogId);
    if (!entry) return fail(res, 404, "not_found", `catalog entry ${body?.catalogId} not found`);
    if (entry.os !== THIS_PC.os || entry.arch !== THIS_PC.arch)
      return fail(res, 409, "conflict", "this catalog entry is not supported on this machine");
    if (state.runtimes.some((r) => r.name === entry.name && r.source === "installed"))
      return fail(res, 409, "conflict", `${entry.name} is already installed`);
    return send(res, 202, runInstall(entry));
  }
  if ((m2 = p.match(/^\/api\/runtimes\/([^/]+)$/)) && m === "DELETE") {
    const rt = state.runtimes.find((r) => r.id === m2[1]);
    if (!rt) return fail(res, 404, "not_found", `runtime ${m2[1]} not found`);
    const user = state.services.find((s) => s.runtimeId === rt.id);
    if (user) return fail(res, 409, "conflict", `runtime is used by service ${user.name}`);
    state.runtimes = state.runtimes.filter((r) => r !== rt);
    emit("runtimes.changed");
    return send(res, 204);
  }
  if (m === "GET" && p === "/api/catalog") {
    return send(
      res,
      200,
      CATALOG.map((c) => ({
        ...c,
        homepage: homepageOf(c),
        fileName: fileNameOf(c),
        installed: state.runtimes.some((r) => r.name === c.name && r.source === "installed"),
        supported: c.os === THIS_PC.os && c.arch === THIS_PC.arch,
      })),
    );
  }
  if ((m2 = p.match(/^\/api\/jobs\/([^/]+)$/)) && m === "GET") {
    const j = jobs.get(m2[1]);
    return j ? send(res, 200, j) : fail(res, 404, "not_found", `job ${m2[1]} not found`);
  }

  // sites
  const suffixes = () =>
    state.settings.domainSuffix
      .split(/[\s,]+/)
      .filter(Boolean)
      .map((s) => s.replace(/^\./, ""));
  const validateSite = (b) => {
    if (!b || !b.name || !b.domain) return "name and domain are required";
    if (!suffixes().some((s) => b.domain.endsWith("." + s))) return `domain must end with .${suffixes().join(" or .")}`;
    if (b.type === "static" && !isAbs(b.root || "")) return "root must be an absolute path";
    if (b.type === "proxy" && !b.serviceId && !b.upstreamPort) return "proxy sites need serviceId or upstreamPort";
    if (b.type === "php" && !state.services.some((s) => s.id === b.serviceId && s.kind === "php"))
      return "php sites need the serviceId of a php service";
    return null;
  };
  const dupe = (b, self) => state.sites.some((s) => s !== self && s.domain === b.domain);
  if (p === "/api/sites") {
    if (m === "GET") return send(res, 200, state.sites.map(siteView));
    if (m === "POST") {
      const err = validateSite(body);
      if (err) return fail(res, 400, "bad_request", err);
      if (dupe(body, null)) return fail(res, 409, "conflict", `domain ${body.domain} is already used by another site`);
      const s = {
        id: id("site"),
        name: body.name,
        domain: body.domain,
        type: body.type,
        root: body.root || "",
        serviceId: body.serviceId || "",
        upstreamPort: body.upstreamPort || 0,
        https: !!body.https,
        enabled: body.enabled !== false,
      };
      state.sites.push(s);
      emit("sites.changed");
      return send(res, 201, siteView(s));
    }
  }
  if ((m2 = p.match(/^\/api\/sites\/([^/]+)$/))) {
    const s = state.sites.find((x) => x.id === m2[1]);
    if (!s) return fail(res, 404, "not_found", `site ${m2[1]} not found`);
    if (m === "PUT") {
      const err = validateSite(body);
      if (err) return fail(res, 400, "bad_request", err);
      if (dupe(body, s)) return fail(res, 409, "conflict", `domain ${body.domain} is already used by another site`);
      Object.assign(s, {
        name: body.name,
        domain: body.domain,
        type: body.type,
        root: body.root || "",
        serviceId: body.serviceId || "",
        upstreamPort: body.upstreamPort || 0,
        https: !!body.https,
        enabled: body.enabled !== false,
      });
      emit("sites.changed");
      return send(res, 200, siteView(s));
    }
    if (m === "DELETE") {
      state.sites = state.sites.filter((x) => x !== s);
      emit("sites.changed");
      return send(res, 204);
    }
  }

  if (p === "/api/settings") {
    if (m === "GET") return send(res, 200, state.settings);
    if (m === "PUT") {
      if (!body) return fail(res, 400, "bad_request", "invalid body");
      const ok = (n) => Number.isInteger(n) && n > 0 && n < 65536;
      if (!ok(body.proxyHttpPort) || !ok(body.proxyHttpsPort))
        return fail(res, 400, "bad_request", "proxy ports must be 1-65535");
      if (body.downloadProxy && !/^https?:\/\/[^/\s]+$/.test(body.downloadProxy))
        return fail(res, 400, "bad_request", "downloadProxy must look like http://host:port");
      if (!body.domainSuffix) return fail(res, 400, "bad_request", "domainSuffix is required");
      Object.assign(state.settings, {
        domainSuffix: body.domainSuffix,
        proxyHttpPort: body.proxyHttpPort,
        proxyHttpsPort: body.proxyHttpsPort,
        openBrowserOnStart: !!body.openBrowserOnStart,
        trustLocalCA: !!body.trustLocalCA,
        downloadProxy: body.downloadProxy || "",
      });
      return send(res, 200, state.settings);
    }
  }
  return fail(res, 404, "not_found", `no route for ${m} ${p}`);
}

const MIME = {
  ".html": "text/html",
  ".js": "text/javascript",
  ".css": "text/css",
  ".svg": "image/svg+xml",
  ".json": "application/json",
  ".png": "image/png",
  ".ico": "image/x-icon",
};
function serveStatic(req, res, url) {
  if (!fs.existsSync(path.join(DIST, "index.html"))) {
    res.writeHead(404, { "Content-Type": "text/plain" });
    return res.end("mock daemon: no UI build found; run `npm run build` or use `npm run dev`.");
  }
  let file = path.join(DIST, decodeURIComponent(url.pathname));
  if (!file.startsWith(DIST) || !fs.existsSync(file) || fs.statSync(file).isDirectory())
    file = path.join(DIST, "index.html");
  res.writeHead(200, { "Content-Type": MIME[path.extname(file)] || "application/octet-stream" });
  fs.createReadStream(file).pipe(res);
}

const server = http.createServer(async (req, res) => {
  const url = new URL(req.url, `http://${req.headers.host}`);
  try {
    if (url.pathname.startsWith("/api/")) await api(req, res, url);
    else serveStatic(req, res, url);
  } catch (e) {
    console.error(e);
    if (!res.headersSent) fail(res, 500, "internal", String(e?.message || e));
  }
});
server.listen(PORT, "127.0.0.1", () => {
  console.log(`mock daemon on http://127.0.0.1:${PORT}/#token=${TOKEN}`);
});
for (const sig of ["SIGINT", "SIGTERM"]) process.on(sig, () => process.exit(0));
