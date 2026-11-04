<script lang="ts">
  import { api, getToken } from "../lib/api";
  import { app, attempt, forgetToken, toast } from "../lib/store.svelte";
  import { hasErrors, parsePort, validateSettings, type SettingsField } from "../lib/validate";
  import Icon from "../ui/Icon.svelte";

  let suffix = $state("");
  let httpPort = $state("");
  let httpsPort = $state("");
  let openBrowser = $state(true);
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
    }
  });

  const dirty = $derived(
    !!app.settings &&
      (suffix !== app.settings.domainSuffix ||
        httpPort !== String(app.settings.proxyHttpPort) ||
        httpsPort !== String(app.settings.proxyHttpsPort) ||
        openBrowser !== app.settings.openBrowserOnStart),
  );

  async function save(e: SubmitEvent) {
    e.preventDefault();
    if (!app.settings) return;
    const hp = parsePort(httpPort);
    const sp = parsePort(httpsPort);
    errors = validateSettings({ domainSuffix: suffix, proxyHttpPort: Number.isNaN(hp) ? -1 : hp, proxyHttpsPort: Number.isNaN(sp) ? -1 : sp });
    if (hasErrors(errors)) return;
    saving = true;
    const res = await attempt(
      () => api.putSettings({ ...app.settings!, domainSuffix: suffix.trim(), proxyHttpPort: hp, proxyHttpsPort: sp, openBrowserOnStart: openBrowser }),
      "Settings saved.",
    );
    saving = false;
    if (res) app.settings = res;
  }

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
      <span class="hint">Changing proxy ports rewrites the Caddyfile; a running Caddy service restarts.</span>
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
    <div class="panel-head"><h2>This installation</h2></div>
    <dl class="card-pad">
      <div><dt>Home folder</dt><dd class="mono">{app.settings?.homeDir ?? "-"}</dd></div>
      <div><dt>API port</dt><dd class="mono">{app.settings?.apiPort ?? "-"}</dd></div>
      <div><dt>Version</dt><dd class="mono">{app.status?.version ?? "-"}</dd></div>
    </dl>
    <p class="hint card-pad tail">Home folder and API port are set with the <code>ZERO2DEV_HOME</code> and <code>ZERO2DEV_PORT</code> environment variables before the daemon starts.</p>
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
  .tail {
    padding-top: 0;
  }
</style>
