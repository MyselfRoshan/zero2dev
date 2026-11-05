<script lang="ts">
  import { api } from "../lib/api";
  import { tailLines } from "../lib/installer";
  import { errMessage, toast } from "../lib/store.svelte";
  import Icon from "./Icon.svelte";

  let open = $state(false);
  let lines = $state<string[] | null>(null);
  let loading = $state(false);
  let error = $state("");
  const uid = `ilog-${Math.random().toString(36).slice(2, 7)}`;
  let box = $state<HTMLPreElement>();

  async function load() {
    loading = true;
    error = "";
    try {
      lines = tailLines((await api.installerLog(200)).lines, 200);
    } catch (e) {
      error = errMessage(e);
    } finally {
      loading = false;
    }
    queueMicrotask(() => {
      if (box) box.scrollTop = box.scrollHeight;
    });
  }
  function toggle() {
    open = !open;
    if (open) void load();
  }
  async function copy() {
    const text = (lines ?? []).join("\n");
    try {
      await navigator.clipboard.writeText(text);
      toast("success", "Installer log copied.");
    } catch {
      toast("error", "Could not copy. Select the text and copy it manually.");
    }
  }
</script>

<div class="ilog">
  <button class="btn sm" type="button" aria-expanded={open} aria-controls={uid} onclick={toggle}>
    <Icon name="logs" size={14} />
    {open ? "Hide installer log" : "Show installer log"}
  </button>
  {#if open}
    <div class="panel" id={uid}>
      <div class="bar">
        <span class="muted small">Last {lines?.length ?? 0} lines of installer.log</span>
        <span class="spacer"></span>
        <button class="btn sm" type="button" onclick={load} disabled={loading}><Icon name="restart" size={14} /> Refresh</button>
        <button class="btn sm" type="button" onclick={copy} disabled={!lines?.length}><Icon name="copy" size={14} /> Copy</button>
      </div>
      {#if error}
        <p class="err pad">{error}</p>
      {:else}
        <!-- svelte-ignore a11y_no_noninteractive_tabindex -->
        <pre class="box mono" bind:this={box} tabindex="0" aria-label="Installer log">{loading && !lines ? "Loading" : lines?.length ? lines.join("\n") : "The installer log is empty."}</pre>
      {/if}
    </div>
  {/if}
</div>

<style>
  .ilog {
    display: grid;
    gap: 8px;
  }
  .panel {
    border: 1px solid var(--line-strong);
    border-radius: var(--radius);
    overflow: hidden;
    background: var(--surface);
  }
  .bar {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 6px 8px;
    border-bottom: 1px solid var(--line);
  }
  .box {
    margin: 0;
    padding: 10px 12px;
    max-height: 240px;
    overflow: auto;
    background: var(--log-bg);
    color: var(--log-text);
    font-size: 12px;
    line-height: 1.5;
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }
  .pad {
    padding: 10px 12px;
  }
</style>
