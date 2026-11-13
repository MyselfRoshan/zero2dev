<script lang="ts">
  // The PHP extensions a service loads, read from the selected PHP version's ext folder:
  // the loaded ones as chips with an "Add" box, the full tick-box list one click away, and
  // an Xdebug row (download it, then load it).
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
  let showAll = $state(false);
  let adding = $state("");
  let jobId = $state("");
  let jobRuntimeId = $state("");
  const job = $derived(jobId && jobRuntimeId === runtimeId ? jobs[jobId] : undefined);

  async function load(id: string) {
    loadError = "";
    if (!id) {
      info = null;
      return;
    }
    try {
      const result = await api.phpExtensions(id);
      if (id !== runtimeId) return;
      info = result;
    } catch (e) {
      if (id !== runtimeId) return;
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
      const id = runtimeId;
      jobId = "";
      void load(id).then(() => {
        if (id === runtimeId) set("xdebug", true);
      });
    }
  });

  const selected = $derived(splitList(value));
  const names = $derived(info?.extensions.map((e) => e.name) ?? []);
  const missing = $derived(info?.managed ? selected.filter((n) => !names.includes(n)) : []);
  const shown = $derived(
    (info?.extensions ?? []).filter((e) => !filter.trim() || e.name.includes(filter.trim().toLowerCase())),
  );

  const available = $derived((info?.extensions ?? []).filter((e) => !selected.includes(e.name) && e.name !== "xdebug"));
  function addTyped() {
    const n = adding.trim().toLowerCase();
    if (n && names.includes(n)) {
      set(n, true);
      adding = "";
    }
  }

  function set(name: string, on: boolean) {
    const cur = splitList(value).filter((n) => n !== name);
    value = (on ? [...cur, name] : cur).join(",");
    onchange?.();
  }

  async function downloadXdebug() {
    const requestedRuntimeId = runtimeId;
    const id = await installXdebug(requestedRuntimeId);
    if (id) {
      jobRuntimeId = requestedRuntimeId;
      jobId = id;
    }
  }
</script>

<fieldset class="exts" aria-describedby="ext-help">
  <legend class="head"><span class="name">Extensions</span>{#if !showAll && info?.managed}<span class="muted small">{selected.length} loaded</span>{/if}</legend>
  {#if !runtimeId}
    <div class="chips" role="list" aria-label="Chosen PHP extensions">
      {#each selected as n (n)}<span class="chip" role="listitem"><span class="mono">{n}</span><button type="button" class="x" onclick={() => set(n, false)} aria-label="Remove {n}">×</button></span>{/each}
    </div>
    <span class="hint">These load once PHP is installed. <a href="#/runtimes">Install PHP</a> to add others from its ext folder.</span>
  {:else if loadError}
    <span class="err">{loadError}</span>
  {:else if !info}
    <span class="hint">Reading the ext folder...</span>
  {:else if !info.managed}
    <span class="hint">This PHP has no ext folder next to it (a system PHP), so its own php.ini decides the extensions.</span>
  {:else}
    {#if showAll}
      <div class="row wrap tools">
        <input type="search" class="filter" placeholder="Filter extensions" aria-label="Filter extensions" bind:value={filter} />
        <button type="button" class="btn sm ghost" onclick={() => (showAll = false)}>Show only the loaded ones</button>
      </div>
      <div class="grid" role="group" aria-label="PHP extensions">
        {#each shown as e (e.name)}
          <label class="check" title={e.file}>
            <input type="checkbox" checked={selected.includes(e.name)} onchange={(ev) => set(e.name, ev.currentTarget.checked)} />
            <span class="mono">{e.name}</span>{#if e.zend}<span class="muted small"> zend</span>{/if}
          </label>
        {/each}
      </div>
    {:else}
      <div class="chips" role="list" aria-label="Loaded PHP extensions">
        {#each selected.filter((n) => names.includes(n)) as n (n)}
          <span class="chip" role="listitem"><span class="mono">{n}</span><button type="button" class="x" onclick={() => set(n, false)} aria-label="Remove {n}">×</button></span>
        {:else}
          <span class="muted small">None. PHP runs with only its built-in extensions.</span>
        {/each}
      </div>
      <div class="row wrap tools">
        <input type="text" class="mono add" list="ext-avail" placeholder="Add an extension, e.g. gd" aria-label="Add a PHP extension" spellcheck="false"
          bind:value={adding} onchange={addTyped} onkeydown={(e) => { if (e.key === "Enter") { e.preventDefault(); addTyped(); } }} />
        <datalist id="ext-avail">{#each available as e (e.name)}<option value={e.name}></option>{/each}</datalist>
        <button type="button" class="btn sm ghost" onclick={() => (showAll = true)}>Show all {info.extensions.length}</button>
      </div>
    {/if}
    {#if missing.length}
      <span class="hint">Not in this PHP version, left out: {#each missing as m (m)}<button type="button" class="linkish gap" onclick={() => set(m, false)} aria-label="Remove {m}">{m} ×</button>{/each}</span>
    {/if}
    <div class="xdebug" data-testid="xdebug-row">
      <span class="name">Xdebug</span>
      {#if info.xdebug}
        <span class="muted small grow">Installed in this PHP version.</span>
        <label class="check"><input type="checkbox" checked={selected.includes("xdebug")} onchange={(ev) => set("xdebug", ev.currentTarget.checked)} aria-label="Load Xdebug" /> <span>Load it</span></label>
      {:else if job && job.state === "running"}
        <span class="hint grow" role="status">Downloading... {job.percent}%</span>
        <div class="progress bar"><span style="width: {job.percent}%"></span></div>
      {:else}
        <span class="muted small grow">The step debugger. Downloaded from xdebug.org and checked against its SHA-256.</span>
        <button type="button" class="btn sm" onclick={downloadXdebug}>Download Xdebug</button>
      {/if}
    </div>
    {#if job?.state === "failed"}<span class="err">{job.error}</span>{/if}
  {/if}
  <span class="hint" id="ext-help">{help} <code class="chip-code">php.ini extension=</code></span>
</fieldset>

<style>
  .exts {
    border: 0;
    margin: 0;
    padding: 0;
    display: grid;
    gap: 8px;
    min-width: 0;
  }
  .head {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 0;
    width: 100%;
  }
  .name {
    font-weight: 600;
    font-size: 14px;
  }
  .chips {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
  }
  .chip {
    display: inline-flex;
    align-items: center;
    gap: 2px;
    padding: 0 4px 0 7px;
    line-height: 20px;
    border: 1px solid var(--line-strong);
    border-radius: var(--radius-sm);
    background: var(--surface-2);
    font-size: 12px;
  }
  .chip .x {
    border: 0;
    background: none;
    color: var(--muted);
    cursor: pointer;
    font-size: 16px;
    line-height: 1;
    padding: 0 5px;
    border-radius: 50%;
  }
  .chip .x:hover {
    color: var(--bad);
  }
  .tools {
    gap: 8px;
  }
  .add,
  .filter {
    max-width: 260px;
    flex: 1 1 200px;
  }
  .grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(140px, 1fr));
    gap: 2px 12px;
    max-height: 240px;
    overflow: auto;
  }
  .gap {
    margin-left: 6px;
  }
  .xdebug {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 6px 12px;
    padding: 10px 12px;
    border: 1px solid var(--line);
    border-radius: var(--radius);
    background: var(--surface-2);
  }
  .xdebug .grow {
    flex: 1 1 200px;
  }
  .bar {
    flex: 1 1 100%;
  }
</style>
