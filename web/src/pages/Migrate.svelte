<script lang="ts">
  // Bring in (#/migrate): copy or move the settings and data of programs installed on this PC
  // into Zero2Dev's portable folders, and revert. Each source opens a plan (what goes where, copy
  // or move, blockers); nothing starts before the review dialog. History lists every migration
  // with Revert. `?source=<id>` (from the Runtimes page) opens that source's plan.
  import { onDestroy, tick, untrack } from "svelte";
  import { ApiError, api } from "../lib/api";
  import { ago, fmtDateTime } from "../lib/format";
  import {
    DB_KINDS,
    blockerAction,
    canRevert,
    dbTargets,
    defaultMode,
    defaultPort,
    defaultRevert,
    defaultUser,
    fmtSize,
    groupSources,
    modeAllowed,
    pickedItems,
    preselect,
    requestItems,
    revertLabel,
    revertText,
    reviewText,
    sourceIconKind,
    sourceSummary,
    startLabel,
    stateLabel,
    targetText,
    totalSize,
    validateDbCopy,
    validateDbForm,
    type DbField,
    type DbKind,
  } from "../lib/migrate";
  import { hashParam } from "../lib/router.svelte";
  import { errMessage, jobs, lists, loadRuntimes, loadSettings } from "../lib/store.svelte";
  import type {
    DbDatabase,
    Job,
    MigrateMode,
    MigrateRequest,
    MigrateSource,
    Migration,
    RevertHow,
  } from "../lib/types";
  import { invalidateBringIn } from "../ui/BringInNote.svelte";
  import ConfirmDialog from "../ui/ConfirmDialog.svelte";
  import Icon from "../ui/Icon.svelte";
  import KindIcon from "../ui/KindIcon.svelte";
  import Modal from "../ui/Modal.svelte";
  import PageHead from "../ui/PageHead.svelte";

  let sources = $state<MigrateSource[] | null>(null);
  let history = $state<Migration[] | null>(null);
  let loadError = $state("");
  let historyError = $state("");
  let checking = $state(false);

  // ---- the open plan (one source at a time) ----
  let openId = $state("");
  let mode = $state<MigrateMode>("copy");
  let checked = $state<string[]>([]);
  let planError = $state("");
  let reviewing = $state(false);
  let starting = $state(false);
  const current = $derived(sources?.find((s) => s.id === openId));

  // ---- database copy form ----
  let dbKind = $state<DbKind>("mysql");
  let dbHost = $state("127.0.0.1");
  let dbPort = $state("3306");
  let dbUser = $state("root");
  let dbPass = $state("");
  let dbErr = $state<Partial<Record<DbField, string>>>({});
  let dbConnError = $state("");
  let dbList = $state<DbDatabase[] | null>(null);
  let dbPicked = $state<string[]>([]);
  let dbTarget = $state("");
  let listing = $state(false);
  const targets = $derived(dbTargets(lists.services, dbKind));
  const targetSvc = $derived(targets.find((t) => t.id === dbTarget));

  // ---- the running job ----
  type Track = { id: string; revert: boolean; label: string; program: string; mode: MigrateMode; migrationId: string };
  let track = $state<Track | null>(null);
  let polled = $state<Job | null>(null);
  const live = $derived.by(() => {
    if (!track) return null;
    const a = jobs[track.id];
    const b = polled;
    if (a && a.state !== "running") return a;
    if (b && b.state !== "running") return b;
    return a ?? b;
  });
  let result = $state<{ tone: "ok" | "bad"; title: string; text: string; next?: { text: string; href: string; link: string } } | null>(null);

  // ---- revert ----
  let revertId = $state("");
  let how = $state<RevertHow | undefined>(undefined);
  let confirmRevert = $state(false);
  let reverting = $state(false);
  let revertError = $state("");
  const revertMig = $derived(history?.find((m) => m.id === revertId));

  const groups = $derived(sources ? groupSources(sources) : []);
  const activeOf = (id: string) => history?.find((m) => m.source === id && m.state === "done");
  const busy = $derived(!!track);
  const serviceName = (id?: string) => lists.services.find((s) => s.id === id)?.name ?? "the Zero2Dev service";

  async function loadHistory() {
    try {
      history = await api.listMigrations();
      historyError = "";
    } catch (e) {
      historyError = errMessage(e);
    }
  }
  async function reload() {
    checking = true;
    await Promise.all([
      (async () => {
        try {
          sources = await api.migrationSources();
          loadError = "";
        } catch (e) {
          loadError = errMessage(e);
        }
      })(),
      loadHistory(),
    ]);
    checking = false;
  }

  // First load, then open the source a `?source=` link names.
  let preselected = "";
  async function applyHash() {
    const want = hashParam("source");
    if (!want || !sources || want === preselected) return;
    preselected = want;
    const id = preselect(sources, want);
    if (!id) {
      planError = "";
      result = { tone: "bad", title: "Not listed.", text: "The program that link names is not on the list any more. It may be adopted already, or no longer registered." };
      return;
    }
    openSource(id);
    await tick();
    const el = document.querySelector<HTMLElement>(`[data-source="${CSS.escape(id)}"]`);
    el?.scrollIntoView({ block: "start", behavior: "smooth" });
    document.getElementById(`plan-h-${id}`)?.focus({ preventScroll: true });
  }
  void reload().then(applyHash);
  const onHash = () => void applyHash();
  window.addEventListener("hashchange", onHash);
  onDestroy(() => {
    window.removeEventListener("hashchange", onHash);
    stopPoll();
  });

  function openSource(id: string) {
    const s = sources?.find((x) => x.id === id);
    if (!s) return;
    revertId = "";
    openId = id;
    mode = defaultMode(s);
    checked = s.items.filter((i) => i.exists).map((i) => i.label);
    planError = "";
    if (s.program === "database") resetDb();
  }
  function toggleSource(id: string) {
    if (openId === id) openId = "";
    else openSource(id);
  }

  // ---- database form ----
  function resetDb() {
    dbErr = {};
    dbConnError = "";
    dbList = null;
    dbPicked = [];
    dbTarget = targets.length === 1 ? targets[0].id : "";
  }
  function pickKind(k: DbKind) {
    if (dbPort === String(defaultPort(dbKind))) dbPort = String(defaultPort(k));
    if (dbUser === defaultUser(dbKind)) dbUser = defaultUser(k);
    dbKind = k;
    dbList = null;
    dbPicked = [];
    const t = dbTargets(lists.services, k);
    dbTarget = t.length === 1 ? t[0].id : "";
  }
  function connChanged() {
    dbList = null;
    dbPicked = [];
    dbConnError = "";
  }
  async function listDatabases() {
    dbConnError = "";
    dbErr = validateDbForm({ kind: dbKind, host: dbHost, port: dbPort, user: dbUser }, lists.services);
    if (Object.keys(dbErr).length) {
      await tick();
      document.querySelector<HTMLElement>('[data-testid="migrate-db-form"] [aria-invalid="true"]')?.focus();
      return;
    }
    listing = true;
    try {
      const r = await api.remoteDatabases({ kind: dbKind, host: dbHost.trim(), port: Number(dbPort), user: dbUser.trim(), password: dbPass });
      dbList = r.databases;
      dbPicked = [];
    } catch (e) {
      dbConnError = errMessage(e);
      dbList = null;
    } finally {
      listing = false;
    }
  }

  // ---- review and start ----
  const plannedItems = $derived(current ? pickedItems(current, checked) : []);
  const pickedDbs = $derived((dbList ?? []).filter((d) => dbPicked.includes(d.name)));
  const blocked = $derived(!!current && current.blockers.length > 0);
  const already = $derived(current && current.program !== "database" ? activeOf(current.id) : undefined);
  const canReview = $derived(
    !!current && !blocked && !already && !busy && (current.program === "database" ? !!dbList : plannedItems.length > 0),
  );

  async function review() {
    if (!current || !canReview) return;
    planError = "";
    if (current.program === "database") {
      dbErr = validateDbCopy(dbPicked, dbTarget, targets);
      if (Object.keys(dbErr).length) {
        await tick();
        document.querySelector<HTMLElement>('[data-testid="migrate-db-form"] [aria-invalid="true"]')?.focus();
        return;
      }
    }
    reviewing = true;
  }
  const reviewSentence = $derived(
    current
      ? reviewText(
          current,
          mode,
          plannedItems,
          current.program === "database" ? { databases: pickedDbs, host: dbHost.trim(), port: Number(dbPort), target: targetSvc?.name ?? "" } : undefined,
        )
      : "",
  );

  async function start() {
    const s = current;
    if (!s) return;
    const req: MigrateRequest = { source: s.id, mode, items: s.program === "database" ? [] : requestItems(s, checked) };
    if (s.program === "database")
      req.database = { kind: dbKind, host: dbHost.trim(), port: Number(dbPort), user: dbUser.trim(), password: dbPass, databases: [...dbPicked], targetServiceId: dbTarget };
    starting = true;
    try {
      const job = await api.startMigration(req);
      reviewing = false;
      openId = "";
      result = null;
      begin(job, { revert: false, label: s.label, program: s.program, mode });
    } catch (e) {
      reviewing = false;
      planError = errMessage(e);
      // A blocker that appeared since the list was read: show the current state.
      if (e instanceof ApiError && e.status === 409) void reload();
    } finally {
      starting = false;
    }
  }

  // ---- job tracking: the event stream (store.jobs) plus a poll as a safety net ----
  let pollTimer: ReturnType<typeof setTimeout> | undefined;
  function stopPoll() {
    if (pollTimer) clearTimeout(pollTimer);
    pollTimer = undefined;
  }
  function poll(id: string) {
    stopPoll();
    pollTimer = setTimeout(async () => {
      if (track?.id !== id) return;
      try {
        const j = await api.getJob(id);
        if (track?.id !== id) return;
        polled = j;
        if (j.state === "running") poll(id);
      } catch {
        poll(id);
      }
    }, 1500);
  }
  function begin(job: Job, t: Omit<Track, "id" | "migrationId">) {
    polled = job;
    track = { ...t, id: job.id, migrationId: job.migrationId ?? "" };
    poll(job.id);
    void tick().then(() => document.querySelector<HTMLElement>('[data-testid="migrate-progress"]')?.scrollIntoView({ block: "nearest", behavior: "smooth" }));
  }
  $effect(() => {
    const j = live;
    if (!j || j.state === "running") return;
    untrack(() => void finish(j));
  });
  async function finish(j: Job) {
    const t = track;
    if (!t) return;
    track = null;
    polled = null;
    stopPoll();
    await reload();
    invalidateBringIn();
    if (t.program === "runtime") void loadRuntimes();
    if (["npm-global", "composer", "go-cache"].includes(t.program)) void loadSettings();
    const m = history?.find((x) => x.id === (j.migrationId || t.migrationId));
    if (j.state === "done") {
      result = t.revert
        ? { tone: "ok", title: "Reverted.", text: `${t.label} is back as it was before it was brought in.` }
        : { tone: "ok", title: "Done.", text: m?.description || `${t.label} is in Zero2Dev now.`, next: nextStep(t.program) };
    } else {
      result = {
        tone: "bad",
        title: t.revert ? `${t.label} was not reverted.` : `${t.label} was not brought in.`,
        text: `${j.error || "Unknown error."} What was there before is kept; see History.`,
      };
    }
    await tick();
    const el = document.querySelector<HTMLElement>('[data-testid="migrate-result"]');
    el?.scrollIntoView({ block: "nearest", behavior: "smooth" });
    el?.focus({ preventScroll: true });
  }
  function nextStep(program: string): { text: string; href: string; link: string } | undefined {
    if (["vscode", "vscodium", "notepadpp", "heidisql", "dbeaver"].includes(program))
      return { text: "Open it from its card to find your settings there.", href: "#/runtimes", link: "Open Runtimes" };
    if (program === "runtime") return { text: "It is listed as installed now, and services that use it keep working.", href: "#/runtimes", link: "Open Runtimes" };
    if (program === "database") return { text: "The databases are in the service now.", href: "#/services", link: "Open Services" };
    return { text: "Zero2Dev's runtimes use the new folder from their next start.", href: "#/runtimes", link: "Open Runtimes" };
  }

  // ---- revert ----
  function openRevert(m: Migration) {
    openId = "";
    revertId = revertId === m.id ? "" : m.id;
    how = defaultRevert(m);
    revertError = "";
  }
  async function doRevert() {
    const m = revertMig;
    if (!m || !how) return;
    reverting = true;
    try {
      const job = await api.revertMigration(m.id, how);
      confirmRevert = false;
      revertId = "";
      result = null;
      begin(job, { revert: true, label: m.label, program: m.program, mode: m.mode });
    } catch (e) {
      confirmRevert = false;
      revertError = errMessage(e);
    } finally {
      reverting = false;
    }
  }

  const modeHint = (s: MigrateSource, md: MigrateMode) =>
    md === "move"
      ? `The originals are saved, then removed from their old place. The installed ${s.label} starts fresh; Revert puts them back.`
      : `The originals stay where they are, and the installed ${s.label} keeps working as before.`;
  const reviewTitle = (s: MigrateSource, md: MigrateMode) =>
    s.program === "runtime"
      ? `Adopt ${s.label}?`
      : s.program === "database"
        ? "Copy these databases?"
        : `${md === "move" ? "Move" : "Copy"} ${s.label} into Zero2Dev?`;
  const openLabel = (s: MigrateSource) => (s.program === "runtime" ? "Adopt…" : s.program === "database" ? "Copy databases…" : "Bring in…");
</script>

{#snippet blockers(s: MigrateSource)}
  {#each s.blockers as b (b)}
    {@const act = blockerAction(b)}
    <div class="notice warn" role="status" data-testid="migrate-blocker">
      <Icon name="alert" />
      <span class="grow">{b}.</span>
      {#if act.type === "link"}<a class="btn sm" href={act.href}>{act.label}</a>
      {:else if act.type === "recheck"}<button class="btn sm" type="button" onclick={() => void reload()} disabled={checking} data-testid="migrate-recheck"><Icon name="restart" size={14} /> {act.label}</button>{/if}
    </div>
  {/each}
{/snippet}

{#snippet plan(s: MigrateSource)}
  {@const act = activeOf(s.id)}
  <div class="plan" role="region" aria-labelledby="plan-h-{s.id}" id="plan-{s.id}" data-testid="migrate-plan">
    <h3 class="sr-only" id="plan-h-{s.id}" tabindex="-1">{s.label}: what to bring in</h3>
    {@render blockers(s)}
    {#if act && s.program !== "database"}
      <div class="notice" data-testid="migrate-already">
        <Icon name="info" />
        <span>Brought in {ago(act.createdAt)}. Revert it in History below to bring it in again.</span>
      </div>
    {/if}

    {#if s.program === "database"}
      <form class="db stack" data-testid="migrate-db-form" novalidate onsubmit={(e) => { e.preventDefault(); void listDatabases(); }}>
        <div class="field">
          <span class="label" id="db-kind-l">Server type</span>
          <div class="seg" role="radiogroup" aria-labelledby="db-kind-l">
            {#each DB_KINDS as k (k.id)}
              <label><input type="radio" name="db-kind" value={k.id} checked={dbKind === k.id} onchange={() => pickKind(k.id)} />{k.label}</label>
            {/each}
          </div>
        </div>
        <div class="grid2">
          <div class="field">
            <label for="db-host" class="label">Host</label>
            <input id="db-host" type="text" class="mono" bind:value={dbHost} oninput={connChanged} aria-invalid={!!dbErr.host} aria-describedby="db-host-m" spellcheck="false" />
            {#if dbErr.host}<span class="err" id="db-host-m">{dbErr.host}</span>{:else}<span class="hint" id="db-host-m">127.0.0.1 for a server installed on this PC.</span>{/if}
          </div>
          <div class="field">
            <label for="db-port" class="label">Port</label>
            <input id="db-port" type="text" inputmode="numeric" class="mono" bind:value={dbPort} oninput={connChanged} aria-invalid={!!dbErr.port} aria-describedby="db-port-m" />
            {#if dbErr.port}<span class="err" id="db-port-m">{dbErr.port}</span>{:else}<span class="hint" id="db-port-m">Usually {defaultPort(dbKind)}.</span>{/if}
          </div>
          <div class="field">
            <label for="db-user" class="label">User</label>
            <input id="db-user" type="text" bind:value={dbUser} oninput={connChanged} aria-invalid={!!dbErr.user} aria-describedby="db-user-m" spellcheck="false" autocomplete="off" />
            {#if dbErr.user}<span class="err" id="db-user-m">{dbErr.user}</span>{/if}
          </div>
          <div class="field">
            <label for="db-pass" class="label">Password</label>
            <input id="db-pass" type="password" bind:value={dbPass} oninput={connChanged} autocomplete="off" aria-describedby="db-pass-m" />
            <span class="hint" id="db-pass-m">Used for this copy only; Zero2Dev does not store it.</span>
          </div>
        </div>
        <div class="row wrap">
          <button class="btn" type="submit" disabled={listing} data-testid="migrate-db-list">
            {#if listing}<span class="spin" aria-hidden="true"></span> Connecting...{:else}<Icon name="database" size={14} /> List databases{/if}
          </button>
        </div>
        {#if dbConnError}
          <div class="notice bad" role="alert" data-testid="migrate-db-error"><Icon name="alert" /><span>Could not list the databases: {dbConnError}</span></div>
        {/if}
        {#if dbList}
          <fieldset class="items" aria-describedby="db-dbs-m" data-testid="migrate-db-list-result">
            <legend class="label">Databases on {dbHost.trim()}:{dbPort}</legend>
            {#if dbList.length === 0}
              <p class="muted">That server has no databases of its own.</p>
            {:else}
              {#each dbList as d (d.name)}
                <label class="check"><input type="checkbox" value={d.name} bind:group={dbPicked} onchange={() => (dbErr = { ...dbErr, databases: undefined })} /><span class="mono">{d.name}</span>{#if d.sizeBytes}<span class="muted small">{fmtSize(d.sizeBytes)}</span>{/if}</label>
              {/each}
            {/if}
            {#if dbErr.databases}<span class="err" id="db-dbs-m">{dbErr.databases}</span>{/if}
          </fieldset>
          <div class="field">
            <label for="db-target" class="label">Copy into</label>
            <select id="db-target" bind:value={dbTarget} aria-invalid={!!dbErr.target} aria-describedby="db-target-m" data-testid="migrate-db-target">
              <option value="">Pick a Zero2Dev service</option>
              {#each targets as t (t.id)}<option value={t.id}>{t.name} ({t.kind}, port {t.port})</option>{/each}
            </select>
            {#if dbErr.target}<span class="err" id="db-target-m">{dbErr.target}</span>
            {:else if targets.length === 0}<span class="hint" id="db-target-m">No Zero2Dev service can take a {dbKind === "postgres" ? "PostgreSQL" : "MySQL or MariaDB"} dump. <a href="#/services/new">Create one</a>.</span>
            {:else if targetSvc && targetSvc.state !== "running"}<span class="hint" id="db-target-m">{targetSvc.name} is stopped. <a href="#/services/{targetSvc.id}">Start it</a> first: the copy imports into the running server.</span>
            {:else}<span class="hint" id="db-target-m">A database that exists there already is replaced.</span>{/if}
          </div>
        {/if}
      </form>
    {:else}
      <fieldset class="items" data-testid="migrate-items">
        <legend class="label">What to bring in</legend>
        {#each s.items as it (it.label)}
          <label class="item" class:off={!it.exists}>
            <input type="checkbox" value={it.label} bind:group={checked} disabled={!it.exists || s.items.length === 1} />
            <span class="what">
              <span class="it-title">{it.label} <span class="muted small">{it.exists ? fmtSize(it.sizeBytes) : "not on this PC"}</span></span>
              <span class="fromto small"><code>{it.from}</code> <Icon name="arrowRight" size={12} /><span class="sr-only">to</span> <code>{it.to}</code></span>
            </span>
          </label>
        {/each}
      </fieldset>

      <div class="field">
        <span class="label" id="mode-l-{s.id}">How</span>
        <div class="seg" role="radiogroup" aria-labelledby="mode-l-{s.id}" data-testid="migrate-mode">
          <label><input type="radio" name="mode-{s.id}" value="copy" bind:group={mode} disabled={!modeAllowed(s, "copy")} />Copy (keep the originals)</label>
          <label title={modeAllowed(s, "move") ? undefined : s.copyOnlyReason}><input type="radio" name="mode-{s.id}" value="move" bind:group={mode} disabled={!modeAllowed(s, "move")} aria-describedby={modeAllowed(s, "move") ? undefined : `copy-only-${s.id}`} />Move (originals moved into Zero2Dev)</label>
        </div>
        {#if !modeAllowed(s, "move") && s.copyOnlyReason}
          <span class="hint" id="copy-only-{s.id}" data-testid="migrate-copy-only">Move is not offered: {s.copyOnlyReason}</span>
        {:else}
          <span class="hint">{modeHint(s, mode)}</span>
        {/if}
      </div>
    {/if}

    {#if targetText(s)}
      <div class="notice warn" data-testid="migrate-target-warning"><Icon name="alert" /><span>{targetText(s)}</span></div>
    {/if}
    {#if s.note}<p class="hint note" data-testid="migrate-note"><Icon name="info" size={14} /> {s.note}</p>{/if}
    {#if planError}
      <div class="notice bad" role="alert" data-testid="migrate-error"><Icon name="alert" /><span>{planError}</span></div>
    {/if}

    <div class="row wrap">
      <button class="btn primary" type="button" onclick={review} disabled={!canReview} data-testid="migrate-review">
        Review{s.program === "database" ? " the copy" : ""}{s.program !== "database" && plannedItems.length ? ` (${fmtSize(totalSize(plannedItems))})` : ""}
      </button>
      <button class="btn ghost" type="button" onclick={() => (openId = "")}>Cancel</button>
      {#if busy}<span class="hint">Wait until the running job is done.</span>{/if}
    </div>
  </div>
{/snippet}

<PageHead sub="Bring the settings and data of programs installed on this PC into Zero2Dev's portable folder. Every step can be reverted.">
  <button class="btn" type="button" onclick={() => void reload()} disabled={checking} data-testid="migrate-refresh">
    {#if checking}<span class="spin" aria-hidden="true"></span> Looking...{:else}<Icon name="restart" size={14} /> Check again{/if}
  </button>
</PageHead>

<div class="page stack" data-testid="migrate-page">
  {#if result}
    <div class="notice" class:ok={result.tone === "ok"} class:bad={result.tone === "bad"} role={result.tone === "bad" ? "alert" : "status"} tabindex="-1" data-testid="migrate-result">
      <Icon name={result.tone === "ok" ? "circleCheck" : "alert"} />
      <div class="grow">
        <b>{result.title}</b> {result.text}
        {#if result.next}<div class="small">{result.next.text} <a href={result.next.href}>{result.next.link}</a></div>{/if}
      </div>
      <button class="btn sm ghost icon" type="button" aria-label="Dismiss" onclick={() => (result = null)}><Icon name="x" size={14} /></button>
    </div>
  {/if}

  {#if track && live}
    <div class="card card-pad prog" role="group" aria-label="{track.revert ? 'Reverting' : 'Bringing in'} {track.label}" data-testid="migrate-progress">
      <div class="row"><span class="spin" aria-hidden="true"></span><b class="grow">{track.revert ? "Reverting" : track.program === "runtime" ? "Adopting" : track.mode === "move" ? "Moving" : "Copying"} {track.label}</b><span class="mono muted">{Math.round(live.percent)}%</span></div>
      <div class="progress" role="progressbar" aria-valuemin="0" aria-valuemax="100" aria-valuenow={Math.round(live.percent)} aria-label="Progress"><span style:width="{live.percent}%"></span></div>
      <span class="small muted">{live.step || "Starting"}</span>
    </div>
  {/if}

  {#if loadError}
    <div class="notice bad" role="alert" data-testid="migrate-load-error">
      <Icon name="alert" /><span class="grow">Could not look for programs: {loadError}</span>
      <button class="btn sm" type="button" onclick={() => void reload()}>Try again</button>
    </div>
  {:else if !sources}
    <p class="muted"><span class="spin" aria-hidden="true"></span> Looking for programs on this PC...</p>
  {:else}
    {#each groups as g (g.group.id)}
      <section class="section-card" aria-labelledby="mg-{g.group.id}" data-group={g.group.id}>
        <header>
          <h2 id="mg-{g.group.id}">{g.group.title}</h2>
          <p>{g.group.text}</p>
        </header>
        <div class="body">
          {#if g.found.length}
            <ul class="sources">
              {#each g.found as s (s.id)}
                {@const act = activeOf(s.id)}
                {@const open = openId === s.id}
                <li class="src" class:open data-testid="migrate-source" data-source={s.id}>
                  <div class="src-head">
                    {#if s.program === "database"}<span class="db-ico"><Icon name="database" size={20} /></span>{:else}<KindIcon kind={sourceIconKind(s, lists.runtimes)} size={22} />{/if}
                    <div class="grow src-name">
                      <span class="src-title">{s.label}
                        {#if act && s.program !== "database"}<span class="badge running"><span class="dot"></span>{stateLabel(act).text}</span>{:else if s.blockers.length}<span class="badge warn"><span class="dot"></span>Needs attention</span>{/if}
                      </span>
                      <span class="muted small">{sourceSummary(s)}{s.program !== "database" && !modeAllowed(s, "move") ? " · copy only" : ""}</span>
                    </div>
                    <button class="btn sm" type="button" aria-expanded={open} aria-controls="plan-{s.id}" onclick={() => toggleSource(s.id)} data-testid="migrate-open">
                      {open ? "Close" : openLabel(s)}
                    </button>
                  </div>
                  {#if open}{@render plan(s)}{/if}
                </li>
              {/each}
            </ul>
          {/if}
          {#if g.missing.length}
            <p class="hint" data-testid="migrate-missing">Not found on this PC: {g.missing.map((s) => s.label).join(", ")}.</p>
          {/if}
        </div>
      </section>
    {/each}
  {/if}

  <section class="section-card" aria-labelledby="mg-history" data-testid="migrate-history">
    <header>
      <h2 id="mg-history"><Icon name="history" size={16} /> History</h2>
      <p>Everything Bring in did, newest first. Revert puts things back as they were.</p>
    </header>
    <div class="body">
      {#if historyError}
        <div class="notice bad" role="alert"><Icon name="alert" /><span>Could not read the history: {historyError}</span></div>
      {:else if !history}
        <p class="muted">Loading...</p>
      {:else if history.length === 0}
        <p class="muted" data-testid="migrate-history-empty">Nothing brought in yet.</p>
      {:else}
        <ul class="hist">
          {#each history as m (m.id)}
            {@const st = stateLabel(m)}
            <li class="h-row" data-testid="migrate-history-row" data-migration={m.id} data-source={m.source} data-state={m.state}>
              <div class="h-main">
                <div class="h-title">
                  <b>{m.label}</b>
                  <span class="badge {st.tone === 'ok' ? 'running' : st.tone === 'bad' ? 'failed' : 'stopped'}"><span class="dot"></span>{st.text}</span>
                </div>
                <span>{m.description}</span>
                <span class="muted small">
                  <span title={fmtDateTime(m.createdAt)}>{m.mode === "move" ? "Moved" : "Copied"} {ago(m.createdAt)}</span>{#if m.revertedAt}<span title={fmtDateTime(m.revertedAt)}>, reverted {ago(m.revertedAt)}</span>{/if}
                </span>
              </div>
              {#if canRevert(m)}
                <button class="btn sm" type="button" aria-expanded={revertId === m.id} aria-controls="rv-{m.id}" onclick={() => openRevert(m)} disabled={busy} data-testid="migrate-revert" aria-label="Revert {m.label}">
                  <Icon name="undo" size={14} /> {revertId === m.id ? "Close" : "Revert…"}
                </button>
              {/if}
              {#if revertId === m.id}
                <div class="rv" id="rv-{m.id}" role="group" aria-labelledby="rv-h-{m.id}" data-testid="migrate-revert-panel">
                  <span class="label" id="rv-h-{m.id}">How to revert {m.label}</span>
                  <div class="hows" role="radiogroup" aria-labelledby="rv-h-{m.id}" data-testid="migrate-revert-how">
                    {#each m.revertOptions as o (o)}
                      <label class="how" class:on={how === o}>
                        <input type="radio" name="how-{m.id}" value={o} bind:group={how} />
                        <span class="what"><span class="it-title">{revertLabel(o)}</span><span class="hint">{revertText(o, m, serviceName(m.serviceId))}</span></span>
                      </label>
                    {/each}
                  </div>
                  {#if revertError}<div class="notice bad" role="alert" data-testid="migrate-revert-error"><Icon name="alert" /><span>{revertError}</span></div>{/if}
                  <div class="row wrap">
                    <button class="btn danger" type="button" disabled={!how || busy} onclick={() => (confirmRevert = true)} data-testid="migrate-revert-go"><Icon name="undo" size={14} /> {how ? revertLabel(how) : "Revert"}</button>
                    <button class="btn ghost" type="button" onclick={() => (revertId = "")}>Cancel</button>
                  </div>
                </div>
              {/if}
            </li>
          {/each}
        </ul>
      {/if}
    </div>
  </section>
</div>

{#if reviewing && current}
  <Modal title={reviewTitle(current, mode)} onclose={() => (reviewing = false)}>
    <div class="dlg-body review" data-testid="migrate-review-dialog">
      <p data-testid="migrate-review-text">{reviewSentence}</p>
      {#if current.program === "database"}
        <ul class="plainlist">{#each pickedDbs as d (d.name)}<li><code>{d.name}</code>{#if d.sizeBytes} <span class="muted small">{fmtSize(d.sizeBytes)}</span>{/if}</li>{/each}</ul>
      {:else}
        <ul class="plainlist">
          {#each plannedItems as it (it.label)}
            <li><b>{it.label}</b> <span class="muted small">{fmtSize(it.sizeBytes)}</span><br /><span class="fromto small"><code>{it.from}</code> <Icon name="arrowRight" size={12} /><span class="sr-only">to</span> <code>{it.to}</code></span></li>
          {/each}
        </ul>
      {/if}
      {#if targetText(current)}<p class="muted">{targetText(current)}</p>{/if}
      <p class="muted">You can revert this later in History.</p>
    </div>
    {#snippet footer()}
      <button class="btn" type="button" onclick={() => (reviewing = false)}>Cancel</button>
      <button class="btn primary" type="button" disabled={starting} onclick={start} data-testid="migrate-start">{startLabel(mode, current!)}</button>
    {/snippet}
  </Modal>
{/if}

{#if confirmRevert && revertMig && how}
  <ConfirmDialog
    title="{revertLabel(how)}: {revertMig.label}?"
    message={revertText(how, revertMig, serviceName(revertMig.serviceId))}
    confirmLabel={revertLabel(how)}
    busy={reverting}
    onconfirm={doRevert}
    oncancel={() => (confirmRevert = false)}
  />
{/if}

<style>
  .page {
    max-width: 920px;
    min-width: 0;
  }
  .notice.ok {
    border-color: var(--ok-border);
    background: var(--ok-soft);
  }
  .notice.ok > :global(svg) {
    color: var(--ok-fill);
  }
  .prog {
    display: grid;
    gap: 8px;
  }
  .sources,
  .hist,
  .plainlist {
    list-style: none;
    margin: 0;
    padding: 0;
    display: grid;
  }
  .plainlist {
    gap: 8px;
    margin-bottom: 12px;
  }
  .src,
  .h-row {
    border-top: 1px solid var(--line);
    padding: 12px 0;
    min-width: 0;
  }
  .src:first-child,
  .h-row:first-child {
    border-top: 0;
    padding-top: 0;
  }
  .src-head {
    display: flex;
    align-items: center;
    gap: 12px;
    min-width: 0;
  }
  .db-ico {
    display: inline-flex;
    color: var(--accent);
  }
  .src-name {
    display: grid;
    gap: 2px;
  }
  .src-title,
  .h-title {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 4px 8px;
    font-weight: 600;
  }
  .plan {
    display: grid;
    gap: 16px;
    margin-top: 12px;
    padding: 16px;
    border: 1px solid var(--line);
    border-radius: var(--radius-lg);
    background: var(--surface-2);
    min-width: 0;
  }
  .items {
    display: grid;
    gap: 8px;
    margin: 0;
    padding: 0;
    border: 0;
    min-width: 0;
  }
  .items legend {
    padding: 0;
    margin-bottom: 4px;
  }
  .item,
  .how {
    display: flex;
    align-items: flex-start;
    gap: 10px;
    padding: 8px 12px;
    border: 1px solid var(--line);
    border-radius: var(--radius);
    background: var(--surface);
    cursor: pointer;
    min-width: 0;
  }
  .item input,
  .how input {
    margin-top: 3px;
    flex: none;
  }
  .item.off {
    color: var(--muted);
    cursor: default;
  }
  .how.on {
    border-color: var(--accent);
  }
  .what {
    display: grid;
    gap: 2px;
    min-width: 0;
  }
  .it-title {
    font-weight: 600;
  }
  .fromto {
    display: inline-flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 4px;
    color: var(--muted);
    min-width: 0;
  }
  .fromto code {
    font-size: 12px;
    overflow-wrap: anywhere;
  }
  .seg label:has(input:disabled) {
    color: var(--disabled);
    cursor: not-allowed;
  }
  .note {
    display: flex;
    align-items: flex-start;
    gap: 6px;
    margin: 0;
  }
  .note :global(svg) {
    flex: none;
    margin-top: 3px;
  }
  .h-row {
    display: grid;
    grid-template-columns: minmax(0, 1fr) auto;
    gap: 8px 12px;
    align-items: start;
  }
  .h-main {
    display: grid;
    gap: 2px;
    min-width: 0;
    overflow-wrap: anywhere;
  }
  .rv {
    grid-column: 1 / -1;
    display: grid;
    gap: 12px;
    padding: 16px;
    border: 1px solid var(--line);
    border-radius: var(--radius-lg);
    background: var(--surface-2);
  }
  .hows {
    display: grid;
    gap: 8px;
  }
  #mg-history {
    display: inline-flex;
    align-items: center;
    gap: 8px;
  }
  .review p {
    margin: 0 0 12px;
  }
  @media (max-width: 560px) {
    .src-head {
      flex-wrap: wrap;
    }
    .h-row {
      grid-template-columns: minmax(0, 1fr);
    }
    .plan,
    .rv {
      padding: 12px;
    }
  }
</style>
