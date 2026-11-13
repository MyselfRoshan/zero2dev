<script lang="ts">
  import { api, ApiError } from "../lib/api";
  import { ago, fmtDateTime } from "../lib/format";
  import { onMount } from "svelte";
  import {
    app,
    attempt,
    catalogInfo,
    initCatalogStatus,
    installJobs,
    installRuntime,
    jobs,
    lists,
    loadRuntimes,
    loadStatus,
    refreshCatalog,
    toast,
  } from "../lib/store.svelte";
  import { CARD_KINDS, filterKinds, kindLabel, RUNTIME_GROUPS, sourceErrors, type RuntimeFilter } from "../lib/catalog";
  import { getRuntimeView, setRuntimeView, type RuntimeView } from "../lib/ux";
  import { hashParam } from "../lib/router.svelte";
  import RuntimeKindCard from "./RuntimeKindCard.svelte";
  import { RUNTIME_KINDS, type CatalogEntry, type DetectedRuntime, type Runtime, type RuntimeKind } from "../lib/types";
  import { hasErrors, validateRegister, type RegisterField } from "../lib/validate";
  import Icon from "../ui/Icon.svelte";
  import KindIcon from "../ui/KindIcon.svelte";
  import Modal from "../ui/Modal.svelte";
  import ImportDialog from "./ImportDialog.svelte";
  import PageHead from "../ui/PageHead.svelte";

  let importing = $state<CatalogEntry | null>(null);
  let detected = $state<DetectedRuntime[] | null>(null);
  let detecting = $state(false);
  let registering = $state(false);

  onMount(() => {
    void initCatalogStatus();
  });
  const sourceProblems = $derived(sourceErrors(catalogInfo.status?.sources ?? []));
  // A card per kind, plus one for runtimes of an unknown kind if any were registered.
  const hasOther = $derived(lists.runtimes.some((r) => !(CARD_KINDS as string[]).includes(r.kind)));

  // Filters: everything, what is installed, or one group.
  const FILTER_IDS: string[] = ["all", "installed", ...RUNTIME_GROUPS.map((g) => g.id)];
  // ?filter=installed (links from the dashboard) picks the filter.
  let filter = $state<RuntimeFilter>(
    FILTER_IDS.includes(hashParam("filter")) ? (hashParam("filter") as RuntimeFilter) : "all",
  );
  let view = $state<RuntimeView>(getRuntimeView());
  function pickView(v: RuntimeView) {
    view = v;
    setRuntimeView(v);
  }
  const shown = $derived(new Set<string>(filterKinds(filter, lists.runtimes)));
  const installedKinds = $derived(filterKinds("installed", lists.runtimes).length);
  const FILTERS = $derived<{ id: RuntimeFilter; label: string; n?: number }[]>([
    { id: "all", label: "All" },
    { id: "installed", label: "Installed", n: installedKinds },
    ...RUNTIME_GROUPS.map((g) => ({ id: g.id, label: g.title })),
  ]);
  const visibleGroups = $derived(RUNTIME_GROUPS.filter((g) => g.kinds.some((k) => shown.has(k))));
  let showSourceProblems = $state(false);

  async function registerAll() {
    for (const d of detected ?? []) if (!known.has(d.binary.toLowerCase())) await registerDetected(d);
  }
  const unregistered = $derived((detected ?? []).filter((d) => !known.has(d.binary.toLowerCase())).length);

  async function detect() {
    detecting = true;
    const r = await attempt(() => api.detectRuntimes());
    detecting = false;
    if (r) detected = r;
  }
  const known = $derived(new Set(lists.runtimes.map((r) => r.binary.toLowerCase())));
  const detectedLabel = (d: DetectedRuntime) => (d.name === "nginx" ? "nginx" : kindLabel(d.kind));
  async function registerDetected(d: DetectedRuntime) {
    const kind = (RUNTIME_KINDS as string[]).includes(d.kind) ? (d.kind as RuntimeKind) : "other";
    const r = await attempt(() => api.registerRuntime({ kind, binary: d.binary, version: d.version || undefined }), `${kind} ${d.version} registered.`);
    if (r) {
      await loadRuntimes();
      void loadStatus();
    }
  }

  // register form
  let rKind = $state<RuntimeKind>("node");
  let rBinary = $state("");
  let rVersion = $state("");
  let rErr = $state<Partial<Record<RegisterField, string>>>({});
  let rForm = $state("");
  let rBusy = $state(false);
  async function submitRegister(e: SubmitEvent) {
    e.preventDefault();
    rForm = "";
    rErr = validateRegister({ kind: rKind, binary: rBinary });
    if (hasErrors(rErr)) return;
    rBusy = true;
    try {
      const r = await api.registerRuntime({ kind: rKind, binary: rBinary.trim(), version: rVersion.trim() || undefined });
      toast("success", `${r.name} registered.`);
      await loadRuntimes();
      void loadStatus();
      registering = false;
      rBinary = "";
      rVersion = "";
    } catch (err) {
      rForm = err instanceof ApiError ? err.message : String(err);
    } finally {
      rBusy = false;
    }
  }

</script>

<PageHead sub="The programs your services run on. Install as many versions as you like; each type has one default.">
  <button class="btn" onclick={detect} disabled={detecting}><Icon name="search" /> {detecting ? "Detecting" : "Detect on this PC"}</button>
  <button class="btn" onclick={() => (registering = true)}><Icon name="plus" /> Register by path</button>
</PageHead>

{#if detected}
  <section class="card detect" aria-labelledby="det-h">
    <div class="panel-head">
      <h2 id="det-h">Found on this PC</h2>
      <span class="muted small">{detected.length ? `${detected.length} program${detected.length === 1 ? "" : "s"} on PATH` : ""}</span>
      <span class="spacer"></span>
      {#if unregistered > 1}<button class="btn sm" onclick={registerAll}><Icon name="plus" size={14} /> Register all {unregistered}</button>{/if}
      <button class="btn sm ghost icon" onclick={() => (detected = null)} aria-label="Dismiss"><Icon name="x" size={14} /></button>
    </div>
    {#if detected.length === 0}
      <p class="card-pad muted">No known programs were found on PATH. If one lives elsewhere, use <b>Register by path</b>.</p>
    {:else}
      <div class="t-wrap">
        <table class="t">
          <tbody>
            {#each detected as d (d.binary)}
              <tr>
                <td><span class="kind-cell"><KindIcon kind={d.name === "nginx" ? "nginx" : d.kind} size={16} /> {detectedLabel(d)}</span></td>
                <td class="mono">{d.version}</td>
                <td class="path">{d.binary}</td>
                <td class="actions">
                  {#if known.has(d.binary.toLowerCase())}
                    <span class="badge running"><span class="dot"></span>Registered</span>
                  {:else}
                    <button class="btn sm" onclick={() => registerDetected(d)} aria-label="Register {detectedLabel(d)} {d.version}"><Icon name="plus" size={14} /> Register</button>
                  {/if}
                </td>
              </tr>
            {/each}
          </tbody>
        </table>
      </div>
      <p class="card-pad small muted foot">Registering only remembers the path; nothing is copied or changed.</p>
    {/if}
  </section>
{/if}

<div class="toolbar">
  <div class="filters" role="group" aria-label="Show">
    {#each FILTERS as f (f.id)}
      <button type="button" class="chip" class:on={filter === f.id} aria-pressed={filter === f.id} onclick={() => (filter = f.id)}>
        {f.label}{#if f.n !== undefined}<span class="n">{f.n}</span>{/if}
      </button>
    {/each}
  </div>
  <div class="seg views" role="group" aria-label="Layout">
    <button type="button" class:on={view === "list"} aria-pressed={view === "list"} onclick={() => pickView("list")} title="List"><Icon name="list" size={15} /><span>List</span></button>
    <button type="button" class:on={view === "grid"} aria-pressed={view === "grid"} onclick={() => pickView("grid")} title="Grid"><Icon name="grid" size={15} /><span>Grid</span></button>
  </div>
  <div class="check-line">
    <span class="muted small">
      {#if catalogInfo.refreshing}Checking the publishers...{:else if catalogInfo.status?.checkedAt}<span title={fmtDateTime(catalogInfo.status.checkedAt)}>Version lists checked {ago(catalogInfo.status.checkedAt)}.</span>{:else}Version lists not checked yet.{/if}
    </span>
    <button class="btn sm" type="button" onclick={() => refreshCatalog()} disabled={catalogInfo.refreshing} aria-busy={catalogInfo.refreshing}>
      {#if catalogInfo.refreshing}<span class="spin" aria-hidden="true"></span> Checking...{:else}<Icon name="restart" size={14} /> Check for new versions{/if}
    </button>
  </div>
</div>

{#if sourceProblems.length}
  <div class="notice warn src" role="status">
    <Icon name="alert" />
    <div class="grow">
      <span>{sourceProblems.length === 1 ? "One version list" : `${sourceProblems.length} version lists`} could not be checked, so {sourceProblems.length === 1 ? "it shows" : "they show"} the versions known before. Check again later, or set a download proxy in <a href="#/settings">Settings</a>.</span>
      <ul class="src-errors">
        {#each sourceProblems as line (line)}<li>{line}</li>{/each}
      </ul>
    </div>
  </div>
{/if}

<section aria-label="Runtimes">
  {#each visibleGroups as g (g.id)}
    <section class="group" aria-labelledby="g-{g.id}" data-group={g.id}>
      <div class="group-head">
        <h2 id="g-{g.id}">{g.title}</h2>
        <span class="muted small">
          {g.text}
          {#if g.id === "servers"}<a href="#/settings">Choose which one in Settings.</a>{/if}
        </span>
      </div>
      <div class={view === "grid" ? "tiles" : "rows card"}>
        {#each g.kinds.filter((k) => shown.has(k)) as k (k)}<RuntimeKindCard kind={k} layout={view} onimport={(e) => (importing = e)} />{/each}
      </div>
    </section>
  {/each}
  {#if hasOther && (filter === "all" || filter === "installed")}
    <section class="group" aria-labelledby="g-other" data-group="other">
      <div class="group-head"><h2 id="g-other">Other</h2><span class="muted small">Programs you registered by path.</span></div>
      <div class={view === "grid" ? "tiles" : "rows card"}><RuntimeKindCard kind="other" layout={view} onimport={(e) => (importing = e)} /></div>
    </section>
  {/if}
  {#if visibleGroups.length === 0 && !(hasOther && filter === "installed")}
    <div class="card empty">
      <h2>Nothing installed yet</h2>
      <p class="muted">Pick a runtime under <button class="linkish" type="button" onclick={() => (filter = "all")}>All</button> and press Install, or use <b>Detect on this PC</b> to add what you already have.</p>
    </div>
  {/if}
</section>

{#if importing}
  <ImportDialog entry={importing} onclose={() => (importing = null)} />
{/if}

{#if registering}
  <Modal title="Register a runtime by path" onclose={() => (registering = false)}>
    <form id="reg-form" class="dlg-body stack" onsubmit={submitRegister} novalidate>
      {#if rForm}<div class="notice bad" role="alert"><Icon name="alert" /><span>{rForm}</span></div>{/if}
      <div class="field"><label>
        <span class="label">Kind</span>
        <select bind:value={rKind}>
          {#each RUNTIME_GROUPS as g (g.id)}
            <optgroup label={g.title}>{#each g.kinds as k (k)}<option value={k}>{kindLabel(k)}</option>{/each}</optgroup>
          {/each}
          <optgroup label="Other"><option value="other">Other program</option></optgroup>
        </select></label>
      </div>
      <div class="field"><label>
        <span class="label">Path to the program</span>
        <input type="text" class="mono" bind:value={rBinary} aria-invalid={!!rErr.binary} placeholder="C:\php\php.exe" spellcheck="false" /></label>
        {#if rErr.binary}<span class="err">{rErr.binary}</span>{:else}<span class="hint">Absolute path to the executable file. Zero2Dev only stores it; nothing is copied.</span>{/if}
      </div>
      <div class="field"><label>
        <span class="label">Version (optional)</span>
        <input type="text" class="mono" bind:value={rVersion} placeholder="Read from --version when empty" /></label>
      </div>
    </form>
    {#snippet footer()}
      <button class="btn" type="button" onclick={() => (registering = false)}>Cancel</button>
      <button class="btn primary" type="submit" form="reg-form" disabled={rBusy}>Register runtime</button>
    {/snippet}
  </Modal>
{/if}

<style>
  .detect {
    margin-bottom: 16px;
  }
  .detect .panel-head {
    gap: 10px;
  }
  .detect .foot {
    padding-top: 8px;
  }
  .toolbar {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    gap: 10px 16px;
    margin-bottom: 14px;
  }
  .filters {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
  }
  .chip {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    min-height: 32px;
    padding: 0 12px;
    border: 1px solid var(--line-strong);
    border-radius: var(--radius);
    background: var(--surface);
    color: var(--text);
    font: inherit;
    font-size: 14px;
    cursor: pointer;
  }
  .chip:hover {
    border-color: var(--accent);
    color: var(--accent-text);
  }
  .chip.on {
    background: var(--accent-solid);
    border-color: var(--accent-solid);
    color: var(--accent-ink);
  }
  .chip.on:hover {
    background: var(--accent-hover);
    border-color: var(--accent-hover);
    color: var(--accent-ink);
  }
  .chip:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 2px;
  }
  .chip .n {
    font-size: 12px;
    padding: 0 6px;
    border-radius: 100px;
    background: var(--sunken);
    color: var(--muted);
  }
  .chip.on .n {
    background: color-mix(in srgb, var(--accent-ink) 20%, transparent);
    color: inherit;
  }
  @media (max-width: 560px) {
    .filters {
      flex-wrap: nowrap;
      overflow-x: auto;
      width: 100%;
      padding-bottom: 2px;
    }
    .chip {
      flex: none;
    }
  }
  .check-line {
    display: flex;
    align-items: center;
    gap: 10px;
    flex-wrap: wrap;
  }
  .src {
    margin-bottom: 14px;
  }
  .src-errors {
    margin: 4px 0 0;
    padding-left: 18px;
    font-size: 12px;
  }
  .group {
    margin-bottom: 20px;
  }
  .group-head {
    display: flex;
    align-items: baseline;
    flex-wrap: wrap;
    gap: 2px 12px;
    margin-bottom: 8px;
  }
  .group-head h2 {
    font-size: 16px;
  }
  .rows {
    overflow: hidden;
  }
  .tiles {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(230px, 1fr));
    gap: 12px;
    align-items: stretch;
  }
  .views {
    margin-left: auto;
  }
  .views button {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    padding: 6px 12px;
    border: 0;
    border-right: 1px solid var(--line);
    background: none;
    color: var(--text);
    font: inherit;
    font-size: 14px;
    cursor: pointer;
  }
  .views button:last-child {
    border-right: 0;
  }
  .views button.on {
    background: var(--accent-soft);
    color: var(--accent-text);
    font-weight: 600;
  }
  .views button:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: -2px;
  }
  @media (max-width: 560px) {
    .views span {
      display: none;
    }
  }
  .toolbar .filters {
    flex: 1 1 auto;
  }
</style>
