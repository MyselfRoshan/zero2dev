<script lang="ts">
  import { api, ApiError } from "../lib/api";
  import { fmtDateTime } from "../lib/format";
  import { onMount } from "svelte";
  import {
    app,
    attempt,
    catalogInfo,
    initCatalogStatus,
    installJobs,
    installRuntime,
    jobs,
    lists,
    loadRuntimes,
    loadStatus,
    refreshCatalog,
    toast,
  } from "../lib/store.svelte";
  import { CARD_KINDS, kindLabel, RUNTIME_GROUPS, sourceErrors } from "../lib/catalog";
    import RuntimeKindCard from "./RuntimeKindCard.svelte";
  import { RUNTIME_KINDS, type CatalogEntry, type DetectedRuntime, type Runtime, type RuntimeKind } from "../lib/types";
  import { hasErrors, validateRegister, type RegisterField } from "../lib/validate";
  import Icon from "../ui/Icon.svelte";
  import KindIcon from "../ui/KindIcon.svelte";
  import Modal from "../ui/Modal.svelte";
  import ImportDialog from "./ImportDialog.svelte";
  import PageHead from "../ui/PageHead.svelte";

  let importing = $state<CatalogEntry | null>(null);
  let detected = $state<DetectedRuntime[] | null>(null);
  let detecting = $state(false);
  let registering = $state(false);

  onMount(() => {
    void initCatalogStatus();
  });
  const sourceProblems = $derived(sourceErrors(catalogInfo.status?.sources ?? []));
  // A card per kind, plus one for runtimes of an unknown kind if any were registered.
  const hasOther = $derived(lists.runtimes.some((r) => !(CARD_KINDS as string[]).includes(r.kind)));

  async function detect() {
    detecting = true;
    const r = await attempt(() => api.detectRuntimes());
    detecting = false;
    if (r) detected = r;
  }
  const known = $derived(new Set(lists.runtimes.map((r) => r.binary.toLowerCase())));
  async function registerDetected(d: DetectedRuntime) {
    const kind = (RUNTIME_KINDS as string[]).includes(d.kind) ? (d.kind as RuntimeKind) : "other";
    const r = await attempt(() => api.registerRuntime({ kind, binary: d.binary, version: d.version || undefined }), `${kind} ${d.version} registered.`);
    if (r) {
      await loadRuntimes();
      void loadStatus();
    }
  }

  // register form
  let rKind = $state<RuntimeKind>("node");
  let rBinary = $state("");
  let rVersion = $state("");
  let rErr = $state<Partial<Record<RegisterField, string>>>({});
  let rForm = $state("");
  let rBusy = $state(false);
  async function submitRegister(e: SubmitEvent) {
    e.preventDefault();
    rForm = "";
    rErr = validateRegister({ kind: rKind, binary: rBinary });
    if (hasErrors(rErr)) return;
    rBusy = true;
    try {
      const r = await api.registerRuntime({ kind: rKind, binary: rBinary.trim(), version: rVersion.trim() || undefined });
      toast("success", `${r.name} registered.`);
      await loadRuntimes();
      void loadStatus();
      registering = false;
      rBinary = "";
      rVersion = "";
    } catch (err) {
      rForm = err instanceof ApiError ? err.message : String(err);
    } finally {
      rBusy = false;
    }
  }

</script>

<PageHead sub="The programs your services run on. Install as many versions as you need; each type has one default.">
  <button class="btn" onclick={detect} disabled={detecting}><Icon name="search" /> {detecting ? "Detecting" : "Detect on this PC"}</button>
  <button class="btn" onclick={() => (registering = true)}><Icon name="plus" /> Register by path</button>
</PageHead>

{#if detected}
  <section class="sect">
    <div class="sect-head"><h2>Found on this PC</h2><span class="spacer"></span><button class="btn sm ghost" onclick={() => (detected = null)}>Dismiss</button></div>
    {#if detected.length === 0}
      <div class="notice"><Icon name="alert" /><span>No tools were found on PATH. If a program lives elsewhere, use Register by path.</span></div>
    {:else}
      <div class="card t-wrap">
        <table class="t">
          <tbody>
            {#each detected as d (d.binary)}
              <tr>
                <td><span class="badge plain kind-badge"><KindIcon kind={d.name === "nginx" ? "nginx" : d.kind} size={12} /> {d.name === "nginx" ? "nginx" : d.kind}</span></td>
                <td class="mono">{d.version}</td>
                <td class="path">{d.binary}</td>
                <td class="actions">
                  {#if known.has(d.binary.toLowerCase())}
                    <span class="badge running"><span class="dot"></span>registered</span>
                  {:else}
                    <button class="btn sm" onclick={() => registerDetected(d)}><Icon name="plus" size={14} /> Register</button>
                  {/if}
                </td>
              </tr>
            {/each}
          </tbody>
        </table>
      </div>
    {/if}
  </section>
{/if}

<section class="sect" aria-label="Runtimes">
  <div class="sect-head">
    <span class="muted small">
      {#if catalogInfo.status?.checkedAt}Version lists checked {fmtDateTime(catalogInfo.status.checkedAt)}.{:else if !catalogInfo.refreshing}Version lists not checked yet.{/if}
    </span>
    <button class="btn sm ghost" type="button" onclick={() => refreshCatalog()} disabled={catalogInfo.refreshing} aria-busy={catalogInfo.refreshing}>
      {#if catalogInfo.refreshing}<span class="spin" aria-hidden="true"></span> Checking...{:else}<Icon name="restart" size={14} /> Check for new versions{/if}
    </button>
  </div>
  {#if sourceProblems.length}
    <ul class="src-errors">
      {#each sourceProblems as line (line)}<li class="err">{line}</li>{/each}
    </ul>
  {/if}
  {#each RUNTIME_GROUPS as g (g.id)}
    <section class="group" aria-labelledby="g-{g.id}" data-group={g.id}>
      <div class="group-head">
        <h2 id="g-{g.id}">{g.title}</h2>
        <span class="muted small">
          {g.text}
          {#if g.id === "servers"}<a href="#/settings">Choose which one in Settings.</a>{/if}
        </span>
      </div>
      <div class="kinds">
        {#each g.kinds as k (k)}<RuntimeKindCard kind={k} onimport={(e) => (importing = e)} />{/each}
      </div>
    </section>
  {/each}
  {#if hasOther}
    <section class="group" aria-labelledby="g-other" data-group="other">
      <div class="group-head"><h2 id="g-other">Other</h2><span class="muted small">Programs you registered by path.</span></div>
      <div class="kinds"><RuntimeKindCard kind="other" onimport={(e) => (importing = e)} /></div>
    </section>
  {/if}
</section>

{#if importing}
  <ImportDialog entry={importing} onclose={() => (importing = null)} />
{/if}

{#if registering}
  <Modal title="Register a runtime by path" onclose={() => (registering = false)}>
    <form id="reg-form" class="dlg-body stack" onsubmit={submitRegister} novalidate>
      {#if rForm}<div class="notice bad" role="alert"><Icon name="alert" /><span>{rForm}</span></div>{/if}
      <div class="field"><label>
        <span class="label">Kind</span>
        <select bind:value={rKind}>
          {#each RUNTIME_GROUPS as g (g.id)}
            <optgroup label={g.title}>{#each g.kinds as k (k)}<option value={k}>{kindLabel(k)}</option>{/each}</optgroup>
          {/each}
          <optgroup label="Other"><option value="other">Other program</option></optgroup>
        </select></label>
      </div>
      <div class="field"><label>
        <span class="label">Path to the program</span>
        <input type="text" class="mono" bind:value={rBinary} aria-invalid={!!rErr.binary} placeholder="C:\php\php.exe" spellcheck="false" /></label>
        {#if rErr.binary}<span class="err">{rErr.binary}</span>{:else}<span class="hint">Absolute path to the executable file. Zero2Dev only stores it; nothing is copied.</span>{/if}
      </div>
      <div class="field"><label>
        <span class="label">Version (optional)</span>
        <input type="text" class="mono" bind:value={rVersion} placeholder="Read from --version when empty" /></label>
      </div>
    </form>
    {#snippet footer()}
      <button class="btn" type="button" onclick={() => (registering = false)}>Cancel</button>
      <button class="btn primary" type="submit" form="reg-form" disabled={rBusy}>Register runtime</button>
    {/snippet}
  </Modal>
{/if}

<style>
  .sect {
    margin-top: 18px;
  }
  .sect-head {
    display: flex;
    align-items: center;
    gap: 8px;
    margin-bottom: 10px;
  }
  .kinds {
    display: grid;
    grid-template-columns: repeat(3, minmax(0, 1fr));
    align-items: start;
    gap: 12px;
  }
  @media (max-width: 1100px) {
    .kinds {
      grid-template-columns: repeat(2, minmax(0, 1fr));
    }
  }
  @media (max-width: 700px) {
    .kinds {
      grid-template-columns: 1fr;
    }
  }
  .group {
    margin-bottom: 22px;
  }
  .group-head {
    display: flex;
    align-items: baseline;
    flex-wrap: wrap;
    gap: 4px 12px;
    margin-bottom: 10px;
  }
  .group-head h2 {
    font-size: 15px;
  }
  .src-errors {
    margin: 0 0 10px;
    padding: 0;
    list-style: none;
    display: grid;
    gap: 2px;
  }
</style>
