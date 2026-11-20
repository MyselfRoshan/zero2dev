// Mock of the 0.0.5 snapshots routes (docs/API.md, the 0.0.5 amendment, "Snapshots").
//   export async function handle(ctx, m, p, body, res, url) -> true when it answered the request.
// ctx: { state, emit, send, fail, id, now, sleep, procs, setState, addLog, startProc, stopProc, fakeJob }
// Snapshots live in ctx.state.snapshots (a plain list, newest last); each keeps deep copies of the
// stored services, sites and project, plus the resolved runtime of every service.
const DB_KINDS = ["mariadb", "mysql", "postgres", "redis"];
const LIVE_FIELDS = ["state", "pid", "startedAt", "lastError", "restarts"];
const clone = (x) => JSON.parse(JSON.stringify(x));
const active = (s) => s.state !== "stopped" && s.state !== "failed";
const names = (list) => list.map((s) => s.name).join(", ");
// One job per owner or snapshot at a time: key -> job id.
const busy = new Map();

function conflict(ctx, res, message, details) {
  ctx.send(res, 409, { error: { code: "conflict", message, ...(details ? { details } : {}) } });
  return true;
}

function stored(svc) {
  const out = clone(svc);
  for (const k of LIVE_FIELDS) delete out[k];
  return out;
}

// Like supervisor.findRuntime: the service's own runtime, else the default (or first) of its kind.
function resolveRuntime(state, svc) {
  if (svc.kind === "custom") return null;
  const rt = svc.runtimeId
    ? state.runtimes.find((r) => r.id === svc.runtimeId)
    : (state.runtimes.find((r) => r.kind === svc.kind && r.default) ?? state.runtimes.find((r) => r.kind === svc.kind));
  if (!rt) return null;
  return {
    kind: rt.kind,
    version: rt.version,
    catalogId: rt.catalogId ?? "",
    binary: rt.binary,
    followedDefault: !svc.runtimeId,
  };
}

// The installed runtime that matches a recorded one: catalog id, then kind + version, then the binary.
function findInstalled(state, ref) {
  return (
    (ref.catalogId && state.runtimes.find((r) => r.catalogId === ref.catalogId)) ||
    state.runtimes.find((r) => r.kind === ref.kind && r.version === ref.version) ||
    (!ref.catalogId && state.runtimes.find((r) => r.binary === ref.binary)) ||
    null
  );
}

function missingOf(state, snap) {
  const out = new Map();
  for (const x of snap.services) {
    if (!x.runtime || findInstalled(state, x.runtime)) continue;
    const key = `${x.runtime.kind}@${x.runtime.version}`;
    const m = out.get(key) ?? {
      kind: x.runtime.kind,
      version: x.runtime.version,
      catalogId: x.runtime.catalogId,
      // Every installed runtime came from the mock catalog; registered programs (no id) cannot be installed.
      installable: !!x.runtime.catalogId,
      services: [],
    };
    m.services.push(x.service.name);
    out.set(key, m);
  }
  return [...out.values()];
}

function siteOut(state, s) {
  const port = s.https ? state.settings.proxyHttpsPort : state.settings.proxyHttpPort;
  return { ...s, url: `${s.https ? "https" : "http"}://${s.domain}:${port}` };
}

function view(state, snap) {
  return { ...clone(snap), sites: snap.sites.map((s) => siteOut(state, s)) };
}

// The services a snapshot of the owner holds (project members, or the one service).
function ownerMembers(state, scope, ownerId) {
  if (scope === "service") return state.services.filter((s) => s.id === ownerId);
  const p = state.projects.find((x) => x.id === ownerId);
  return p ? p.serviceIds.map((id) => state.services.find((s) => s.id === id)).filter(Boolean) : [];
}

function takeSnapshot(ctx, res, scope, ownerId, body) {
  const { state } = ctx;
  let project = null;
  if (scope === "project") {
    project = state.projects.find((x) => x.id === ownerId);
    if (!project) return (ctx.fail(res, 404, "not_found", `project ${ownerId} not found`), true);
  } else {
    const svc = state.services.find((s) => s.id === ownerId);
    if (!svc) return (ctx.fail(res, 404, "not_found", `service ${ownerId} not found`), true);
    if (!DB_KINDS.includes(svc.kind)) return (ctx.fail(res, 400, "bad_request", `${svc.name} is not a database`), true);
  }
  const name = typeof body?.name === "string" ? body.name.trim() : "";
  if (name.length < 1 || name.length > 64)
    return (ctx.fail(res, 400, "bad_request", "name must be 1-64 characters"), true);
  if (busy.has(ownerId)) return conflict(ctx, res, "a job is already working on it", { jobId: busy.get(ownerId) });
  const members = ownerMembers(state, scope, ownerId);
  const running = members.filter(active);
  if (running.length && !body.restart)
    return conflict(
      ctx,
      res,
      `${names(running)} ${running.length === 1 ? "is" : "are"} running; stop ${running.length === 1 ? "it" : "them"} first or take the snapshot with restart`,
      { running: names(running) },
    );
  for (const s of running) ctx.stopProc(s);
  const steps = running.length ? ["Stopping", "Copying", "Starting"] : ["Copying"];
  const job = ctx.fakeJob({
    type: "snapshot",
    fields: scope === "project" ? { projectId: ownerId } : { serviceId: ownerId },
    steps,
    ms: 350,
    work: () => {
      busy.delete(ownerId);
      const warnings = [];
      const services = members.map((s) => {
        const db = DB_KINDS.includes(s.kind);
        if (db && s.customArgs)
          warnings.push(
            `${s.name} uses its own command line, so its data folder is unknown: only its settings are kept.`,
          );
        return {
          service: stored(s),
          runtime: resolveRuntime(state, s),
          hasData: db && !s.customArgs,
          phpIni: s.kind === "php",
        };
      });
      const sites = project ? project.siteIds.map((id) => state.sites.find((s) => s.id === id)).filter(Boolean) : [];
      const sizeBytes =
        4096 + services.reduce((n, x) => n + (x.hasData ? 24 * 1024 * 1024 + x.service.name.length * 4096 : 2048), 0);
      const snap = {
        format: 1,
        id: ctx.id("snp"),
        name,
        scope,
        ownerId,
        createdAt: ctx.now(),
        version: "0.0.5",
        sizeBytes,
        project: project ? clone(project) : null,
        sites: clone(sites),
        services,
        warnings,
      };
      (state.snapshots ??= []).push(snap);
      for (const s of running) ctx.startProc(s);
      ctx.emit("snapshots.changed", { ownerId });
      return { snapshotId: snap.id };
    },
  });
  busy.set(ownerId, job.id);
  ctx.send(res, 202, job);
  return true;
}

function restoreSnapshot(ctx, res, snap, body) {
  const { state } = ctx;
  if (busy.has(snap.ownerId) || busy.has(snap.id))
    return conflict(ctx, res, "a job is already working on it", { jobId: busy.get(snap.ownerId) ?? busy.get(snap.id) });
  const missing = missingOf(state, snap);
  if (missing.length)
    return conflict(
      ctx,
      res,
      `install the missing runtimes first: ${missing.map((m) => `${m.kind} ${m.version}`).join(", ")}`,
      { missing: missing.map((x) => `${x.kind} ${x.version}`).join(", ") },
    );
  const ids = new Set(snap.services.map((x) => x.service.id));
  const running = state.services.filter((s) => ids.has(s.id) && active(s));
  if (running.length && !body?.restart)
    return conflict(
      ctx,
      res,
      `${names(running)} ${running.length === 1 ? "is" : "are"} running; stop ${running.length === 1 ? "it" : "them"} first or restore with restart`,
      { running: names(running) },
    );
  for (const s of running) ctx.stopProc(s);
  const job = ctx.fakeJob({
    type: "snapshot-restore",
    fields: {
      snapshotId: snap.id,
      ...(snap.scope === "project" ? { projectId: snap.ownerId } : { serviceId: snap.ownerId }),
    },
    steps: running.length ? ["Stopping", "Restoring", "Starting"] : ["Restoring"],
    ms: 350,
    work: () => {
      busy.delete(snap.id);
      for (const x of snap.services) {
        const rt = x.runtime ? findInstalled(state, x.runtime) : null;
        const fields = { ...clone(x.service), runtimeId: rt ? rt.id : x.service.runtimeId };
        // Links to services that no longer exist are dropped, like the daemon's normalize.
        fields.dependsOn = (fields.dependsOn ?? []).filter((d) => state.services.some((s) => s.id === d) || ids.has(d));
        const cur = state.services.find((s) => s.id === x.service.id);
        if (cur) Object.assign(cur, fields);
        else state.services.push({ ...fields, state: "stopped", pid: 0, startedAt: null, lastError: "", restarts: 0 });
      }
      for (const s of snap.sites) {
        const cur = state.sites.find((x) => x.id === s.id);
        if (cur) Object.assign(cur, clone(s));
        else state.sites.push(clone(s));
      }
      if (snap.scope === "project" && snap.project) {
        const cur = state.projects.find((x) => x.id === snap.project.id);
        if (cur) Object.assign(cur, clone(snap.project));
        else state.projects.push(clone(snap.project));
      }
      state.services.sort((a, b) => a.name.localeCompare(b.name));
      for (const s of running) {
        const now = state.services.find((x) => x.id === s.id);
        if (now) ctx.startProc(now);
      }
      ctx.emit("services.changed");
      ctx.emit("sites.changed");
      ctx.emit("projects.changed");
      ctx.emit("snapshots.changed", { ownerId: snap.ownerId });
    },
  });
  busy.set(snap.id, job.id);
  ctx.send(res, 202, job);
  return true;
}

// shop.localhost + "-copy" -> shop-copy.localhost, made unique with -2, -3 ...
function suffixDomain(domain, suffix, taken) {
  const dot = domain.indexOf(".");
  const first = dot < 0 ? domain : domain.slice(0, dot);
  const rest = dot < 0 ? "" : domain.slice(dot);
  let candidate = first + suffix + rest;
  for (let n = 2; taken.has(candidate.toLowerCase()); n++) candidate = `${first}${suffix}-${n}${rest}`;
  return candidate;
}

function cloneSnapshot(ctx, res, snap, body) {
  const { state } = ctx;
  if (snap.scope !== "project")
    return (ctx.fail(res, 400, "bad_request", "only a project snapshot can be cloned"), true);
  const name = typeof body?.name === "string" ? body.name.trim() : "";
  if (name.length < 1 || name.length > 64)
    return (ctx.fail(res, 400, "bad_request", "name must be 1-64 characters"), true);
  const suffix = body?.domainSuffix ?? "-copy";
  if (typeof suffix !== "string" || !/^[A-Za-z0-9-]{1,30}$/.test(suffix))
    return (ctx.fail(res, 400, "bad_request", "domainSuffix must be 1-30 letters, digits or -"), true);
  if (busy.has(snap.id)) return conflict(ctx, res, "a job is already working on it", { jobId: busy.get(snap.id) });
  const job = ctx.fakeJob({
    type: "snapshot-clone",
    fields: { snapshotId: snap.id },
    steps: ["Copying", "Creating"],
    ms: 350,
    work: () => {
      busy.delete(snap.id);
      const ids = new Map(snap.services.map((x) => [x.service.id, ctx.id("svc")]));
      const usedPorts = new Set(state.services.map((s) => s.port).filter(Boolean));
      const usedNames = new Set(state.services.map((s) => s.name));
      const copies = snap.services.map((x) => {
        const s = clone(x.service);
        let port = s.port;
        if (port > 0) {
          while (usedPorts.has(port)) port++;
          usedPorts.add(port);
        }
        let svcName = `${s.name} (${name})`.slice(0, 64);
        for (let n = 2; usedNames.has(svcName); n++) svcName = `${s.name} (${name} ${n})`.slice(0, 64);
        usedNames.add(svcName);
        return {
          ...s,
          id: ids.get(s.id),
          name: svcName,
          port,
          dependsOn: (s.dependsOn ?? []).map((d) => ids.get(d) ?? d),
          databaseId: s.databaseId ? (ids.get(s.databaseId) ?? s.databaseId) : "",
          state: "stopped",
          pid: 0,
          startedAt: null,
          lastError: "",
          restarts: 0,
        };
      });
      const portOf = new Map(copies.map((c) => [c.id, c.port]));
      const taken = new Set(state.sites.map((s) => s.domain.toLowerCase()));
      const sites = snap.sites.map((s) => {
        const domain = suffixDomain(s.domain, suffix, taken);
        taken.add(domain.toLowerCase());
        const serviceId = s.serviceId ? (ids.get(s.serviceId) ?? s.serviceId) : "";
        return {
          ...clone(s),
          id: ctx.id("site"),
          name: `${s.name} (${name})`.slice(0, 64),
          domain,
          serviceId,
          upstreamPort: serviceId && portOf.has(serviceId) && s.upstreamPort ? portOf.get(serviceId) : s.upstreamPort,
        };
      });
      const project = { id: ctx.id("prj"), name, serviceIds: copies.map((c) => c.id), siteIds: sites.map((s) => s.id) };
      state.services.push(...copies);
      state.services.sort((a, b) => a.name.localeCompare(b.name));
      state.sites.push(...sites);
      state.projects.push(project);
      ctx.emit("services.changed");
      ctx.emit("sites.changed");
      ctx.emit("projects.changed");
      return { projectId: project.id };
    },
  });
  busy.set(snap.id, job.id);
  ctx.send(res, 202, job);
  return true;
}

export async function handle(ctx, m, p, body, res) {
  const { state } = ctx;
  state.snapshots ??= [];
  let r;
  if ((r = p.match(/^\/api\/(projects|services)\/([^/]+)\/snapshots$/))) {
    const scope = r[1] === "projects" ? "project" : "service";
    const ownerId = decodeURIComponent(r[2]);
    if (m === "GET") {
      if (scope === "service") {
        const svc = state.services.find((s) => s.id === ownerId);
        if (svc && !DB_KINDS.includes(svc.kind))
          return (ctx.fail(res, 400, "bad_request", `${svc.name} is not a database`), true);
      }
      const list = state.snapshots
        .filter((s) => s.ownerId === ownerId && s.scope === scope)
        .reverse()
        .map((s) => view(state, s));
      ctx.send(res, 200, list);
      return true;
    }
    if (m === "POST") return takeSnapshot(ctx, res, scope, ownerId, body ?? {});
    return false;
  }
  if ((r = p.match(/^\/api\/snapshots\/([^/]+)(?:\/(restore|clone))?$/))) {
    const snap = state.snapshots.find((s) => s.id === decodeURIComponent(r[1]));
    if (!snap) return (ctx.fail(res, 404, "not_found", `snapshot ${r[1]} not found`), true);
    const action = r[2];
    if (!action && m === "GET") {
      ctx.send(res, 200, { ...view(state, snap), missing: missingOf(state, snap) });
      return true;
    }
    if (!action && m === "DELETE") {
      if (busy.has(snap.id)) return conflict(ctx, res, "a job is already working on it", { jobId: busy.get(snap.id) });
      state.snapshots = state.snapshots.filter((s) => s !== snap);
      ctx.emit("snapshots.changed", { ownerId: snap.ownerId });
      ctx.send(res, 204);
      return true;
    }
    if (action === "restore" && m === "POST") return restoreSnapshot(ctx, res, snap, body ?? {});
    if (action === "clone" && m === "POST") return cloneSnapshot(ctx, res, snap, body ?? {});
  }
  return false;
}
