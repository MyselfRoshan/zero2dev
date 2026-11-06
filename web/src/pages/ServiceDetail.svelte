<script lang="ts">
  import { onMount } from "svelte";
  import { api } from "../lib/api";
  import { deleteService, isActive, serviceUrl } from "../lib/actions";
  import { serviceRuntimeLabel } from "../lib/catalog";
  import { commandLine, connectionInfo, dbToolsFor, deleteServiceMessage, editorsFor, serviceKindLabel } from "../lib/ux";
  import { dbDataPath } from "../lib/installer";
  import { kindLabel } from "../lib/catalog";
  import type { Runtime } from "../lib/types";
  import { fmtDateTime } from "../lib/format";
  import { navigate } from "../lib/router.svelte";
  import { app, attempt, clearLogs, lists, logs, seedLogs, toast, upsertService } from "../lib/store.svelte";
  import ConfirmDialog from "../ui/ConfirmDialog.svelte";
  import Icon from "../ui/Icon.svelte";
  import KindIcon from "../ui/KindIcon.svelte";
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
  const editors = $derived(editorsFor(lists.runtimes));
  const conn = $derived(service ? connectionInfo(service) : null);
  const dataDir = $derived(service ? dbDataPath(app.status?.homeDir ?? "<data folder>", service.id) : "");
  async function copy(text: string) {
    try {
      await navigator.clipboard.writeText(text);
      toast("success", "Connection URL copied.");
    } catch {
      toast("error", "Could not copy to the clipboard in this browser context.");
    }
  }
  const dbTools = $derived(service ? dbToolsFor(lists.runtimes, service.kind) : []);
  async function openIn(r: Runtime, body: { path?: string; serviceId?: string }) {
    await attempt(() => api.launchRuntime(r.id, body), `Opening in ${kindLabel(r.kind)}.`);
  }

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
      <span class="badge plain kind-badge"><KindIcon kind={service.kind} size={12} /> {serviceKindLabel(service.kind)}</span>
      {#if serviceRuntimeLabel(service.runtimeId, lists.runtimes)}<span class="badge plain" data-testid="svc-runtime">{serviceRuntimeLabel(service.runtimeId, lists.runtimes)}</span>{/if}
      <span class="spacer"></span>
      <ServiceControls {service} showRestart />
      <button class="btn sm" disabled={isActive(service)} onclick={() => (editing = true)} title={isActive(service) ? "Stop the service to edit it" : ""}><Icon name="edit" size={14} /> Edit</button>
      <button class="btn sm danger" onclick={() => (deleting = true)}><Icon name="trash" size={14} /> Delete</button>
    </div>
    {#if editors.length || dbTools.length}
      <div class="row wrap open-in" data-testid="open-in">
        {#each editors as r (r.id)}
          <button class="btn sm" type="button" onclick={() => openIn(r, { path: service.cwd })} aria-label="Open {service.name} folder in {kindLabel(r.kind)}"><KindIcon kind={r.kind} size={14} /> Open in {kindLabel(r.kind)}</button>
        {/each}
        {#each dbTools as r (r.id)}
          <button class="btn sm" type="button" onclick={() => openIn(r, { serviceId: service.id })} disabled={!service.port} aria-label="Open {service.name} in {kindLabel(r.kind)}"><KindIcon kind={r.kind} size={14} /> Open in {kindLabel(r.kind)}</button>
        {/each}
      </div>
    {/if}
    {#if service.state === "failed" && service.lastError}
      <div class="notice bad" role="alert"><Icon name="alert" /><span>{service.lastError}</span></div>
    {/if}
    <dl>
      {#if !conn}<div><dt>Address</dt><dd class="mono">{#if url}<a href={url} target="_blank" rel="noreferrer">{url} <Icon name="external" size={12} /></a>{:else}no port{/if}</dd></div>{/if}
      <div><dt>Process</dt><dd class="mono">{service.pid ? `pid ${service.pid}` : "not running"}</dd></div>
      <div><dt>Started</dt><dd>{fmtDateTime(service.startedAt) || "-"}</dd></div>
      <div class="wide"><dt>Runs</dt><dd class="mono" data-testid="command-line">{commandLine(service, lists.runtimes, { dataDir, windows: (app.status?.os ?? "windows") === "windows" })}</dd></div>
      <div class="wide"><dt>Folder</dt><dd class="mono">{service.cwd}</dd></div>
    </dl>
    {#if conn}
      <section class="conn" aria-labelledby="conn-h" data-testid="connection">
        <h3 id="conn-h">Connection</h3>
        <dl>
          <div><dt>Host</dt><dd class="mono">{conn.host}</dd></div>
          <div><dt>Port</dt><dd class="mono">{conn.port}</dd></div>
          <div><dt>User</dt><dd class="mono">{conn.user || "none"}</dd></div>
          <div class="wide"><dt>URL</dt><dd class="row"><code class="mono" data-testid="connection-url">{conn.url}</code><button class="btn sm ghost" type="button" onclick={() => copy(conn.url)}><Icon name="copy" size={14} /> Copy</button></dd></div>
        </dl>
        <p class="hint">No password: the server only accepts connections from this PC (127.0.0.1). Its files are in <code class="mono">{dataDir}</code>.</p>
      </section>
    {/if}
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
      message={deleteServiceMessage(service, isActive(service), dataDir)}
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
  .conn {
    border-top: 1px solid var(--line);
    padding-top: 12px;
    display: grid;
    gap: 8px;
  }
  .conn h3 {
    margin: 0;
    font-size: 14px;
  }
  .conn .row {
    gap: 8px;
    align-items: center;
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
