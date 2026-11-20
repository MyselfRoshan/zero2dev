<script lang="ts">
  // Import (#/migrate): copy or move the settings and data of programs installed on this PC into
  // Zero2Dev's portable folders, and revert. Programs tab: a summary, what is worth importing, the
  // programs found (with a side panel for the open one, or under the row on a narrow screen) and a bar
  // for importing several at once. History tab: every migration with its details and what can be
  // done with it. Nothing starts before a review dialog. `?source=<id>` opens that program's plan,
  // `?tab=history` opens History.
  import { onDestroy, tick, untrack } from "svelte";
  import { ApiError, api } from "../lib/api";
  import {
    activeMigration,
    freeSpace,
    groupSources,
    defaultChecked,
    defaultMode,
    pickedItems,
    preselect,
    recommended,
    requestItems,
    reviewText,
    selectable,
    sourceStatus,
    startLabel,
    summary,
    targetText,
    totalSize,
    fmtSize,
  } from "../lib/migrate";
  import { hashParam } from "../lib/router.svelte";
  import { errMessage, jobs, loadRuntimes, loadSettings } from "../lib/store.svelte";
  import type { Job, MigrateMode, MigrateProgram, MigrateRequest, MigrateSource, Migration } from "../lib/types";
  import { invalidateBringIn } from "../ui/BringInNote.svelte";
  import Icon from "../ui/Icon.svelte";
  import MigrateBatch from "../ui/MigrateBatch.svelte";
  import MigrateDbForm from "../ui/MigrateDbForm.svelte";
  import MigrateHistory from "../ui/MigrateHistory.svelte";
  import MigratePlan from "../ui/MigratePlan.svelte";
  import MigrateSourceRow from "../ui/MigrateSourceRow.svelte";
  import MigrateSummary from "../ui/MigrateSummary.svelte";
  import Modal from "../ui/Modal.svelte";
  import PageHead from "../ui/PageHead.svelte";

  type Tab = "programs" | "history";
  let tab = $state<Tab>("programs");
  let sources = $state<MigrateSource[] | null>(null);
  let history = $state<Migration[] | null>(null);
  let loadError = $state("");
  let historyError = $state("");
  let checking = $state(false);

  // ---- layout: a side panel from 960 px, else the plan opens under its row ----
  const mq = window.matchMedia("(min-width: 960px)");
  let wide = $state(mq.matches);
  const onMq = (e: MediaQueryListEvent) => (wide = e.matches);
  mq.addEventListener("change", onMq);

  // ---- the open plan (one program at a time) ----
  let openId = $state("");
  let mode = $state<MigrateMode>("copy");
  let checked = $state<string[]>([]);
  let planError = $state("");
  let reviewing = $state(false);
  let starting = $state(false);
  const current = $derived(sources?.find((s) => s.id === openId));

  // ---- the database copy form (its own component) ----
  let dbf = $state<ReturnType<typeof MigrateDbForm>>();
  let dbReady = $state(false);

  // ---- several at once ----
  let pickedIds = $state<string[]>([]);
  let batchRunning = $state(false);

  // ---- the running job ----
  type Track = {
    id: string;
    kind: "bring" | "revert" | "cleanup";
    label: string;
    program: MigrateProgram;
    mode: MigrateMode;
    migrationId: string;
  };
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

  const groups = $derived(sources ? groupSources(sources) : []);
  const busy = $derived(!!track || batchRunning);
  const sum = $derived(sources && history ? summary(sources, history) : null);
  const rec = $derived(sources ? recommended(sources, history) : []);
  const allRecPicked = $derived(rec.length > 0 && rec.every((r) => pickedIds.includes(r.source.id)));

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
    // Ticks of programs that are not ready any more (just imported, a blocker appeared) fall away.
    const list = sources;
    if (list) pickedIds = pickedIds.filter((id) => list.some((s) => s.id === id && selectable(s, history)));
    checking = false;
  }

  // First load, then open what a `?source=` or `?tab=` link names.
  let preselected = "";
  async function applyHash() {
    if (hashParam("tab") === "history") tab = "history";
    const want = hashParam("source");
    if (!want || !sources || want === preselected) return;
    preselected = want;
    tab = "programs";
    const id = preselect(sources, want);
    if (!id) {
      planError = "";
      result = { tone: "bad", title: "Not listed.", text: "The program that link names is not on the list any more. It may be adopted already, or no longer registered." };
      return;
    }
    openSource(id);
    await tick();
    document.querySelector<HTMLElement>(`[data-source="${CSS.escape(id)}"]`)?.scrollIntoView({ block: "nearest", behavior: "smooth" });
    document.getElementById(`plan-h-${id}`)?.focus({ preventScroll: true });
  }
  void reload().then(applyHash);
  const onHash = () => void applyHash();
  window.addEventListener("hashchange", onHash);
  onDestroy(() => {
    window.removeEventListener("hashchange", onHash);
    mq.removeEventListener("change", onMq);
    stopPoll();
  });

  function openSource(id: string) {
    const s = sources?.find((x) => x.id === id);
    if (!s) return;
    openId = id;
    mode = defaultMode(s);
    checked = defaultChecked(s);
    planError = "";
    dbReady = false;
  }
  function toggleSource(id: string) {
    if (openId === id) openId = "";
    else openSource(id);
  }
  function pick(id: string, on: boolean) {
    pickedIds = on ? [...pickedIds, id] : pickedIds.filter((x) => x !== id);
  }
  function selectRecommended() {
    pickedIds = allRecPicked ? [] : rec.map((r) => r.source.id);
    openId = "";
  }

  // ---- review and start ----
  const plannedItems = $derived(current ? pickedItems(current, checked) : []);
  const isDb = $derived(current?.program === "database");
  const free = $derived(freeSpace(current ?? { targetFreeBytes: 0 }, isDb ? 0 : totalSize(plannedItems)));
  const blocked = $derived(!!current && current.blockers.length > 0);
  const active = $derived(current && !isDb ? activeMigration(history, current.id) : undefined);
  const canReview = $derived(
    !!current && !blocked && !active && !busy && free.state !== "short" && (isDb ? dbReady : plannedItems.length > 0),
  );

  async function review() {
    if (!current || !canReview) return;
    planError = "";
    if (isDb && !(await dbf?.validate())) return;
    reviewing = true;
  }
  const reviewSentence = $derived(current ? reviewText(current, mode, plannedItems, isDb && reviewing ? dbf?.review() : undefined) : "");
  const pickedDbs = $derived(isDb && reviewing ? (dbf?.review().databases ?? []) : []);

  async function start() {
    const s = current;
    if (!s) return;
    const req: MigrateRequest = { source: s.id, mode, items: isDb ? [] : requestItems(s, checked) };
    if (isDb && dbf) req.database = dbf.request();
    starting = true;
    try {
      const job = await api.startMigration(req);
      reviewing = false;
      openId = "";
      result = null;
      begin(job, { kind: "bring", label: s.label, program: s.program, mode });
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
  function begin(job: Job, t: Omit<Track, "id" | "migrationId"> & { migrationId?: string }) {
    polled = job;
    track = { ...t, id: job.id, migrationId: t.migrationId ?? job.migrationId ?? "" };
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
      result =
        t.kind === "revert"
          ? { tone: "ok", title: "Reverted.", text: `${t.label} is back as it was before it was imported.` }
          : t.kind === "cleanup"
            ? { tone: "ok", title: "Old files removed.", text: `The old ${t.label} files are gone and Zero2Dev's copy is the one in use. Revert in History can put them back.` }
            : { tone: "ok", title: "Done.", text: m?.description || `${t.label} is in Zero2Dev now.`, next: nextStep(t.program) };
    } else {
      const what = t.kind === "revert" ? "reverted" : t.kind === "cleanup" ? "cleaned up" : "imported";
      result = {
        tone: "bad",
        title: `${t.label} was not ${what}.`,
        text: `${j.error || "Unknown error."} ${t.kind === "cleanup" ? "Nothing was removed." : "What was there before is kept; see History."}`,
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

  async function batchFinished() {
    await reload();
    invalidateBringIn();
    void loadSettings();
  }

  const reviewTitle = (s: MigrateSource, md: MigrateMode) =>
    s.program === "runtime" ? `Adopt ${s.label}?` : s.program === "database" ? "Copy these databases?" : `${md === "move" ? "Move" : "Copy"} ${s.label} into Zero2Dev?`;
  const openLabel = (s: MigrateSource) => (s.program === "runtime" ? "Adopt…" : s.program === "database" ? "Copy databases…" : "Import…");

  // Tabs: arrow keys move between them.
  function tabKey(e: KeyboardEvent) {
    if (e.key !== "ArrowLeft" && e.key !== "ArrowRight") return;
    tab = tab === "programs" ? "history" : "programs";
    void tick().then(() => document.getElementById(`tab-${tab}`)?.focus());
  }
  // One primary button per view: the open plan, else the batch bar, else Select recommended.
  const primaryPlan = $derived(!!current);
  const primaryBatch = $derived(!current && pickedIds.length > 0);
  const primaryRec = $derived(!current && pickedIds.length === 0);
</script>

{#snippet dbformSnip()}
  <MigrateDbForm bind:this={dbf} bind:ready={dbReady} />
{/snippet}

{#snippet planOf(s: MigrateSource)}
  <MigratePlan
    {s}
    active={activeMigration(history, s.id)}
    bind:mode
    bind:checked
    {free}
    {planError}
    {canReview}
    {busy}
    {checking}
    primary={primaryPlan}
    onreview={review}
    onclose={() => (openId = "")}
    onrecheck={() => void reload()}
    dbform={s.program === "database" ? dbformSnip : undefined}
  />
{/snippet}

<PageHead sub="Import the settings and data of programs installed on this PC into Zero2Dev's portable folder. Every step can be reverted.">
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
    <div class="card card-pad prog" role="group" aria-label="{track.kind === 'revert' ? 'Reverting' : track.kind === 'cleanup' ? 'Removing the old files of' : 'Importing'} {track.label}" data-testid="migrate-progress">
      <div class="row"><span class="spin" aria-hidden="true"></span><b class="grow">{track.kind === "revert" ? "Reverting" : track.kind === "cleanup" ? "Removing the old files of" : track.program === "runtime" ? "Adopting" : track.mode === "move" ? "Moving" : "Copying"} {track.label}</b><span class="mono muted">{Math.round(live.percent)}%</span></div>
      <div class="progress" role="progressbar" aria-valuemin="0" aria-valuemax="100" aria-valuenow={Math.round(live.percent)} aria-label="Progress"><span style:width="{live.percent}%"></span></div>
      <span class="small muted">{live.step || "Starting"}</span>
    </div>
  {/if}

  <div class="tabs" role="tablist" aria-label="Import from this PC" tabindex="-1" onkeydown={tabKey}>
    <button id="tab-programs" class="tab" role="tab" type="button" aria-selected={tab === "programs"} aria-controls="panel-programs" tabindex={tab === "programs" ? 0 : -1} onclick={() => (tab = "programs")} data-testid="migrate-tab-programs">Programs</button>
    <button id="tab-history" class="tab" role="tab" type="button" aria-selected={tab === "history"} aria-controls="panel-history" tabindex={tab === "history" ? 0 : -1} onclick={() => (tab = "history")} data-testid="migrate-tab-history">History{history ? ` (${history.length})` : ""}</button>
  </div>

  {#if tab === "history"}
    <div id="panel-history" role="tabpanel" aria-labelledby="tab-history">
      <MigrateHistory
        {history}
        error={historyError}
        {busy}
        onjob={(job, t) => begin(job, t)}
        onchanged={() => void reload().then(invalidateBringIn)}
      />
    </div>
  {:else}
    <div id="panel-programs" class="stack" role="tabpanel" aria-labelledby="tab-programs">
      {#if loadError}
        <div class="notice bad" role="alert" data-testid="migrate-load-error">
          <Icon name="alert" /><span class="grow">Could not look for programs: {loadError}</span>
          <button class="btn sm" type="button" onclick={() => void reload()}>Try again</button>
        </div>
      {:else if !sources || !sum}
        <p class="muted"><span class="spin" aria-hidden="true"></span> Looking for programs on this PC...</p>
      {:else}
        <MigrateSummary
          {sum}
          {rec}
          allPicked={allRecPicked}
          primary={primaryRec}
          onselect={selectRecommended}
          onbackups={() => (tab = "history")}
        />
        <div class="cols" class:wide>
          <div class="list stack">
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
                        <MigrateSourceRow
                          {s}
                          status={sourceStatus(s, history)}
                          open={openId === s.id}
                          pickable={selectable(s, history)}
                          picked={pickedIds.includes(s.id)}
                          onopen={() => toggleSource(s.id)}
                          onpick={(on) => pick(s.id, on)}
                          openLabel={openLabel(s)}
                        >
                          {#if openId === s.id && !wide}<div class="inline">{@render planOf(s)}</div>{/if}
                        </MigrateSourceRow>
                      {/each}
                    </ul>
                  {/if}
                  {#if g.missing.length}
                    <p class="hint" data-testid="migrate-missing">Not found on this PC: {g.missing.map((s) => s.label).join(", ")}.</p>
                  {/if}
                </div>
              </section>
            {/each}
          </div>
          {#if wide}
            <aside class="side" aria-label="Plan">
              {#if current}
                {@render planOf(current)}
              {:else}
                <div class="empty card card-pad" data-testid="migrate-empty-plan">
                  <Icon name="download" size={20} />
                  <b>Pick a program</b>
                  <p class="muted">Its plan opens here: what comes in, whether it is worth it, copy or move, and how much room it needs. Nothing starts before you review it.</p>
                </div>
              {/if}
            </aside>
          {/if}
        </div>
        <MigrateBatch
          {sources}
          {history}
          picked={pickedIds}
          primary={primaryBatch}
          bind:running={batchRunning}
          onclear={() => (pickedIds = [])}
          onfinished={() => void batchFinished()}
        />
      {/if}
    </div>
  {/if}
</div>

{#if reviewing && current}
  <Modal title={reviewTitle(current, mode)} onclose={() => (reviewing = false)}>
    <div class="dlg-body review" data-testid="migrate-review-dialog">
      <p data-testid="migrate-review-text">{reviewSentence}</p>
      {#if isDb}
        <ul class="plainlist">{#each pickedDbs as d (d.name)}<li><code>{d.name}</code>{#if d.sizeBytes} <span class="muted small">{fmtSize(d.sizeBytes)}</span>{/if}</li>{/each}</ul>
      {:else}
        <ul class="plainlist">
          {#each plannedItems as it (it.label)}
            <li><b>{it.label}</b> <span class="muted small">{fmtSize(it.sizeBytes)}</span><br /><span class="fromto small"><code>{it.from}</code> <Icon name="arrowRight" size={12} /><span class="sr-only">to</span> <code>{it.to}</code></span></li>
          {/each}
        </ul>
      {/if}
      {#if targetText(current)}<p class="muted">{targetText(current)}</p>{/if}
      {#if mode === "copy" && !isDb && current.program !== "runtime"}<p class="muted">The old files stay. You can remove them later from History, after you have checked Zero2Dev's copy.</p>{/if}
      <p class="muted">You can revert this later in History.</p>
    </div>
    {#snippet footer()}
      <button class="btn" type="button" onclick={() => (reviewing = false)}>Cancel</button>
      <button class="btn primary" type="button" disabled={starting} onclick={start} data-testid="migrate-start">{startLabel(mode, current!)}</button>
    {/snippet}
  </Modal>
{/if}

<style>
  .page {
    max-width: 1200px;
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
  .tabs {
    display: flex;
    gap: 4px;
    border-bottom: 1px solid var(--line);
  }
  .tab {
    all: unset;
    cursor: pointer;
    padding: 8px 16px;
    margin-bottom: -1px;
    border-bottom: 2px solid transparent;
    color: var(--muted);
    font-weight: 500;
  }
  .tab:hover {
    color: var(--text);
  }
  .tab[aria-selected="true"] {
    color: var(--accent-text);
    border-bottom-color: var(--accent);
  }
  .tab:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: -2px;
    border-radius: var(--radius-sm);
  }
  .cols {
    display: grid;
    gap: 16px;
    min-width: 0;
  }
  .cols.wide {
    grid-template-columns: minmax(0, 1fr) 420px;
    align-items: start;
  }
  .list {
    min-width: 0;
  }
  .side {
    position: sticky;
    top: calc(16px + env(safe-area-inset-top, 0px));
    min-width: 0;
    max-height: calc(100vh - 32px);
    overflow-y: auto;
  }
  .empty {
    display: grid;
    gap: 8px;
    justify-items: start;
    color: var(--muted);
  }
  .empty p {
    margin: 0;
  }
  .sources,
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
  .inline {
    margin-top: 12px;
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
  .review p {
    margin: 0 0 12px;
  }
</style>
