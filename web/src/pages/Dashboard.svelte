<script lang="ts">
  import { serviceUrl } from "../lib/actions";
  import { app, lists } from "../lib/store.svelte";
  import { fmtUptime } from "../lib/format";
  import Icon from "../ui/Icon.svelte";
  import ServiceControls from "../ui/ServiceControls.svelte";
  import StateBadge from "../ui/StateBadge.svelte";

  const counts = $derived(app.status?.counts);
  const stats = $derived([
    { label: "Services", value: counts?.services ?? lists.services.length, href: "#/services" },
    { label: "Running", value: counts?.running ?? lists.services.filter((s) => s.state === "running").length, href: "#/services", ok: true },
    { label: "Runtimes", value: counts?.runtimes ?? lists.runtimes.length, href: "#/runtimes" },
    { label: "Sites", value: counts?.sites ?? lists.sites.length, href: "#/sites" },
  ]);
  const noRuntimes = $derived(app.loaded.runtimes && lists.runtimes.length === 0);
  const noServices = $derived(app.loaded.services && lists.services.length === 0);
</script>

<div class="stats">
  {#each stats as s (s.label)}
    <a class="card stat" href={s.href}>
      <span class="n mono" class:ok={s.ok && s.value > 0}>{s.value}</span>
      <span class="muted">{s.label}</span>
    </a>
  {/each}
  <div class="card stat">
    <span class="n mono small-n">{app.status ? fmtUptime(app.status.uptimeSeconds) : "-"}</span>
    <span class="muted">Daemon uptime</span>
  </div>
</div>

<section class="sect">
  <div class="sect-head">
    <h2>Services</h2>
    <span class="spacer"></span>
    <a class="btn sm" href="#/services">View all</a>
  </div>
  {#if noServices}
    <div class="card empty">
      <h2>No services yet</h2>
      <p>A service is a command Zero2Dev starts for you, such as <code>node server.js</code> or a PHP built-in server, with its logs and port in one place.</p>
      <div class="row wrap">
        <a class="btn primary" href="#/services?new"><Icon name="plus" /> Create service</a>
        {#if noRuntimes}<a class="btn" href="#/runtimes">Add a runtime first</a>{/if}
      </div>
    </div>
  {:else}
    <div class="cards">
      {#each lists.services as s (s.id)}
        {@const url = serviceUrl(s)}
        <article class="card svc">
          <div class="row">
            <a class="name" href="#/services/{s.id}">{s.name}</a>
            <span class="spacer"></span>
            <StateBadge state={s.state} />
          </div>
          <div class="meta muted small">
            <span class="badge plain">{s.kind}</span>
            {#if url}<a class="mono" href={url} target="_blank" rel="noreferrer">:{s.port}</a>{:else}<span>no port</span>{/if}
            {#if s.pid}<span class="mono">pid {s.pid}</span>{/if}
          </div>
          {#if s.lastError && s.state === "failed"}<p class="err">{s.lastError}</p>{/if}
          <div class="row">
            <ServiceControls service={s} />
            <a class="btn sm ghost" href="#/services/{s.id}"><Icon name="logs" size={14} /> Logs</a>
          </div>
        </article>
      {/each}
    </div>
  {/if}
</section>

<section class="sect">
  <div class="sect-head"><h2>Get things done</h2></div>
  <div class="quick">
    <a class="card q" href="#/services?new">
      <Icon name="services" size={20} />
      <span><b>Create a service</b><span class="muted small">Run a Node, PHP, Go, Java or custom command.</span></span>
    </a>
    <a class="card q" href="#/runtimes">
      <Icon name="download" size={20} />
      <span><b>Install a runtime</b><span class="muted small">Download from the catalog, or detect what is already on this PC.</span></span>
    </a>
    <a class="card q" href="#/sites?new">
      <Icon name="sites" size={20} />
      <span><b>Create a site</b><span class="muted small">Give a folder or service a name like shop.localhost.</span></span>
    </a>
  </div>
</section>

<style>
  .stats {
    display: grid;
    grid-template-columns: repeat(5, 1fr);
    gap: 12px;
    margin-bottom: 26px;
  }
  .stat {
    display: grid;
    gap: 2px;
    padding: 14px 16px;
    text-decoration: none;
    color: var(--text);
  }
  a.stat:hover {
    border-color: var(--line-strong);
    background: var(--surface-2);
  }
  .n {
    font-size: 28px;
    font-weight: 600;
    line-height: 1.1;
    letter-spacing: -0.02em;
  }
  .n.ok {
    color: var(--ok);
  }
  .small-n {
    font-size: 22px;
    line-height: 1.5;
  }
  .sect {
    margin-bottom: 28px;
  }
  .sect-head {
    display: flex;
    align-items: center;
    margin-bottom: 10px;
  }
  .cards {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(280px, 1fr));
    gap: 12px;
  }
  .svc {
    padding: 14px 16px;
    display: grid;
    gap: 10px;
    align-content: start;
  }
  .name {
    font-weight: 650;
    font-size: 15px;
    color: var(--text);
    text-decoration: none;
    overflow-wrap: anywhere;
  }
  .name:hover {
    text-decoration: underline;
  }
  .meta {
    display: flex;
    gap: 10px;
    align-items: center;
    flex-wrap: wrap;
  }
  .quick {
    display: grid;
    grid-template-columns: repeat(3, 1fr);
    gap: 12px;
  }
  .q {
    display: flex;
    gap: 12px;
    padding: 14px 16px;
    text-decoration: none;
    color: var(--text);
    align-items: flex-start;
  }
  .q:hover {
    border-color: var(--accent);
  }
  .q :global(svg) {
    color: var(--accent);
    margin-top: 2px;
    flex: none;
  }
  .q span {
    display: grid;
    gap: 2px;
  }
  @media (max-width: 1100px) {
    .stats {
      grid-template-columns: repeat(3, 1fr);
    }
    .quick {
      grid-template-columns: 1fr;
    }
  }
</style>
