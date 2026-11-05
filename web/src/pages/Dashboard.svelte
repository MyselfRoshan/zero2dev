<script lang="ts">
  import { serviceUrl } from "../lib/actions";
  import { app, lists } from "../lib/store.svelte";
  import { serviceKindLabel, setupSteps } from "../lib/ux";
  import Icon from "../ui/Icon.svelte";
  import KindIcon from "../ui/KindIcon.svelte";
  import ServiceControls from "../ui/ServiceControls.svelte";
  import StateBadge from "../ui/StateBadge.svelte";

  const loaded = $derived(app.loaded.runtimes && app.loaded.services && app.loaded.sites);
  const setup = $derived(
    setupSteps({ runtimes: lists.runtimes.length, services: lists.services.length, sites: lists.sites.length }),
  );
  const running = $derived(lists.services.filter((s) => s.state === "running").length);
  const failed = $derived(lists.services.filter((s) => s.state === "failed").length);
</script>

{#if loaded && !setup.complete}
  <section class="card setup" aria-labelledby="setup-h" data-testid="setup">
    <div class="setup-head">
      <h2 id="setup-h">Get started</h2>
      <span class="muted small">{setup.steps.filter((s) => s.done).length} of {setup.steps.length} done</span>
    </div>
    <ol class="steps">
      {#each setup.steps as s, i (s.id)}
        <li class:done={s.done} class:current={setup.next?.id === s.id} data-step={s.id}>
          <span class="num" aria-hidden="true">{#if s.done}<Icon name="check" size={14} />{:else}{i + 1}{/if}</span>
          <div class="txt">
            <span class="t-row"><b>{s.title}</b>{#if s.optional}<span class="badge plain">optional</span>{/if}</span>
            {#if !s.done}<span class="muted small">{s.text}</span>{/if}
          </div>
          {#if s.done}
            <span class="sr-only">Done</span>
          {:else}
            <a class="btn sm {setup.next?.id === s.id ? 'primary' : ''}" href={s.href}>{s.action}</a>
          {/if}
        </li>
      {/each}
    </ol>
  </section>
{/if}

{#if lists.services.length > 0}
  <section class="sect" aria-labelledby="svc-h">
    <div class="sect-head">
      <h2 id="svc-h">Services</h2>
      <span class="muted small">
        {running} running{#if failed}, <span class="bad-t">{failed} failed</span>{/if}
      </span>
      <span class="spacer"></span>
      <a class="btn sm ghost" href="#/services">Manage services</a>
    </div>
    <div class="cards">
      {#each lists.services as s (s.id)}
        {@const url = serviceUrl(s)}
        <article class="card svc">
          <div class="row">
            <KindIcon kind={s.kind} size={18} label={serviceKindLabel(s.kind)} />
            <a class="name" href="#/services/{s.id}">{s.name}</a>
            <span class="spacer"></span>
            <StateBadge state={s.state} />
          </div>
          {#if s.lastError && s.state === "failed"}<p class="err">{s.lastError}</p>{/if}
          <div class="row">
            <ServiceControls service={s} />
            {#if url && s.state === "running"}
              <a class="btn sm ghost" href={url} target="_blank" rel="noreferrer">Open <Icon name="external" size={12} /></a>
            {/if}
            <span class="spacer"></span>
            {#if url}<span class="mono muted small">:{s.port}</span>{/if}
          </div>
        </article>
      {/each}
    </div>
  </section>
{/if}

{#if lists.sites.length > 0}
  <section class="sect" aria-labelledby="site-h">
    <div class="sect-head">
      <h2 id="site-h">Sites</h2>
      <span class="spacer"></span>
      <a class="btn sm ghost" href="#/sites">Manage sites</a>
    </div>
    <ul class="card sites">
      {#each lists.sites as s (s.id)}
        <li>
          <span class="grow">{s.name}</span>
          {#if s.enabled}
            <a class="mono" href={s.url} target="_blank" rel="noreferrer">{s.url} <Icon name="external" size={12} /></a>
          {:else}
            <span class="muted small">disabled</span>
          {/if}
        </li>
      {/each}
    </ul>
  </section>
{/if}

<style>
  .setup {
    padding: 18px 20px;
    margin-bottom: 26px;
  }
  .setup-head {
    display: flex;
    align-items: baseline;
    gap: 12px;
    margin-bottom: 12px;
  }
  .steps {
    list-style: none;
    margin: 0;
    padding: 0;
    display: grid;
    gap: 4px;
  }
  .steps li {
    display: flex;
    align-items: center;
    gap: 14px;
    padding: 10px 12px;
    border-radius: var(--radius);
  }
  .steps li.current {
    background: var(--accent-soft);
  }
  .num {
    width: 26px;
    height: 26px;
    flex: none;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    border-radius: 50%;
    border: 1.5px solid var(--line-strong);
    font-weight: 650;
    font-size: 13px;
    color: var(--muted);
    background: var(--surface);
  }
  .current .num {
    border-color: var(--accent);
    color: var(--accent);
  }
  .done .num {
    border-color: var(--ok);
    background: var(--ok);
    color: #fff;
  }
  .done .txt b {
    color: var(--muted);
    text-decoration: line-through;
    font-weight: 550;
  }
  .txt {
    display: grid;
    gap: 2px;
    flex: 1;
    min-width: 0;
  }
  .t-row {
    display: flex;
    align-items: center;
    gap: 8px;
  }
  .sect {
    margin-bottom: 28px;
  }
  .sect-head {
    display: flex;
    align-items: baseline;
    gap: 10px;
    margin-bottom: 10px;
  }
  .bad-t {
    color: var(--bad);
  }
  .cards {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(280px, 1fr));
    gap: 12px;
  }
  .svc {
    padding: 14px 16px;
    display: grid;
    gap: 12px;
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
  .sites {
    list-style: none;
    margin: 0;
    padding: 4px 16px;
  }
  .sites li {
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 10px 0;
    border-bottom: 1px solid var(--line);
  }
  .sites li:last-child {
    border-bottom: 0;
  }
</style>
