<script lang="ts">
  import { onMount } from "svelte";
  import { deleteService, isActive, serviceUrl } from "../lib/actions";
  import { consumeNewFlag } from "../lib/router.svelte";
  import { app, lists } from "../lib/store.svelte";
  import type { Service } from "../lib/types";
  import ConfirmDialog from "../ui/ConfirmDialog.svelte";
  import Icon from "../ui/Icon.svelte";
  import PageHead from "../ui/PageHead.svelte";
  import ServiceControls from "../ui/ServiceControls.svelte";
  import StateBadge from "../ui/StateBadge.svelte";
  import ServiceForm from "./ServiceForm.svelte";

  let editing = $state<Service | "new" | null>(null);
  let deleting = $state<Service | null>(null);
  let busy = $state(false);

  onMount(() => {
    if (consumeNewFlag()) editing = "new";
  });

  async function confirmDelete() {
    if (!deleting) return;
    busy = true;
    await deleteService(deleting);
    busy = false;
    deleting = null;
  }
</script>

<PageHead sub="Commands that Zero2Dev starts, stops and watches.">
  <button class="btn primary" onclick={() => (editing = "new")}><Icon name="plus" /> New service</button>
</PageHead>

{#if app.loaded.services && lists.services.length === 0}
  <div class="card empty">
    <h2>No services yet</h2>
    <p>Create a service for each app you run during development. Pick the kind, point it at a folder and Zero2Dev handles the process, the port check and the logs.</p>
    <button class="btn primary" onclick={() => (editing = "new")}><Icon name="plus" /> Create your first service</button>
  </div>
{:else}
  <div class="card t-wrap">
    <table class="t">
      <thead>
        <tr><th>Name</th><th>State</th><th>Kind</th><th>Port</th><th>Folder</th><th><span class="sr-only">Actions</span></th></tr>
      </thead>
      <tbody>
        {#each lists.services as s (s.id)}
          {@const url = serviceUrl(s)}
          <tr data-service={s.name}>
            <td>
              <a class="cell-title" href="#/services/{s.id}">{s.name}</a>
              {#if s.autostart}<span class="badge plain" title="Starts with the daemon">autostart</span>{/if}
              {#if s.state === "failed" && s.lastError}<div class="err">{s.lastError}</div>{/if}
            </td>
            <td><StateBadge state={s.state} /></td>
            <td>{s.kind}</td>
            <td class="mono">{#if url}<a href={url} target="_blank" rel="noreferrer">{s.port}</a>{:else}<span class="muted">none</span>{/if}</td>
            <td class="path">{s.cwd}</td>
            <td class="actions">
              <ServiceControls service={s} showRestart />
              <a class="btn sm ghost icon" href="#/services/{s.id}" aria-label="Open {s.name} and its logs" title="Logs"><Icon name="logs" size={14} /></a>
              <button class="btn sm ghost icon" disabled={isActive(s)} onclick={() => (editing = s)} aria-label="Edit {s.name}" title={isActive(s) ? "Stop the service to edit it" : "Edit"}><Icon name="edit" size={14} /></button>
              <button class="btn sm ghost icon danger" onclick={() => (deleting = s)} aria-label="Delete {s.name}" title="Delete"><Icon name="trash" size={14} /></button>
            </td>
          </tr>
        {/each}
      </tbody>
    </table>
  </div>
{/if}

{#if editing}
  <ServiceForm service={editing === "new" ? undefined : editing} onsaved={() => (editing = null)} oncancel={() => (editing = null)} />
{/if}
{#if deleting}
  <ConfirmDialog
    title="Delete {deleting.name}?"
    message={isActive(deleting)
      ? "This service is running. Zero2Dev will stop it, then delete it and its log file. This cannot be undone."
      : "This deletes the service and its log file. This cannot be undone."}
    confirmLabel="Delete service"
    {busy}
    onconfirm={confirmDelete}
    oncancel={() => (deleting = null)}
  />
{/if}
