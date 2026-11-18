<script lang="ts">
  // The Connection section of a database service's page: host, port, user and the URL to copy.
  // Moved out of ServiceDetail.svelte in 0.0.5 (the db-ui work adds the password here).
  import { connectionInfo } from "../lib/ux";
  import type { Service } from "../lib/types";
  import { toast } from "../lib/store.svelte";
  import Icon from "./Icon.svelte";

  let { service, dataDir }: { service: Service; dataDir: string } = $props();
  const conn = $derived(connectionInfo(service));

  async function copy(text: string) {
    try {
      await navigator.clipboard.writeText(text);
      toast("success", "Connection URL copied.");
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
      <div><dt>User</dt><dd class="mono">{conn.user || "none"}</dd></div>
      <div class="wide"><dt>URL</dt><dd class="row"><code class="mono" data-testid="connection-url">{conn.url}</code><button class="btn sm ghost" type="button" onclick={() => copy(conn.url)}><Icon name="copy" size={14} /> Copy</button></dd></div>
    </dl>
    <p class="hint">No password: the server only accepts connections from this PC (127.0.0.1). Its files are in <code class="mono">{dataDir}</code>.</p>
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
</style>
