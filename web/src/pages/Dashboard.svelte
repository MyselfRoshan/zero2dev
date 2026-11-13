<script lang="ts">
  // The dashboard: what runs right now and what to do next, every part clickable.
  import { onMount } from "svelte";
  import { api } from "../lib/api";
  import { isActive, serviceUrl, startService, stopService } from "../lib/actions";
  import { lastLine, overview, recentActivity, statusSentence } from "../lib/dashboard";
  import { fmtTime, since } from "../lib/format";
  import { navigate } from "../lib/router.svelte";
  import { isSiteServerService, siteServer } from "../lib/sites";
  import { app, attempt, lists, loadSites, logs, seedLogs } from "../lib/store.svelte";
  import { startTour } from "../lib/tour.svelte";
  import { SETUP_TOUR } from "../lib/tours";
  import type { Service, Site } from "../lib/types";
  import { serviceKindLabel, setupSteps } from "../lib/ux";
  import Icon from "../ui/Icon.svelte";
  import KindIcon from "../ui/KindIcon.svelte";
  import ServiceControls from "../ui/ServiceControls.svelte";
  import StateBadge from "../ui/StateBadge.svelte";

  const loaded = $derived(app.loaded.runtimes && app.loaded.services && app.loaded.sites);
  const setup = $derived(
    setupSteps({ runtimes: lists.runtimes.length, services: lists.services.length, sites: lists.sites.length }),
  );
  const o = $derived(overview(lists, app.settings));
  const sentence = $derived(statusSentence(o));
  const activity = $derived(recentActivity(logs.byService, lists.services, 8));
  const fresh = $derived(loaded && lists.runtimes.length === 0 && lists.services.length === 0);

  // Uptimes and "x min ago" move on by themselves.
  let now = $state(Date.now());
  onMount(() => {
    const t = setInterval(() => (now = Date.now()), 15000);
    return () => clearInterval(t);
  });
  // The latest output of each service, for its card and the activity list.
  let seeded = false;
  $effect(() => {
    if (seeded || !app.loaded.services) return;
    seeded = true;
    for (const s of lists.services.slice(0, 12)) if (!logs.byService[s.id]) void seedLogs(s.id, 20);
  });

  const server = $derived(siteServer(app.settings));
  const stoppable = $derived(lists.services.filter((s) => isActive(s)));
  const startable = $derived(lists.services.filter((s) => !isActive(s) && s.state !== "stopping"));
  let bulk = $state(false);
  async function all(start: boolean) {
    bulk = true;
    for (const s of start ? startable : stoppable) await (start ? startService(s) : stopService(s));
    bulk = false;
  }

  function siteLive(s: Site): boolean {
    const serverUp = lists.services.some((x) => isSiteServerService(x, server) && x.state === "running");
    if (!serverUp || !s.enabled) return false;
    if (s.type === "proxy" && s.serviceId) return lists.services.find((x) => x.id === s.serviceId)?.state === "running";
    return true;
  }
  async function toggleSite(s: Site, on: boolean) {
    const r = await attempt(() => (on ? api.enableSite(s.id) : api.disableSite(s.id)), `${s.name} ${on ? "enabled" : "disabled"}.`);
    if (r) await loadSites();
  }
  const uptime = (s: Service) => (s.state === "running" && s.startedAt ? `up ${since(s.startedAt, now)}` : "");
  const dotClass = (st: string) => (st === "running" ? "ok" : st === "failed" ? "bad" : st === "starting" || st === "stopping" ? "warn" : "idle");
</script>

<div class="dash">
  <div class="head">
    <div class="grow">
      <p class="lede" data-testid="status-sentence">{loaded ? sentence : "Loading..."}</p>
    </div>
    <div class="row wrap actions">
      <!-- one primary action per view (antd): the tour when new, the next setup step, else New service -->
      <button class="btn" class:primary={loaded && setup.complete} onclick={() => navigate("#/services/new")}><Icon name="plus" /> New service</button>
      <a class="btn" href="#/sites?new"><Icon name="sites" /> New site</a>
      <a class="btn" href="#/runtimes"><Icon name="download" /> Install a runtime</a>
    </div>
  </div>

  {#if fresh}
    <section class="card welcome" aria-labelledby="welcome-h" data-tour="overview">
      <div class="w-ic" aria-hidden="true"><Icon name="rocket" size={26} /></div>
      <div class="grow">
        <h2 id="welcome-h">Welcome to Zero2Dev</h2>
        <p class="muted">Run your apps, databases and local sites from one place, with nothing installed system-wide. A short tour shows you around.</p>
      </div>
      <div class="row wrap">
        <button class="btn primary" onclick={() => startTour("tour")}><Icon name="compass" /> Take the 1-minute tour</button>
        <button class="btn" onclick={() => startTour("runtime")}>Install your first runtime</button>
      </div>
    </section>
  {/if}

  {#if loaded && !setup.complete}
    <section class="card setup" aria-labelledby="setup-h" data-testid="setup">
      <div class="setup-head">
        <h2 id="setup-h">Get started</h2>
        <span class="muted small">{setup.steps.filter((s) => s.done).length} of {setup.steps.length} done</span>
        <span class="spacer"></span>
        <a class="small" href="#/help">All tutorials</a>
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
              <div class="row">
                {#if SETUP_TOUR[s.id]}<button class="btn sm ghost" type="button" onclick={() => startTour(SETUP_TOUR[s.id])} aria-label="Show me how: {s.title}"><Icon name="playCircle" size={14} /> Show me</button>{/if}
                <a class="btn sm {setup.next?.id === s.id && !fresh ? 'primary' : ''}" href={s.href}>{s.action}</a>
              </div>
            {/if}
          </li>
        {/each}
      </ol>
    </section>
  {/if}

  {#if !fresh}
  <div class="tiles" data-tour="overview">
    <a class="tile" href="#/services" data-testid="tile-services">
      <span class="t-ic"><Icon name="services" size={18} /></span>
      <span class="t-label">Services</span>
      <span class="t-num">{o.services.running}<span class="t-of">/ {o.services.total} running</span></span>
      <span class="dots" aria-hidden="true">{#each o.services.states.slice(0, 24) as st, i (i)}<span class="d {dotClass(st)}"></span>{/each}</span>
      {#if o.services.failed}<span class="t-sub bad-t">{o.services.failed} failed</span>{:else}<span class="t-sub">{o.services.total ? "All fine" : "None yet"}</span>{/if}
    </a>
    <a class="tile" href="#/sites" data-testid="tile-sites">
      <span class="t-ic"><Icon name="sites" size={18} /></span>
      <span class="t-label">Sites</span>
      <span class="t-num">{o.sites.enabled}<span class="t-of">/ {o.sites.total} on</span></span>
      <span class="t-sub"><span class="d {o.sites.serverRunning ? 'ok' : 'idle'}"></span>{o.sites.server} {o.sites.serverRunning ? "is serving" : o.sites.serverInstalled ? "is stopped" : "not installed"}</span>
    </a>
    <a class="tile" href="#/runtimes?filter=installed" data-testid="tile-runtimes">
      <span class="t-ic"><Icon name="runtimes" size={18} /></span>
      <span class="t-label">Runtimes</span>
      <span class="t-num">{o.runtimes.kinds}<span class="t-of">installed</span></span>
      {#if o.runtimes.updates}<span class="t-sub acc-t">{o.runtimes.updates} update{o.runtimes.updates === 1 ? "" : "s"} available</span>{:else}<span class="t-sub">{o.runtimes.versions} version{o.runtimes.versions === 1 ? "" : "s"} in total</span>{/if}
    </a>
    <a class="tile" href="#/runtimes?filter=databases" data-testid="tile-databases">
      <span class="t-ic"><Icon name="database" size={18} /></span>
      <span class="t-label">Databases</span>
      <span class="t-num">{o.databases.running}<span class="t-of">/ {o.databases.total} running</span></span>
      <span class="t-sub">{o.databases.total ? "MariaDB, MySQL, PostgreSQL, Redis" : "Add one as a service"}</span>
    </a>
  </div>
  {/if}

  <div class="cols">
    <section class="col-main" aria-labelledby="svc-h">
      <div class="sect-head">
        <h2 id="svc-h">Services</h2>
        <span class="spacer"></span>
        {#if lists.services.length > 1}
          <button class="btn sm ghost" type="button" disabled={bulk || startable.length === 0} onclick={() => all(true)}><Icon name="play" size={14} /> Start all</button>
          <button class="btn sm ghost" type="button" disabled={bulk || stoppable.length === 0} onclick={() => all(false)}><Icon name="stop" size={14} /> Stop all</button>
        {/if}
        <a class="small" href="#/services">Manage services</a>
      </div>
      {#if lists.services.length === 0}
        <div class="card empty-s">
          <p class="muted">No services yet. A service is one of your apps or a database that Zero2Dev runs for you.</p>
          <div class="row wrap">
            <a class="btn" href="#/services/new"><Icon name="plus" /> Add a service</a>
            <button class="btn" type="button" onclick={() => startTour("service")}><Icon name="playCircle" size={14} /> Show me how</button>
          </div>
        </div>
      {:else}
        <div class="cards">
          {#each lists.services as s (s.id)}
            {@const url = serviceUrl(s)}
            {@const line = lastLine(logs.byService[s.id])}
            <article class="card svc" class:failed={s.state === "failed"} class:up={s.state === "running"} data-service={s.name}>
              <div class="svc-head">
                <KindIcon kind={s.kind} size={20} />
                <a class="nm" href="#/services/{s.id}" title="Open {s.name}">{s.name}</a>
                <StateBadge state={s.state} />
              </div>
              <div class="meta muted small">
                {#if serviceKindLabel(s.kind) !== s.name}<span>{serviceKindLabel(s.kind)}</span>{/if}
                {#if s.port > 0}<span class="mono">:{s.port}</span>{/if}
                {#if uptime(s)}<span>{uptime(s)}</span>{/if}
              </div>
              {#if s.state === "failed" && s.lastError}
                <p class="err small">{s.lastError}</p>
              {:else if line}
                <p class="last mono" class:stderr={line.stream === "stderr"} title={line.text}>{line.text}</p>
              {:else}
                <p class="none muted small">{s.state === "running" ? "Running, no output yet." : "Not running."}</p>
              {/if}
              <div class="svc-acts">
                <ServiceControls service={s} showRestart />
                <span class="spacer"></span>
                {#if url && s.state === "running"}<a class="btn sm ghost icon" href={url} target="_blank" rel="noreferrer" aria-label="Open {s.name} in the browser" title="Open in the browser"><Icon name="external" size={15} /></a>{/if}
                {#if s.state === "failed"}
                  <a class="btn sm ghost" href="#/logs?service={s.id}" aria-label="Logs of {s.name}"><Icon name="logs" size={14} /> See logs</a>
                {:else}
                  <a class="btn sm ghost icon" href="#/logs?service={s.id}" aria-label="Logs of {s.name}" title="Logs"><Icon name="logs" size={15} /></a>
                {/if}
              </div>
            </article>
          {/each}
        </div>
      {/if}
    </section>

    <aside class="col-side">
      <section aria-labelledby="site-h">
        <div class="sect-head">
          <h2 id="site-h">Sites</h2>
          <span class="spacer"></span>
          <a class="small" href="#/sites">Manage sites</a>
        </div>
        <div class="card list">
          {#each lists.sites as s (s.id)}
            <div class="site" class:off={!s.enabled}>
              <span class="d {siteLive(s) ? 'ok' : 'idle'}" title={siteLive(s) ? "Reachable" : s.enabled ? "Not reachable: its server or service is stopped" : "Disabled"}></span>
              <div class="grow site-t">
                <span class="nm">{s.name}</span>
                {#if s.enabled}<a class="mono small" href={s.url} target="_blank" rel="noreferrer">{s.url.replace(/^https?:\/\//, "")}</a>{:else}<span class="mono small muted">{s.url.replace(/^https?:\/\//, "")}</span>{/if}
              </div>
              <input type="checkbox" role="switch" class="switch" checked={s.enabled} onchange={(e) => toggleSite(s, e.currentTarget.checked)} aria-label="{s.enabled ? 'Disable' : 'Enable'} {s.name}" />
            </div>
          {:else}
            <div class="empty-l">
              <p class="muted small">Give a service an address like shop.localhost.</p>
              <a class="btn sm" href="#/sites?new">New site</a>
            </div>
          {/each}
        </div>
      </section>

      <section aria-labelledby="act-h">
        <div class="sect-head">
          <h2 id="act-h">Recent activity</h2>
          <span class="spacer"></span>
          <a class="small" href="#/logs">All logs</a>
        </div>
        <div class="card list activity" data-testid="activity">
          {#each activity as a (a.serviceId + a.seq)}
            <a class="act {a.stream}" href="#/services/{a.serviceId}">
              <span class="when mono">{fmtTime(a.ts)}</span>
              <span class="who">{a.serviceName}</span>
              <span class="what mono" title={a.text}>{a.text}</span>
            </a>
          {:else}
            <p class="muted small empty-l">Output of your services shows up here while they run.</p>
          {/each}
        </div>
      </section>
    </aside>
  </div>
</div>

<style>
  .dash {
    display: grid;
    gap: 18px;
    max-width: 1240px;
  }
  .head {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 10px 16px;
  }
  .lede {
    margin: 0;
    font-size: 16px;
    color: var(--text);
  }
  .welcome {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 16px;
    padding: 18px 20px;
    background: linear-gradient(120deg, var(--accent-soft), var(--surface) 70%);
  }
  .welcome h2 {
    margin: 0 0 4px;
    font-size: 16px;
  }
  .welcome p {
    margin: 0;
  }
  .w-ic {
    width: 52px;
    height: 52px;
    border-radius: var(--radius-lg);
    display: grid;
    place-items: center;
    background: var(--accent-solid);
    color: var(--accent-ink);
  }
  .tiles {
    display: grid;
    grid-template-columns: repeat(4, minmax(0, 1fr));
    gap: 12px;
  }
  .tile {
    display: grid;
    grid-template-columns: auto 1fr;
    grid-template-areas: "ic label" "num num" "dots dots" "sub sub";
    align-items: center;
    gap: 4px 10px;
    padding: 14px 16px;
    border: 1px solid var(--line);
    border-radius: var(--radius-lg);
    background: var(--surface);
    color: var(--text);
    text-decoration: none;
    transition:
      border-color var(--motion-mid) var(--ease-in-out),
      transform var(--motion-mid) var(--ease-in-out);
  }
  .tile:hover {
    border-color: var(--accent);
    transform: translateY(-1px);
  }
  .tile:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 2px;
  }
  .t-ic {
    grid-area: ic;
    width: 30px;
    height: 30px;
    border-radius: 8px;
    display: grid;
    place-items: center;
    background: var(--accent-soft);
    color: var(--accent-text);
  }
  .t-label {
    grid-area: label;
    font-weight: 600;
    font-size: 14px;
    color: var(--muted);
  }
  .t-num {
    grid-area: num;
    font-size: 24px;
    font-weight: 600;
    line-height: 1.1;
    margin-top: 6px;
  }
  .t-of {
    font-size: 14px;
    font-weight: 400;
    color: var(--muted);
    margin-left: 6px;
  }
  .dots {
    grid-area: dots;
    display: flex;
    flex-wrap: wrap;
    gap: 4px;
    min-height: 8px;
  }
  .t-sub {
    grid-area: sub;
    display: flex;
    align-items: center;
    gap: 6px;
    font-size: 12px;
    color: var(--muted);
  }
  .bad-t {
    color: var(--bad);
  }
  .acc-t {
    color: var(--accent-text);
  }
  .d {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    flex: none;
    background: var(--idle);
  }
  .d.ok {
    background: var(--ok-fill);
  }
  .d.bad {
    background: var(--bad-fill);
  }
  .d.warn {
    background: var(--warn-fill);
  }
  .cols {
    display: grid;
    grid-template-columns: minmax(0, 1fr) 360px;
    gap: 18px;
    align-items: start;
  }
  .col-side {
    display: grid;
    gap: 18px;
  }
  .sect-head {
    display: flex;
    align-items: center;
    gap: 8px;
    margin-bottom: 10px;
  }
  .sect-head h2 {
    font-size: 16px;
    margin: 0;
  }
  .cards {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(280px, 1fr));
    gap: 12px;
  }
  .svc {
    display: grid;
    gap: 6px;
    padding: 12px 16px;
  }
  .svc.failed {
    border-color: var(--bad-border);
  }
  .svc-head {
    display: flex;
    align-items: center;
    gap: 8px;
  }
  .svc-head .nm {
    flex: 1;
    min-width: 0;
    font-weight: 600;
    color: var(--text);
    text-decoration: none;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .svc-head .nm:hover {
    text-decoration: underline;
  }
  .meta {
    display: flex;
    flex-wrap: wrap;
    gap: 4px 10px;
  }
  .last {
    margin: 0;
    padding: 5px 8px;
    border-radius: var(--radius-sm);
    background: var(--sunken);
    font-size: 11.5px;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .last.stderr {
    color: var(--bad);
  }
  .none {
    margin: 0;
    padding: 5px 0;
  }
  .svc .err {
    margin: 0;
  }
  .svc-acts {
    display: flex;
    flex-wrap: nowrap;
    align-items: center;
    gap: 6px;
  }
  .empty-s {
    padding: 16px;
    display: grid;
    gap: 10px;
  }
  .empty-s p {
    margin: 0;
  }
  .list {
    overflow: hidden;
  }
  .site {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 10px 14px;
    border-top: 1px solid var(--line);
  }
  .site:first-child {
    border-top: 0;
  }
  .site.off .nm {
    color: var(--muted);
  }
  .site-t {
    display: grid;
    min-width: 0;
  }
  .site-t .nm {
    font-weight: 600;
    font-size: 14px;
  }
  .site-t a,
  .site-t span.mono {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .empty-l {
    padding: 14px;
    display: grid;
    gap: 8px;
    justify-items: start;
    margin: 0;
  }
  .empty-l p {
    margin: 0;
  }
  .act {
    display: grid;
    grid-template-columns: auto auto minmax(0, 1fr);
    gap: 8px;
    padding: 6px 14px;
    border-top: 1px solid var(--line);
    color: var(--text);
    text-decoration: none;
    font-size: 12px;
    align-items: baseline;
  }
  .act:first-child {
    border-top: 0;
  }
  .act:hover {
    background: var(--surface-2);
  }
  .when {
    color: var(--muted);
    font-size: 11.5px;
  }
  .who {
    font-weight: 600;
    white-space: nowrap;
  }
  .what {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-size: 11.5px;
  }
  .act.stderr .what {
    color: var(--bad);
  }
  .act.system .what {
    color: var(--accent-text);
  }
  /* setup checklist (kept from before) */
  .setup {
    padding: 14px 18px;
  }
  .setup-head {
    display: flex;
    align-items: baseline;
    gap: 10px;
    margin-bottom: 10px;
  }
  .setup-head h2 {
    font-size: 16px;
    margin: 0;
  }
  .steps {
    list-style: none;
    margin: 0;
    padding: 0;
    display: grid;
    gap: 8px;
  }
  .steps li {
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 8px 10px;
    border-radius: 8px;
  }
  .steps li.current {
    background: var(--accent-soft);
  }
  .num {
    width: 26px;
    height: 26px;
    border-radius: 50%;
    display: grid;
    place-items: center;
    flex: none;
    font-weight: 600;
    font-size: 12px;
    border: 1px solid var(--line-strong);
    background: var(--surface);
  }
  li.done .num {
    background: var(--ok-solid);
    border-color: var(--ok-solid);
    color: var(--ok-ink);
  }
  li.done b {
    color: var(--muted);
    text-decoration: line-through;
  }
  .txt {
    flex: 1;
    display: grid;
    gap: 2px;
    min-width: 0;
  }
  .t-row {
    display: flex;
    gap: 8px;
    align-items: center;
  }
  @media (max-width: 1100px) {
    .cols {
      grid-template-columns: minmax(0, 1fr);
    }
    .tiles {
      grid-template-columns: repeat(2, minmax(0, 1fr));
    }
  }
  @media (max-width: 560px) {
    .tiles {
      grid-template-columns: 1fr 1fr;
      gap: 8px;
    }
    .tile {
      padding: 12px;
    }
    .t-num {
      font-size: 20px;
    }
    .steps li {
      flex-wrap: wrap;
    }
  }
</style>
