<script lang="ts">
  // The Mail section of a Mailpit service's page: where apps send their mail (SMTP host and port,
  // no login), the web inbox and the .env lines to copy (0.0.5, mailpit).
  import { mailInfo } from "../lib/mail";
  import type { Service } from "../lib/types";
  import { toast } from "../lib/store.svelte";
  import Icon from "./Icon.svelte";

  let { service }: { service: Service } = $props();
  const info = $derived(mailInfo(service));
  const envText = $derived(info ? info.envLines.join("\n") : "");

  async function copy(text: string) {
    try {
      await navigator.clipboard.writeText(text);
      toast("success", "Mail settings copied.");
    } catch {
      toast("error", "Could not copy to the clipboard in this browser context.");
    }
  }
</script>

{#if info}
  <section class="mail" aria-labelledby="mail-h" data-testid="mail-info">
    <h3 id="mail-h">Sending mail</h3>
    <dl>
      <div><dt>SMTP host</dt><dd class="mono" data-testid="mail-host">{info.host}</dd></div>
      <div><dt>SMTP port</dt><dd class="mono" data-testid="mail-port">{info.smtpPort ?? "not set"}</dd></div>
      <div><dt>Login</dt><dd>No authentication</dd></div>
      <div class="wide">
        <dt>Inbox</dt>
        <dd>
          {#if info.webUrl}
            <a href={info.webUrl} target="_blank" rel="noreferrer" data-testid="mail-web">{info.webUrl} <Icon name="external" size={12} /></a>
            {#if service.state !== "running"}<span class="muted small"> (start the service to open it)</span>{/if}
          {:else}
            <span class="muted">Set a port to open the inbox.</span>
          {/if}
        </dd>
      </div>
      {#if info.envLines.length}
        <div class="wide">
          <dt>For your .env</dt>
          <dd class="env">
            <pre class="mono" data-testid="mail-env">{envText}</pre>
            <button class="btn sm ghost" type="button" onclick={() => copy(envText)}><Icon name="copy" size={14} /> Copy</button>
          </dd>
        </div>
      {/if}
    </dl>
    <p class="hint">
      Point your app at this SMTP server: every e-mail it sends lands in the inbox instead of a real mailbox. Only this PC can
      connect, and nothing is passed on.
    </p>
  </section>
{/if}

<style>
  .mail {
    border-top: 1px solid var(--line);
    padding-top: 12px;
    display: grid;
    gap: 8px;
  }
  .mail h3 {
    margin: 0;
    font-size: 14px;
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
  .env {
    display: flex;
    flex-wrap: wrap;
    gap: 8px;
    align-items: flex-start;
  }
  pre {
    margin: 0;
    padding: 8px 12px;
    background: var(--surface-2);
    border: 1px solid var(--line);
    border-radius: var(--radius);
    font-size: 12px;
    white-space: pre;
    overflow-x: auto;
    max-width: 100%;
  }
  @media (max-width: 560px) {
    dl {
      grid-template-columns: 1fr 1fr;
    }
  }
</style>
