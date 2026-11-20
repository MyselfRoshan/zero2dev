<script lang="ts">
  // Restore a snapshot: what changes (settings, data, runtime versions), the runtimes to install
  // first (each with Install when the catalog has it), and the restart choice when services run.
  import { api } from "../lib/api";
  import { fmtDateTime } from "../lib/format";
  import { waitForJob } from "../lib/snapshotJobs";
  import { catalogEntryFor, restoreChanges, restoreReadiness, runtimeRefLabel, joinNames } from "../lib/snapshots";
  import { errMessage, installJobs, installRuntime, jobs, lists, toast } from "../lib/store.svelte";
  import type { CatalogEntry, Job, Snapshot, SnapshotDetail } from "../lib/types";
  import Icon from "./Icon.svelte";
  import JobProgress from "./JobProgress.svelte";
  import Modal from "./Modal.svelte";

  type Props = { snap: Snapshot; ondone: () => void; onclose: () => void };
  let { snap, ondone, onclose }: Props = $props();

  let detail = $state<SnapshotDetail | null>(null);
  let loadError = $state("");
  let restart = $state(true);
  let busy = $state(false);
  let job = $state<Job | null>(null);
  let error = $state("");

  async function loadDetail() {
    try {
      detail = await api.getSnapshot(snap.id);
      loadError = "";
    } catch (e) {
      loadError = errMessage(e);
    }
  }
  // Load now, and again whenever the runtimes change (an install from this dialog finished).
  $effect(() => {
    void lists.runtimes.length;
    void lists.runtimes.map((r) => r.id).join();
    void loadDetail();
  });

  const shown = $derived(detail ?? snap);
  const missing = $derived(detail?.missing ?? []);
  const currentProject = $derived(snap.scope === "project" ? lists.projects.find((p) => p.id === snap.ownerId) : undefined);
  const changes = $derived(restoreChanges(shown, lists.services, lists.sites, lists.runtimes, currentProject));
  const ready = $derived(restoreReadiness(shown, missing, lists.services, restart));

  const installJobOf = (c: CatalogEntry | undefined) => (c && installJobs[c.id] ? jobs[installJobs[c.id]] : undefined);

  async function restore() {
    if (!ready.ready || busy || !detail) return;
    busy = true;
    error = "";
    try {
      const started = await api.restoreSnapshot(snap.id, ready.running.length > 0 && restart);
      const end = await waitForJob(started, (j) => (job = j));
      if (end.state === "done") {
        toast("success", `"${snap.name}" restored.`);
        ondone();
      } else error = end.error || "The restore failed.";
    } catch (e) {
      error = errMessage(e);
      void loadDetail();
    } finally {
      busy = false;
    }
  }
</script>

<Modal title="Restore {snap.name}?" onclose={() => { if (!busy) onclose(); }}>
  <div class="dlg-body restore" data-testid="restore-dialog">
    <p>This puts {snap.scope === "project" ? `the project ${snap.project?.name ?? ""}` : snap.services[0]?.service.name ?? "the service"} back as it was on {fmtDateTime(snap.createdAt)}. What it holds now is replaced.</p>
    <ul class="changes" aria-label="What changes">
      {#each changes as c (c)}<li>{c}</li>{/each}
    </ul>
    {#if loadError}<div class="notice bad" role="alert"><Icon name="alert" /><span>Could not check the runtimes: {loadError}</span></div>{/if}

    {#if missing.length}
      <div class="notice warn missing" data-testid="restore-missing">
        <Icon name="alert" />
        <div class="grow">
          <p>{missing.length === 1 ? "This runtime is" : "These runtimes are"} not installed. Install {missing.length === 1 ? "it" : "them"} first; then you can restore.</p>
          <ul>
            {#each missing as m (m.kind + m.version)}
              {@const entry = catalogEntryFor(m, lists.catalog)}
              {@const ij = installJobOf(entry)}
              <li data-missing={runtimeRefLabel(m)}>
                <span class="grow"><b>{runtimeRefLabel(m)}</b> <span class="muted small">for {joinNames(m.services)}</span></span>
                {#if entry && ij?.state === "running"}
                  <span class="small mini" role="status"><span class="spin" aria-hidden="true"></span> Installing… {Math.round(ij.percent)}%</span>
                {:else if entry}
                  <button class="btn sm" type="button" onclick={() => installRuntime(entry)} aria-label="Install {runtimeRefLabel(m)}"><Icon name="download" size={14} /> Install</button>
                {:else}
                  <a class="small" href="#/runtimes">Install or register it on the Runtimes page</a>
                {/if}
              </li>
            {/each}
          </ul>
        </div>
      </div>
    {/if}

    {#if ready.running.length}
      <div class="notice warn" data-testid="restore-running">
        <Icon name="alert" />
        <div class="grow">
          <p>{joinNames(ready.running.map((s) => s.name))} {ready.running.length === 1 ? "is" : "are"} running.</p>
          <label class="check"><input type="checkbox" bind:checked={restart} disabled={busy} /> Stop {ready.running.length === 1 ? "it" : "them"}, restore, and start {ready.running.length === 1 ? "it" : "them"} again</label>
        </div>
      </div>
    {/if}
    {#if job}<JobProgress {job} label="Restore" />{/if}
    {#if error}<div class="notice bad" role="alert"><Icon name="alert" /><span>{error}</span></div>{/if}
  </div>
  {#snippet footer()}
    {#if !ready.ready && !busy}<span class="muted small why" data-testid="restore-why">{ready.reason}</span>{/if}
    <button class="btn" type="button" disabled={busy} onclick={onclose}>Cancel</button>
    <button class="btn primary" type="button" disabled={busy || !ready.ready || !detail} onclick={restore} data-testid="restore-confirm">{#if busy}<span class="spin" aria-hidden="true"></span>{/if} Restore</button>
  {/snippet}
</Modal>

<style>
  .restore {
    display: grid;
    gap: 12px;
  }
  .restore p {
    margin: 0;
  }
  .changes {
    margin: 0;
    padding-left: 20px;
    display: grid;
    gap: 2px;
  }
  .notice .grow {
    display: grid;
    gap: 4px;
  }
  .missing ul {
    list-style: none;
    margin: 0;
    padding: 0;
    display: grid;
    gap: 4px;
  }
  .missing li {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 8px;
  }
  .mini {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    color: var(--accent-text);
  }
  .why {
    margin-right: auto;
    align-self: center;
  }
  :global(dialog:has([data-testid="restore-dialog"]) .dlg-foot) {
    flex-wrap: wrap;
  }
</style>
