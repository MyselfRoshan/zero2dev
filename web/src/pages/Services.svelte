<script lang="ts">
  import { onMount } from "svelte";
  import { deleteService, isActive, serviceUrl } from "../lib/actions";
  import { consumeNewFlag } from "../lib/router.svelte";
  import { serviceRuntimeLabel } from "../lib/catalog";
  import { deleteServiceMessage, serviceKindLabel } from "../lib/ux";
  import { dbDataPath } from "../lib/installer";
  import { profileFor } from "../lib/kinds";
  import { app, lists } from "../lib/store.svelte";
  import type { Service } from "../lib/types";
  import ConfirmDialog from "../ui/ConfirmDialog.svelte";
  import Icon from "../ui/Icon.svelte";
  import KindIcon from "../ui/KindIcon.svelte";
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

<PageHead sub="Your apps. Zero2Dev starts them, checks their port and keeps their logs.">
  <button class="btn primary" onclick={() => (editing = "new")}><Icon name="plus" /> New service</button>
</PageHead>

{#if app.loaded.services && lists.services.length === 0}
  <div class="card empty">
    <h2>No services yet</h2>
    <p>Add each app you run during development: choose what it is (Node.js, PHP, Go...), point at its folder, and Zero2Dev runs it for you.</p>
    <button class="btn primary" onclick={() => (editing = "new")}><Icon name="plus" /> Add your first service</button>
  </div>
{:else}
  <div class="card t-wrap">
    <table class="t">
      <thead>
        <tr><th>Name</th><th>Status</th><th>Type</th><th>Port</th><th>Folder</th><th><span class="sr-only">Actions</span></th></tr>
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
            <td><span class="kind-cell"><KindIcon kind={s.kind} size={16} /> {serviceKindLabel(s.kind)}</span>{#if s.runtimeId && serviceRuntimeLabel(s.runtimeId, lists.runtimes)} <span class="badge plain" data-testid="svc-runtime">{serviceRuntimeLabel(s.runtimeId, lists.runtimes)}</span>{/if}</td>
            <td class="mono">{#if url}<a href={url} target="_blank" rel="noreferrer">{s.port}</a>{:else if s.port > 0}{s.port}{:else}<span class="muted">none</span>{/if}</td>
            <td class="path">{#if profileFor(lists.kinds, s.kind)?.folder.use === "hidden"}<span class="muted">data folder</span>{:else}{s.cwd}{/if}</td>
            <td class="actions">
              <ServiceControls service={s} showRestart />
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
    message={deleteServiceMessage(deleting, isActive(deleting), dbDataPath(app.status?.homeDir ?? "<data folder>", deleting.kind, deleting.id))}
    confirmLabel="Delete service"
    {busy}
    onconfirm={confirmDelete}
    oncancel={() => (deleting = null)}
  />
{/if}
