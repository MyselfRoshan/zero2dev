<script lang="ts">
  import {
    UNVERIFIED_MESSAGE,
    availability,
    canMakeDefault,
    CHECK_TEXT,
    defaultSelection,
    isEntryInstalled,
    kindLabel,
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

  let { kind, onimport }: { kind: RuntimeKind; onimport: (entry: CatalogEntry) => void } = $props();

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

<article class="card kind" data-kind={kind} aria-labelledby="k-{kind}">
  <div class="head">
    <h3 id="k-{kind}" class="kind-title"><KindIcon {kind} size={22} /> {label}</h3>
    {#if (kind === "caddy" || kind === "nginx") && siteServer(app.settings) === kind}
      <a class="badge accent serves" href="#/settings" title="Change the site server in Settings" data-testid="serves-sites">Serves your sites</a>
    {/if}
    <span class="spacer"></span>
    <span class="badge plain" data-testid="count-{kind}">{installed.length ? `${installed.length} installed` : "Not installed"}</span>
  </div>

  {#if installed.length}
    <ul class="list" aria-label="Installed {label} versions">
      {#each installed as r (r.id)}
        <li data-runtime={r.id} data-version={r.version}>
          <div class="info">
            <span class="mono ver">{r.version || "unknown"}</span>
            {#if r.default}<span class="badge accent" data-testid="default-badge">Default</span>{/if}
            <span class="muted small" title={r.installedAt ? fmtDateTime(r.installedAt) : r.binary}>{sourceLabel(r)}</span>
          </div>
          <div class="acts">
            {#if canMakeDefault(r, lists.runtimes)}
              <button class="btn sm" type="button" onclick={() => makeDefaultRuntime(r.id)} aria-label="Make {runtimeTitle(r)} the default">Make default</button>
            {/if}
            <button class="btn sm ghost danger" type="button" onclick={() => (removing = r)} aria-label="Remove {runtimeTitle(r)}"><Icon name="trash" size={14} /> {r.source === "installed" ? "Remove" : "Forget"}</button>
          </div>
        </li>
      {/each}
    </ul>
  {/if}

  {#if avail === "check"}
    <p class="muted">{CHECK_TEXT}</p>
  {:else if avail === "unsupported"}
    <p class="muted">{noDownloadText(app.status?.os ?? "this", app.status?.arch ?? "PC")}</p>
  {:else}
    {#each updates as u (u.id)}
      <div class="notice update" data-testid="update-{kind}">
        <Icon name="download" />
        <span class="grow">Update available: <b>{u.version}</b></span>
        <button class="btn sm" type="button" onclick={() => { selectedId = u.id; touched = true; }}>Select {u.version}</button>
      </div>
    {/each}

    <div class="field">
      <label for={selectId}><span class="label">{installed.length ? "Install another version" : "Install a version"}</span></label>
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
    </div>

    {#if runningJob && runningEntry}
      <div class="prog" role="group" aria-label="Installing {runningEntry.name}">
        <div class="progress" role="progressbar" aria-valuemin="0" aria-valuemax="100" aria-valuenow={Math.round(runningJob.percent)} aria-label="{runningEntry.name} install progress"><span style:width="{runningJob.percent}%"></span></div>
        <div class="small"><span>{runningEntry.version}: {runningJob.step || "Starting"}</span> <span class="mono muted">{Math.round(runningJob.percent)}%</span></div>
      </div>
    {:else}
      <div class="row wrap">
        {#if isInstalled}
          <button class="btn primary" type="button" disabled>Installed</button>
        {:else}
          <button class="btn primary" type="button" disabled={busy || !selected} onclick={begin}><Icon name="download" size={14} /> Install</button>
          {#if selected}
            <button class="linkish small" type="button" onclick={() => onimport(selected)} aria-label="Import file for {label} {selected.version}" title="Already downloaded the archive, or the download is blocked? Import it instead.">or import a downloaded file</button>
          {/if}
        {/if}
      </div>
      {#if selected?.unverified && !isInstalled}
        <p class="hint">No checksum is published for this download, so it cannot be verified.</p>
      {/if}
    {/if}

    {#if failedJob?.state === "failed" && selected}
      <InstallFailure entry={selected} job={failedJob} onimport={() => onimport(selected)} />
    {/if}
  {/if}
</article>

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
  .kind {
    padding: 14px 16px;
    display: grid;
    gap: 12px;
    align-content: start;
    min-width: 0;
  }
  .head {
    display: flex;
    align-items: center;
    gap: 8px;
  }
  .serves {
    text-decoration: none;
  }
  .list {
    list-style: none;
    margin: 0;
    padding: 0;
    display: grid;
    gap: 6px;
  }
  .list li {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
    flex-wrap: wrap;
    padding: 6px 0;
    border-bottom: 1px solid var(--border, rgba(128, 128, 128, 0.2));
  }
  .info,
  .acts {
    display: flex;
    align-items: center;
    gap: 8px;
    flex-wrap: wrap;
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
  .kind p {
    margin: 0;
  }
</style>
