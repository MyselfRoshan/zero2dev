<script lang="ts">
  import { onMount } from "svelte";
  import { api } from "../lib/api";
  import { deleteService, isActive, openTerminal, serviceUrl } from "../lib/actions";
  import { serviceRuntimeLabel } from "../lib/catalog";
  import { connectionInfo, dbToolsFor, deleteServiceMessage, editorsFor, serviceKindLabel } from "../lib/ux";
  import { dbDataPath } from "../lib/installer";
  import { kindLabel } from "../lib/catalog";
  import { commandText, profileFor } from "../lib/kinds";
  import type { CommandPreview, Runtime, Service, ServiceInput } from "../lib/types";
  import { fmtDateTime } from "../lib/format";
  import { navigate } from "../lib/router.svelte";
  import { app, attempt, clearLogs, lists, logs, seedLogs, upsertService } from "../lib/store.svelte";
  import ConfirmDialog from "../ui/ConfirmDialog.svelte";
  import ConnectionInfo from "../ui/ConnectionInfo.svelte";
  import CrashBanner from "../ui/CrashBanner.svelte";
  import DatabaseTools from "../ui/DatabaseTools.svelte";
  import MailInfo from "../ui/MailInfo.svelte";
  import ServiceSnapshots from "../ui/ServiceSnapshots.svelte";
  import Icon from "../ui/Icon.svelte";
  import KindIcon from "../ui/KindIcon.svelte";
  import LogViewer from "../ui/LogViewer.svelte";
  import ServiceControls from "../ui/ServiceControls.svelte";
  import StateBadge from "../ui/StateBadge.svelte";

  let { id }: { id: string } = $props();
  const service = $derived(lists.services.find((s) => s.id === id));
  let missing = $state(false);
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
  // Editors open the project folder; databases and the web servers have none.
  const editors = $derived(
    service?.cwd && profileFor(lists.kinds, service.kind)?.folder.use !== "hidden" ? editorsFor(lists.runtimes) : [],
  );
  const conn = $derived(service ? connectionInfo(service) : null);
  const profile = $derived(service ? profileFor(lists.kinds, service.kind) : undefined);
  // The exact command line, from the daemon; fetched again whenever the service changes.
  let command = $state<CommandPreview | null>(null);
  $effect(() => {
    if (!service) return;
    void JSON.stringify([service.args, service.settings, service.customArgs, service.command, service.runtimeId, service.port, service.cwd, lists.runtimes.length]);
    api.serviceCommand(service.id).then((c) => (command = c), () => (command = null));
  });
  const dataDir = $derived(
    command?.dataDir || (service ? dbDataPath(app.status?.homeDir ?? "<data folder>", service.kind, service.id) : ""),
  );

  function inputOf(s: Service): ServiceInput {
    const { name, kind, command, args, settings, customArgs, cwd, env, port, healthPath, runtimeId, autostart } = s;
    const { dependsOn, databaseId, envFile, restartOnCrash, restartLimit } = s;
    return {
      name, kind, command, args, settings, customArgs, cwd, env, port, healthPath, runtimeId, autostart,
      dependsOn, databaseId, envFile, restartOnCrash, restartLimit,
    };
  }
  // A service from before settings existed runs its own command line; offer the guided settings.
  let guided = $state<CommandPreview | null>(null);
  async function askGuided() {
    if (!service) return;
    guided = (await attempt(() => api.previewService({ ...inputOf(service!), customArgs: false, args: [], command: "", id: service!.id }))) ?? null;
  }
  async function switchToGuided() {
    if (!service) return;
    busy = true;
    const saved = await attempt(() => api.updateService(service!.id, { ...inputOf(service!), customArgs: false, args: [], command: "" }), "Now using the settings.");
    busy = false;
    if (saved) upsertService(saved);
    guided = null;
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
      <button class="btn sm" disabled={isActive(service)} onclick={() => navigate(`#/services/${encodeURIComponent(service.id)}/edit`)} title={isActive(service) ? "Stop the service to edit it" : ""}><Icon name="edit" size={14} /> Edit</button>
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
    <div class="row wrap open-terminal" data-testid="open-terminal">
      <button class="btn sm" type="button" onclick={() => openTerminal(service)}><Icon name="terminal" size={14} /> Open terminal</button>
      <span class="muted small">The terminal uses the same runtime versions and variables as this service.</span>
    </div>
    <CrashBanner serviceId={service.id} />
    {#if service.state === "failed" && service.lastError}
      <div class="notice bad" role="alert"><Icon name="alert" /><span>{service.lastError}</span></div>
    {/if}
    <dl>
      {#if !conn}<div><dt>Address</dt><dd class="mono">{#if url}<a href={url} target="_blank" rel="noreferrer">{url} <Icon name="external" size={12} /></a>{:else}no port{/if}</dd></div>{/if}
      <div><dt>Process</dt><dd class="mono">{service.pid ? `pid ${service.pid}` : "not running"}</dd></div>
      <div><dt>Started</dt><dd>{fmtDateTime(service.startedAt) || "-"}</dd></div>
      <div class="wide"><dt>Runs</dt><dd class="mono" data-testid="command-line">{command ? commandText(command.program, command.args) : "..."}
        {#if service.customArgs && service.kind !== "custom"}<span class="muted small"> (your own command line, <button type="button" class="linkish" onclick={askGuided}>use the settings instead</button>)</span>{/if}</dd></div>
      {#if service.cwd && profile?.folder.use !== "hidden"}<div class="wide"><dt>{profile?.folder.label ?? "Folder"}</dt><dd class="mono">{service.cwd}</dd></div>{/if}
      {#if command?.dataDir}<div class="wide"><dt>Data folder</dt><dd class="mono" data-testid="data-dir">{command.dataDir}</dd></div>{/if}
      {#if command?.warnings.length}<div class="wide"><dt>Notes</dt><dd>{#each command.warnings as w (w)}<div class="small muted">{w}</div>{/each}</dd></div>{/if}
    </dl>
    <ConnectionInfo {service} {dataDir} />
    <MailInfo {service} />
  </div>

  <!-- 0.0.5: each of these renders nothing for a service it does not apply to. -->
  <DatabaseTools {service} />
  <ServiceSnapshots {service} />

  <h2 class="lh">Live output</h2>
  <div class="vh">
    <LogViewer
      lines={logs.byService[id] ?? []}
      onclear={() => clearLogs(id)}
      emptyText={service.state === "running" ? "Waiting for output." : "No output yet. Start the service to see its logs here."}
    />
  </div>

  {#if guided}
    <ConfirmDialog
      title="Use the settings for {service.name}?"
      message={guided.warnings.length
        ? `${guided.warnings.join(" ")} Use Edit to complete the settings before switching.`
        : `Its own command line is replaced by the one its settings make: ${commandText(guided.program, guided.args)}. Check the settings with Edit afterwards.`}
      confirmLabel="Use the settings"
      danger={false}
      {busy}
      onconfirm={switchToGuided}
      oncancel={() => (guided = null)}
    />
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
    font-size: 16px;
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
    font-size: 12px;
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
