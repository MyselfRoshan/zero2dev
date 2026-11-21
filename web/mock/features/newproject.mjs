// Mock of the 0.0.6 New project routes (docs/API.md, the 0.0.6 amendment, "New project").
//   export async function handle(ctx, m, p, body, res, url) -> true when it answered the request.
// ctx: { state, emit, send, fail, id, now, sleep, procs, setState, addLog, startProc, stopProc, fakeJob }
// POST /api/new-project/plan computes a plausible plan from the request and the mock's state;
// POST /api/new-project runs it as a job (type "new-project") that really adds the runtimes, the
// services, the site and the project to the mock's state. Special names:
//   "taken"   the folder exists and is not empty (a problem in the plan)
//   "fail-me" the generator step fails: nothing is created, the folder is kept
// A folder whose path contains "missing" does not exist; a domain of an existing site, or a port of an
// existing service, is taken.
import { listFrameworks } from "./frameworks.mjs";

// The newest version the mock catalog offers for each kind a framework can ask for.
const LATEST = {
  php: "8.4.1",
  node: "26.14.0",
  python: "3.13.18",
  go: "1.23.3",
  java: "21.0.4",
  composer: "2.11.1",
  mariadb: "11.4.13",
  mysql: "8.4.13",
  postgres: "17.11",
};
const NAMES = {
  php: "PHP",
  node: "Node.js",
  python: "Python",
  go: "Go",
  java: "Java",
  composer: "Composer",
  mariadb: "MariaDB",
  mysql: "MySQL",
  postgres: "PostgreSQL",
};
const BINARY = {
  php: "php.exe",
  node: "node.exe",
  python: "python.exe",
  go: "bin\\go.exe",
  java: "bin\\java.exe",
  composer: "composer.bat",
  mariadb: "bin\\mariadbd.exe",
  mysql: "bin\\mysqld.exe",
  postgres: "bin\\postgres.exe",
};
const SIZE_MB = {
  php: 32,
  node: 30,
  python: 28,
  go: 70,
  java: 190,
  composer: 3,
  mariadb: 95,
  mysql: 220,
  postgres: 280,
};
const slugOf = (name) =>
  name
    .normalize("NFKD")
    .replace(/[\u0300-\u036f]/g, "")
    .toLowerCase()
    .trim()
    .replace(/\s+/g, "-")
    .replace(/[^a-z0-9_-]/g, "")
    .replace(/-{2,}/g, "-")
    .replace(/^[.\-_]+/, "")
    .replace(/-+$/, "")
    .slice(0, 64);

const cmp = (a, b) => {
  const x = String(a).split(".").map(Number);
  const y = String(b).split(".").map(Number);
  for (let i = 0; i < Math.max(x.length, y.length); i++) {
    const d = (x[i] ?? 0) - (y[i] ?? 0);
    if (d) return d;
  }
  return 0;
};
// min and max are inclusive bounds on the leading numbers (max "8.3" accepts 8.3.12).
const fits = (version, spec) =>
  (!spec.min || cmp(version, spec.min) >= 0) &&
  (!spec.max || cmp(version.split(".").slice(0, spec.max.split(".").length).join("."), spec.max) <= 0);

// Folders the mock "has on disk" after a run (a failed run keeps its folder).
const folders = new Set();
const dirKey = (d) => d.toLowerCase();
const join = (parent, slug) =>
  `${parent
    .trim()
    .replace(/[\\/]+$/, "")
    .replace(/\//g, "\\")}\\${slug}`;

function bad(ctx, res, message) {
  ctx.fail(res, 400, "bad_request", message);
  return true;
}

// Returns { error } for a 400, else { plan, f, req }.
function makePlan(ctx, body) {
  const { state } = ctx;
  if (!body || typeof body !== "object") return { error: "invalid JSON body" };
  if (typeof body.framework !== "string" || !body.framework) return { error: "framework is required" };
  const f = listFrameworks().find((x) => x.id === body.framework);
  if (!f) return { error: `unknown framework ${body.framework}` };
  if (typeof body.name !== "string" || !body.name.trim()) return { error: "name is required" };
  if (typeof body.parent !== "string" || !body.parent.trim()) return { error: "parent is required" };
  const slug = slugOf(body.name);
  if (!slug) return { error: "name needs at least one letter or digit" };
  const suffix = (state.settings.domainSuffix || "localhost").split(/[\s,]+/)[0].replace(/^\./, "") || "localhost";
  const domain = (body.domain || "").trim() || `${slug}.${suffix}`;
  const dir = join(body.parent, slug);
  const problems = [];
  const warnings = [];

  // ---- the folder ----
  if (/missing/i.test(body.parent)) problems.push(`The parent folder ${body.parent.trim()} does not exist.`);
  else if (slug === "taken" || folders.has(dirKey(dir)))
    problems.push(`The folder ${dir} exists and is not empty. Choose another name or folder.`);

  // ---- the domain and the port ----
  const site = state.sites.find((s) => s.domain === domain);
  if (site) problems.push(`The domain ${domain} is used by the site ${site.name}.`);
  const used = new Map();
  for (const s of state.services) if (s.port) used.set(s.port, s.name);
  for (const s of state.sites) if (s.upstreamPort) used.set(s.upstreamPort, s.name);
  const wanted = Number(body.port) || 0;
  let port = wanted || f.services[0]?.port || 0;
  if (wanted && used.has(wanted)) problems.push(`The port ${wanted} is used by ${used.get(wanted)}.`);
  else if (!wanted) while (port && used.has(port)) port++;

  // ---- what to install ----
  const installs = [];
  const want = (kind, why, spec = {}) => {
    const have = state.runtimes.some((r) => r.kind === kind && fits(r.version, spec));
    if (have) return;
    const version = LATEST[kind];
    if (!version || !fits(version, spec)) {
      problems.push(
        `No ${NAMES[kind] ?? kind} version fits ${f.label}: it needs ${NAMES[kind] ?? kind} ${spec.min ?? ""}${spec.max ? ` to ${spec.max}` : " or newer"}.`,
      );
      return;
    }
    installs.push({
      kind,
      version,
      catalogId: `${kind}-${version}-windows-amd64`,
      name: `${NAMES[kind] ?? kind} ${version}`,
      reason: why,
    });
  };
  if (f.runtime?.kind)
    want(
      f.runtime.kind,
      `${f.label} needs ${NAMES[f.runtime.kind]}${f.runtime.min ? ` ${f.runtime.min} or newer` : ""}.`,
      f.runtime,
    );
  if (f.composer) want("composer", `${f.label} is created and updated with Composer.`);

  // ---- the database ----
  let database = null;
  const dbKind = f.database ? body.databaseKind || "" : "";
  if (body.databaseKind && !f.database) problems.push(`${f.label} does not use a database.`);
  else if (body.databaseKind && !f.database.kinds.includes(body.databaseKind))
    problems.push(`${f.label} does not offer ${NAMES[body.databaseKind] ?? body.databaseKind}.`);
  else if (f.database && !dbKind && !f.database.optional) problems.push(`${f.label} needs a database: choose one.`);
  if (dbKind && f.database?.kinds.includes(dbKind)) {
    want(dbKind, `The database of ${f.label} runs on ${NAMES[dbKind]}.`);
    const svc = state.services.find((s) => s.kind === dbKind);
    database = { kind: dbKind, name: slug.replace(/-/g, "_"), user: slug.replace(/-/g, "_"), serviceId: svc?.id ?? "" };
  }

  // ---- steps, creates, warnings ----
  const steps = ["Checking the plan"];
  for (const i of installs) steps.push(`Installing ${i.name}`);
  if (database) {
    steps.push(
      database.serviceId
        ? `Starting ${NAMES[database.kind]}`
        : `Creating and starting the ${NAMES[database.kind]} service`,
    );
    steps.push(`Creating the database ${database.name}`);
  }
  for (const s of f.steps) steps.push(s.label);
  if (f.envFile) steps.push(`Writing ${f.envFile}`);
  steps.push("Creating the services, the site and the project");
  if (body.openEditor !== false) steps.push("Opening the folder in your editor");

  const creates = [];
  for (const s of f.services) {
    const label = NAMES[s.kind] ?? s.kind;
    creates.push(`Service ${body.name.trim()} (${label}${port ? `, port ${port}` : ""})`);
  }
  if (database && !database.serviceId) creates.push(`Service ${NAMES[database.kind]} (database)`);
  if (database) creates.push(`Database ${database.name} and a user with all privileges on it`);
  if (f.site)
    creates.push(
      `Site ${f.site.https ? "https" : "http"}://${domain}${f.site.type === "static" ? " for the folder" : ""}`,
    );
  creates.push(`Project ${body.name.trim()}`);
  if (f.envFile) creates.push(`${f.envFile} in ${dir}`);

  const mb = installs.reduce((n, i) => n + (SIZE_MB[i.kind] ?? 20), 0);
  if (mb)
    warnings.push(`About ${mb} MB is downloaded first. Zero2Dev checks each file against its publisher's checksum.`);
  if (f.site?.https && !state.settings.trustLocalCA)
    warnings.push(
      "The local certificate is not trusted yet, so a browser warns on https sites. Switch it on under Settings > HTTPS.",
    );
  if (f.id === "springboot") warnings.push("The first build downloads Maven dependencies and can take a few minutes.");

  return {
    f,
    req: body,
    plan: { dir, slug, domain, port, installs, database, steps, creates, problems, warnings },
  };
}

function addRuntime(ctx, i) {
  const { state } = ctx;
  if (state.runtimes.some((r) => r.catalogId === i.catalogId)) return;
  state.runtimes.push({
    id: ctx.id("rt"),
    kind: i.kind,
    version: i.version,
    name: i.name,
    binary: `D:\\zero2dev\\bin\\data\\runtimes\\${i.catalogId}\\${BINARY[i.kind] ?? "bin"}`,
    source: "installed",
    installedAt: ctx.now(),
    catalogId: i.catalogId,
    default: !state.runtimes.some((r) => r.kind === i.kind && r.default),
  });
}

function makeService(ctx, input) {
  return {
    settings: {},
    dependsOn: [],
    databaseId: "",
    envFile: "",
    restartOnCrash: false,
    restartLimit: 5,
    command: "",
    args: [],
    env: {},
    healthPath: "",
    runtimeId: "",
    autostart: false,
    customArgs: false,
    ...input,
    id: ctx.id("svc"),
    restarts: 0,
    state: "stopped",
    pid: 0,
    startedAt: null,
    lastError: "",
  };
}

function startJob(ctx, { plan, f, req }) {
  const failing = plan.slug === "fail-me";
  let steps = plan.steps;
  // A failure happens at the generator: the steps after it never run.
  const gen = steps.indexOf(f.steps[0]?.label);
  if (failing && gen >= 0) steps = steps.slice(0, gen + 1);
  return ctx.fakeJob({
    type: "new-project",
    steps,
    ms: 300,
    work: () => {
      if (failing) {
        folders.add(dirKey(plan.dir));
        const prog = f.steps[0]?.program ?? "the generator";
        throw new Error(
          `${f.steps[0]?.label ?? "The generator"} failed: ${prog} exited with code 1: could not reach the package server. The folder ${plan.dir} was kept.`,
        );
      }
      const { state } = ctx;
      for (const i of plan.installs) addRuntime(ctx, i);
      if (plan.installs.length) ctx.emit("runtimes.changed");
      const name = req.name.trim();
      const serviceIds = [];
      let dbService = plan.database ? state.services.find((s) => s.id === plan.database.serviceId) : null;
      if (plan.database && !dbService) {
        dbService = makeService(ctx, {
          name: NAMES[plan.database.kind],
          kind: plan.database.kind,
          cwd: "",
          port: plan.database.kind === "postgres" ? 5432 : 3306,
        });
        state.services.push(dbService);
        serviceIds.push(dbService.id);
      }
      const made = [];
      for (const s of f.services) {
        const svc = makeService(ctx, {
          name,
          kind: s.kind,
          cwd: plan.dir,
          port: plan.port || s.port || 0,
          healthPath: s.port ? "/" : "",
          envFile: f.envFile ?? "",
          databaseId: s.usesDatabase && dbService ? dbService.id : "",
          dependsOn: s.usesDatabase && dbService ? [dbService.id] : [],
        });
        state.services.push(svc);
        made.push(svc);
        serviceIds.push(svc.id);
        // The generator's output goes to the log of the first service once it exists.
        if (made.length === 1)
          for (const l of [
            `$ ${f.steps[0]?.program ?? "generate"} ${(f.steps[0]?.args ?? []).join(" ")}`.trim(),
            `Creating a ${f.label} project in ${plan.dir}`,
            "Installing dependencies from lock file",
            "Generating optimized autoload files",
            "Application ready. Build something amazing.",
          ])
            ctx.addLog(svc, "stdout", l);
      }
      const siteIds = [];
      if (f.site) {
        const first = made[0];
        const site = {
          id: ctx.id("site"),
          name,
          domain: plan.domain,
          type: f.site.type,
          root: f.site.type === "static" ? plan.dir : f.site.root ? `${plan.dir}\\${f.site.root}` : plan.dir,
          serviceId: f.site.type === "static" ? "" : (first?.id ?? ""),
          upstreamPort: 0,
          https: !!f.site.https,
          enabled: true,
        };
        state.sites.push(site);
        siteIds.push(site.id);
      }
      const project = { id: ctx.id("prj"), name, serviceIds, siteIds };
      state.projects.push(project);
      folders.add(dirKey(plan.dir));
      if (req.openEditor !== false) state.launches.push({ editor: "vscode", dir: plan.dir });
      ctx.emit("services.changed");
      if (siteIds.length) ctx.emit("sites.changed");
      ctx.emit("projects.changed");
      return { projectId: project.id, path: plan.dir, serviceId: made[0]?.id ?? "" };
    },
  });
}

export async function handle(ctx, m, p, body, res) {
  if (m === "POST" && p === "/api/new-project/plan") {
    const r = makePlan(ctx, body);
    if (r.error) return bad(ctx, res, r.error);
    ctx.send(res, 200, r.plan);
    return true;
  }
  if (m === "POST" && p === "/api/new-project") {
    const r = makePlan(ctx, body);
    if (r.error) return bad(ctx, res, r.error);
    if (r.plan.problems.length) {
      ctx.send(res, 409, { error: { code: "conflict", message: r.plan.problems[0] } });
      return true;
    }
    ctx.send(res, 202, startJob(ctx, r));
    return true;
  }
  return false;
}
