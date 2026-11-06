<script lang="ts">
  // Tick-box list of the PHP extensions in the selected PHP version's ext folder, plus a
  // one-click Xdebug download when that version has none.
  import { api } from "../lib/api";
  import { splitList } from "../lib/kinds";
  import { errMessage, installXdebug, jobs } from "../lib/store.svelte";
  import type { PHPExtensions } from "../lib/types";

  type Props = {
    /** The PHP runtime the service uses ("" = none installed). */
    runtimeId: string;
    value: string;
    help: string;
    onchange?: () => void;
  };
  let { runtimeId, value = $bindable(), help, onchange }: Props = $props();

  let info = $state<PHPExtensions | null>(null);
  let loadError = $state("");
  let filter = $state("");
  let jobId = $state("");
  const job = $derived(jobId ? jobs[jobId] : undefined);

  async function load(id: string) {
    loadError = "";
    if (!id) {
      info = null;
      return;
    }
    try {
      info = await api.phpExtensions(id);
    } catch (e) {
      info = null;
      loadError = errMessage(e);
    }
  }
  $effect(() => {
    void load(runtimeId);
  });
  // Reload once Xdebug is in place, and tick it.
  $effect(() => {
    if (job?.state === "done" && runtimeId) {
      jobId = "";
      void load(runtimeId).then(() => set("xdebug", true));
    }
  });

  const selected = $derived(splitList(value));
  const names = $derived(info?.extensions.map((e) => e.name) ?? []);
  const missing = $derived(info?.managed ? selected.filter((n) => !names.includes(n)) : []);
  const shown = $derived(
    (info?.extensions ?? []).filter((e) => !filter.trim() || e.name.includes(filter.trim().toLowerCase())),
  );

  function set(name: string, on: boolean) {
    const cur = splitList(value).filter((n) => n !== name);
    value = (on ? [...cur, name] : cur).join(",");
    onchange?.();
  }

  async function downloadXdebug() {
    const id = await installXdebug(runtimeId);
    if (id) jobId = id;
  }
</script>

<fieldset class="exts" aria-describedby="ext-help">
  <legend class="label">Extensions <code class="writes">php.ini extension=</code></legend>
  {#if !runtimeId}
    <span class="hint">Install PHP on the Runtimes page to pick its extensions. Selected: {selected.join(", ") || "none"}.</span>
  {:else if loadError}
    <span class="err">{loadError}</span>
  {:else if !info}
    <span class="hint">Reading the ext folder...</span>
  {:else if !info.managed}
    <span class="hint">This PHP has no ext folder next to it (a system PHP), so its own php.ini decides the extensions.</span>
  {:else}
    {#if info.extensions.length > 12}
      <input type="search" class="filter" placeholder="Filter extensions" aria-label="Filter extensions" bind:value={filter} />
    {/if}
    <div class="grid" role="group" aria-label="PHP extensions">
      {#each shown as e (e.name)}
        <label class="check" title={e.file}>
          <input type="checkbox" checked={selected.includes(e.name)} onchange={(ev) => set(e.name, ev.currentTarget.checked)} />
          <span class="mono">{e.name}</span>{#if e.zend}<span class="muted small"> zend</span>{/if}
        </label>
      {/each}
    </div>
    {#if missing.length}
      <span class="hint">Not in this PHP, left out: {#each missing as m (m)}<button type="button" class="linkish chip" onclick={() => set(m, false)} aria-label="Remove {m}">{m} ×</button>{/each}</span>
    {/if}
    {#if !info.xdebug}
      <div class="xdebug">
        {#if job && job.state === "running"}
          <span class="hint" role="status">Downloading Xdebug... {job.percent}%</span>
          <div class="progress"><span style="width: {job.percent}%"></span></div>
        {:else}
          <button type="button" class="btn sm" onclick={downloadXdebug}>Download Xdebug</button>
          <span class="hint">The step debugger, from xdebug.org, checked against its SHA-256. It goes into this PHP version's ext folder.</span>
          {#if job?.state === "failed"}<span class="err">{job.error}</span>{/if}
        {/if}
      </div>
    {/if}
  {/if}
  <span class="hint" id="ext-help">{help}</span>
</fieldset>

<style>
  .exts {
    border: 0;
    margin: 0;
    padding: 0;
    display: grid;
    gap: 6px;
  }
  .exts legend {
    padding: 0;
    margin-bottom: 4px;
  }
  .grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(140px, 1fr));
    gap: 2px 12px;
    max-height: 220px;
    overflow: auto;
  }
  .writes {
    font-size: 11px;
    font-weight: 400;
    color: var(--muted, #6b7280);
    margin-left: 6px;
  }
  .filter {
    max-width: 240px;
  }
  .chip {
    margin-left: 6px;
  }
  .xdebug {
    display: grid;
    gap: 4px;
    justify-items: start;
  }
</style>
