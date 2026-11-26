// Mock of the 0.0.6 project file routes (docs/API.md, "Amendment: 0.0.6, part 2", "Project file").
//   export async function handle(ctx, m, p, body, res, url) -> true when it answered the request.
// ctx: { state, emit, send, fail, id, now, sleep, procs, setState, addLog, startProc, stopProc, fakeJob }
//
// POST /api/projects/{id}/export    writes nothing; answers the file's path and one warning per left-out
//                                   secret-looking env variable and per service folder outside the file's folder.
//                                   A path that contains "exists", or one that was exported before, is a 409
//                                   "<file> exists" unless overwrite is set.
// POST /api/projects/import/plan    a plan from the folder name:
//                                     "bad"             a problem (the import is refused)
//                                     "missing-runtime" an installs entry (PHP)
//                                     "port"            a changes entry (the port is taken)
//                                     "secret"          a warning
//                                     anything else     a clean plan
// POST /api/projects/import         a job (type "project-import") that adds a service, a site and a project to
//                                   the mock's state; "fail-me" in the path makes the job fail.
const SECRET = /PASSWORD|SECRET|TOKEN|KEY|CREDENTIAL|AUTH|PRIVATE|SIGNATURE|COOKIE|SESSION/i;

const written = new Set();
const norm = (p) =>
  String(p ?? "")
    .trim()
    .replace(/\//g, "\\")
    .replace(/\\+$/, "");
const fileOf = (p) => (/\.json$/i.test(norm(p)) ? norm(p) : `${norm(p)}\\zero2dev.json`);
const dirOf = (file) => file.slice(0, Math.max(0, file.lastIndexOf("\\")));
const under = (dir, p) => {
  const a = norm(p).toLowerCase();
  const d = dir.toLowerCase();
  return a === d || a.startsWith(`${d}\\`);
};
const slugOf = (s) =>
  String(s)
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "") || "project";
const titleOf = (s) =>
  String(s)
    .split(/[-_ ]+/)
    .filter(Boolean)
    .map((w) => w[0].toUpperCase() + w.slice(1))
    .join(" ") || "Project";

function exportRoute(ctx, project, body, res) {
  if (!body || !String(body.path ?? "").trim()) return ctx.fail(res, 400, "bad_request", "path is required");
  const file = fileOf(body.path);
  if (!body.overwrite && (written.has(file.toLowerCase()) || /exists/i.test(file)))
    return ctx.fail(res, 409, "conflict", `${file} exists`);
  const dir = dirOf(file);
  const warnings = [];
  for (const sid of project.serviceIds) {
    const svc = ctx.state.services.find((s) => s.id === sid);
    if (!svc) continue;
    for (const key of Object.keys(svc.env ?? {}))
      if (SECRET.test(key)) warnings.push(`${svc.name}: the variable ${key} looks like a secret and was left out.`);
    if (svc.cwd && !under(dir, svc.cwd))
      warnings.push(`${svc.name}: its folder ${svc.cwd} is outside ${dir}, so the service was left out.`);
  }
  written.add(file.toLowerCase());
  return ctx.send(res, 200, { path: file, warnings });
}

function makePlan(ctx, body) {
  const path = norm(body?.path);
  if (!path) return { error: "path is required" };
  const file = fileOf(path);
  const dir = dirOf(file);
  const folder = dir.split("\\").pop() || "project";
  const low = path.toLowerCase();
  const name = String(body?.name ?? "").trim() || titleOf(folder);
  const slug = slugOf(name);
  const { state } = ctx;
  const problems = [];
  const changes = [];
  const warnings = [];
  const installs = [];
  if (low.includes("bad")) problems.push("zero2dev.json has format 2, but this Zero2Dev reads format 1.");
  if (low.includes("missing-runtime")) {
    installs.push({
      kind: "php",
      version: "8.4.1",
      catalogId: "php-8.4.1-windows-amd64",
      name: "PHP",
      reason: `${name} needs PHP 8.2 or newer`,
    });
  }
  const taken = new Set(state.services.map((s) => s.port));
  let port = 8000;
  const forced = low.includes("port");
  if (forced || taken.has(port)) {
    while (taken.has(port) || (forced && port === 8000)) port++;
    changes.push(`Port 8000 is taken: ${name} uses ${port}`);
  }
  let domain = slug;
  const domains = new Set(state.sites.map((s) => s.domain.split(".")[0]));
  if (domains.has(domain)) {
    let n = 2;
    while (domains.has(`${slug}-${n}`)) n++;
    domain = `${slug}-${n}`;
    changes.push(`The domain ${slug}.localhost is taken: ${name} uses ${domain}.localhost`);
  }
  if (low.includes("secret")) warnings.push("DB_PASSWORD is never in the file: set it again in the service.");
  return {
    plan: {
      path: file,
      dir,
      name,
      services: [`${name} (PHP, port ${port})`],
      sites: [`https://${domain}.localhost -> ${name}`],
      installs,
      changes,
      problems,
      warnings,
    },
    port,
    domain,
    low,
  };
}

function startJob(ctx, r) {
  const { plan } = r;
  const failing = r.low.includes("fail-me");
  return ctx.fakeJob({
    type: "project-import",
    steps: [
      "Checking the file",
      ...(plan.installs.length ? ["Installing runtimes"] : []),
      "Creating the services",
      "Creating the sites",
      "Saving the project",
    ],
    ms: 300,
    work: () => {
      if (failing)
        throw new Error("The service port could not be reserved: another program took it while the import ran.");
      const { state } = ctx;
      for (const i of plan.installs) {
        if (state.runtimes.some((rt) => rt.catalogId === i.catalogId)) continue;
        state.runtimes.push({
          id: ctx.id("rt"),
          kind: i.kind,
          version: i.version,
          name: i.name,
          binary: `D:\\zero2dev\\bin\\data\\runtimes\\${i.catalogId}\\php.exe`,
          source: "installed",
          installedAt: ctx.now(),
          catalogId: i.catalogId,
          default: !state.runtimes.some((rt) => rt.kind === i.kind && rt.default),
        });
      }
      if (plan.installs.length) ctx.emit("runtimes.changed");
      const svc = {
        settings: { docroot: "public" },
        dependsOn: [],
        databaseId: "",
        envFile: "",
        restartOnCrash: false,
        restartLimit: 5,
        command: "",
        args: [],
        env: { APP_ENV: "local" },
        healthPath: "/",
        runtimeId: "",
        autostart: false,
        customArgs: false,
        name: plan.name,
        kind: "php",
        cwd: plan.dir,
        port: r.port,
        id: ctx.id("svc"),
        restarts: 0,
        state: "stopped",
        pid: 0,
        startedAt: null,
        lastError: "",
      };
      state.services.push(svc);
      const site = {
        id: ctx.id("site"),
        name: plan.name,
        domain: `${r.domain}.localhost`,
        type: "php",
        root: `${plan.dir}\\public`,
        serviceId: svc.id,
        upstreamPort: 0,
        https: true,
        enabled: true,
      };
      state.sites.push(site);
      const project = { id: ctx.id("prj"), name: plan.name, serviceIds: [svc.id], siteIds: [site.id] };
      state.projects.push(project);
      ctx.emit("services.changed");
      ctx.emit("sites.changed");
      ctx.emit("projects.changed");
      return { projectId: project.id, path: plan.dir, serviceId: svc.id };
    },
  });
}

export async function handle(ctx, m, p, body, res) {
  if (m !== "POST") return false;
  if (p === "/api/projects/import/plan" || p === "/api/projects/import") {
    const r = makePlan(ctx, body);
    if (r.error) {
      ctx.fail(res, 400, "bad_request", r.error);
      return true;
    }
    if (p.endsWith("/plan")) {
      ctx.send(res, 200, r.plan);
      return true;
    }
    if (r.plan.problems.length) {
      ctx.send(res, 409, { error: { code: "conflict", message: r.plan.problems[0] } });
      return true;
    }
    ctx.send(res, 202, startJob(ctx, r));
    return true;
  }
  const m2 = p.match(/^\/api\/projects\/([^/]+)\/export$/);
  if (m2) {
    const project = ctx.state.projects.find((x) => x.id === m2[1]);
    if (!project) {
      ctx.fail(res, 404, "not_found", `project ${m2[1]} not found`);
      return true;
    }
    exportRoute(ctx, project, body, res);
    return true;
  }
  return false;
}
