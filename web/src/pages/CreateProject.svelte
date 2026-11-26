<script lang="ts">
  // Create a project from a framework (#/create): choose a language and a framework, a name and a
  // folder; the plan on the right always shows what Zero2Dev will install and create. Create runs the
  // daemon's new-project job (docs/API.md, 0.0.6 "New project"): a progress view with the steps, then a
  // result card, or the failure with the choices kept. `?framework=laravel&name=Shop` prefills the form
  // (also `parent`, `db`, `domain`, `port`).
  import { onDestroy, onMount, tick, untrack } from "svelte";
  import { startAll } from "../lib/actions";
  import { api } from "../lib/api";
  import {
    buildRequest,
    canPlan,
    createState,
    createdOf,
    databaseChoices,
    defaultDatabase,
    defaultDomain,
    failureParts,
    frameworksOf,
    installTitle,
    joinProjectPath,
    languageOfFramework,
    languagesOf,
    lastLines,
    loadParent,
    needsLine,
    outline,
    problemAction,
    projectDomainError,
    projectPortError,
    saveParent,
    slugify,
    stepRows,
    type CreateForm,
    type PlanPhase,
    type ProblemAction,
  } from "../lib/newproject";
  import { hashParam } from "../lib/router.svelte";
  import { waitForJob } from "../lib/snapshotJobs";
  import { app, errMessage, lists, loadProjects, loadRuntimes, loadServices, loadSites, logs, toast } from "../lib/store.svelte";
  import type { Framework, Job, NewProjectPlan } from "../lib/types";
  import { isAbsolutePath, parseSuffixes } from "../lib/validate";
  import FolderInput from "../ui/FolderInput.svelte";
  import Icon from "../ui/Icon.svelte";
  import KindIcon from "../ui/KindIcon.svelte";
  import PageHead from "../ui/PageHead.svelte";

  type View = "form" | "running" | "done" | "failed";
  let view = $state<View>("form");

  // ---- the framework list ----
  let frameworks = $state<Framework[] | null>(null);
  let loadError = $state("");
  async function loadList() {
    loadError = "";
    try {
      const r = await api.listFrameworks();
      frameworks = r.frameworks;
      applyHash();
      if (!frameworkId) chooseLanguage(languages[0]?.id ?? "");
    } catch (e) {
      loadError = errMessage(e);
    }
  }

  // ---- the form ----
  let language = $state("");
  let frameworkId = $state("");
  let name = $state("");
  let parent = $state(loadParent());
  let database = $state("");
  let domain = $state("");
  let domainTouched = $state(false);
  let port = $state("");
  let openEditor = $state(true);

  const languages = $derived(languagesOf(frameworks ?? []));
  const options = $derived(frameworksOf(frameworks ?? [], language));
  const fw = $derived(frameworks?.find((f) => f.id === frameworkId));
  const dbChoices = $derived(databaseChoices(fw));
  const suffixes = $derived(parseSuffixes(app.settings?.domainSuffix ?? "localhost"));
  const slug = $derived(slugify(name));
  const folder = $derived(joinProjectPath(parent, slug));
  $effect(() => {
    if (!domainTouched) domain = defaultDomain(slug, suffixes[0] ?? "localhost");
  });

  const form = $derived<CreateForm>({ framework: frameworkId, name, parent, database, domain, port, openEditor });
  const nameError = $derived(name.trim() && !slug ? "Use at least one letter or digit." : "");
  const parentError = $derived(parent.trim() && !isAbsolutePath(parent.trim()) ? "Use an absolute path, for example D:\\www." : "");
  const domainError = $derived(projectDomainError(domain, suffixes) ?? "");
  const portError = $derived(projectPortError(port) ?? "");

  function chooseLanguage(id: string) {
    language = id;
    const first = frameworksOf(frameworks ?? [], id)[0];
    if (first && !(first.id === frameworkId)) chooseFramework(first.id);
  }
  function chooseFramework(id: string) {
    frameworkId = id;
    const f = frameworks?.find((x) => x.id === id);
    if (f) language = f.language;
    database = defaultDatabase(f);
  }

  // `?framework=&name=&parent=&db=&domain=&port=`: applied when the list is there and when the link changes.
  let applied = "";
  function applyHash() {
    if (!frameworks) return;
    const q = ["framework", "name", "parent", "db", "domain", "port"].map((k) => hashParam(k)).join("\u0000");
    if (q === applied) return;
    applied = q;
    const want = hashParam("framework");
    if (want && languageOfFramework(frameworks, want)) {
      chooseFramework(want);
      view = "form";
    }
    const n = hashParam("name");
    if (n) name = n;
    const p = hashParam("parent");
    if (p) parent = p;
    const db = hashParam("db");
    if (db && databaseChoices(fw).some((c) => c.value === db)) database = db;
    const d = hashParam("domain");
    if (d) {
      domain = d;
      domainTouched = true;
    }
    const pt = hashParam("port");
    if (pt) port = pt;
  }
  const onHash = () => applyHash();
  onMount(() => {
    window.addEventListener("hashchange", onHash);
    void loadList();
  });

  // ---- the plan: asked for 400 ms after the last change ----
  let plan = $state<NewProjectPlan | null>(null);
  let phase = $state<PlanPhase>("idle");
  let planError = $state("");
  let planSeq = 0;
  $effect(() => {
    if (view !== "form") return;
    const f = fw;
    const current = form;
    if (!f || !canPlan(current, suffixes)) {
      untrack(() => {
        planSeq++;
        plan = null;
        phase = "idle";
        planError = "";
      });
      return;
    }
    const req = buildRequest(f, current);
    untrack(() => (phase = "loading"));
    const n = ++planSeq;
    const t = setTimeout(async () => {
      try {
        const p = await api.planNewProject(req);
        if (n !== planSeq) return;
        plan = p;
        planError = "";
        phase = "ready";
      } catch (e) {
        if (n !== planSeq) return;
        plan = null;
        planError = errMessage(e);
        phase = "error";
      }
    }, 400);
    return () => clearTimeout(t);
  });

  const outlineOf = $derived(fw ? outline(fw) : { steps: [], creates: [] });
  let starting = $state(false);
  let startError = $state("");
  const can = $derived(createState(form, suffixes, plan, phase, starting));

  function act(a: ProblemAction) {
    if (a.kind !== "focus") return;
    const el = document.querySelector<HTMLElement>(`[data-field="${a.field}"] input`);
    el?.scrollIntoView({ block: "center", behavior: "smooth" });
    el?.focus({ preventScroll: true });
  }

  // ---- the running job ----
  let alive = true;
  onDestroy(() => {
    alive = false;
    window.removeEventListener("hashchange", onHash);
  });
  let job = $state<Job | null>(null);
  let runPlan = $state<NewProjectPlan | null>(null);
  let runName = $state("");
  let startedIds = $state<string[]>([]);
  let finished = $state<Job | null>(null);
  let startedProject = $state(false);
  let startingProject = $state(false);

  const rows = $derived(stepRows(runPlan?.steps ?? [], job));
  const live = $derived(job && job.state === "running" ? job.step || "Starting" : "");
  const outputService = $derived(lists.services.find((s) => !startedIds.includes(s.id)));
  const output = $derived(outputService ? lastLines(logs.byService[outputService.id] ?? [], 8) : []);
  const created = $derived(finished?.projectId ? createdOf(finished.projectId, lists.projects, lists.services, lists.sites) : { services: [], sites: [] });
  const failure = $derived(finished?.state === "failed" ? failureParts(finished.error) : null);

  async function create() {
    const f = fw;
    if (!f || !can.ok || !plan) return;
    startError = "";
    starting = true;
    try {
      const started = await api.startNewProject(buildRequest(f, form));
      saveParent(parent);
      runPlan = plan;
      runName = name.trim();
      startedIds = lists.services.map((s) => s.id);
      finished = null;
      startedProject = false;
      job = started;
      view = "running";
      void tick().then(() => document.querySelector<HTMLElement>('[data-testid="create-progress"]')?.focus({ preventScroll: true }));
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
      toast(end.state === "done" ? "success" : "error", end.state === "done" ? `${runName} created.` : `${runName} was not created. ${end.error}`);
      return;
    }
    finished = end;
    job = end;
    view = end.state === "done" ? "done" : "failed";
    await tick();
    document.querySelector<HTMLElement>('[data-testid="create-result"], [data-testid="create-failed"]')?.focus({ preventScroll: true });
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
    name = "";
    domainTouched = false;
    port = "";
    startError = "";
    backToForm();
    void tick().then(() => document.getElementById("cp-name")?.focus());
  }
</script>

<PageHead sub="Choose a language and a framework. Zero2Dev installs what is missing, runs the framework's own generator and sets the project up." />

<div class="page" data-testid="create-page">
  {#if loadError}
    <div class="notice bad" role="alert" data-testid="create-load-error">
      <Icon name="alert" /><span class="grow">Could not load the frameworks: {loadError}</span>
      <button class="btn sm" type="button" onclick={() => void loadList()}>Try again</button>
    </div>
  {:else if !frameworks}
    <p class="muted"><span class="spin" aria-hidden="true"></span> Loading frameworks...</p>
  {:else if view === "running"}
    <section class="card card-pad run" role="group" aria-labelledby="run-h" tabindex="-1" data-testid="create-progress">
      <div class="row"><span class="spin" aria-hidden="true"></span><h2 id="run-h" class="grow">Creating {runName}</h2><span class="mono muted">{Math.round(job?.percent ?? 0)}%</span></div>
      <div class="progress" role="progressbar" aria-valuemin="0" aria-valuemax="100" aria-valuenow={Math.round(job?.percent ?? 0)} aria-label="Progress"><span style:width="{job?.percent ?? 0}%"></span></div>
      <p class="small muted" role="status" data-testid="create-live-step">{live}</p>
      {@render stepList()}
      {#if output.length}
        <pre class="out mono small" aria-label="Output of the generator" data-testid="create-output">{output.join("\n")}</pre>
      {/if}
      <p class="hint">You can leave this page: the project is made in the background and you get a message when it is ready.</p>
    </section>
  {:else if view === "done" && finished}
    <section class="card card-pad stack" tabindex="-1" aria-labelledby="done-h" data-testid="create-result">
      <div class="row"><Icon name="circleCheck" size={20} /><h2 id="done-h" class="grow">{runName} is ready</h2></div>
      <p>Created in <code>{finished.path ?? runPlan?.dir}</code>.{#if (runPlan?.steps ?? []).some((s) => /editor/i.test(s))} Zero2Dev opened the folder in your editor.{/if}</p>
      <ul class="made">
        {#each created.services as s (s.id)}
          <li><Icon name="services" size={14} /> <span>Service <a href="#/services/{encodeURIComponent(s.id)}" data-testid="create-link-service">{s.name}</a> <span class="muted small">{s.kind}{s.port ? `, port ${s.port}` : ""}</span></span></li>
        {/each}
        {#each created.sites as s (s.id)}
          <li><Icon name="sites" size={14} /> <span>Site <a href={s.url} target="_blank" rel="noreferrer" data-testid="create-link-site">{s.https ? "https" : "http"}://{s.domain}</a></span></li>
        {/each}
        {#if created.project}
          <li><Icon name="projects" size={14} /> <span>Project <a href="#/projects/{encodeURIComponent(created.project.id)}/edit" data-testid="create-link-project">{created.project.name}</a></span></li>
        {/if}
        {#each runPlan?.installs ?? [] as i (i.catalogId)}
          <li><Icon name="download" size={14} /> <span>Installed {installTitle(i)}</span></li>
        {/each}
      </ul>
      <div class="row wrap">
        <button class="btn primary" type="button" disabled={startingProject || startedProject} onclick={startProject} data-testid="create-start">
          {#if startingProject}<span class="spin" aria-hidden="true"></span> Starting...{:else}<Icon name="play" size={14} /> {startedProject ? "Started" : "Start the project"}{/if}
        </button>
        <button class="btn" type="button" onclick={another} data-testid="create-another">Create another</button>
      </div>
    </section>
  {:else if view === "failed" && failure}
    <section class="notice bad fail" role="alert" tabindex="-1" aria-labelledby="fail-h" data-testid="create-failed">
      <Icon name="alert" />
      <div class="grow stack">
        <h2 id="fail-h">{runName} was not created</h2>
        <p data-testid="create-error">{failure.message}</p>
        <p>
          Zero2Dev removed what it had made for the project (services, site, project, database and its user).
          {#if failure.kept}The generated folder <code>{failure.kept}</code> was kept so you can look at it, or delete it.{/if}
          Runtimes that were installed stay installed.
        </p>
        <div class="row wrap">
          <button class="btn primary" type="button" onclick={backToForm} data-testid="create-back">Back to the form</button>
        </div>
      </div>
    </section>
  {:else}
    <div class="cols">
      <div class="left stack">
        <section class="section-card" aria-labelledby="sec-lang" data-tour="create-framework">
          <header><h2 id="sec-lang">Language and framework</h2></header>
          <div class="body stack">
            <div class="field" role="radiogroup" aria-labelledby="lang-l" data-testid="create-languages">
              <span class="label" id="lang-l">Language</span>
              <div class="seg">
                {#each languages as l (l.id)}
                  <label>
                    <input type="radio" name="cp-language" value={l.id} checked={language === l.id} onchange={() => chooseLanguage(l.id)} data-testid="create-language-{l.id}" />
                    {#if l.icon}<KindIcon kind={l.icon} size={16} />{:else}<Icon name="sites" size={16} />{/if}{l.label}
                  </label>
                {/each}
              </div>
            </div>
            <div class="field" role="radiogroup" aria-labelledby="fw-l">
              <span class="label" id="fw-l">Framework</span>
              <div class="fws" data-testid="create-frameworks">
                {#each options as f (f.id)}
                  <label class="fw" data-testid="create-framework" data-framework={f.id}>
                    <input type="radio" name="cp-framework" value={f.id} checked={frameworkId === f.id} onchange={() => chooseFramework(f.id)} />
                    <span class="fw-t">
                      <b>{f.label}</b>
                      <span class="muted">{f.summary}</span>
                      <span class="needs small" data-testid="create-needs">Needs: {needsLine(f)}</span>
                    </span>
                  </label>
                {/each}
              </div>
            </div>
          </div>
        </section>

        <section class="section-card" aria-labelledby="sec-where" data-tour="create-name">
          <header><h2 id="sec-where">Name and folder</h2></header>
          <div class="body stack">
            <div class="field" data-field="name">
              <label>
                <span class="label">Project name</span>
                <input id="cp-name" type="text" bind:value={name} maxlength="64" aria-invalid={!!nameError} aria-describedby="cp-name-h" placeholder="My shop" spellcheck="false" autocomplete="off" />
              </label>
              {#if nameError}<span class="err" id="cp-name-h">{nameError}</span>{:else}<span class="hint" id="cp-name-h" data-testid="create-slug">{slug ? `Short name: ${slug}` : "Letters, digits, - and _ are kept; spaces become -."}</span>{/if}
            </div>
            <div class="field" data-field="parent">
              <span class="label">Folder to create it in</span>
              <FolderInput bind:value={parent} label="Parent folder" title="Folder to create the project in" placeholder="D:\www" invalid={!!parentError} onpicked={saveParent} />
              {#if parentError}<span class="err">{parentError}</span>{:else if folder}<span class="hint" data-testid="create-folder">The project goes in <code>{folder}</code>.</span>{:else}<span class="hint">The project gets its own folder inside this one.</span>{/if}
            </div>
          </div>
        </section>

        <section class="section-card" aria-labelledby="sec-opt">
          <header><h2 id="sec-opt">Settings</h2><p>All optional: the defaults work.</p></header>
          <div class="body stack">
            {#if dbChoices.length}
              <div class="field" role="radiogroup" aria-labelledby="db-l" data-field="database" data-testid="create-database">
                <span class="label" id="db-l">Database</span>
                <div class="seg">
                  {#each dbChoices as c (c.value)}
                    <label><input type="radio" name="cp-db" value={c.value} bind:group={database} />{c.label}</label>
                  {/each}
                </div>
                <span class="hint">Zero2Dev makes a database and a user for this project only. The password goes in the project's .env file.</span>
              </div>
            {/if}
            {#if fw?.site}
              <div class="field" data-field="domain">
                <label>
                  <span class="label">Domain</span>
                  <input type="text" class="mono" bind:value={domain} oninput={() => (domainTouched = true)} aria-invalid={!!domainError} aria-describedby="cp-domain-h" placeholder="shop.{suffixes[0] ?? 'localhost'}" spellcheck="false" autocapitalize="off" />
                </label>
                {#if domainError}<span class="err" id="cp-domain-h">{domainError}</span>{:else}<span class="hint" id="cp-domain-h">The local address of the site. Must end in .{suffixes.join(" or .")}.</span>{/if}
              </div>
            {/if}
            {#if fw && fw.services.length}
              <div class="field" data-field="port">
                <label>
                  <span class="label">Port</span>
                  <input type="text" inputmode="numeric" class="mono" bind:value={port} aria-invalid={!!portError} aria-describedby="cp-port-h" placeholder="Free port" />
                </label>
                {#if portError}<span class="err" id="cp-port-h">{portError}</span>{:else}<span class="hint" id="cp-port-h">Leave it empty and Zero2Dev uses the framework's usual port, or the next free one.</span>{/if}
              </div>
            {/if}
            <label class="check"><input type="checkbox" bind:checked={openEditor} data-testid="create-editor" /> Open in my editor when it is ready</label>
          </div>
        </section>
      </div>

      <aside class="side" aria-label="What will happen">
        <div class="card plan" aria-labelledby="plan-h" aria-busy={phase === "loading"} data-testid="create-plan" data-tour="create-plan" data-phase={phase}>
          <header>
            <h2 id="plan-h">What will happen</h2>
            {#if phase === "loading"}<span class="muted small"><span class="spin" aria-hidden="true"></span> Checking...</span>{/if}
          </header>
          <div class="plan-body stack" aria-live="polite">
            {#if phase === "error"}
              <div class="notice bad" role="alert" data-testid="create-plan-error"><Icon name="alert" /><span>{planError}</span></div>
            {/if}
            {#if plan}
              {#if plan.problems.length}
                <div class="notice bad" role="alert" data-testid="create-problems">
                  <Icon name="alert" />
                  <div class="grow">
                    <b>{plan.problems.length === 1 ? "A problem stops this" : "Problems stop this"}</b>
                    <ul class="plain">
                      {#each plan.problems as pr (pr)}
                        {@const a = problemAction(pr)}
                        <li data-testid="create-problem">
                          {pr}
                          {#if a?.kind === "link"}<a href={a.href}>{a.label}</a>{:else if a}<button class="linkish" type="button" onclick={() => act(a)}>{a.label}</button>{/if}
                        </li>
                      {/each}
                    </ul>
                  </div>
                </div>
              {/if}
              <p class="small muted">In <code>{plan.dir}</code>, at <span class="mono">{plan.domain}</span>{plan.port ? `, port ${plan.port}` : ""}.</p>
              <div data-testid="create-installs">
                <h3>Installed first</h3>
                {#if plan.installs.length}
                  <ul class="plain">
                    {#each plan.installs as i (i.catalogId)}
                      <li data-testid="create-install" data-kind={i.kind}><KindIcon kind={i.kind} size={16} /><span class="grow"><b>{installTitle(i)}</b> <span class="muted small">{i.reason}</span></span></li>
                    {/each}
                  </ul>
                {:else}
                  <p class="muted small">Everything it needs is installed already.</p>
                {/if}
              </div>
              <div data-testid="create-steps">
                <h3>Steps, in order</h3>
                <ol class="steps">{#each plan.steps as s, i (i)}<li>{s}</li>{/each}</ol>
              </div>
              <div data-testid="create-creates">
                <h3>What is created</h3>
                <ul class="plain">{#each plan.creates as c, i (i)}<li><Icon name="check" size={14} /> {c}</li>{/each}</ul>
              </div>
              {#if plan.warnings.length}
                <div class="notice warn" data-testid="create-warnings">
                  <Icon name="alert" />
                  <ul class="plain grow">{#each plan.warnings as w (w)}<li>{w}</li>{/each}</ul>
                </div>
              {/if}
            {:else if phase !== "error"}
              <p class="muted small" data-testid="create-outline-note">Give it a name and a folder to see exactly what will happen. In short, for {fw?.label ?? "the framework"}:</p>
              <div>
                <h3>Steps</h3>
                <ol class="steps">{#each outlineOf.steps as s, i (i)}<li>{s}</li>{/each}</ol>
              </div>
              <div>
                <h3>What is created</h3>
                <ul class="plain">{#each outlineOf.creates as c, i (i)}<li><Icon name="check" size={14} /> {c}</li>{/each}</ul>
              </div>
            {/if}
          </div>
          <div class="go stack">
            {#if startError}<div class="notice bad" role="alert" data-testid="create-start-error"><Icon name="alert" /><span>{startError}</span></div>{/if}
            <button class="btn primary" type="button" disabled={!can.ok} onclick={create} aria-describedby="create-why" data-tour="create-go" data-testid="create-go">
              {#if starting}<span class="spin" aria-hidden="true"></span> Starting...{:else}<Icon name="plus" size={14} /> Create project{/if}
            </button>
            <p class="hint" id="create-why" data-testid="create-why">{can.ok ? `Creates ${folder}.` : can.reason}</p>
            <p class="hint" data-testid="create-assurance">Nothing outside <code>{folder || "the project folder"}</code> and the Zero2Dev data folder is touched.</p>
          </div>
        </div>
      </aside>
    </div>
  {/if}
</div>

{#snippet stepList()}
  <ol class="rows" data-testid="create-steps-live">
    {#each rows as r, i (i)}
      <li data-state={r.state} data-testid="create-step">
        <span class="mark" aria-hidden="true">
          {#if r.state === "running"}<span class="spin"></span>{:else if r.state === "done"}<Icon name="circleCheck" size={16} />{:else if r.state === "failed"}<Icon name="alert" size={16} />{:else}<span class="dot"></span>{/if}
        </span>
        <span class="grow" class:muted={r.state === "waiting"}>{r.label}</span>
        <span class="small muted state-t">{r.state === "running" ? `${r.percent}%` : r.state === "done" ? "Done" : r.state === "failed" ? "Failed" : "Waiting"}</span>
      </li>
    {/each}
  </ol>
{/snippet}

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
  .fws {
    display: grid;
    gap: 8px;
  }
  .fw {
    display: flex;
    align-items: flex-start;
    gap: 10px;
    padding: 10px 12px;
    border: 1px solid var(--line);
    border-radius: var(--radius);
    cursor: pointer;
    background: var(--surface);
  }
  .fw:hover {
    background: var(--surface-2);
  }
  .fw:has(input:checked) {
    border-color: var(--accent);
    background: var(--accent-soft);
  }
  .fw:has(input:focus-visible) {
    outline: 2px solid var(--accent);
    outline-offset: 2px;
  }
  .fw input {
    margin-top: 4px;
    accent-color: var(--accent);
  }
  .fw-t {
    display: grid;
    gap: 2px;
    min-width: 0;
  }
  .needs {
    color: var(--muted);
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
  .plan h3 {
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
  .steps {
    margin: 0;
    padding-left: 20px;
    display: grid;
    gap: 4px;
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
  .linkish {
    all: unset;
    cursor: pointer;
    color: var(--accent-text);
    text-decoration: underline;
  }
  .linkish:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 2px;
  }
  .run {
    display: grid;
    gap: 12px;
    max-width: 760px;
  }
  .run p {
    margin: 0;
  }
  .rows {
    list-style: none;
    margin: 0;
    padding: 0;
    border: 1px solid var(--line);
    border-radius: var(--radius);
  }
  .rows li {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 8px 12px;
  }
  .rows li + li {
    border-top: 1px solid var(--line);
  }
  .rows li[data-state="running"] {
    background: var(--accent-soft);
  }
  .rows li[data-state="failed"] {
    background: var(--bad-soft);
  }
  .rows li[data-state="done"] .mark {
    color: var(--ok-fill);
  }
  .rows li[data-state="failed"] .mark {
    color: var(--bad-fill);
  }
  .mark {
    flex: none;
    width: 16px;
    height: 16px;
    display: inline-grid;
    place-items: center;
  }
  .mark .dot {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    border: 1.5px solid var(--muted);
  }
  .state-t {
    flex: none;
  }
  .out {
    margin: 0;
    padding: 8px 12px;
    max-height: 180px;
    overflow: auto;
    background: var(--sunken);
    border-radius: var(--radius);
    white-space: pre-wrap;
    overflow-wrap: anywhere;
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
  [data-testid="create-result"] {
    max-width: 760px;
  }
  [data-testid="create-result"] > .row > :global(svg) {
    color: var(--ok-fill);
  }
</style>
