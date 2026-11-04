<script lang="ts">
  import { onMount } from "svelte";
  import { api } from "../lib/api";
  import { deleteService, isActive, serviceUrl } from "../lib/actions";
  import { fmtDateTime } from "../lib/format";
  import { navigate } from "../lib/router.svelte";
  import { app, attempt, clearLogs, lists, logs, seedLogs, upsertService } from "../lib/store.svelte";
  import ConfirmDialog from "../ui/ConfirmDialog.svelte";
  import Icon from "../ui/Icon.svelte";
  import LogViewer from "../ui/LogViewer.svelte";
  import ServiceControls from "../ui/ServiceControls.svelte";
  import StateBadge from "../ui/StateBadge.svelte";
  import ServiceForm from "./ServiceForm.svelte";

  let { id }: { id: string } = $props();
  const service = $derived(lists.services.find((s) => s.id === id));
  let missing = $state(false);
  let editing = $state(false);
  let deleting = $state(false);
  let busy = $state(false);

  onMount(() => {
    void seedLogs(id);
  });
  // Direct link before the list has loaded, or an id the list does not contain.
  $effect(() => {
    if (!service && app.loaded.services) {
      attempt(() => api.getService(id)).then((s) => {
        if (s) upsertService(s);
        else missing = true;
      });
    }
  });
  let lastReconnects = app.reconnects;
  $effect(() => {
    if (app.reconnects !== lastReconnects) {
      lastReconnects = app.reconnects;
      void seedLogs(id);
    }
  });

  const url = $derived(service ? serviceUrl(service) : null);

  async function confirmDelete() {
    if (!service) return;
    busy = true;
    const ok = await deleteService(service);
    busy = false;
    if (ok) navigate("#/services");
    deleting = false;
  }
</script>

<a class="back muted" href="#/services"><Icon name="back" size={14} /> All services</a>

{#if !service}
  <div class="card empty">
    {#if missing}
      <h2>Service not found</h2>
      <p>This service was deleted or the link is wrong.</p>
      <a class="btn" href="#/services">Back to services</a>
    {:else}
      <p>Loading service.</p>
    {/if}
  </div>
{:else}
  <div class="head card">
    <div class="row wrap">
      <h2 class="nm">{service.name}</h2>
      <StateBadge state={service.state} />
      <span class="badge plain">{service.kind}</span>
      <span class="spacer"></span>
      <ServiceControls {service} showRestart />
      <button class="btn sm" disabled={isActive(service)} onclick={() => (editing = true)} title={isActive(service) ? "Stop the service to edit it" : ""}><Icon name="edit" size={14} /> Edit</button>
      <button class="btn sm danger" onclick={() => (deleting = true)}><Icon name="trash" size={14} /> Delete</button>
    </div>
    {#if service.state === "failed" && service.lastError}
      <div class="notice bad" role="alert"><Icon name="alert" /><span>{service.lastError}</span></div>
    {/if}
    <dl>
      <div><dt>Port</dt><dd class="mono">{#if url}<a href={url} target="_blank" rel="noreferrer">{url} <Icon name="external" size={12} /></a>{:else}none{/if}</dd></div>
      <div><dt>Process</dt><dd class="mono">{service.pid ? `pid ${service.pid}` : "not running"}</dd></div>
      <div><dt>Started</dt><dd>{fmtDateTime(service.startedAt) || "-"}</dd></div>
      <div class="wide"><dt>Command</dt><dd class="mono">{service.command || "(runtime binary)"} {service.args.join(" ")}</dd></div>
      <div class="wide"><dt>Folder</dt><dd class="mono">{service.cwd}</dd></div>
    </dl>
  </div>

  <h2 class="lh">Live output</h2>
  <div class="vh">
    <LogViewer
      lines={logs.byService[id] ?? []}
      onclear={() => clearLogs(id)}
      emptyText={service.state === "running" ? "Waiting for output." : "No output yet. Start the service to see its logs here."}
    />
  </div>

  {#if editing}
    <ServiceForm {service} onsaved={() => (editing = false)} oncancel={() => (editing = false)} />
  {/if}
  {#if deleting}
    <ConfirmDialog
      title="Delete {service.name}?"
      message="This stops the service if it is running, then deletes it and its log file. This cannot be undone."
      confirmLabel="Delete service"
      {busy}
      onconfirm={confirmDelete}
      oncancel={() => (deleting = false)}
    />
  {/if}
{/if}

<style>
  .back {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    margin-bottom: 12px;
    text-decoration: none;
  }
  .back:hover {
    color: var(--text);
  }
  .head {
    padding: 14px 16px;
    display: grid;
    gap: 12px;
  }
  .nm {
    font-size: 17px;
  }
  dl {
    display: grid;
    grid-template-columns: repeat(3, 1fr);
    gap: 10px 20px;
    margin: 0;
  }
  dl .wide {
    grid-column: 1 / -1;
  }
  dt {
    font-size: 12.5px;
    color: var(--muted);
    font-weight: 600;
  }
  dd {
    margin: 0;
    overflow-wrap: anywhere;
  }
  .lh {
    margin: 20px 0 8px;
  }
  .vh {
    height: clamp(300px, 52vh, 640px);
  }
</style>
