// Mock of the 0.0.6 task routes (docs/API.md, the 0.0.6 amendment, "One-off tasks").
//   export async function handle(ctx, m, p, body, res, url) -> true when it answered the request.
// ctx: { state, emit, send, fail, id, now, sleep, procs, setState, addLog, startProc, stopProc, fakeJob }
//
// The presets are the real ones: the `tasks` of the kind's profile (internal/kinds/profiles.json),
// with {s.<setting>} and {port} expanded like the daemon. A task is a fake job (type "task") that
// writes lines with the stream "task" to the service's log: "$ npm install", a few output lines.
// Steering, for the e2e test and for trying the UI:
//   a program or argument containing "fail" ends with an error and the last output lines;
//   a second task while one runs is 409 (details.jobId);
//   composer needs a Composer runtime (409 without one).
import { effective, profileOf } from "../kinds.mjs";

const busy = new Map(); // serviceId -> id of its running task job

// The scripts a Node project's package.json has (the mock has no folders to read).
const NODE_SCRIPTS = [
  { name: "dev", command: "vite" },
  { name: "build", command: "vite build" },
  { name: "lint", command: "eslint ." },
  { name: "test", command: "vitest run" },
  { name: "storybook", command: "storybook dev -p 6006" },
];

const conflict = (ctx, res, message, details) =>
  ctx.send(res, 409, { error: { code: "conflict", message, ...(details ? { details } : {}) } });

const expand = (text, settings, port) =>
  text
    .replace(/\{s\.([A-Za-z0-9_]+)\}/g, (_, k) => settings[k] ?? "")
    .replace(/\{port\}/g, port > 0 ? String(port) : "");

// Output of a few well-known commands, so the log reads like the real thing.
function outputFor(program, args) {
  const line = `${program} ${args.join(" ")}`;
  if (/install|\badd\b/.test(line))
    return ["added 214 packages, and audited 215 packages in 4s", "found 0 vulnerabilities"];
  if (/build/.test(line))
    return ["vite v6.0.0 building for production...", "transforming (42) src/main.ts", "built in 1.3s"];
  if (/test/.test(line)) return [" RUN  v3.0.0", " Test Files  3 passed (3)", "      Tests  12 passed (12)"];
  if (/migrate/.test(line)) return ["Running migrations.", "  2026_01_01_000000_create_users_table ........ DONE"];
  if (/list/.test(line)) return ["Package    Version", "---------- -------", "pip        24.0"];
  return ["working...", "done."];
}

export async function handle(ctx, m, p, body, res) {
  const match = p.match(/^\/api\/services\/([^/]+)\/tasks$/);
  if (!match) return false;
  const svc = ctx.state.services.find((s) => s.id === match[1]);
  if (!svc) {
    ctx.fail(res, 404, "not_found", `service ${match[1]} not found`);
    return true;
  }
  const profile = profileOf(svc.kind);
  const settings = effective(profile, svc.settings ?? {});
  const presets = profile?.tasks ?? [];

  if (m === "GET") {
    ctx.send(res, 200, {
      presets,
      scripts: svc.kind === "node" ? NODE_SCRIPTS : [],
      running: busy.get(svc.id) ?? "",
    });
    return true;
  }
  if (m !== "POST") return false;

  const b = body ?? {};
  const bad = (msg) => {
    ctx.fail(res, 400, "bad_request", msg);
    return true;
  };
  const hasPreset = b.preset !== undefined && b.preset !== "";
  const hasProgram = b.program !== undefined && b.program !== "";
  if (hasPreset === hasProgram) return bad("give either a preset or a program, not both and not neither");
  let program;
  let args;
  let label;
  if (hasPreset) {
    if (b.args !== undefined && b.args.length) return bad("a preset takes no args");
    const t = presets.find((x) => x.id === b.preset);
    if (!t) {
      ctx.fail(res, 404, "not_found", `${svc.kind} has no task ${b.preset}`);
      return true;
    }
    program = expand(t.program, settings, svc.port);
    args = t.args.map((a) => expand(a, settings, svc.port));
    label = t.label;
  } else {
    program = String(b.program);
    args = b.args ?? [];
    if (!Array.isArray(args) || args.length > 200 || args.some((a) => typeof a !== "string"))
      return bad("args must be a list of at most 200 texts");
    if (/[\\/\0\r\n]/.test(program)) return bad("program must be a name without a folder, on one line");
    if (args.some((a) => /[\0\r\n]/.test(a))) return bad("an argument has a NUL or a line break");
    label = `${program} ${args.join(" ")}`.trim();
  }

  if (busy.has(svc.id)) {
    conflict(ctx, res, `a task of ${svc.name} is already running`, { jobId: busy.get(svc.id) });
    return true;
  }
  if (program === "composer" && !ctx.state.runtimes.some((r) => r.kind === "composer")) {
    conflict(ctx, res, "Composer is not installed: install it on the Runtimes page");
    return true;
  }

  const failing = /fail/i.test(program + " " + args.join(" "));
  const job = ctx.fakeJob({
    type: "task",
    fields: { serviceId: svc.id },
    steps: [label],
    ms: 500,
    work: async () => {
      try {
        ctx.addLog(svc, "task", `$ ${program} ${args.join(" ")}`.trimEnd());
        for (const l of outputFor(program, args)) {
          await ctx.sleep(350);
          ctx.addLog(svc, "task", l);
        }
        await ctx.sleep(350);
        if (failing) {
          const tail = ["npm ERR! code ELIFECYCLE", "npm ERR! Missing script: fail"];
          for (const l of tail) ctx.addLog(svc, "task", l);
          ctx.addLog(svc, "task", `${program} exited with code 1`);
          throw new Error(`${program} exited with code 1: ${tail.join("; ")}`);
        }
        return {};
      } finally {
        busy.delete(svc.id);
      }
    },
  });
  busy.set(svc.id, job.id);
  ctx.send(res, 202, job);
  return true;
}
