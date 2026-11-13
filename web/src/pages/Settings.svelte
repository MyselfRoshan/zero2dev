<script lang="ts">
  import { api, getToken } from "../lib/api";
  import { app, attempt, forgetToken, toast } from "../lib/store.svelte";
  import { caRootPath, portableNote } from "../lib/installer";
  import { hasErrors, parsePort, validateSettings, type SettingsField } from "../lib/validate";
  import Icon from "../ui/Icon.svelte";
  import KindIcon from "../ui/KindIcon.svelte";
  import UnsavedBar from "../ui/UnsavedBar.svelte";
  import { nginxCaPath, serverLabel, siteServer } from "../lib/sites";
  import type { SiteServer } from "../lib/types";

  let suffix = $state("");
  let httpPort = $state("");
  let httpsPort = $state("");
  let openBrowser = $state(true);
  let proxy = $state("");
  let trustCA = $state(false);
  let server = $state<SiteServer>("caddy");
  let loadedFor = $state<object | null>(null);
  let errors = $state<Partial<Record<SettingsField, string>>>({});
  let saving = $state(false);
  let revealWarn = $state(false);

  // Fill the form once settings are available.
  $effect(() => {
    const s = app.settings;
    if (s && loadedFor !== s && !saving) {
      loadedFor = s;
      suffix = s.domainSuffix;
      httpPort = String(s.proxyHttpPort);
      httpsPort = String(s.proxyHttpsPort);
      openBrowser = s.openBrowserOnStart;
      proxy = s.downloadProxy ?? "";
      trustCA = s.trustLocalCA ?? false;
      server = siteServer(s);
    }
  });

  const dirty = $derived(
    !!app.settings &&
      (suffix !== app.settings.domainSuffix ||
        httpPort !== String(app.settings.proxyHttpPort) ||
        httpsPort !== String(app.settings.proxyHttpsPort) ||
        openBrowser !== app.settings.openBrowserOnStart ||
        proxy.trim() !== (app.settings.downloadProxy ?? "") ||
        trustCA !== (app.settings.trustLocalCA ?? false) ||
        server !== siteServer(app.settings)),
  );

  async function save(e: SubmitEvent) {
    e.preventDefault();
    if (!app.settings) return;
    const hp = parsePort(httpPort);
    const sp = parsePort(httpsPort);
    errors = validateSettings({ domainSuffix: suffix, proxyHttpPort: Number.isNaN(hp) ? -1 : hp, proxyHttpsPort: Number.isNaN(sp) ? -1 : sp,
      downloadProxy: proxy,
    });
    if (hasErrors(errors)) {
      queueMicrotask(() => {
        const el = document.querySelector<HTMLElement>("#settings-form [aria-invalid='true']");
        el?.scrollIntoView({ block: "center" });
        el?.focus();
      });
      return;
    }
    saving = true;
    const res = await attempt(
      () => api.putSettings({ ...app.settings!, domainSuffix: suffix.trim(), proxyHttpPort: hp, proxyHttpsPort: sp, openBrowserOnStart: openBrowser, downloadProxy: proxy.trim(), trustLocalCA: trustCA, siteServer: server }),
      "Settings saved.",
    );
    saving = false;
    if (res) app.settings = res;
  }

  async function copyText(text: string, what: string) {
    try {
      await navigator.clipboard.writeText(text);
      toast("success", `${what} copied.`);
    } catch {
      toast("error", "Could not copy to the clipboard in this browser context.");
    }
  }
  const dataDir = $derived(app.status?.homeDir ?? app.settings?.homeDir ?? "");
  const rootCert = $derived(dataDir ? caRootPath(dataDir) : "<data folder>\\caddy\\data\\caddy\\pki\\authorities\\local\\root.crt");
  const nginxCert = $derived(dataDir ? nginxCaPath(dataDir) : "<data folder>\\nginx\\certs\\ca.crt");
  const switching = $derived(!!app.settings && server !== siteServer(app.settings));

  /** Puts every field back to the saved settings. */
  function discard() {
    errors = {};
    loadedFor = null; // the effect above refills the form
  }

  // Sections and the one in view (for the side navigation).
  const SECTIONS = [
    { id: "set-sites", label: "Sites" },
    { id: "set-https", label: "HTTPS" },
    { id: "set-downloads", label: "Downloads" },
    { id: "set-startup", label: "Startup" },
    { id: "set-pc", label: "This PC" },
    { id: "set-access", label: "Access" },
  ];
  let current = $state("set-sites");
  function go(e: MouseEvent, id: string) {
    e.preventDefault(); // keep the #/settings route
    current = id;
    document.getElementById(id)?.scrollIntoView({ block: "start" });
  }
  // The highlighted section is the last one whose top is near the top of the page,
  // or the last section once the page is scrolled to the end.
  $effect(() => {
    const root = document.getElementById("main");
    if (!root) return;
    const onScroll = () => {
      const top = root.getBoundingClientRect().top;
      if (root.scrollTop + root.clientHeight >= root.scrollHeight - 4) {
        current = SECTIONS[SECTIONS.length - 1].id;
        return;
      }
      let id = SECTIONS[0].id;
      for (const s of SECTIONS) {
        const el = document.getElementById(s.id);
        if (el && el.getBoundingClientRect().top - top <= 96) id = s.id;
      }
      current = id;
    };
    root.addEventListener("scroll", onScroll, { passive: true });
    return () => root.removeEventListener("scroll", onScroll);
  });

  async function copyToken() {
    const t = getToken();
    if (!t) return toast("error", "No token is stored in this browser.");
    try {
      await navigator.clipboard.writeText(t);
      toast("success", "Access token copied. Treat it like a password.");
    } catch {
      toast("error", "Could not copy to the clipboard in this browser context.");
    }
  }
</script>

<div class="layout">
  <nav class="subnav" aria-label="Settings sections">
    {#each SECTIONS as s (s.id)}
      <a href="#/settings" class:on={current === s.id} onclick={(e) => go(e, s.id)}>{s.label}</a>
    {/each}
  </nav>

  <div class="panes">
    <form id="settings-form" class="panes" onsubmit={save} novalidate>
      <section class="section-card" id="set-sites" aria-labelledby="h-sites">
        <header><h2 id="h-sites">Sites</h2><p>How your sites at addresses like shop.{suffix.split(" ")[0] || "localhost"} are served.</p></header>
        <div class="body">
          <fieldset class="field server-pick">
            <legend class="label">Site server</legend>
            <div class="row wrap">
              {#each ["caddy", "nginx"] as const as k (k)}
                <label class="opt" class:on={server === k}>
                  <input type="radio" name="site-server" value={k} bind:group={server} />
                  <KindIcon kind={k} size={18} />
                  <span><b>{serverLabel(k)}</b><span class="muted small">{k === "caddy" ? "Default. Built-in local HTTPS." : "Uses an installed nginx runtime."}</span></span>
                </label>
              {/each}
            </div>
            {#if switching}
              <span class="hint" data-testid="server-switch-hint">On save, a running {serverLabel(siteServer(app.settings))} service is stopped and {serverLabel(server)} is started in its place. Both use the same ports, so only one can run.</span>
            {:else}
              <span class="hint">The program that serves your sites. Zero2Dev writes its configuration and creates its service when the runtime is installed.</span>
            {/if}
          </fieldset>
          <div class="field"><label>
            <span class="label">Domain suffix</span>
            <input type="text" class="mono" bind:value={suffix} aria-invalid={!!errors.domainSuffix} spellcheck="false" /></label>
            {#if errors.domainSuffix}<span class="err">{errors.domainSuffix}</span>{:else}<span class="hint">Site domains must end with it. <code>localhost</code> works in browsers with no setup; separate several suffixes with spaces.</span>{/if}
          </div>
          <div class="grid2">
            <div class="field"><label>
              <span class="label">HTTP proxy port</span>
              <input type="text" inputmode="numeric" class="mono" bind:value={httpPort} aria-invalid={!!errors.proxyHttpPort} /></label>
              {#if errors.proxyHttpPort}<span class="err">{errors.proxyHttpPort}</span>{/if}
            </div>
            <div class="field"><label>
              <span class="label">HTTPS proxy port</span>
              <input type="text" inputmode="numeric" class="mono" bind:value={httpsPort} aria-invalid={!!errors.proxyHttpsPort} /></label>
              {#if errors.proxyHttpsPort}<span class="err">{errors.proxyHttpsPort}</span>{/if}
            </div>
          </div>
          <span class="hint">Sites open at <code>http://name.{suffix.split(" ")[0] || "localhost"}:{httpPort || "8080"}</code>. Changing a port restarts a running {serverLabel(server)}.</span>
        </div>
      </section>

      <section class="section-card" id="set-https" aria-labelledby="h-https">
        <header><h2 id="h-https">HTTPS</h2><p>Whether browsers trust your local HTTPS sites.</p></header>
        <div class="body">
          {#if server === "nginx"}
            <div class="notice" data-testid="nginx-ca">
              <Icon name="key" />
              <div class="stack-s">
                <span>For nginx, Zero2Dev creates its own local certificate authority and one certificate that covers every HTTPS site. It is reissued when you add or remove HTTPS sites.</span>
                <span>Nothing is added to the Windows certificate store. To open HTTPS sites without a browser warning, trust <code class="path-inline">{nginxCert}</code> yourself.</span>
              </div>
            </div>
          {:else}
            <div class="switch-line">
              <label for="trust-ca" class="grow"><span class="label">Let Caddy trust its local certificate authority</span>
                <span class="hint">Adds Caddy's certificate authority to Windows, so local HTTPS sites open without a warning.</span></label>
              <input id="trust-ca" type="checkbox" role="switch" class="switch" bind:checked={trustCA} />
            </div>
            {#if trustCA}
              <div class="notice warn" id="ca-warn">
                <Icon name="alert" />
                <span>On save, Caddy changes the Windows certificate store, and Windows may show a security prompt. Turn this off again to leave Windows untouched.</span>
              </div>
            {:else}
              <details class="howto">
                <summary>Browsers show a certificate warning for HTTPS sites. <span class="linkish">How to trust it yourself</span></summary>
                <p class="small">Nothing outside the data folder is touched while this is off. Import this certificate into "Trusted Root Certification Authorities" to stop the warning, or keep clicking through it:</p>
                <div class="row">
                  <code class="dir grow">{rootCert}</code>
                  <button class="btn sm" type="button" onclick={() => copyText(rootCert, "Certificate path")}><Icon name="copy" size={14} /> Copy path</button>
                </div>
              </details>
            {/if}
          {/if}
        </div>
      </section>

      <section class="section-card" id="set-downloads" aria-labelledby="h-downloads">
        <header><h2 id="h-downloads">Downloads</h2><p>How Zero2Dev reaches the internet when it installs runtimes.</p></header>
        <div class="body">
          <div class="field">
            <label>
              <span class="label">Download proxy (optional)</span>
              <input type="text" class="mono" bind:value={proxy} aria-invalid={!!errors.downloadProxy} aria-describedby="proxy-help" placeholder="http://proxy.company.com:8080" spellcheck="false" autocomplete="off" />
            </label>
            {#if errors.downloadProxy}<span class="err">{errors.downloadProxy}</span>{:else}<span class="hint" id="proxy-help">Empty uses the HTTP_PROXY and HTTPS_PROXY environment variables. Zero2Dev does not read the Windows proxy setting, so company networks often need this.</span>{/if}
          </div>
        </div>
      </section>

      <section class="section-card" id="set-startup" aria-labelledby="h-startup">
        <header><h2 id="h-startup">Startup</h2></header>
        <div class="body">
          <div class="switch-line">
            <label for="open-browser" class="grow"><span class="label">Open this dashboard when Zero2Dev starts</span>
              <span class="hint">Opens your browser on the dashboard each time zero2dev.exe starts.</span></label>
            <input id="open-browser" type="checkbox" role="switch" class="switch" bind:checked={openBrowser} />
          </div>
        </div>
      </section>
    </form>

    <section class="section-card" id="set-pc" aria-labelledby="h-pc">
      <header><h2 id="h-pc">This PC</h2>{#if app.status?.portable}<span class="badge accent">portable</span>{/if}</header>
      <div class="body">
        <div class="field">
          <span class="label" id="dd-l">Data folder</span>
          <div class="row">
            <code class="dir grow" aria-labelledby="dd-l" data-testid="data-folder">{dataDir || "-"}</code>
            <button class="btn sm" type="button" onclick={() => copyText(dataDir, "Data folder path")} disabled={!dataDir}><Icon name="copy" size={14} /> Copy</button>
          </div>
          <span class="hint" data-testid="portable-note">{portableNote(app.status?.homeSource)}</span>
        </div>
        <dl class="facts">
          <div><dt>Version</dt><dd class="mono">{app.status?.version ?? "-"}</dd></div>
          <div><dt>API port</dt><dd class="mono">{app.settings?.apiPort ?? "-"}</dd></div>
          {#if app.status?.exeDir}<div class="wide"><dt>Program folder</dt><dd class="mono">{app.status.exeDir}</dd></div>{/if}
        </dl>
        <span class="hint">The API port is set with the <code>ZERO2DEV_PORT</code> environment variable before Zero2Dev starts.</span>
      </div>
    </section>

    <section class="section-card" id="set-access" aria-labelledby="h-access">
      <header><h2 id="h-access">Access</h2><p>The token this browser uses to talk to Zero2Dev.</p></header>
      <div class="body">
        <div class="row wrap">
          <button class="btn" type="button" onclick={copyToken}><Icon name="copy" /> Copy access token</button>
          <span class="hint">Anyone with the token can start programs on this PC through Zero2Dev. Do not paste it into chats, tickets or screenshots.</span>
        </div>
        <div class="danger-zone">
          <div class="grow">
            <div class="label">Sign out of this browser</div>
            <span class="hint">Forgets the token here. You will need the link Zero2Dev prints when it starts.</span>
          </div>
          {#if !revealWarn}
            <button class="btn danger" type="button" onclick={() => (revealWarn = true)} aria-label="Sign out of this browser">Sign out</button>
          {:else}
            <button class="btn danger solid" type="button" onclick={forgetToken}>Forget token</button>
            <button class="btn" type="button" onclick={() => (revealWarn = false)}>Cancel</button>
          {/if}
        </div>
      </div>
    </section>

    {#if dirty}
      <UnsavedBar formId="settings-form" {saving} ondiscard={discard} />
    {/if}
  </div>
</div>

<style>
  .layout {
    display: grid;
    grid-template-columns: 170px minmax(0, 760px);
    gap: 24px;
    align-items: start;
  }
  .subnav {
    position: sticky;
    top: 0;
    display: grid;
    gap: 2px;
  }
  .subnav a {
    padding: 7px 10px;
    border-radius: var(--radius);
    color: var(--muted);
    text-decoration: none;
    font-size: 14px;
  }
  .subnav a:hover {
    background: var(--surface-2);
    color: var(--text);
  }
  .subnav a.on {
    background: var(--accent-soft);
    color: var(--accent-text);
    font-weight: 600;
  }
  .panes {
    display: grid;
    gap: 16px;
    min-width: 0;
  }
  .section-card {
    scroll-margin-top: 8px;
  }
  .switch-line {
    display: flex;
    align-items: flex-start;
    gap: 12px;
  }
  .switch-line label {
    display: grid;
    gap: 3px;
    cursor: pointer;
  }
  .howto summary {
    cursor: pointer;
    font-size: 14px;
    color: var(--muted);
  }
  .howto p {
    margin: 8px 0;
  }
  .facts {
    margin: 0;
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(160px, 1fr));
    gap: 10px 18px;
  }
  .facts .wide {
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
  .stack-s {
    display: grid;
    gap: 6px;
    min-width: 0;
  }
  .path-inline {
    overflow-wrap: anywhere;
  }
  .dir {
    padding: 6px 10px;
    border: 1px solid var(--line);
    border-radius: var(--radius);
    background: var(--sunken);
    overflow-wrap: anywhere;
  }
  .server-pick {
    border: 0;
    margin: 0;
    padding: 0;
    min-width: 0;
  }
  .opt {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 10px 12px;
    border: 1px solid var(--line);
    border-radius: var(--radius);
    cursor: pointer;
    flex: 1 1 220px;
  }
  .opt.on {
    border-color: var(--accent);
    background: var(--accent-soft);
  }
  .opt > span:last-child {
    display: grid;
    gap: 2px;
  }
  .danger-zone {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 10px;
    padding: 12px 14px;
    border: 1px solid var(--bad-soft);
    border-radius: var(--radius);
    background: color-mix(in srgb, var(--bad-soft) 40%, transparent);
  }
  .danger-zone .grow {
    display: grid;
    gap: 2px;
    flex: 1 1 240px;
  }
  @media (max-width: 900px) {
    .layout {
      grid-template-columns: minmax(0, 1fr);
    }
    .subnav {
      position: static;
      display: flex;
      overflow-x: auto;
      gap: 4px;
      border-bottom: 1px solid var(--line);
      padding-bottom: 6px;
    }
    .subnav a {
      white-space: nowrap;
    }
  }
</style>
