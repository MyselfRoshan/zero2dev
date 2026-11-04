<script lang="ts">
  import { onMount } from "svelte";
  import { api } from "../lib/api";
  import { consumeNewFlag } from "../lib/router.svelte";
  import { app, attempt, lists, loadSites, loadStatus } from "../lib/store.svelte";
  import type { Site } from "../lib/types";
  import ConfirmDialog from "../ui/ConfirmDialog.svelte";
  import Icon from "../ui/Icon.svelte";
  import PageHead from "../ui/PageHead.svelte";
  import SiteForm from "./SiteForm.svelte";

  let editing = $state<Site | "new" | null>(null);
  let deleting = $state<Site | null>(null);
  onMount(() => {
    if (consumeNewFlag()) editing = "new";
  });

  const hasCaddyRuntime = $derived(lists.runtimes.some((r) => r.kind === "caddy"));
  const caddyService = $derived(lists.services.find((s) => s.kind === "caddy"));
  const caddyRunning = $derived(caddyService?.state === "running");

  function target(s: Site): string {
    if (s.type === "static") return s.root;
    if (s.serviceId) {
      const svc = lists.services.find((x) => x.id === s.serviceId);
      return svc ? `${svc.name}${svc.port ? ` :${svc.port}` : ""}` : s.serviceId;
    }
    return s.upstreamPort ? `127.0.0.1:${s.upstreamPort}` : "";
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

<PageHead sub="Local domains that point at a folder or a running service.">
  <button class="btn primary" onclick={() => (editing = "new")}><Icon name="plus" /> New site</button>
</PageHead>

{#if app.loaded.runtimes && !hasCaddyRuntime}
  <div class="notice warn caddy" role="note">
    <Icon name="alert" />
    <span>
      Sites are served by <b>Caddy</b>, and no Caddy runtime is installed. <a href="#/runtimes">Install or register Caddy on the Runtimes page</a>, then Zero2Dev creates a Caddy service for you.
    </span>
  </div>
{:else if hasCaddyRuntime && !caddyRunning}
  <div class="notice warn caddy" role="note">
    <Icon name="alert" />
    <span>
      Sites only work while the <b>Caddy</b> service is running.
      {#if caddyService}<a href="#/services/{caddyService.id}">Open the Caddy service</a> and start it.{:else}Zero2Dev will create the Caddy service shortly; check the <a href="#/services">Services page</a>.{/if}
    </span>
  </div>
{:else}
  <div class="notice caddy" role="note">
    <Icon name="check" />
    <span>Caddy is running. Sites use HTTP on port {app.settings?.proxyHttpPort ?? 8080} and HTTPS on {app.settings?.proxyHttpsPort ?? 8443}.</span>
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
      <thead><tr><th>Site</th><th>Address</th><th>Type</th><th>Target</th><th>Status</th><th><span class="sr-only">Actions</span></th></tr></thead>
      <tbody>
        {#each lists.sites as s (s.id)}
          <tr data-site={s.domain}>
            <td class="cell-title">{s.name}</td>
            <td class="mono"><a href={s.url} target="_blank" rel="noreferrer">{s.url} <Icon name="external" size={12} /></a></td>
            <td>{s.type}{s.https ? " + https" : ""}</td>
            <td class="path">{target(s)}</td>
            <td>{#if s.enabled}<span class="badge running"><span class="dot"></span>enabled</span>{:else}<span class="badge stopped"><span class="dot"></span>disabled</span>{/if}</td>
            <td class="actions">
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
  .caddy {
    margin-bottom: 14px;
    align-items: flex-start;
  }
  .caddy :global(svg) {
    flex: none;
    margin-top: 3px;
  }
</style>
