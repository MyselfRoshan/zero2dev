<script lang="ts">
  // The snapshots of one project or database service: the list (name, date, size, services,
  // runtime versions, warnings) with Restore, Clone (projects only) and Delete, and Take snapshot.
  import { api } from "../lib/api";
  import { ago, fmtDateTime } from "../lib/format";
  import { fmtBytes, snapshotCounts, snapshotRuntimes, snapshotServiceNames } from "../lib/snapshots";
  import { attempt, errMessage, lists } from "../lib/store.svelte";
  import type { Snapshot, SnapshotScope } from "../lib/types";
  import CloneSnapshotDialog from "./CloneSnapshotDialog.svelte";
  import ConfirmDialog from "./ConfirmDialog.svelte";
  import Icon from "./Icon.svelte";
  import RestoreSnapshotDialog from "./RestoreSnapshotDialog.svelte";
  import TakeSnapshotDialog from "./TakeSnapshotDialog.svelte";

  type Props = {
    scope: SnapshotScope;
    ownerId: string;
    ownerName: string;
    /** false: the owner was deleted; its snapshots can still be restored. */
    exists?: boolean;
    /** The Take snapshot button is the view's primary button (the project's Snapshots page). */
    primary?: boolean;
  };
  let { scope, ownerId, ownerName, exists = true, primary = false }: Props = $props();

  let snapshots = $state<Snapshot[]>([]);
  let loaded = $state(false);
  let loadError = $state("");
  let taking = $state(false);
  let restoring = $state<Snapshot | null>(null);
  let cloning = $state<Snapshot | null>(null);
  let deleting = $state<Snapshot | null>(null);
  let deleteBusy = $state(false);

  async function reload() {
    try {
      snapshots = await api.listSnapshots(scope, ownerId);
      loadError = "";
    } catch (e) {
      loadError = errMessage(e);
    } finally {
      loaded = true;
    }
  }
  $effect(() => {
    void ownerId;
    void reload();
  });
  // Another tab (or a snapshot offer) may take one: reload when the owner's services change state.
  let lastKey = "";
  $effect(() => {
    const key = lists.services.map((s) => `${s.id}:${s.state}`).join() + "|" + lists.projects.length;
    if (lastKey && key !== lastKey) void reload();
    lastKey = key;
  });

  async function confirmDelete() {
    const s = deleting;
    if (!s) return;
    deleteBusy = true;
    const ok = await attempt(async () => {
      await api.deleteSnapshot(s.id);
      return true;
    }, `Snapshot "${s.name}" deleted.`);
    deleteBusy = false;
    deleting = null;
    if (ok) await reload();
  }
</script>

<div class="snaps" data-testid="snapshot-list" data-scope={scope}>
  <div class="bar">
    <span class="muted small grow" role="status">
      {#if !loaded}Loading snapshots…{:else if snapshots.length === 0}No snapshots yet.{:else}{snapshots.length} snapshot{snapshots.length === 1 ? "" : "s"}, newest first.{/if}
    </span>
    <button class="btn" class:primary class:sm={!primary} type="button" disabled={!exists} onclick={() => (taking = true)} data-testid="take-snapshot" title={exists ? undefined : "The project was deleted; restore a snapshot to bring it back"}><Icon name="plus" size={primary ? 16 : 14} /> Take snapshot</button>
  </div>
  {#if loadError}<div class="notice bad" role="alert"><Icon name="alert" /><span>{loadError}</span></div>{/if}

  {#if loaded && snapshots.length === 0 && !loadError && scope === "project"}
    <p class="empty-text">{scope === "project" ? "A snapshot saves this project's services, settings, runtime versions, sites and database data, so you can go back to it or start a copy." : "A snapshot saves this database's settings and data, so you can go back to it after an import or a reset."}</p>
  {:else if snapshots.length}
    <ul class="list" aria-label="Snapshots">
      {#each snapshots as s (s.id)}
        {@const rts = snapshotRuntimes(s)}
        <li data-testid="snapshot-row" data-snapshot={s.name} data-id={s.id}>
          <div class="main">
            <div class="title-line">
              <span class="name">{s.name}</span>
              {#if s.warnings.length}<span class="badge warn" title={s.warnings.join(" ")}><span class="dot"></span>{s.warnings.length} warning{s.warnings.length === 1 ? "" : "s"}</span>{/if}
            </div>
            <div class="meta small muted">
              <span title={fmtDateTime(s.createdAt)}>{ago(s.createdAt)}</span>
              <span aria-hidden="true">·</span>
              <span data-testid="snapshot-size">{fmtBytes(s.sizeBytes)}</span>
              {#if scope === "project"}<span aria-hidden="true">·</span><span title={snapshotServiceNames(s).join(", ")}>{snapshotCounts(s)}</span>{/if}
            </div>
            {#if scope === "project" && s.services.length}<div class="small muted svcs">{snapshotServiceNames(s).join(", ")}</div>{/if}
            {#if rts.length}
              <div class="chips" aria-label="Runtime versions">
                {#each rts as r (r)}<span class="chip-code" data-testid="snapshot-runtime">{r}</span>{/each}
              </div>
            {/if}
            {#each s.warnings as w (w)}<div class="small warn-text">{w}</div>{/each}
          </div>
          <div class="acts">
            <button class="btn sm" type="button" onclick={() => (restoring = s)} aria-label="Restore {s.name}"><Icon name="undo" size={14} /> Restore</button>
            {#if scope === "project"}
              <button class="btn sm" type="button" onclick={() => (cloning = s)} aria-label="Clone {s.name} into a new project"><Icon name="copy" size={14} /> Clone</button>
            {/if}
            <button class="btn sm ghost icon danger" type="button" onclick={() => (deleting = s)} aria-label="Delete {s.name}" title="Delete"><Icon name="trash" size={14} /></button>
          </div>
        </li>
      {/each}
    </ul>
  {/if}
</div>

{#if taking}
  <TakeSnapshotDialog {scope} {ownerId} {ownerName} ondone={() => { taking = false; void reload(); }} onclose={() => (taking = false)} />
{/if}
{#if restoring}
  <RestoreSnapshotDialog snap={restoring} ondone={() => { restoring = null; void reload(); }} onclose={() => (restoring = null)} />
{/if}
{#if cloning}
  <CloneSnapshotDialog snap={cloning} ondone={() => (cloning = null)} onclose={() => (cloning = null)} />
{/if}
{#if deleting}
  <ConfirmDialog
    title="Delete the snapshot {deleting.name}?"
    message="Its saved settings and data copy are deleted. {scope === 'project' ? 'The project' : 'The service'} itself is not changed."
    confirmLabel="Delete snapshot"
    busy={deleteBusy}
    onconfirm={confirmDelete}
    oncancel={() => (deleting = null)}
  />
{/if}

<style>
  .snaps {
    display: grid;
    gap: 12px;
  }
  .bar {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 8px;
  }
  .empty-text {
    margin: 0;
    color: var(--muted);
  }
  .list {
    list-style: none;
    margin: 0;
    padding: 0;
    border: 1px solid var(--line);
    border-radius: var(--radius-lg);
    background: var(--surface);
  }
  .list li {
    display: flex;
    flex-wrap: wrap;
    align-items: flex-start;
    gap: 8px 16px;
    padding: 12px 16px;
    border-top: 1px solid var(--line);
  }
  .list li:first-child {
    border-top: 0;
  }
  .main {
    flex: 1 1 260px;
    min-width: 0;
    display: grid;
    gap: 4px;
  }
  .title-line {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 8px;
  }
  .name {
    font-weight: 600;
    overflow-wrap: anywhere;
  }
  .meta {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
  }
  .svcs {
    overflow-wrap: anywhere;
  }
  .chips {
    display: flex;
    flex-wrap: wrap;
    gap: 4px;
  }
  .warn-text {
    color: var(--warn);
  }
  .acts {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 8px;
  }
</style>
