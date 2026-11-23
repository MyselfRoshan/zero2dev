// Helpers of "Run a task" (0.0.6): the command line parser of "Run your own command", the request
// a package.json script turns into, and the text of a task's result. Plain functions, no state.
import type { KindProfile } from "./kinds";
import type { Job, KindTask, Service, TaskList, TaskRequest, TaskScript } from "./types";

export interface ParsedCommand {
  args: string[];
  /** Set when the text cannot be split (an open quote). */
  error?: string;
}

/**
 * Splits an arguments field the way a person writes it: on spaces, with "double" or 'single' quotes
 * around a value that has spaces in it. A backslash is a plain character (Windows paths), so
 * "C:\my folder\a.txt" is one argument. Quotes inside a word join its parts (a"b c"d is "ab cd");
 * an empty pair of quotes is an empty argument. There is no shell: nothing else is special.
 */
export function parseCommandLine(text: string): ParsedCommand {
  const args: string[] = [];
  let cur = "";
  let started = false;
  let quote = "";
  for (const ch of text) {
    if (quote) {
      if (ch === quote) quote = "";
      else cur += ch;
    } else if (ch === '"' || ch === "'") {
      quote = ch;
      started = true;
    } else if (/\s/.test(ch)) {
      if (started) args.push(cur);
      cur = "";
      started = false;
    } else {
      cur += ch;
      started = true;
    }
  }
  if (quote) return { args: [], error: `A ${quote === '"' ? "double" : "single"} quote is not closed.` };
  if (started) args.push(cur);
  return { args };
}

/** The message for a program name the daemon would refuse (docs/API.md, "How a task finds its program"), "" when fine. */
export function programError(name: string): string {
  const n = name.trim();
  if (!n) return "Type the program to run, for example npm.";
  if (/[\\/]/.test(n)) return "Use the program's name only, without a folder (for example npm, not C:\\tools\\npm).";
  // eslint-disable-next-line no-control-regex
  if (/[\u0000\r\n]/.test(n)) return "The program name must be on one line.";
  if (/\s/.test(n)) return "The program name has no spaces: put the rest in the arguments.";
  return "";
}

/** The request of "Run your own command". */
export function customRequest(program: string, argsText: string): { request?: TaskRequest; error?: string } {
  const pe = programError(program);
  if (pe) return { error: pe };
  const parsed = parseCommandLine(argsText);
  if (parsed.error) return { error: parsed.error };
  if (parsed.args.length > 200) return { error: "At most 200 arguments." };
  return { request: { program: program.trim(), args: parsed.args } };
}

/** The package manager of a Node service: its setting, `npm` when not set. */
export function packageManagerOf(profile: KindProfile | undefined, service: Pick<Service, "settings">): string {
  const set = service.settings?.packageManager?.trim();
  if (set) return set;
  return profile?.options.find((o) => o.key === "packageManager")?.default || "npm";
}

/** A package.json script runs with the service's package manager: `<pm> run <name>`. */
export function scriptRequest(packageManager: string, script: Pick<TaskScript, "name">): TaskRequest {
  return { program: packageManager || "npm", args: ["run", script.name] };
}

/** What the person sees for a script's command: `pnpm run build`. */
export function scriptCommandText(packageManager: string, script: Pick<TaskScript, "name">): string {
  return `${packageManager || "npm"} run ${script.name}`;
}

/** The scripts whose name is not already covered by a preset of the same command (build, test). */
export function extraScripts(list: Pick<TaskList, "presets" | "scripts">, packageManager: string): TaskScript[] {
  const used = new Set(
    list.presets.map((p) => `${p.program} ${p.args.join(" ")}`.replace("{s.packageManager}", packageManager)),
  );
  return list.scripts.filter((s) => !used.has(scriptCommandText(packageManager, s)));
}

/** True for the kinds whose service page offers tasks: those with presets in the profile, and Node (its scripts). */
export function offersTasks(profile: KindProfile | undefined): boolean {
  return !!profile && (!!profile.tasks?.length || profile.kind === "node");
}

/** A failed task's error: the headline and the last output lines the daemon appends ("npm exited with code 1: a; b"). */
export function splitTaskError(error: string): { headline: string; lines: string[] } {
  const text = (error || "").trim();
  if (!text) return { headline: "The task failed.", lines: [] };
  const m = /^(.*? exited with code -?\d+): ([\s\S]+)$/.exec(text);
  if (!m) return { headline: text, lines: [] };
  return {
    headline: m[1],
    lines: m[2]
      .split(/;\s+|\r?\n/)
      .map((l) => l.trim())
      .filter(Boolean),
  };
}

/** The one-line result shown after a task ends. */
export function resultLine(job: Pick<Job, "state" | "error" | "step">): string {
  if (job.state === "done") return `${job.step || "The task"} finished.`;
  return splitTaskError(job.error).headline;
}

/** A conflict of POST .../tasks, told apart by its message: a Composer that is not installed has its own link. */
export function isComposerMissing(message: string): boolean {
  return /composer is not installed/i.test(message);
}

/** `{s.<setting>}` and `{port}` of a preset's program or arguments, as the daemon expands them. */
export function expandTemplate(text: string, settings: Record<string, string>, port: number): string {
  return text
    .replace(/\{s\.([A-Za-z0-9_]+)\}/g, (_, k: string) => settings[k] ?? "")
    .replace(/\{port\}/g, port > 0 ? String(port) : "");
}

/** What a preset runs, for the person to read: `npm install`. */
export function presetCommand(
  task: Pick<KindTask, "program" | "args">,
  settings: Record<string, string>,
  port: number,
): string {
  return [task.program, ...task.args]
    .map((p) => expandTemplate(p, settings, port))
    .join(" ")
    .trim();
}
