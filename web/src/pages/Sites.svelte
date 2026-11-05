<script lang="ts">
  import { onMount } from "svelte";
  import { api } from "../lib/api";
  import { consumeNewFlag } from "../lib/router.svelte";
  import { app, attempt, lists, loadSites, loadStatus } from "../lib/store.svelte";
  import type { Site } from "../lib/types";
  import ConfirmDialog from "../ui/ConfirmDialog.svelte";
  import Icon from "../ui/Icon.svelte";
  import KindIcon from "../ui/KindIcon.svelte";
  import { isSiteServerService, serverLabel, siteServer } from "../lib/sites";
  import { siteTargetOf } from "../lib/ux";
  import PageHead from "../ui/PageHead.svelte";
  import ServiceControls from "../ui/ServiceControls.svelte";
  import SiteForm from "./SiteForm.svelte";

  let editing = $state<Site | "new" | null>(null);
  let deleting = $state<Site | null>(null);
  onMount(() => {
    if (consumeNewFlag()) editing = "new";
  });

  // Caddy or nginx serves the sites (Settings > Sites and proxy).
  const server = $derived(siteServer(app.settings));
  const serverName = $derived(serverLabel(server));
  const hasServerRuntime = $derived(lists.runtimes.some((r) => r.kind === server));
  const serverService = $derived(lists.services.find((s) => isSiteServerService(s, server)));
  const serverRunning = $derived(serverService?.state === "running");

  function target(s: Site): { kind: string; text: string } {
    const t = siteTargetOf(s);
    if (t === "folder") return { kind: "Folder", text: s.root };
    if (t === "service") {
      const svc = lists.services.find((x) => x.id === s.serviceId);
      return { kind: "Service", text: svc ? `${svc.name}${svc.port ? ` (:${svc.port})` : ""}` : "deleted service" };
    }
    return { kind: "Port", text: String(s.upstreamPort) };
  }

  // Switching a site on or off works like starting or stopping a service: one click, no form.
  let toggling = $state<string | null>(null);
  async function toggle(s: Site) {
    toggling = s.id;
    const on = !s.enabled;
    const r = await attempt(() => (on ? api.enableSite(s.id) : api.disableSite(s.id)), `${s.name} ${on ? "enabled" : "disabled"}.`);
    toggling = null;
    if (r) await loadSites();
  }

  async function confirmDelete() {
    if (!deleting) return;
    const s = deleting;
    deleting = null;
    const ok = await attempt(async () => {
      await api.deleteSite(s.id);
      return true;
    }, `${s.name} deleted.`);
    if (ok) {
      await loadSites();
      void loadStatus();
    }
  }
</script>

<PageHead sub="Friendly addresses like shop.localhost for your services, ports or folders.">
  <button class="btn primary" onclick={() => (editing = "new")}><Icon name="plus" /> New site</button>
</PageHead>

{#if app.loaded.runtimes && !hasServerRuntime}
  <div class="notice warn server" role="note" data-server={server}>
    <Icon name="alert" />
    <span class="grow">Sites need <b>{serverName}</b> to run, and it is not installed yet.</span>
    <a class="btn sm" href="#/runtimes">Install {serverName}</a>
    <a class="btn sm ghost" href="#/settings">Use {server === "nginx" ? "Caddy" : "nginx"} instead</a>
  </div>
{:else if hasServerRuntime && !serverRunning}
  <div class="notice warn server" role="note" data-server={server}>
    <Icon name="alert" />
    {#if serverService}
      <span class="grow"><b>{serverService.name}</b> is not running, so your sites cannot be opened.</span>
      <ServiceControls service={serverService} />
    {:else}
      <span class="grow">Setting up the {serverName} service...</span>
    {/if}
  </div>
{:else}
  <div class="notice server" role="note" data-server={server}>
    <KindIcon kind={server} size={16} />
    <span>{serverName} is running. Sites use HTTP on port {app.settings?.proxyHttpPort ?? 8080} and HTTPS on {app.settings?.proxyHttpsPort ?? 8443}. <a href="#/settings">Change the site server</a></span>
  </div>
{/if}

{#if app.loaded.sites && lists.sites.length === 0}
  <div class="card empty">
    <h2>No sites yet</h2>
    <p>A site gives a folder or service a friendly address such as <code>shop.{app.settings?.domainSuffix?.split(/[\s,]+/)[0] ?? "localhost"}</code>. Browsers resolve <code>.localhost</code> names with no hosts-file edit.</p>
    <button class="btn primary" onclick={() => (editing = "new")}><Icon name="plus" /> Create a site</button>
  </div>
{:else}
  <div class="card t-wrap">
    <table class="t">
      <thead><tr><th>Site</th><th>Address</th><th>Points to</th><th>Status</th><th><span class="sr-only">Actions</span></th></tr></thead>
      <tbody>
        {#each lists.sites as s (s.id)}
          {@const tg = target(s)}
          <tr data-site={s.domain}>
            <td class="cell-title">{s.name}</td>
            <td class="mono">{#if s.enabled}<a href={s.url} target="_blank" rel="noreferrer">{s.url} <Icon name="external" size={12} /></a>{:else}<span class="muted">{s.url}</span>{/if}</td>
            <td><span class="muted small">{tg.kind}</span> <span class={tg.kind === "Service" ? "" : "path"}>{tg.text}</span></td>
            <td>{#if s.enabled}<span class="badge running"><span class="dot"></span>enabled</span>{:else}<span class="badge stopped"><span class="dot"></span>disabled</span>{/if}</td>
            <td class="actions">
              {#if s.enabled}
                <button class="btn sm" disabled={toggling === s.id} onclick={() => toggle(s)} aria-label="Disable {s.name}"><Icon name="stop" size={14} /> Disable</button>
              {:else}
                <button class="btn sm" disabled={toggling === s.id} onclick={() => toggle(s)} aria-label="Enable {s.name}"><Icon name="play" size={14} /> Enable</button>
              {/if}
              <button class="btn sm ghost icon" onclick={() => (editing = s)} aria-label="Edit {s.name}" title="Edit"><Icon name="edit" size={14} /></button>
              <button class="btn sm ghost icon danger" onclick={() => (deleting = s)} aria-label="Delete {s.name}" title="Delete"><Icon name="trash" size={14} /></button>
            </td>
          </tr>
        {/each}
      </tbody>
    </table>
  </div>
{/if}

{#if editing}
  <SiteForm site={editing === "new" ? undefined : editing} onsaved={() => (editing = null)} oncancel={() => (editing = null)} />
{/if}
{#if deleting}
  <ConfirmDialog title="Delete {deleting.name}?" message="The domain {deleting.domain} stops working. The folder or service it pointed at is not touched." confirmLabel="Delete site" onconfirm={confirmDelete} oncancel={() => (deleting = null)} />
{/if}

<style>
  .server {
    margin-bottom: 14px;
    align-items: center;
    flex-wrap: wrap;
  }
  .server :global(svg) {
    flex: none;
  }
</style>
