<script lang="ts">
  import {
    UNVERIFIED_MESSAGE,
    isLaunchable,
    availability,
    canMakeDefault,
    CHECK_TEXT,
    defaultSelection,
    isEntryInstalled,
    kindLabel,
    KIND_SUMMARY,
    noDownloadText,
    optionGroups,
    optionLabel,
    runtimesOfKind,
    runtimeTitle,
    sourceLabel,
    supportedForKind,
    updatesAvailable,
  } from "../lib/catalog";
  import { api } from "../lib/api";
  import { siteServer } from "../lib/sites";
  import { fmtDateTime } from "../lib/format";
  import {
    app,
    attempt,
    installJobs,
    jobs,
    lists,
    loadRuntimes,
    loadStatus,
    makeDefaultRuntime,
    tryInstall,
    toast,
  } from "../lib/store.svelte";
  import type { CatalogEntry, Runtime, RuntimeKind } from "../lib/types";
  import ConfirmDialog from "../ui/ConfirmDialog.svelte";
  import Icon from "../ui/Icon.svelte";
  import KindIcon from "../ui/KindIcon.svelte";
  import InstallFailure from "../ui/InstallFailure.svelte";
  import Modal from "../ui/Modal.svelte";

  let {
    kind,
    onimport,
    layout = "list",
  }: { kind: RuntimeKind; onimport: (entry: CatalogEntry) => void; layout?: "list" | "grid" } = $props();

  const label = $derived(kindLabel(kind));
  const installed = $derived(runtimesOfKind(lists.runtimes, kind));
  const entries = $derived(supportedForKind(lists.catalog, kind));
  const avail = $derived(availability(lists.catalog, kind));
  const groups = $derived(optionGroups(entries, kind));
  const updates = $derived(updatesAvailable(lists.catalog, installed));
  const selectId = $derived(`pick-${kind}`);

  let selectedId = $state("");
  // Until the user picks something, follow the newest version that is not installed yet.
  let touched = $state(false);
  $effect(() => {
    const next = defaultSelection(entries, lists.runtimes, touched ? selectedId : undefined);
    if (next !== selectedId) selectedId = next;
  });
  const selected = $derived(entries.find((e) => e.id === selectedId));
  const isInstalled = $derived(!!selected && isEntryInstalled(selected, lists.runtimes));

  const jobOf = (e: CatalogEntry) => (installJobs[e.id] ? jobs[installJobs[e.id]] : undefined);
  const runningEntry = $derived(entries.find((e) => jobOf(e)?.state === "running"));
  const runningJob = $derived(runningEntry ? jobOf(runningEntry) : undefined);
  const failedJob = $derived(selected ? jobOf(selected) : undefined);
  const failed = $derived(failedJob?.state === "failed");
  const serves = $derived((kind === "caddy" || kind === "nginx") && siteServer(app.settings) === kind);

  // Every runtime is one line (its versions are chips in it) until it is opened with Manage or
  // Install. An install in progress or a failed one opens it, so the details are never hidden.
  let userOpen = $state<boolean | null>(null);
  const open = $derived(layout === "grid" ? userOpen === true : (userOpen ?? (!!runningJob || failed)));
  const bodyId = $derived(`rt-body-${kind}`);

  // ---- install: unverified confirm -> request ----
  let confirmUnverified = $state(false);
  let busy = $state(false);

  function begin() {
    if (!selected) return;
    if (selected.unverified) confirmUnverified = true;
    else void run(false);
  }
  async function run(accept: boolean) {
    const target = selected;
    if (!target) return;
    confirmUnverified = false;
    busy = true;
    const err = await tryInstall(target, { acceptUnverified: accept });
    busy = false;
    if (!err) return;
    if (err.code === "unverified" && !accept) confirmUnverified = true;
    else toast("error", err.message);
  }

  // ---- open (editors and database tools are opened, not run as services) ----
  const launchable = $derived(isLaunchable(kind));
  async function openTool(r: Runtime) {
    await attempt(() => api.launchRuntime(r.id), `${runtimeTitle(r)} is opening.`);
  }

  // ---- remove ----
  let removing = $state<Runtime | null>(null);
  let removeBusy = $state(false);
  async function confirmRemove() {
    const r = removing;
    if (!r) return;
    removeBusy = true;
    // A 409 names the service that still uses it; attempt() shows that message as an error toast.
    const ok = await attempt(async () => {
      await api.deleteRuntime(r.id);
      return true;
    }, `${r.name} removed.`);
    removeBusy = false;
    removing = null;
    await loadRuntimes();
    if (ok) void loadStatus();
  }
</script>

<article class="rt" class:open={open && layout === "list"} class:tile={layout === "grid"} data-kind={kind} aria-labelledby="k-{kind}">
  <div class="head">
    <KindIcon {kind} size={26} />
    <!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions (the button beside it is the keyboard control) -->
    <div class="name" onclick={(e) => { if (!(e.target as HTMLElement).closest("a")) userOpen = !open; }}>
      <h3 id="k-{kind}" class="kind-title">{label}
        {#if serves}<a class="badge accent serves" href="#/settings" title="Change the site server in Settings" data-testid="serves-sites">Serves your sites</a>{/if}
      </h3>
      <span class="muted small">{KIND_SUMMARY[kind] ?? ""}</span>
    </div>
    <div class="versions" data-testid="count-{kind}">
      {#if runningJob && runningEntry}
        <span class="mini" role="status" aria-label="Installing {runningEntry.name}: {Math.round(runningJob.percent)}%">
          <span class="spin" aria-hidden="true"></span> Installing {runningEntry.version}… {Math.round(runningJob.percent)}%
        </span>
      {:else if installed.length}
        <span class="sr-only">{installed.length} installed:</span>
        {#each installed as r (r.id)}
          <span class="vchip" class:def={r.default} title={r.default ? "The default version" : sourceLabel(r)}>{r.version || "unknown"}{#if r.default}<span class="tag">default</span>{/if}</span>
        {/each}
        {#if updates.length}<span class="badge accent upd" title="Update available">Update {updates[0].version}</span>{/if}
      {:else if failed}
        <span class="badge failed"><span class="dot"></span>Install failed</span>
      {:else}
        <span class="muted small not">Not installed</span>
      {/if}
    </div>
    <button type="button" class="btn sm toggle" data-tour="rt-toggle-{kind}" aria-expanded={open} aria-controls={bodyId} aria-label="{open && layout === 'list' ? 'Close' : installed.length ? 'Manage' : 'Install'} {label}"
      onclick={() => (userOpen = !open)}>
      {#if open && layout === "list"}Close{:else if installed.length}Manage{:else}<Icon name="download" size={14} /> Install…{/if}
    </button>
  </div>

  {#if open && layout === "list"}{@render details()}{/if}
</article>

{#if open && layout === "grid"}
  <Modal title={label} onclose={() => (userOpen = false)}>
    <div class="dlg-body rt-dlg" data-kind-dialog={kind}>
      <div class="dlg-sum">
        <KindIcon {kind} size={30} />
        <div class="grow">
          <div class="muted">{KIND_SUMMARY[kind] ?? ""}</div>
          {#if serves}<a class="badge accent serves" href="#/settings">Serves your sites</a>{/if}
        </div>
      </div>
      {@render details()}
    </div>
    {#snippet footer()}
      <button class="btn" type="button" onclick={() => (userOpen = false)}>Close</button>
    {/snippet}
  </Modal>
{/if}

{#snippet details()}
    <div class="body" id={bodyId}>
      {#if installed.length}
        <div class="col">
          <h4>Installed</h4>
          <ul class="list" aria-label="Installed {label} versions">
            {#each installed as r (r.id)}
              <li data-runtime={r.id} data-version={r.version}>
                <div class="info">
                  <span class="mono ver">{r.version || "unknown"}</span>
                  {#if r.default}<span class="badge accent" data-testid="default-badge">Default</span>{/if}
                  <span class="muted small" title={r.installedAt ? fmtDateTime(r.installedAt) : r.binary}>{sourceLabel(r)}</span>
                </div>
                <div class="acts">
                  {#if launchable}
                    <button class="btn sm" type="button" onclick={() => openTool(r)} aria-label="Open {runtimeTitle(r)}"><Icon name="external" size={14} /> Open</button>
                  {/if}
                  {#if canMakeDefault(r, lists.runtimes)}
                    <button class="btn sm" type="button" onclick={() => makeDefaultRuntime(r.id)} aria-label="Make {runtimeTitle(r)} the default">Make default</button>
                  {/if}
                  <button class="btn sm ghost danger icon-text" type="button" onclick={() => (removing = r)} aria-label="Remove {runtimeTitle(r)}" title={r.source === "installed" ? "Delete this version's folder" : "Forget this program (it stays on disk)"}><Icon name="trash" size={14} /> {r.source === "installed" ? "Remove" : "Forget"}</button>
                </div>
              </li>
            {/each}
          </ul>
          {#if installed.length > 1}<p class="hint">Services use the default unless they pick a version.</p>{/if}
        </div>
      {/if}

      <div class="col">
        <h4>{installed.length ? "Install another version" : "Install"}</h4>
        {#if avail === "check"}
          <p class="muted small">{CHECK_TEXT}</p>
        {:else if avail === "unsupported"}
          <p class="muted small">{noDownloadText(app.status?.os ?? "this", app.status?.arch ?? "PC")}</p>
        {:else}
          {#each updates as u (u.id)}
            <div class="notice update" data-testid="update-{kind}">
              <Icon name="download" />
              <span class="grow">Update available: <b>{u.version}</b></span>
              <button class="btn sm" type="button" onclick={() => { selectedId = u.id; touched = true; }}>Select {u.version}</button>
            </div>
          {/each}
          <div class="pick">
            <label for={selectId} class="sr-only">{installed.length ? "Install another version" : "Install a version"}</label>
            <select id={selectId} bind:value={selectedId} onchange={() => (touched = true)} disabled={!!runningJob}>
              {#each groups as g (g.label || "all")}
                {#if g.label}
                  <optgroup label={g.label}>
                    {#each g.entries as e (e.id)}<option value={e.id}>{optionLabel(e, lists.runtimes)}</option>{/each}
                  </optgroup>
                {:else}
                  {#each g.entries as e (e.id)}<option value={e.id}>{optionLabel(e, lists.runtimes)}</option>{/each}
                {/if}
              {/each}
            </select>
            {#if !runningJob}
              {#if isInstalled}
                <button class="btn" type="button" disabled>Installed</button>
              {:else}
                <button class="btn primary" type="button" disabled={busy || !selected} onclick={begin} data-tour="rt-install"><Icon name="download" size={14} /> Install</button>
              {/if}
            {/if}
          </div>

          {#if runningJob && runningEntry}
            <div class="prog" role="group" aria-label="Installing {runningEntry.name}">
              <div class="progress" role="progressbar" aria-valuemin="0" aria-valuemax="100" aria-valuenow={Math.round(runningJob.percent)} aria-label="{runningEntry.name} install progress"><span style:width="{runningJob.percent}%"></span></div>
              <div class="small"><span>{runningEntry.version}: {runningJob.step || "Starting"}</span> <span class="mono muted">{Math.round(runningJob.percent)}%</span></div>
            </div>
          {:else if selected && !isInstalled}
            <p class="hint">
              {#if selected.unverified}The publisher lists no checksum for this download, so it cannot be verified. You will be asked to confirm.
              {:else if selected.checksum?.type === "md5"}<span data-testid="md5-hint">Checked against the MD5 the publisher lists. That catches damaged or swapped downloads, but it is weaker than SHA-256.</span>
              {:else}Downloaded from the publisher and checked against its checksum.{/if}
            </p>
            <p class="hint">Download blocked, or already have the file? <button class="linkish" type="button" onclick={() => onimport(selected)} aria-label="Import file for {label} {selected.version}">Import it</button>.</p>
          {/if}

          {#if failed && failedJob && selected}
            <InstallFailure entry={selected} job={failedJob} onimport={() => onimport(selected)} />
          {/if}
        {/if}
      </div>
    </div>
{/snippet}

{#if confirmUnverified && selected}
  <ConfirmDialog title="Install {label} {selected.version} without a checksum?" message={UNVERIFIED_MESSAGE} confirmLabel="Install anyway" danger={false} {busy} onconfirm={() => run(true)} oncancel={() => (confirmUnverified = false)} />
{/if}

{#if removing}
  <ConfirmDialog
    title="{removing.source === 'installed' ? 'Remove' : 'Forget'} {runtimeTitle(removing)}?"
    message={removing.source === "installed"
      ? "Zero2Dev installed this runtime, so its folder will be deleted. A service that uses it must be changed first."
      : "This only removes the entry from Zero2Dev. The program on disk is not touched."}
    confirmLabel={removing.source === "installed" ? "Remove runtime" : "Forget runtime"}
    busy={removeBusy}
    onconfirm={confirmRemove}
    oncancel={() => (removing = null)}
  />
{/if}

<style>
  .rt {
    background: var(--surface);
    border-top: 1px solid var(--line);
    min-width: 0;
  }
  .rt:first-child {
    border-top: 0;
  }
  .head {
    display: grid;
    grid-template-columns: 26px minmax(160px, 1.1fr) minmax(0, 1.4fr) auto;
    align-items: center;
    gap: 14px;
    padding: 12px 16px;
  }
  .name {
    display: grid;
    gap: 1px;
    min-width: 0;
    cursor: pointer;
  }
  .name h3 {
    display: flex;
    align-items: center;
    gap: 8px;
    font-size: 14px;
    margin: 0;
  }
  .serves {
    text-decoration: none;
  }
  .versions {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 6px;
    min-width: 0;
  }
  .vchip {
    font-family: var(--mono);
    font-size: 12px;
    line-height: 20px;
    padding: 0 7px;
    border-radius: var(--radius-sm);
    border: 1px solid var(--line-strong);
    background: var(--surface-2);
  }
  .vchip.def {
    border-color: var(--accent-border);
    color: var(--accent-text);
    background: var(--accent-soft);
  }
  .tag {
    font-family: var(--sans);
    font-size: 10.5px;
    font-weight: 600;
    margin-left: 6px;
  }
  .upd {
    font-size: 11.5px;
  }
  .mini {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    font-size: 12px;
    color: var(--accent-text);
  }
  .toggle {
    min-width: 92px;
    justify-content: center;
  }
  .body {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(300px, 1fr));
    gap: 14px 28px;
    padding: 4px 16px 16px 56px;
  }
  .col {
    display: grid;
    gap: 8px;
    align-content: start;
    min-width: 0;
  }
  h4 {
    margin: 0;
    font-size: 12px;
    font-weight: 600;
    color: var(--muted);
    text-transform: none;
  }
  .list {
    list-style: none;
    margin: 0;
    padding: 0;
    display: grid;
    border: 1px solid var(--line);
    border-radius: var(--radius);
  }
  .list li {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
    flex-wrap: wrap;
    padding: 6px 6px 6px 12px;
    border-top: 1px solid var(--line);
  }
  .list li:first-child {
    border-top: 0;
  }
  .info,
  .acts {
    display: flex;
    align-items: center;
    gap: 8px;
    flex-wrap: wrap;
  }
  .pick {
    display: flex;
    gap: 8px;
    max-width: 560px;
  }
  .pick select {
    flex: 1;
    min-width: 0;
  }
  .update {
    align-items: center;
    padding: 8px 10px;
  }
  .prog {
    display: grid;
    gap: 4px;
  }
  .prog .small {
    display: flex;
    justify-content: space-between;
  }
  .hint {
    margin: 0;
  }
  .body {
    border-top: 1px dashed var(--line);
    padding-top: 14px;
    background: var(--surface-2);
  }
  /* ---- grid view: a closed runtime is a tile; an open one spans the whole row ---- */
  .rt.tile {
    border: 1px solid var(--line);
    border-radius: 8px;
    display: flex;
    flex-direction: column;
  }
  .rt.tile:hover {
    border-color: var(--line-strong);
  }
  .tile .head {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: 8px;
    flex: 1;
    padding: 14px;
  }
  .tile .head > :global(svg) {
    width: 32px;
    height: 32px;
  }
  .tile .versions {
    flex: 1;
    align-content: flex-start;
  }
  .tile .toggle {
    width: 100%;
    margin-top: auto;
  }
  .rt.tile:has(.mini) {
    border-color: var(--accent);
  }
  .rt-dlg {
    display: grid;
    gap: 14px;
  }
  .dlg-sum {
    display: flex;
    align-items: center;
    gap: 12px;
  }
  .dlg-sum .grow {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 6px 10px;
  }
  .rt-dlg .body {
    padding: 0;
    border: 0;
    background: none;
    grid-template-columns: 1fr;
  }
  @media (max-width: 760px) {
    .head {
      grid-template-columns: 26px minmax(0, 1fr) auto;
    }
    .versions {
      grid-column: 2 / -1;
      grid-row: 2;
    }
    .versions:has(.not) {
      display: none; /* the Install button already says it */
    }
    .body {
      padding-left: 16px;
      grid-template-columns: 1fr;
    }
  }
</style>
