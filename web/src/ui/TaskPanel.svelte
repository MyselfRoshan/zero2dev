<script lang="ts" module>
  import { waitForJob } from "../lib/snapshotJobs";
  import type { Job } from "../lib/types";

  // The task last started on each service. Kept outside the component, so leaving the page and
  // coming back still shows its progress (or its result, until the next run).
  type Tracked = { id: string; label: string; job: Job; ended: boolean };
  const tracked = $state<Record<string, Tracked>>({});

  /** Follows a task job to its end: job.progress events plus a poll of GET /api/jobs/{id}. */
  async function follow(serviceId: string, started: Job, label: string, onEnd: () => void) {
    tracked[serviceId] = { id: started.id, label, job: started, ended: started.state !== "running" };
    const final = await waitForJob(started, (j) => {
      if (tracked[serviceId]?.id === started.id) tracked[serviceId].job = j;
    });
    if (tracked[serviceId]?.id === started.id) {
      tracked[serviceId].job = final;
      tracked[serviceId].ended = true;
      onEnd();
    }
  }
</script>

<script lang="ts">
  // "Run a task" on a service's page (0.0.6): ready-made tasks of its kind (npm install, composer
  // install, go mod download ...), the scripts of a Node project's package.json, and a program of
  // your own. The output goes to the service's log, shown under "Live output" on the same page.
  // docs/API.md, "One-off tasks". There is no Cancel: the daemon has none.
  import { untrack } from "svelte";
  import { api, ApiError } from "../lib/api";
  import { effective, profileFor } from "../lib/kinds";
  import { errMessage, lists, loadKinds } from "../lib/store.svelte";
  import {
    customRequest,
    extraScripts,
    isComposerMissing,
    offersTasks,
    packageManagerOf,
    presetCommand,
    resultLine,
    scriptCommandText,
    scriptRequest,
    splitTaskError,
  } from "../lib/tasks";
  import type { KindTask, Service, TaskList, TaskRequest } from "../lib/types";
  import ConfirmDialog from "./ConfirmDialog.svelte";
  import Icon from "./Icon.svelte";
  import JobProgress from "./JobProgress.svelte";

  let { service }: { service: Service } = $props();

  // The profiles say which kinds have tasks; the page may be the first one to need them.
  $effect(() => {
    if (lists.kinds.length === 0) void loadKinds();
  });
  const profile = $derived(profileFor(lists.kinds, service.kind));
  const offered = $derived(offersTasks(profile));
  const settings = $derived(effective(profile, service.settings));
  const pm = $derived(packageManagerOf(profile, service));

  // ---- what the daemon offers (GET .../tasks) ----
  let list = $state<TaskList | null>(null);
  let loadError = $state("");
  let seq = 0;
  async function load() {
    const my = ++seq;
    const sid = service.id;
    try {
      const l = await api.listTasks(sid);
      if (my !== seq) return;
      list = l;
      loadError = "";
      // A task started elsewhere (or before this page was opened) is followed from here on.
      if (l.running && tracked[sid]?.id !== l.running) {
        const j = await api.getJob(l.running).catch(() => null);
        if (my === seq && j && j.state === "running") void follow(sid, j, j.step || "A task", () => void load());
      }
    } catch (e) {
      if (my !== seq) return;
      loadError = errMessage(e);
    }
  }
  $effect(() => {
    void service.id;
    if (!offered) return;
    list = null;
    untrack(() => void load());
  });

  // ---- the task that runs, or ran last ----
  const ref = $derived(tracked[service.id]);
  const running = $derived(!!ref && !ref.ended);
  const scripts = $derived(list ? extraScripts(list, pm) : []);

  // ---- starting one ----
  let starting = $state(false);
  let startError = $state("");
  let composerMissing = $state(false);
  let confirming = $state<{ label: string; text: string; request: TaskRequest } | null>(null);

  async function start(request: TaskRequest, label: string) {
    confirming = null;
    startError = "";
    composerMissing = false;
    starting = true;
    const sid = service.id;
    try {
      const j = await api.runTask(sid, request);
      void follow(sid, j, label, () => void load());
    } catch (e) {
      if (e instanceof ApiError && e.status === 409 && e.details.jobId) {
        // A task of this service already runs: show it instead of starting another.
        const j = await api.getJob(e.details.jobId).catch(() => null);
        if (j && j.state === "running") void follow(sid, j, j.step || "A task", () => void load());
        startError = e.message;
      } else if (e instanceof ApiError && e.status === 409 && isComposerMissing(e.message)) {
        composerMissing = true;
        startError = e.message;
      } else {
        startError = errMessage(e);
      }
    } finally {
      starting = false;
    }
  }

  function runPreset(t: KindTask) {
    const request: TaskRequest = { preset: t.id };
    if (t.confirm) confirming = { label: t.label, text: t.confirm, request };
    else void start(request, t.label);
  }

  // ---- your own command ----
  let ownOpen = $state(false);
  let program = $state("");
  let argsText = $state("");
  let ownError = $state("");
  function runOwn(e: SubmitEvent) {
    e.preventDefault();
    const r = customRequest(program, argsText);
    ownError = r.error ?? "";
    if (r.request) void start(r.request, `${r.request.program} ${(r.request.args ?? []).join(" ")}`.trim());
  }

  const off = $derived(running || starting);
  const fail = $derived(ref?.ended && ref.job.state === "failed" ? splitTaskError(ref.job.error) : null);
</script>

{#if offered}
  <section class="section-card task-panel" aria-labelledby="task-panel-h" data-testid="task-panel" data-service={service.id} data-busy={off ? "true" : "false"} data-tour="task-panel">
    <header>
      <h2 id="task-panel-h"><Icon name="terminal" size={16} /> Run a task</h2>
      <p>One-off commands that run in {service.name}'s project folder, with the environment it starts with. The output appears under Live output below.</p>
    </header>
    <div class="body">
      {#if running && ref}
        <div class="state" data-testid="task-running">
          <div class="row"><span class="spin" aria-hidden="true"></span><span class="grow" role="status" aria-live="polite">Running {ref.label}</span></div>
          <JobProgress job={ref.job} label={ref.label} />
        </div>
      {:else if ref}
        <div class="state result" class:ok={ref.job.state === "done"} class:bad={ref.job.state === "failed"} role="status" data-testid="task-result" data-state={ref.job.state}>
          <div class="row">
            <span class="ic" aria-hidden="true"><Icon name={ref.job.state === "done" ? "circleCheck" : "alert"} size={14} /></span>
            <span class="grow"><strong>{ref.label}</strong>: {resultLine(ref.job)}</span>
          </div>
          {#if fail && fail.lines.length}
            <pre class="tail mono" data-testid="task-result-lines">{fail.lines.join("\n")}</pre>
          {/if}
        </div>
      {/if}

      {#if startError}
        <div class="notice warn" role="alert" data-testid="task-error">
          <Icon name="alert" />
          <span class="grow">{startError}</span>
          {#if composerMissing}<a class="btn sm" href="#/runtimes" data-testid="task-open-runtimes">Open Runtimes</a>{/if}
        </div>
      {/if}
      {#if loadError}
        <div class="notice warn" role="alert" data-testid="task-load-error"><Icon name="alert" /><span>Could not read the tasks: {loadError}</span></div>
      {/if}

      {#if list && list.presets.length}
        <ul class="tasks" data-testid="task-presets">
          {#each list.presets as t (t.id)}
            <li data-task={t.id}>
              <div class="what">
                <span class="nm">{t.label}</span>
                {#if t.help}<span class="muted small">{t.help}</span>{/if}
                <code class="cmd mono">{presetCommand(t, settings, service.port)}</code>
              </div>
              <button class="btn sm" type="button" disabled={off} onclick={() => runPreset(t)} aria-label="Run {t.label}" data-testid="task-run-{t.id}"><Icon name="play" size={14} /> Run</button>
            </li>
          {/each}
        </ul>
      {/if}

      {#if scripts.length}
        <h3 class="subhead">Scripts in package.json</h3>
        <ul class="tasks" data-testid="task-scripts">
          {#each scripts as s (s.name)}
            <li data-script={s.name}>
              <div class="what">
                <span class="nm mono">{s.name}</span>
                <span class="muted small mono script-cmd" title={s.command}>{s.command}</span>
                <code class="cmd mono">{scriptCommandText(pm, s)}</code>
              </div>
              <button class="btn sm" type="button" disabled={off} onclick={() => void start(scriptRequest(pm, s), scriptCommandText(pm, s))} aria-label="Run the script {s.name}" data-testid="task-script-{s.name}"><Icon name="play" size={14} /> Run</button>
            </li>
          {/each}
        </ul>
      {/if}

      {#if list && !list.presets.length && !scripts.length}
        <p class="muted small none">Nothing is offered here yet: the project folder has none of the files these tasks need.</p>
      {/if}

      <details class="own" bind:open={ownOpen} data-testid="task-own">
        <summary>Run your own command</summary>
        <form class="col own-form" onsubmit={runOwn} novalidate>
          <p class="muted small">
            A program and its arguments, run in the project folder. There is <strong>no shell</strong>: pipes (<code>|</code>), <code>&amp;&amp;</code>, <code>&gt;</code> and
            <code>%VARIABLES%</code> do not work. Put a value with spaces in quotes. The program is found on the service's PATH.
          </p>
          <div class="grid2">
            <div class="field"><label>
              <span class="label">Program</span>
              <input type="text" class="mono" bind:value={program} placeholder="npm" spellcheck="false" autocomplete="off" aria-invalid={!!ownError} data-testid="task-program" /></label>
              <span class="hint">A name such as npm or php, without a folder.</span>
            </div>
            <div class="field"><label>
              <span class="label">Arguments</span>
              <input type="text" class="mono" bind:value={argsText} placeholder={'run build -- --mode "my mode"'} spellcheck="false" autocomplete="off" data-testid="task-args" /></label>
              <span class="hint">Split on spaces; "quotes" keep a value together.</span>
            </div>
          </div>
          {#if ownError}<span class="err" role="alert" data-testid="task-own-error">{ownError}</span>{/if}
          <div><button class="btn" type="submit" disabled={off} data-testid="task-run-own"><Icon name="play" size={14} /> Run</button></div>
        </form>
      </details>
    </div>
  </section>
{/if}

{#if confirming}
  <ConfirmDialog
    title={confirming.label}
    message={confirming.text}
    confirmLabel="Run"
    danger={false}
    busy={starting}
    onconfirm={() => confirming && void start(confirming.request, confirming.label)}
    oncancel={() => (confirming = null)}
  />
{/if}

<style>
  .task-panel {
    margin-top: 16px;
  }
  .task-panel h2 {
    display: inline-flex;
    align-items: center;
    gap: 8px;
  }
  .state {
    display: grid;
    gap: 8px;
    padding: 12px 16px;
    border: 1px solid var(--line);
    border-radius: var(--radius-lg);
    background: var(--surface-2);
  }
  .state .row {
    align-items: flex-start;
  }
  .state .grow {
    overflow-wrap: anywhere;
  }
  .ic {
    display: inline-flex;
    margin-top: 4px;
  }
  .result.ok .ic {
    color: var(--ok);
  }
  .result.bad .ic {
    color: var(--bad);
  }
  .tail {
    margin: 0;
    padding: 8px 10px;
    max-height: 160px;
    overflow: auto;
    white-space: pre-wrap;
    overflow-wrap: anywhere;
    font-size: 12px;
    border: 1px solid var(--line);
    border-radius: var(--radius);
    background: var(--surface);
  }
  .tasks {
    list-style: none;
    margin: 0;
    padding: 0;
    border: 1px solid var(--line);
    border-radius: var(--radius-lg);
  }
  .tasks li {
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 10px 16px;
  }
  .tasks li + li {
    border-top: 1px solid var(--line);
  }
  .what {
    flex: 1;
    min-width: 0;
    display: grid;
    gap: 2px;
  }
  .nm {
    font-weight: 600;
  }
  .cmd {
    justify-self: start;
    max-width: 100%;
    overflow-wrap: anywhere;
    font-size: 12px;
    color: var(--muted);
  }
  .script-cmd {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .tasks .btn {
    flex: none;
  }
  .none {
    margin: 0;
  }
  .subhead {
    margin: 4px 0 -4px;
  }
  .own summary {
    cursor: pointer;
    font-weight: 600;
    width: fit-content;
  }
  .own-form {
    margin-top: 10px;
    gap: 10px;
  }
  .own-form p {
    margin: 0;
  }
  code {
    overflow-wrap: anywhere;
  }
</style>
