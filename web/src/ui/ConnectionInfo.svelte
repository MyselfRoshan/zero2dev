<script lang="ts">
  // The Connection section of a database service's page: host, port, user, the administrator
  // password (masked, with Show/Hide and Copy) and the URL to copy, with the password when one is set.
  import { connectionInfo } from "../lib/ux";
  import type { Service } from "../lib/types";
  import { toast } from "../lib/store.svelte";
  import Icon from "./Icon.svelte";

  let { service, dataDir }: { service: Service; dataDir: string } = $props();
  const conn = $derived(connectionInfo(service));
  let shown = $state(false);
  // A new or removed password starts hidden again.
  let lastPassword = "";
  $effect(() => {
    const pw = conn?.password ?? "";
    if (pw !== lastPassword) {
      lastPassword = pw;
      shown = false;
    }
  });

  async function copy(text: string, what: string) {
    try {
      await navigator.clipboard.writeText(text);
      toast("success", `${what} copied.`);
    } catch {
      toast("error", "Could not copy to the clipboard in this browser context.");
    }
  }
</script>

{#if conn}
  <section class="conn" aria-labelledby="conn-h" data-testid="connection">
    <h3 id="conn-h">Connection</h3>
    <dl>
      <div><dt>Host</dt><dd class="mono">{conn.host}</dd></div>
      <div><dt>Port</dt><dd class="mono">{conn.port}</dd></div>
      <div><dt>User</dt><dd class="mono" data-testid="connection-user">{conn.user || "none"}</dd></div>
      <div class="wide">
        <dt>Password</dt>
        <dd class="row wrap" data-testid="connection-password">
          {#if conn.password}
            <code class="mono" data-testid="connection-password-value">{#if shown}{conn.password}{:else}<span aria-hidden="true">••••••••</span><span class="sr-only">hidden</span>{/if}</code>
            <button class="btn sm ghost" type="button" aria-pressed={shown} aria-label={shown ? "Hide the password" : "Show the password"} onclick={() => (shown = !shown)} data-testid="connection-password-toggle"><Icon name={shown ? "eyeOff" : "eye"} size={14} /> {shown ? "Hide" : "Show"}</button>
            <button class="btn sm ghost" type="button" onclick={() => copy(conn.password, "Password")} aria-label="Copy the password" data-testid="connection-password-copy"><Icon name="copy" size={14} /> Copy</button>
          {:else}
            <span class="mono">none</span>
          {/if}
        </dd>
      </div>
      <div class="wide"><dt>URL</dt><dd class="row wrap"><code class="mono" data-testid="connection-url">{shown ? conn.url : conn.maskedUrl}</code><button class="btn sm ghost" type="button" onclick={() => copy(conn.url, "Connection URL")} aria-label={conn.password ? "Copy the connection URL, with the password" : "Copy the connection URL"} data-testid="connection-url-copy"><Icon name="copy" size={14} /> Copy</button></dd></div>
    </dl>
    <p class="hint" data-testid="connection-hint">{conn.hint} Its files are in <code class="mono">{dataDir}</code>.</p>
  </section>
{/if}

<style>
  .conn {
    border-top: 1px solid var(--line);
    padding-top: 12px;
    display: grid;
    gap: 8px;
  }
  .conn h3 {
    margin: 0;
    font-size: 14px;
  }
  .conn .row {
    gap: 8px;
    align-items: center;
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
  code {
    overflow-wrap: anywhere;
    min-width: 0;
  }
</style>
