<script lang="ts">
  import {
    UNVERIFIED_MESSAGE,
    actionFor,
    availability,
    CHECK_TEXT,
    defaultSelection,
    installedRuntime,
    kindLabel,
    noDownloadText,
    optionGroups,
    optionLabel,
    replaceMessage,
    runtimeTitle,
    supportedForKind,
    updateAvailable,
  } from "../lib/catalog";
  import { app, installJobs, jobs, lists, loadCatalog, loadRuntimes, tryInstall, toast } from "../lib/store.svelte";
  import type { CatalogEntry, RuntimeKind } from "../lib/types";
  import ConfirmDialog from "../ui/ConfirmDialog.svelte";
  import Icon from "../ui/Icon.svelte";
  import InstallFailure from "../ui/InstallFailure.svelte";

  let { kind, onimport }: { kind: RuntimeKind; onimport: (entry: CatalogEntry) => void } = $props();

  const label = $derived(kindLabel(kind));
  const entries = $derived(supportedForKind(lists.catalog, kind));
  const avail = $derived(availability(lists.catalog, kind));
  const installed = $derived(installedRuntime(lists.runtimes, kind));
  const registered = $derived(lists.runtimes.filter((r) => r.kind === kind && r.source === "registered"));
  const groups = $derived(optionGroups(entries, kind));
  const update = $derived(updateAvailable(lists.catalog, installed));
  const selectId = $derived(`pick-${kind}`);

  let selectedId = $state("");
  // Until the user picks something, follow the installed (or newest) entry as the catalog changes.
  let touched = $state(false);
  $effect(() => {
    const next = defaultSelection(entries, installed, touched ? selectedId : undefined);
    if (next !== selectedId) selectedId = next;
  });
  const selected = $derived(entries.find((e) => e.id === selectedId));
  const action = $derived(actionFor(selected, installed));

  const jobOf = (e: CatalogEntry) => (installJobs[e.id] ? jobs[installJobs[e.id]] : undefined);
  const runningEntry = $derived(entries.find((e) => jobOf(e)?.state === "running"));
  const runningJob = $derived(runningEntry ? jobOf(runningEntry) : undefined);
  const failedJob = $derived(selected ? jobOf(selected) : undefined);

  // ---- install / switch flow: replace confirm -> unverified confirm -> request ----
  let confirm = $state<{ stage: "replace" | "unverified"; replace: boolean } | null>(null);
  let busy = $state(false);

  function begin() {
    if (!selected) return;
    if (action === "switch") confirm = { stage: "replace", replace: true };
    else if (selected.unverified) confirm = { stage: "unverified", replace: false };
    else void run(false, false);
  }
  function confirmed() {
    if (!confirm || !selected) return;
    if (confirm.stage === "replace" && selected.unverified) confirm = { stage: "unverified", replace: true };
    else void run(confirm.replace, selected.unverified);
  }

  async function run(replace: boolean, accept: boolean) {
    const target = selected;
    if (!target) return;
    confirm = null;
    busy = true;
    const err = await tryInstall(target, { replace, acceptUnverified: accept });
    busy = false;
    if (!err) return;
    if (err.code === "single_install" && !replace) {
      // The list on screen was stale: another version is installed. Refresh and ask again.
      await Promise.all([loadRuntimes(), loadCatalog()]);
      confirm = { stage: "replace", replace: true };
    } else if (err.code === "unverified" && !accept) {
      confirm = { stage: "unverified", replace };
    } else {
      toast("error", err.message); // in_use names the running service
    }
  }
</script>

<article class="card kind" data-kind={kind} aria-labelledby="k-{kind}">
  <div class="head">
    <h3 id="k-{kind}">{label}</h3>
    {#if installed}
      <span class="badge running" data-testid="installed-{kind}"><span class="dot"></span>{installed.version}</span>
    {:else}
      <span class="badge plain" data-testid="installed-{kind}">Not installed</span>
    {/if}
  </div>
  {#if registered.length}
    <p class="muted small">Also registered by path: <span class="mono">{registered.map((r) => r.version || "unknown").join(", ")}</span></p>
  {/if}

  {#if avail === "check"}
    <p class="muted">{CHECK_TEXT}</p>
  {:else if avail === "unsupported"}
    <p class="muted">{noDownloadText(app.status?.os ?? "this", app.status?.arch ?? "PC")}</p>
  {:else}
    <div class="field">
      <label for={selectId}><span class="label">Version</span></label>
      <select id={selectId} bind:value={selectedId} onchange={() => (touched = true)} disabled={!!runningJob}>
        {#each groups as g (g.label || "all")}
          {#if g.label}
            <optgroup label={g.label}>
              {#each g.entries as e (e.id)}<option value={e.id}>{optionLabel(e, installed)}</option>{/each}
            </optgroup>
          {:else}
            {#each g.entries as e (e.id)}<option value={e.id}>{optionLabel(e, installed)}</option>{/each}
          {/if}
        {/each}
      </select>
    </div>

    {#if update}
      <div class="notice update" data-testid="update-{kind}">
        <Icon name="download" />
        <span class="grow">Update available: <b>{update.version}</b></span>
        <button class="btn sm" type="button" onclick={() => {
            selectedId = update.id;
            touched = true;
          }}>Select {update.version}</button>
      </div>
    {/if}

    {#if runningJob && runningEntry}
      <div class="prog" role="group" aria-label="Installing {runningEntry.name}">
        <div class="progress" role="progressbar" aria-valuemin="0" aria-valuemax="100" aria-valuenow={Math.round(runningJob.percent)} aria-label="{runningEntry.name} install progress"><span style:width="{runningJob.percent}%"></span></div>
        <div class="small"><span>{runningEntry.version}: {runningJob.step || "Starting"}</span> <span class="mono muted">{Math.round(runningJob.percent)}%</span></div>
      </div>
    {:else}
      <div class="row wrap">
        {#if action === "installed"}
          <button class="btn primary" type="button" disabled>Installed</button>
        {:else if action === "switch" && selected}
          <button class="btn primary" type="button" disabled={busy} onclick={begin}><Icon name="restart" size={14} /> Switch to {selected.version}</button>
        {:else}
          <button class="btn primary" type="button" disabled={busy || !selected} onclick={begin}><Icon name="download" size={14} /> Install</button>
        {/if}
        {#if selected && action !== "installed"}
          <button class="btn sm" type="button" onclick={() => onimport(selected)} aria-label="Import file for {label} {selected.version}">Import file</button>
        {/if}
      </div>
      {#if selected?.unverified && action !== "installed"}
        <p class="hint">nginx.org publishes no checksum for this download, so it cannot be verified.</p>
      {/if}
    {/if}

    {#if failedJob?.state === "failed" && selected}
      <InstallFailure entry={selected} job={failedJob} onimport={() => onimport(selected)} />
    {/if}
  {/if}
</article>

{#if confirm && selected}
  {#if confirm.stage === "replace" && installed}
    {@const m = replaceMessage(installed, selected)}
    <ConfirmDialog title={m.title} message={m.message} confirmLabel="Replace {label}" danger={false} {busy} onconfirm={confirmed} oncancel={() => (confirm = null)} />
  {:else}
    <ConfirmDialog title="Install {label} {selected.version} without a checksum?" message={UNVERIFIED_MESSAGE} confirmLabel="Install anyway" danger={false} {busy} onconfirm={confirmed} oncancel={() => (confirm = null)} />
  {/if}
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
    justify-content: space-between;
    gap: 8px;
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
