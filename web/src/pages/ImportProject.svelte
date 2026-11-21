<script lang="ts">
  // Import project (#/projects/import): read a zero2dev.json (or the folder of a cloned repository), see
  // what would be installed and created, and import it (docs/API.md, 0.0.6 "Project file"): Check shows the
  // plan, Import runs the daemon's project-import job, then a result card. `?path=` prefills the path.
  import { onDestroy, onMount, tick, untrack } from "svelte";
  import { startAll } from "../lib/actions";
  import { api } from "../lib/api";
  import { dirOf } from "../lib/databases";
  import { createdOf, failureParts } from "../lib/newproject";
  import { PROJECT_FILE, PROJECT_FILE_FILTERS, importName, importState, installLine, planCounts, planSource, type ImportPhase } from "../lib/projectfile";
  import { hashParam } from "../lib/router.svelte";
  import { waitForJob } from "../lib/snapshotJobs";
  import { app, errMessage, lists, loadProjects, loadRuntimes, loadServices, loadSites, toast } from "../lib/store.svelte";
  import type { Job, ProjectImportPlan } from "../lib/types";
  import { isAbsolutePath } from "../lib/validate";
  import Icon from "../ui/Icon.svelte";
  import KindIcon from "../ui/KindIcon.svelte";
  import PageHead from "../ui/PageHead.svelte";

  type View = "form" | "running" | "done" | "failed";
  let view = $state<View>("form");

  // ---- the form ----
  let path = $state(hashParam("path"));
  let name = $state("");
  let browsing = $state(false);
  const canBrowse = $derived(!!app.status?.folderPicker);
  const pathError = $derived(path.trim() && !isAbsolutePath(path.trim()) ? "Use an absolute path, for example D:\\work\\shop\\zero2dev.json." : "");

  async function browseFile() {
    browsing = true;
    try {
      const r = await api.pickFile({ title: "Choose a zero2dev.json to import", start: dirOf(path), filters: PROJECT_FILE_FILTERS });
      if (!r.cancelled && r.path) path = r.path;
    } catch (e) {
      toast("error", errMessage(e));
    } finally {
      browsing = false;
    }
  }
  async function browseFolder() {
    browsing = true;
    try {
      const r = await api.pickFolder("Choose the folder of a cloned project", path.trim());
      if (!r.cancelled && r.path) path = r.path;
    } catch (e) {
      toast("error", errMessage(e));
    } finally {
      browsing = false;
    }
  }

  // ---- Check: the plan. A change of path or name makes the plan stale, so it is dropped. ----
  let plan = $state<ProjectImportPlan | null>(null);
  let phase = $state<ImportPhase>("idle");
  let planError = $state("");
  let planSeq = 0;
  $effect(() => {
    void path;
    void name;
    untrack(() => {
      planSeq++;
      plan = null;
      planError = "";
      phase = "idle";
    });
  });

  async function check() {
    if (!path.trim() || pathError) return;
    const n = ++planSeq;
    phase = "loading";
    planError = "";
    try {
      const p = await api.planProjectImport({ path: path.trim(), name: name.trim() });
      if (n !== planSeq) return;
      plan = p;
      phase = "ready";
    } catch (e) {
      if (n !== planSeq) return;
      plan = null;
      planError = errMessage(e);
      phase = "error";
    }
  }

  let starting = $state(false);
  let startError = $state("");
  const can = $derived(importState(path, plan, phase, starting));

  // ---- the running job ----
  let alive = true;
  onMount(() => {
    if (path.trim() && !pathError) void check();
  });
  onDestroy(() => (alive = false));
  let job = $state<Job | null>(null);
  let runPlan = $state<ProjectImportPlan | null>(null);
  let runName = $state("");
  let finished = $state<Job | null>(null);
  let startedProject = $state(false);
  let startingProject = $state(false);

  const live = $derived(job && job.state === "running" ? job.step || "Starting" : "");
  const created = $derived(finished?.projectId ? createdOf(finished.projectId, lists.projects, lists.services, lists.sites) : { services: [], sites: [] });
  const failure = $derived(finished?.state === "failed" ? failureParts(finished.error) : null);

  async function importIt() {
    if (!can.ok || !plan) return;
    startError = "";
    starting = true;
    try {
      const started = await api.importProject({ path: path.trim(), name: name.trim() });
      runPlan = plan;
      runName = importName(plan, name);
      finished = null;
      startedProject = false;
      job = started;
      view = "running";
      void tick().then(() => document.querySelector<HTMLElement>('[data-testid="import-progress"]')?.focus({ preventScroll: true }));
      const end = await waitForJob(started, (j) => {
        if (alive) job = j;
      });
      await finish(end);
    } catch (e) {
      startError = errMessage(e);
    } finally {
      starting = false;
    }
  }

  async function finish(end: Job) {
    await Promise.all([loadProjects(), loadServices(), loadSites(), loadRuntimes()]);
    if (!alive) {
      toast(end.state === "done" ? "success" : "error", end.state === "done" ? `${runName} imported.` : `${runName} was not imported. ${end.error}`);
      return;
    }
    finished = end;
    job = end;
    view = end.state === "done" ? "done" : "failed";
    await tick();
    document.querySelector<HTMLElement>('[data-testid="import-result"], [data-testid="import-failed"]')?.focus({ preventScroll: true });
  }

  async function startProject() {
    const id = finished?.projectId;
    if (!id) return;
    startingProject = true;
    try {
      await startAll(id);
      await loadSites();
      startedProject = true;
    } finally {
      startingProject = false;
    }
  }

  function backToForm() {
    view = "form";
    finished = null;
    job = null;
  }
  function another() {
    path = "";
    name = "";
    startError = "";
    backToForm();
    void tick().then(() => document.getElementById("ip-path")?.focus());
  }
</script>

<PageHead sub="Import a project from its zero2dev.json, or from the folder of a cloned repository. Check first to see what would be installed and created." />

<div class="page" data-testid="import-project-page">
  {#if view === "running"}
    <section class="card card-pad run" role="group" aria-labelledby="run-h" tabindex="-1" data-testid="import-progress">
      <div class="row"><span class="spin" aria-hidden="true"></span><h2 id="run-h" class="grow">Importing {runName}</h2><span class="mono muted">{Math.round(job?.percent ?? 0)}%</span></div>
      <div class="progress" role="progressbar" aria-valuemin="0" aria-valuemax="100" aria-valuenow={Math.round(job?.percent ?? 0)} aria-label="Progress"><span style:width="{job?.percent ?? 0}%"></span></div>
      <p class="small muted" role="status" data-testid="import-live-step">{live}</p>
      {#if runPlan?.installs.length}
        <p class="small">Installing first: {runPlan.installs.map(installLine).join(", ")}.</p>
      {/if}
      <p class="hint">You can leave this page: the project is set up in the background and you get a message when it is ready.</p>
    </section>
  {:else if view === "done" && finished}
    <section class="card card-pad stack" tabindex="-1" aria-labelledby="done-h" data-testid="import-result">
      <div class="row"><Icon name="circleCheck" size={20} /><h2 id="done-h" class="grow">{runName} is imported</h2></div>
      <p>Set up from <code>{finished.path ?? runPlan?.path}</code>. No file in the project's folder was changed.</p>
      <ul class="made">
        {#each created.services as s (s.id)}
          <li><Icon name="services" size={14} /> <span>Service <a href="#/services/{encodeURIComponent(s.id)}" data-testid="import-link-service">{s.name}</a> <span class="muted small">{s.kind}{s.port ? `, port ${s.port}` : ""}</span></span></li>
        {/each}
        {#each created.sites as s (s.id)}
          <li><Icon name="sites" size={14} /> <span>Site <a href={s.url} target="_blank" rel="noreferrer" data-testid="import-link-site">{s.https ? "https" : "http"}://{s.domain}</a></span></li>
        {/each}
        {#if created.project}
          <li><Icon name="projects" size={14} /> <span>Project <a href="#/projects/{encodeURIComponent(created.project.id)}/edit" data-testid="import-link-project">{created.project.name}</a></span></li>
        {/if}
        {#each runPlan?.installs ?? [] as i (i.catalogId)}
          <li><Icon name="download" size={14} /> <span>Installed {installLine(i)}</span></li>
        {/each}
      </ul>
      {#if runPlan?.changes.length}
        <div data-testid="import-result-changes">
          <h3>Different from the file</h3>
          <ul class="plain">{#each runPlan.changes as c (c)}<li><Icon name="info" size={14} /> {c}</li>{/each}</ul>
        </div>
      {/if}
      <p class="hint">Secrets are never in the file: open the project's services and set any password or key again.</p>
      <div class="row wrap">
        <button class="btn primary" type="button" disabled={startingProject || startedProject} onclick={startProject} data-testid="import-start">
          {#if startingProject}<span class="spin" aria-hidden="true"></span> Starting...{:else}<Icon name="play" size={14} /> {startedProject ? "Started" : "Start the project"}{/if}
        </button>
        <button class="btn" type="button" onclick={another} data-testid="import-another">Import another</button>
      </div>
    </section>
  {:else if view === "failed" && failure}
    <section class="notice bad fail" role="alert" tabindex="-1" aria-labelledby="fail-h" data-testid="import-failed">
      <Icon name="alert" />
      <div class="grow stack">
        <h2 id="fail-h">{runName} was not imported</h2>
        <p data-testid="import-error">{failure.message}</p>
        <p>Zero2Dev removed what it had made for the project, so nothing is left half done. Runtimes that were installed stay installed.</p>
        <div class="row wrap">
          <button class="btn primary" type="button" onclick={backToForm} data-testid="import-back">Back to the form</button>
        </div>
      </div>
    </section>
  {:else}
    <div class="cols">
      <div class="left stack">
        <section class="section-card" aria-labelledby="sec-file" data-tour="import-file">
          <header><h2 id="sec-file">The project file</h2><p>A <code>{PROJECT_FILE}</code>, or the folder of a cloned repository that has one.</p></header>
          <div class="body stack">
            <div class="field">
              <label>
                <span class="label">File or folder</span>
                <input id="ip-path" type="text" class="mono" bind:value={path} aria-invalid={!!pathError} aria-describedby="ip-path-h" placeholder="D:\work\shop\{PROJECT_FILE}" spellcheck="false" autocomplete="off" data-testid="import-path" onkeydown={(e) => e.key === "Enter" && void check()} />
              </label>
              {#if pathError}<span class="err" id="ip-path-h">{pathError}</span>{:else}<span class="hint" id="ip-path-h">Nothing is read from the project's code except this file.</span>{/if}
              {#if canBrowse}
                <div class="row wrap">
                  <button class="btn sm" type="button" onclick={browseFile} disabled={browsing} data-testid="import-browse-file"><Icon name="list" size={14} /> Choose file...</button>
                  <button class="btn sm" type="button" onclick={browseFolder} disabled={browsing} data-testid="import-browse-folder"><Icon name="folder" size={14} /> Choose folder...</button>
                  {#if browsing}<span class="hint" role="status">A Windows dialog is open. Choose there, or close it to cancel.</span>{/if}
                </div>
              {/if}
            </div>
            <div class="field">
              <label>
                <span class="label">Project name (optional)</span>
                <input type="text" bind:value={name} maxlength="64" placeholder="Keep the name in the file" spellcheck="false" autocomplete="off" data-testid="import-name" />
              </label>
              <span class="hint">Leave it empty to use the name in the file.</span>
            </div>
            <div class="row wrap">
              <button class="btn" type="button" disabled={!path.trim() || !!pathError || phase === "loading"} onclick={check} data-testid="import-check">
                {#if phase === "loading"}<span class="spin" aria-hidden="true"></span> Checking...{:else}<Icon name="search" size={14} /> Check{/if}
              </button>
              <span class="hint">Check changes nothing: it only shows what Import would do.</span>
            </div>
          </div>
        </section>
      </div>

      <aside class="side" aria-label="What will happen">
        <div class="card plan" aria-labelledby="plan-h" aria-busy={phase === "loading"} data-testid="import-plan" data-tour="import-plan" data-phase={phase}>
          <header>
            <h2 id="plan-h">What will happen</h2>
            {#if phase === "loading"}<span class="muted small"><span class="spin" aria-hidden="true"></span> Checking...</span>{/if}
          </header>
          <div class="plan-body stack" aria-live="polite">
            {#if phase === "error"}
              <div class="notice bad" role="alert" data-testid="import-plan-error"><Icon name="alert" /><span>{planError}</span></div>
            {/if}
            {#if plan}
              {#if plan.problems.length}
                <div class="notice bad" role="alert" data-testid="import-problems">
                  <Icon name="alert" />
                  <div class="grow">
                    <b>{plan.problems.length === 1 ? "A problem stops this" : "Problems stop this"}</b>
                    <ul class="plain">{#each plan.problems as pr (pr)}<li data-testid="import-problem">{pr}</li>{/each}</ul>
                  </div>
                </div>
              {/if}
              <p class="small muted"><b>{importName(plan, name)}</b> from <code>{planSource(plan)}</code> in <code>{plan.dir}</code>: {planCounts(plan)}.</p>
              <div data-testid="import-installs">
                <h3>Installed first</h3>
                {#if plan.installs.length}
                  <ul class="plain">
                    {#each plan.installs as i (i.catalogId)}
                      <li data-testid="import-install" data-kind={i.kind}><KindIcon kind={i.kind} size={16} /><span class="grow"><b>{installLine(i)}</b> <span class="muted small">{i.reason}</span></span></li>
                    {/each}
                  </ul>
                {:else}
                  <p class="muted small">Everything it needs is installed already.</p>
                {/if}
              </div>
              <div data-testid="import-services">
                <h3>Services that are created</h3>
                {#if plan.services.length}
                  <ul class="plain">{#each plan.services as s (s)}<li><Icon name="services" size={14} /> {s}</li>{/each}</ul>
                {:else}
                  <p class="muted small">None.</p>
                {/if}
              </div>
              <div data-testid="import-sites">
                <h3>Sites that are created</h3>
                {#if plan.sites.length}
                  <ul class="plain">{#each plan.sites as s (s)}<li><Icon name="sites" size={14} /> {s}</li>{/each}</ul>
                {:else}
                  <p class="muted small">None.</p>
                {/if}
              </div>
              {#if plan.changes.length}
                <div data-testid="import-changes">
                  <h3>Different from the file</h3>
                  <ul class="plain">{#each plan.changes as c (c)}<li data-testid="import-change"><Icon name="info" size={14} /> {c}</li>{/each}</ul>
                </div>
              {/if}
              {#if plan.warnings.length}
                <div class="notice warn" data-testid="import-warnings">
                  <Icon name="alert" />
                  <ul class="plain grow">{#each plan.warnings as w (w)}<li>{w}</li>{/each}</ul>
                </div>
              {/if}
            {:else if phase !== "error"}
              <p class="muted small" data-testid="import-outline-note">Choose a file or folder and press Check. You will see here what would be installed and what would be created, before anything is changed.</p>
            {/if}
          </div>
          <div class="go stack">
            {#if startError}<div class="notice bad" role="alert" data-testid="import-start-error"><Icon name="alert" /><span>{startError}</span></div>{/if}
            <button class="btn primary" type="button" disabled={!can.ok} onclick={importIt} aria-describedby="import-why" data-tour="import-go" data-testid="import-go">
              {#if starting}<span class="spin" aria-hidden="true"></span> Starting...{:else}<Icon name="download" size={14} /> Import{/if}
            </button>
            <p class="hint" id="import-why" data-testid="import-why">{can.ok ? "Creates the services, the sites and the project. No file in the project's folder is written." : can.reason}</p>
          </div>
        </div>
      </aside>
    </div>
  {/if}
</div>

<style>
  .page {
    max-width: 1200px;
    min-width: 0;
  }
  .cols {
    display: grid;
    gap: 16px;
    min-width: 0;
    align-items: start;
  }
  @media (min-width: 960px) {
    .cols {
      grid-template-columns: minmax(0, 1fr) 420px;
    }
  }
  .left,
  .side {
    min-width: 0;
  }
  .stack {
    gap: 16px;
  }
  .plan > header {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 12px 16px;
    border-bottom: 1px solid var(--line);
  }
  .plan > header h2 {
    flex: 1;
    font-size: 16px;
  }
  .plan-body {
    padding: 16px;
  }
  .plan h3,
  [data-testid="import-result"] h3 {
    font-size: 14px;
    margin: 0 0 6px;
  }
  .plain {
    list-style: none;
    margin: 0;
    padding: 0;
    display: grid;
    gap: 6px;
  }
  .plain li {
    display: flex;
    gap: 8px;
    align-items: flex-start;
    min-width: 0;
    overflow-wrap: anywhere;
  }
  .plain li :global(svg) {
    flex: none;
    margin-top: 3px;
  }
  .go {
    padding: 16px;
    border-top: 1px solid var(--line);
  }
  .go .hint {
    margin: 0;
  }
  .go .btn.primary {
    justify-self: start;
  }
  .run {
    display: grid;
    gap: 12px;
    max-width: 760px;
  }
  .run p {
    margin: 0;
  }
  .made {
    list-style: none;
    margin: 0;
    padding: 0;
    display: grid;
    gap: 8px;
  }
  .made li {
    display: flex;
    gap: 8px;
    align-items: flex-start;
    overflow-wrap: anywhere;
  }
  .made li :global(svg) {
    flex: none;
    margin-top: 3px;
  }
  .stack p {
    margin: 0;
  }
  .fail {
    max-width: 760px;
  }
  .fail :global(h2) {
    font-size: 16px;
  }
  [data-testid="import-result"] {
    max-width: 760px;
  }
  [data-testid="import-result"] > .row > :global(svg) {
    color: var(--ok-fill);
  }
</style>
