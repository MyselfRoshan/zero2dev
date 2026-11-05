<script lang="ts">
  import { api, getToken } from "../lib/api";
  import { app, attempt, forgetToken, toast } from "../lib/store.svelte";
  import { caRootPath, portableNote } from "../lib/installer";
  import { hasErrors, parsePort, validateSettings, type SettingsField } from "../lib/validate";
  import Icon from "../ui/Icon.svelte";
  import KindIcon from "../ui/KindIcon.svelte";
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
    if (hasErrors(errors)) return;
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

<div class="wrap">
  <form class="card" onsubmit={save} novalidate>
    <div class="panel-head"><h2>Sites and proxy</h2></div>
    <div class="card-pad stack">
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
          <span class="hint">The program that serves your sites. Zero2Dev writes its config and creates its service when the runtime is installed.</span>
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
      <span class="hint">Changing proxy ports rewrites the {server === "nginx" ? "nginx config" : "Caddyfile"}; a running {serverLabel(server)} service restarts.</span>
    </div>
    <div class="panel-head sub"><h2>HTTPS certificate</h2></div>
    <div class="card-pad stack">
      {#if server === "nginx"}
        <div class="notice" data-testid="nginx-ca">
          <Icon name="key" />
          <div class="stack-s">
            <span>For nginx, Zero2Dev creates its own local certificate authority and one certificate that covers every HTTPS site. It is reissued when you add or remove HTTPS sites.</span>
            <span>Nothing is added to the Windows certificate store. To open HTTPS sites without a browser warning, trust <code class="path-inline">{nginxCert}</code> yourself.</span>
          </div>
        </div>
      {:else}
        <label class="check"><input type="checkbox" bind:checked={trustCA} aria-describedby="ca-warn" /> <span>Let Caddy trust its local certificate authority</span></label>
        <div class="notice warn" id="ca-warn">
          <Icon name="alert" />
          <div class="stack-s">
            <span><b>Turn this on only if you want Windows to trust local HTTPS sites.</b> Caddy then changes the Windows certificate store and Windows may show a security prompt.</span>
            <span>While it is off, nothing outside the data folder is touched and HTTPS sites show a certificate warning in the browser until you trust <code class="path-inline">{rootCert}</code> yourself.</span>
          </div>
        </div>
      {/if}
    </div>
    <div class="panel-head sub"><h2>Downloads</h2></div>
    <div class="card-pad stack">
      <div class="field">
        <label>
          <span class="label">Download proxy (optional)</span>
          <input type="text" class="mono" bind:value={proxy} aria-invalid={!!errors.downloadProxy} aria-describedby="proxy-help" placeholder="http://proxy.company.com:8080" spellcheck="false" autocomplete="off" />
        </label>
        {#if errors.downloadProxy}<span class="err">{errors.downloadProxy}</span>{:else}<span class="hint" id="proxy-help">Used when installing runtimes. Empty means the HTTP_PROXY and HTTPS_PROXY environment variables. Zero2Dev does not read the Windows proxy setting, so company networks often need this.</span>{/if}
      </div>
    </div>
    <div class="panel-head sub"><h2>Startup</h2></div>
    <div class="card-pad">
      <label class="check"><input type="checkbox" bind:checked={openBrowser} /> <span>Open this dashboard in the browser when Zero2Dev starts</span></label>
    </div>
    <div class="foot">
      <button class="btn primary" type="submit" disabled={!dirty || saving}>Save settings</button>
      {#if !dirty && app.settings}<span class="muted small">No unsaved changes.</span>{/if}
    </div>
  </form>

  <section class="card">
    <div class="panel-head">
      <h2>Portable</h2>
      {#if app.status?.portable}<span class="badge accent">portable</span>{/if}
    </div>
    <div class="card-pad stack">
      <div class="field">
        <span class="label" id="dd-l">Data folder</span>
        <div class="row">
          <code class="dir grow" aria-labelledby="dd-l" data-testid="data-folder">{dataDir || "-"}</code>
          <button class="btn sm" type="button" onclick={() => copyText(dataDir, "Data folder path")} disabled={!dataDir}><Icon name="copy" size={14} /> Copy</button>
        </div>
      </div>
      <p class="muted" data-testid="portable-note">{portableNote(app.status?.homeSource)}</p>
      {#if app.status?.exeDir}<p class="hint">Program folder: <code>{app.status.exeDir}</code></p>{/if}
    </div>
  </section>

  <section class="card">
    <div class="panel-head"><h2>This installation</h2></div>
    <dl class="card-pad">
      <div><dt>API port</dt><dd class="mono">{app.settings?.apiPort ?? "-"}</dd></div>
      <div><dt>Version</dt><dd class="mono">{app.status?.version ?? "-"}</dd></div>
    </dl>
    <p class="hint card-pad tail">The API port is set with the <code>ZERO2DEV_PORT</code> environment variable before the daemon starts.</p>
  </section>

  <section class="card">
    <div class="panel-head"><h2>Access token</h2></div>
    <div class="card-pad stack">
      <div class="notice warn">
        <Icon name="alert" />
        <span>Anyone with this token can start programs on this PC through Zero2Dev. Do not paste it into chats, tickets or screenshots.</span>
      </div>
      <div class="row wrap">
        <button class="btn" type="button" onclick={copyToken}><Icon name="copy" /> Copy access token</button>
        {#if !revealWarn}
          <button class="btn ghost danger" type="button" onclick={() => (revealWarn = true)}>Sign out of this browser</button>
        {:else}
          <span class="small">This forgets the token here. You will need the daemon's link again.</span>
          <button class="btn danger solid" type="button" onclick={forgetToken}>Forget token</button>
          <button class="btn" type="button" onclick={() => (revealWarn = false)}>Cancel</button>
        {/if}
      </div>
    </div>
  </section>
</div>

<style>
  .wrap {
    display: grid;
    gap: 18px;
    max-width: 720px;
  }
  .sub {
    border-top: 1px solid var(--line);
  }
  .foot {
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 12px 16px;
    border-top: 1px solid var(--line);
    background: var(--surface-2);
    border-radius: 0 0 8px 8px;
  }
  dl {
    margin: 0;
    display: grid;
    gap: 10px;
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
  .tail {
    padding-top: 0;
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
    background: var(--surface-2);
  }
  .opt > span:last-child {
    display: grid;
    gap: 2px;
  }
</style>
