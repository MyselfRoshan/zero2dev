// Pure helpers behind Create a project (#/create): the language and framework choices, the slug and
// folder preview, the request, whether Create may be pressed, and the steps of the running job.
import { kindLabel } from "./catalog";
import type {
  Framework,
  FrameworkLanguage,
  Job,
  NewProjectInstall,
  NewProjectPlan,
  NewProjectRequest,
  Project,
  Service,
  Site,
} from "./types";
import { isAbsolutePath, isValidPort, parsePort, validateDomain } from "./validate";

// ---- languages ----

export interface LanguageInfo {
  id: FrameworkLanguage;
  label: string;
  /** The kind whose mark (ui/KindIcon) stands for the language; "" has no brand mark. */
  icon: string;
}

/** The order the choices are shown in. */
export const LANGUAGES: LanguageInfo[] = [
  { id: "php", label: "PHP", icon: "php" },
  { id: "javascript", label: "JavaScript / TypeScript", icon: "node" },
  { id: "python", label: "Python", icon: "python" },
  { id: "go", label: "Go", icon: "go" },
  { id: "java", label: "Java", icon: "java" },
  { id: "rust", label: "Rust", icon: "rust" },
  { id: "static", label: "Static", icon: "" },
];

export const languageLabel = (id: string): string => LANGUAGES.find((l) => l.id === id)?.label ?? id;

/** The languages that have at least one framework in the list, in the usual order. */
export function languagesOf(frameworks: Framework[]): LanguageInfo[] {
  const have = new Set(frameworks.map((f) => f.language));
  return LANGUAGES.filter((l) => have.has(l.id));
}

export const frameworksOf = (frameworks: Framework[], language: string): Framework[] =>
  frameworks.filter((f) => f.language === language);

/** The language a framework id belongs to, "" for an unknown id. */
export function languageOfFramework(frameworks: Framework[], id: string): FrameworkLanguage | "" {
  return frameworks.find((f) => f.id === id)?.language ?? "";
}

// ---- what a framework needs ----

const DB_LABEL: Record<string, string> = { mariadb: "MariaDB", mysql: "MySQL", postgres: "PostgreSQL" };
export const databaseLabel = (k: string): string => DB_LABEL[k] ?? k;

function runtimeNeed(f: Framework): string {
  const r = f.runtime;
  if (!r.kind) return "";
  const name = kindLabel(r.kind);
  if (r.min && r.max) return `${name} ${r.min} to ${r.max}`;
  if (r.min) return `${name} ${r.min} or newer`;
  if (r.max) return `${name} up to ${r.max}`;
  return name;
}

/** "PHP 8.2 or newer, Composer, a database (MariaDB, MySQL or PostgreSQL)" */
export function needsLine(f: Framework): string {
  const parts: string[] = [];
  const rt = runtimeNeed(f);
  if (rt) parts.push(rt);
  if (f.composer) parts.push("Composer");
  if (f.database?.kinds.length) {
    const names = f.database.kinds.map(databaseLabel);
    const list = names.length > 1 ? `${names.slice(0, -1).join(", ")} or ${names[names.length - 1]}` : names[0];
    parts.push(f.database.optional ? `optional database (${list})` : `a database (${list})`);
  }
  return parts.length ? parts.join(", ") : "Nothing to install";
}

export interface DbChoice {
  value: string;
  label: string;
}
/** The database kinds of the framework, with "None" first when it is optional. [] when it has no database. */
export function databaseChoices(f: Framework | undefined): DbChoice[] {
  const d = f?.database;
  if (!d || d.kinds.length === 0) return [];
  const list: DbChoice[] = d.kinds.map((k) => ({ value: k, label: databaseLabel(k) }));
  return d.optional ? [{ value: "", label: "None" }, ...list] : list;
}
/** The kind selected at first: the framework's default, else the first offered kind. */
export function defaultDatabase(f: Framework | undefined): string {
  const d = f?.database;
  if (!d || d.kinds.length === 0) return "";
  return (d.kinds as string[]).includes(d.default) ? d.default : d.kinds[0];
}

/** What a framework does, from its own description: shown until the daemon's plan arrives. */
export function outline(f: Framework): { steps: string[]; creates: string[] } {
  const steps = f.steps.map((s) => s.label);
  const creates: string[] = [];
  for (const s of f.services)
    creates.push(
      `Service ${s.name.replace("{name}", "your project")} (${kindLabel(s.kind)}${s.port ? `, port ${s.port}` : ""})`,
    );
  if (f.site) creates.push(f.site.type === "static" ? "A site for the folder" : "A local site (domain)");
  creates.push("A project that starts it all together");
  return { steps, creates };
}

// ---- names, folders, domain ----

/** Letters, digits, "-" and "_", lower case; spaces become "-"; leading dots and dashes are dropped. */
export function slugify(name: string): string {
  const s = name
    .normalize("NFKD")
    .replace(/[\u0300-\u036f]/g, "")
    .toLowerCase()
    .trim()
    .replace(/\s+/g, "-")
    .replace(/[^a-z0-9_-]/g, "")
    .replace(/-{2,}/g, "-")
    .replace(/^[.\-_]+/, "")
    .replace(/-+$/, "");
  return s.slice(0, 64);
}

/** <parent>\<slug> with backslashes, for display and the request preview. "" when either is missing. */
export function joinProjectPath(parent: string, slug: string): string {
  const p = parent.trim().replace(/\//g, "\\").replace(/\\+$/, "");
  if (!p || !slug) return "";
  return `${p}\\${slug}`;
}

export const defaultDomain = (slug: string, suffix: string): string => (slug ? `${slug}.${suffix || "localhost"}` : "");

/** An empty domain means the default; else it is checked like the site form does. */
export function projectDomainError(domain: string, suffixes: string[]): string | undefined {
  if (!domain.trim()) return undefined;
  return validateDomain(domain, suffixes);
}

/** An empty port means "a free one". */
export function projectPortError(text: string): string | undefined {
  const n = parsePort(text);
  if (Number.isNaN(n) || !isValidPort(n))
    return "Use a whole number from 1 to 65535, or leave it empty for a free port.";
  return undefined;
}

// ---- the remembered parent folder ----

export const PARENT_KEY = "zero2dev.newproject.parent";
type Store = Pick<Storage, "getItem" | "setItem">;
export function loadParent(st: Store | undefined = globalThis.localStorage): string {
  try {
    return st?.getItem(PARENT_KEY) ?? "";
  } catch {
    return "";
  }
}
export function saveParent(path: string, st: Store | undefined = globalThis.localStorage): void {
  try {
    if (path.trim()) st?.setItem(PARENT_KEY, path.trim());
  } catch {
    /* storage unavailable: the folder is asked again next time */
  }
}

// ---- the form and the request ----

export interface CreateForm {
  framework: string;
  name: string;
  parent: string;
  /** "" is None. */
  database: string;
  /** "" means <slug>.<suffix>. */
  domain: string;
  /** "" means a free port. */
  port: string;
  openEditor: boolean;
}

export function buildRequest(f: Framework, form: CreateForm): NewProjectRequest {
  const hasDb = !!f.database && f.database.kinds.length > 0;
  const db = hasDb ? form.database : "";
  return {
    framework: f.id,
    name: form.name.trim(),
    parent: form.parent.trim(),
    databaseKind: (["mariadb", "mysql", "postgres"].includes(db) ? db : "") as NewProjectRequest["databaseKind"],
    domain: form.domain.trim(),
    port: Number.isNaN(parsePort(form.port)) ? 0 : parsePort(form.port),
    openEditor: form.openEditor,
  };
}

/** Whether the plan can be asked for: the daemon answers 400 for a missing name or folder. */
export function canPlan(form: CreateForm, suffixes: string[]): boolean {
  return (
    !!form.framework &&
    !!slugify(form.name) &&
    isAbsolutePath(form.parent.trim()) &&
    !projectDomainError(form.domain, suffixes) &&
    !projectPortError(form.port)
  );
}

export type PlanPhase = "idle" | "loading" | "ready" | "error";

/** Whether Create may be pressed, and when not, the one reason to show under the button. */
export function createState(
  form: CreateForm,
  suffixes: string[],
  plan: NewProjectPlan | null,
  phase: PlanPhase,
  busy = false,
): { ok: boolean; reason: string } {
  if (!form.framework) return { ok: false, reason: "Choose a framework." };
  if (!form.name.trim()) return { ok: false, reason: "Enter a name." };
  if (!slugify(form.name)) return { ok: false, reason: "The name needs at least one letter or digit." };
  if (!form.parent.trim()) return { ok: false, reason: "Choose the folder the project goes in." };
  if (!isAbsolutePath(form.parent.trim()))
    return { ok: false, reason: "Use an absolute folder path, for example D:\\www." };
  if (projectDomainError(form.domain, suffixes)) return { ok: false, reason: "Fix the domain." };
  if (projectPortError(form.port)) return { ok: false, reason: "Fix the port." };
  if (busy) return { ok: false, reason: "Starting..." };
  if (phase === "error") return { ok: false, reason: "The plan could not be made. See the message on the right." };
  if (phase !== "ready" || !plan) return { ok: false, reason: "Checking the plan..." };
  if (plan.problems.length)
    return {
      ok: false,
      reason:
        plan.problems.length === 1
          ? "Fix the problem in the plan first."
          : `Fix the ${plan.problems.length} problems in the plan first.`,
    };
  return { ok: true, reason: "" };
}

// ---- the plan ----

export const installTitle = (i: Pick<NewProjectInstall, "name" | "version">): string =>
  `${i.name}${i.name.includes(i.version) ? "" : ` ${i.version}`}`.trim();

export type ProblemAction =
  | { kind: "link"; href: string; label: string }
  | { kind: "focus"; field: "name" | "parent" | "domain" | "port" | "database"; label: string };

/** Something the user can do about a problem text of the plan, where one can be guessed. */
export function problemAction(problem: string): ProblemAction | undefined {
  const p = problem.toLowerCase();
  if (/\bparent\b|does not exist/.test(p)) return { kind: "focus", field: "parent", label: "Choose another folder" };
  if (/\bdomain\b/.test(p)) return { kind: "focus", field: "domain", label: "Change the domain" };
  if (/\bport\b/.test(p)) return { kind: "focus", field: "port", label: "Change the port" };
  if (/\bdatabase\b/.test(p)) return { kind: "focus", field: "database", label: "Choose a database" };
  if (/\bfolder\b|not empty|already exists/.test(p)) return { kind: "focus", field: "name", label: "Change the name" };
  if (/runtime|version|php|python|node|java|\bgo\b|composer/.test(p))
    return { kind: "link", href: "#/runtimes", label: "Open Runtimes" };
  return undefined;
}

// ---- the running job ----

export type StepState = "waiting" | "running" | "done" | "failed";
export interface StepRow {
  label: string;
  state: StepState;
  /** The job's percent, on the running step only. */
  percent?: number;
}

const norm = (s: string) => s.trim().toLowerCase();

/** Which of the plan's steps the job is on: its name, else (by prefix) its percent, else the first. */
export function currentStep(steps: string[], job: Pick<Job, "step" | "percent">): number {
  if (steps.length === 0) return -1;
  const s = norm(job.step || "");
  if (s) {
    const exact = steps.findIndex((x) => norm(x) === s);
    if (exact >= 0) return exact;
    const near = steps.findIndex((x) => s.startsWith(norm(x)) || norm(x).startsWith(s));
    if (near >= 0) return near;
  }
  const pct = Math.max(0, Math.min(99, job.percent || 0));
  return Math.min(steps.length - 1, Math.floor((pct / 100) * steps.length));
}

/** The steps of the plan as a list with a state each. A job that is done has every step done. */
export function stepRows(steps: string[], job: Pick<Job, "state" | "step" | "percent"> | null): StepRow[] {
  if (!job) return steps.map((label) => ({ label, state: "waiting" }));
  if (job.state === "done") return steps.map((label) => ({ label, state: "done" }));
  const at = currentStep(steps, job);
  return steps.map((label, i) => {
    if (i < at) return { label, state: "done" };
    if (i > at) return { label, state: "waiting" };
    return job.state === "failed"
      ? { label, state: "failed" }
      : { label, state: "running", percent: Math.round(job.percent || 0) };
  });
}

/** The folder a failure says was kept ("The folder D:\www\shop was kept."), and the text without it. */
export function failureParts(error: string): { message: string; kept: string } {
  const m = /The folder (.+?) was kept\.?/.exec(error);
  return { message: error.trim() || "Unknown error.", kept: m ? m[1] : "" };
}

/** What a finished project holds, for the result card. */
export function createdOf(
  projectId: string,
  projects: Project[],
  services: Service[],
  sites: Site[],
): { project?: Project; services: Service[]; sites: Site[] } {
  const project = projects.find((p) => p.id === projectId);
  if (!project) return { services: [], sites: [] };
  return {
    project,
    services: project.serviceIds.map((id) => services.find((s) => s.id === id)).filter((s): s is Service => !!s),
    sites: project.siteIds.map((id) => sites.find((s) => s.id === id)).filter((s): s is Site => !!s),
  };
}

/** The last lines of a log, as text, for the live output box. */
export const lastLines = (lines: { text: string }[], n = 8): string[] => lines.slice(-n).map((l) => l.text);
