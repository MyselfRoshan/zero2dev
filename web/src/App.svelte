<script lang="ts">
  import { onMount } from "svelte";
  import { app, forgetToken, lists, retryConnection, startApp } from "./lib/store.svelte";
  import { router } from "./lib/router.svelte";
  import Icon from "./ui/Icon.svelte";
  import ThemeToggle from "./ui/ThemeToggle.svelte";
  import Toasts from "./ui/Toasts.svelte";
  import FirstRun from "./pages/FirstRun.svelte";
  import Dashboard from "./pages/Dashboard.svelte";
  import Services from "./pages/Services.svelte";
  import ServiceDetail from "./pages/ServiceDetail.svelte";
  import ServiceEditor from "./pages/ServiceEditor.svelte";
  import Runtimes from "./pages/Runtimes.svelte";
  import Sites from "./pages/Sites.svelte";
  import Logs from "./pages/Logs.svelte";
  import Settings from "./pages/Settings.svelte";
  import Help from "./pages/Help.svelte";
  import TourGuide from "./ui/TourGuide.svelte";
  import ConfirmDialog from "./ui/ConfirmDialog.svelte";
  import { quitDaemon } from "./lib/store.svelte";
  let quitting = $state(false);
  let quitBusy = $state(false);
  const runningNames = $derived(lists.services.filter((s) => s.state === "running" || s.state === "starting").map((s) => s.name));
  async function confirmQuit() {
    quitBusy = true;
    await quitDaemon();
    quitBusy = false;
    quitting = false;
  }

  onMount(startApp);

  const nav = [
    { href: "#/dashboard", name: "dashboard", label: "Dashboard", icon: "dashboard" },
    { href: "#/services", name: "services", label: "Services", icon: "services" },
    { href: "#/runtimes", name: "runtimes", label: "Runtimes", icon: "runtimes" },
    { href: "#/sites", name: "sites", label: "Sites", icon: "sites" },
    { href: "#/logs", name: "logs", label: "Logs", icon: "logs" },
    { href: "#/settings", name: "settings", label: "Settings", icon: "settings" },
    { href: "#/help", name: "help", label: "Help", icon: "help" },
  ];

  const section = $derived(router.route.name.startsWith("service") ? "services" : router.route.name);
  const title = $derived.by(() => {
    const r = router.route;
    if (r.name === "service") return lists.services.find((s) => s.id === r.id)?.name ?? "Service";
    if (r.name === "serviceNew") return "New service";
    if (r.name === "serviceEdit") return `Edit ${lists.services.find((s) => s.id === r.id)?.name ?? "service"}`;
    return nav.find((n) => n.name === r.name)?.label ?? "";
  });
  $effect(() => {
    document.title = `${title} - Zero2Dev`;
  });

  const running = $derived(lists.services.filter((s) => s.state === "running").length);
  const connLabel = $derived(
    app.conn === "unauthorized" ? "Unauthorized" : app.conn === "offline" ? "Daemon offline" : app.stream === "open" ? "Connected" : "Connecting",
  );
  const connClass = $derived(app.conn === "online" && app.stream === "open" ? "ok" : app.conn === "online" ? "warn" : "bad");
</script>

{#if app.stopped}
  <main class="stopped" aria-labelledby="stopped-h">
    <div class="stopped-card">
      <svg width="40" height="40" viewBox="0 0 32 32" aria-hidden="true"><rect width="32" height="32" rx="7" fill="var(--text)" /><path d="M9 10h9l-9 12h14" fill="none" stroke="var(--bg)" stroke-width="3" stroke-linecap="square" /></svg>
      <h1 id="stopped-h">Zero2Dev has stopped</h1>
      <p>Your services were stopped cleanly. Start <b>zero2dev.exe</b> again to continue; this page reconnects when you open the link it opens.</p>
      <p class="muted small">You can close this tab.</p>
    </div>
  </main>
{:else if app.phase === "notoken"}
  <FirstRun />
{:else}
  <div class="shell">
    <aside class="side">
      <a class="brand" href="#/dashboard" aria-label="Zero2Dev home">
        <svg width="22" height="22" viewBox="0 0 32 32" aria-hidden="true"><rect width="32" height="32" rx="7" fill="var(--text)" /><path d="M9 10h9l-9 12h14" fill="none" stroke="var(--bg)" stroke-width="3" stroke-linecap="square" /></svg>
        <span class="word">zero<b>2</b>dev</span>
      </a>
      <nav aria-label="Main">
        {#each nav as n (n.name)}
          <a href={n.href} class="nav" aria-current={section === n.name ? "page" : undefined} data-tour="nav-{n.name}" aria-label={n.label} title={n.label}>
            <Icon name={n.icon} />
            <span>{n.label}</span>
            {#if n.name === "services" && running > 0}<span class="count" aria-label="{running} running">{running}</span>{/if}
          </a>
        {/each}
      </nav>
      <div class="conn {connClass}" role="status">
        <span class="pip"></span>
        <div>
          <div class="conn-l">{connLabel}</div>
          <div class="conn-s mono">{app.status ? `v${app.status.version}` : "127.0.0.1"}</div>
        </div>
      </div>
      <button class="quit" type="button" onclick={() => (quitting = true)} title="Stop all services and close Zero2Dev" aria-label="Quit Zero2Dev"><Icon name="power" size={15} /><span>Quit Zero2Dev</span></button>
    </aside>

    <div class="main">
      <header class="top">
        <h1>{title}</h1>
        <span class="spacer"></span>
        {#if router.route.name !== "help"}<a class="btn sm ghost help-link" href="#/help" title="Tutorials and help"><Icon name="help" size={15} /> Help</a>{/if}
        <ThemeToggle />
      </header>

      {#if app.conn !== "online"}
        <div class="banner bad" role="alert">
          <Icon name="alert" />
          <span class="grow">
            {#if app.conn === "unauthorized"}
              Unauthorized. The saved access token was rejected. Open the link printed by the daemon again, or enter the token.
            {:else}
              Daemon offline. The UI cannot reach Zero2Dev on this machine, so the lists below may be out of date.
            {/if}
          </span>
          <button class="btn sm" onclick={retryConnection}>Retry</button>
          {#if app.conn === "unauthorized"}<button class="btn sm" onclick={forgetToken}>Enter token</button>{/if}
        </div>
      {/if}

      <main class="content" id="main" tabindex="-1">
        {#key "id" in router.route ? `${router.route.name}:${router.route.id}` : router.route.name}
          {#if router.route.name === "dashboard"}
            <Dashboard />
          {:else if router.route.name === "services"}
            <Services />
          {:else if router.route.name === "service"}
            <ServiceDetail id={router.route.id} />
          {:else if router.route.name === "serviceNew"}
            <ServiceEditor />
          {:else if router.route.name === "serviceEdit"}
            <ServiceEditor id={router.route.id} />
          {:else if router.route.name === "runtimes"}
            <Runtimes />
          {:else if router.route.name === "sites"}
            <Sites />
          {:else if router.route.name === "logs"}
            <Logs />
          {:else if router.route.name === "settings"}
            <Settings />
          {:else if router.route.name === "help"}
            <Help />
          {/if}
        {/key}
      </main>
    </div>
  </div>
{/if}
<Toasts />
{#if !app.stopped && app.phase === "ready"}<TourGuide />{/if}
{#if quitting}
  <ConfirmDialog
    title="Quit Zero2Dev?"
    message={runningNames.length
      ? `This stops ${runningNames.length === 1 ? "the running service" : `the ${runningNames.length} running services`} (${runningNames.join(", ")}) and closes Zero2Dev. Start zero2dev.exe again to come back.`
      : "This closes Zero2Dev. Start zero2dev.exe again to come back."}
    confirmLabel="Quit Zero2Dev"
    busy={quitBusy}
    onconfirm={confirmQuit}
    oncancel={() => (quitting = false)}
  />
{/if}

<style>
  .shell {
    display: grid;
    grid-template-columns: var(--sidebar) 1fr;
    height: 100%;
  }
  .side {
    display: flex;
    flex-direction: column;
    gap: 16px;
    padding: 16px 8px;
    background: var(--surface);
    border-right: 1px solid var(--line);
    min-height: 0;
  }
  .brand {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 4px 16px;
    color: var(--text);
    text-decoration: none;
  }
  .word {
    font-family: var(--mono);
    font-size: 16px;
    font-weight: 600;
    letter-spacing: -0.02em;
  }
  .word b {
    color: var(--accent-text);
    font-weight: 600;
  }
  nav {
    display: grid;
    gap: 4px;
  }
  .nav {
    position: relative;
    display: flex;
    align-items: center;
    gap: 10px;
    height: 40px;
    padding: 0 16px;
    border-radius: var(--radius-lg);
    color: var(--text);
    text-decoration: none;
    font-weight: 400;
    transition:
      color var(--motion-mid) var(--ease-in-out),
      background var(--motion-mid) var(--ease-in-out);
  }
  .nav:hover {
    background: var(--fill);
    color: var(--text);
    text-decoration: none;
  }
  .nav[aria-current="page"] {
    background: var(--accent-soft);
    color: var(--accent-text);
  }
  .count {
    margin-left: auto;
    min-width: 20px;
    padding: 0 6px;
    text-align: center;
    border-radius: 100px;
    font-size: 12px;
    line-height: 20px;
    background: var(--ok-solid);
    color: var(--ok-ink);
  }
  .conn {
    margin-top: auto;
    display: flex;
    gap: 10px;
    align-items: center;
    padding: 8px 12px;
    border: 1px solid var(--line);
    border-radius: var(--radius-lg);
    background: var(--surface-2);
  }
  .quit {
    display: flex;
    align-items: center;
    gap: 8px;
    margin-top: 4px;
    height: 40px;
    padding: 0 16px;
    border: 0;
    border-radius: var(--radius-lg);
    background: none;
    color: var(--muted);
    font: inherit;
    font-size: 14px;
    cursor: pointer;
    text-align: left;
  }
  .quit:hover {
    background: var(--bad-soft);
    color: var(--bad);
  }
  .quit:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 1px;
  }
  .stopped {
    min-height: 100%;
    display: grid;
    place-items: center;
    padding: 24px;
    background: var(--bg);
  }
  .stopped-card {
    max-width: 440px;
    text-align: center;
    display: grid;
    gap: 8px;
    justify-items: center;
    padding: 32px 24px;
    border: 1px solid var(--line);
    border-radius: var(--radius-lg);
    background: var(--surface);
  }
  .stopped-card h1 {
    font-size: 20px;
    margin: 6px 0 0;
  }
  .stopped-card p {
    margin: 0;
  }
  .conn-l {
    font-weight: 600;
    font-size: 14px;
  }
  .conn-s {
    font-size: 12px;
    color: var(--muted);
  }
  .pip {
    width: 10px;
    height: 10px;
    border-radius: 50%;
    background: var(--idle);
    flex: none;
  }
  .conn.ok .pip {
    background: var(--ok-fill);
    box-shadow: 0 0 0 3px var(--ok-soft);
  }
  .conn.warn .pip {
    background: var(--warn-fill);
    box-shadow: 0 0 0 3px var(--warn-soft);
  }
  .conn.bad .pip {
    border-radius: 2px;
    transform: rotate(45deg);
    background: var(--bad-fill);
    box-shadow: 0 0 0 3px var(--bad-soft);
  }
  .main {
    display: flex;
    flex-direction: column;
    min-width: 0;
    min-height: 0;
  }
  .top {
    display: flex;
    align-items: center;
    height: 56px;
    flex: none;
    padding: 0 24px;
    border-bottom: 1px solid var(--line);
    background: var(--surface);
  }
  .content {
    flex: 1;
    min-height: 0;
    overflow: auto;
    padding: 24px 24px 40px;
  }
  .content:focus {
    outline: none;
  }
  @media (max-width: 1000px) {
    .shell {
      --sidebar: 64px;
    }
    .nav span:not(.count),
    .brand .word,
    .conn > div,
    .quit span {
      display: none;
    }
    .quit {
      justify-content: center;
    }
    .nav {
      justify-content: center;
      padding: 0;
    }
    .count {
      position: absolute;
      top: 2px;
      right: 4px;
      min-width: 16px;
      font-size: 10px;
      padding: 0 3px;
    }
    .conn {
      justify-content: center;
      padding: 10px 0;
    }
    .brand {
      justify-content: center;
    }
    .content {
      padding: 18px 16px 32px;
    }
    .top {
      padding: 0 16px;
    }
  }
</style>
