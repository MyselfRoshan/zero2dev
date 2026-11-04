<script lang="ts">
  import { api, ApiError } from "../lib/api";
  import { fmtDateTime } from "../lib/format";
  import { app, attempt, installJobs, installRuntime, jobs, lists, loadRuntimes, loadStatus, toast } from "../lib/store.svelte";
  import { RUNTIME_KINDS, type CatalogEntry, type DetectedRuntime, type Runtime, type RuntimeKind } from "../lib/types";
  import { hasErrors, validateRegister, type RegisterField } from "../lib/validate";
  import ConfirmDialog from "../ui/ConfirmDialog.svelte";
  import Icon from "../ui/Icon.svelte";
  import Modal from "../ui/Modal.svelte";
  import PageHead from "../ui/PageHead.svelte";

  const MAIN_KINDS: RuntimeKind[] = ["node", "go", "php", "java", "caddy"];

  let removing = $state<Runtime | null>(null);
  let removeBusy = $state(false);
  let detected = $state<DetectedRuntime[] | null>(null);
  let detecting = $state(false);
  let registering = $state(false);

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

  async function confirmRemove() {
    if (!removing) return;
    removeBusy = true;
    const ok = await attempt(async () => {
      await api.deleteRuntime(removing!.id);
      return true;
    }, `${removing.name} removed.`);
    removeBusy = false;
    removing = null;
    if (ok) {
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

  const byKind = $derived.by(() => {
    const m = new Map<string, CatalogEntry[]>();
    for (const c of lists.catalog) m.set(c.kind, [...(m.get(c.kind) ?? []), c]);
    return m;
  });
  const missingKinds = $derived(MAIN_KINDS.filter((k) => app.loaded.catalog && !byKind.has(k)));

  function jobFor(c: CatalogEntry) {
    const id = installJobs[c.id];
    return id ? jobs[id] : undefined;
  }
  const unsupportedNote = (c: CatalogEntry) => `Built for ${c.os}/${c.arch}, not for this PC${app.status ? ` (${app.status.os}/${app.status.arch})` : ""}.`;
</script>

<PageHead title="Installed runtimes" sub="Programs that services use to run your code.">
  <button class="btn" onclick={detect} disabled={detecting}><Icon name="search" /> {detecting ? "Detecting" : "Detect on this PC"}</button>
  <button class="btn" onclick={() => (registering = true)}><Icon name="plus" /> Register by path</button>
</PageHead>

{#if app.loaded.runtimes && lists.runtimes.length === 0}
  <div class="card empty">
    <h2>No runtimes yet</h2>
    <p>Install one from the catalog below, or let Zero2Dev look for tools already on this PC. Without a registered runtime, services fall back to programs on your PATH.</p>
    <button class="btn primary" onclick={detect} disabled={detecting}><Icon name="search" /> Detect on this PC</button>
  </div>
{:else}
  <div class="card t-wrap">
    <table class="t">
      <thead><tr><th>Kind</th><th>Version</th><th>Binary</th><th>Source</th><th><span class="sr-only">Actions</span></th></tr></thead>
      <tbody>
        {#each lists.runtimes as r (r.id)}
          <tr data-runtime={r.id}>
            <td><span class="badge accent">{r.kind}</span></td>
            <td class="mono">{r.version || "unknown"}</td>
            <td class="path">{r.binary}</td>
            <td title={r.installedAt ? fmtDateTime(r.installedAt) : ""}>{r.source}</td>
            <td class="actions">
              <button class="btn sm ghost danger" onclick={() => (removing = r)} aria-label="Remove {r.name}"><Icon name="trash" size={14} /> {r.source === "installed" ? "Remove" : "Forget"}</button>
            </td>
          </tr>
        {/each}
      </tbody>
    </table>
  </div>
{/if}

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
                <td><span class="badge plain">{d.kind}</span></td>
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

<section class="sect">
  <div class="sect-head"><h2>Catalog</h2><span class="muted small">&nbsp;Downloads are verified against published checksums before extraction.</span></div>
  {#if app.loaded.catalog && lists.catalog.length === 0}
    <div class="card empty"><p>The catalog is empty. Use Detect or Register for tools you already have.</p></div>
  {:else}
    <div class="card t-wrap">
      <table class="t">
        <thead><tr><th>Runtime</th><th>Platform</th><th class="w-install">Install</th></tr></thead>
        <tbody>
          {#each lists.catalog as c (c.id)}
            {@const job = jobFor(c)}
            {@const running = job?.state === "running"}
            <tr data-catalog={c.id}>
              <td>
                <div class="cell-title">{c.name}</div>
                <div class="muted small"><span class="badge plain">{c.kind}</span> <span class="mono">{c.version}</span></div>
              </td>
              <td class="mono small">{c.os}/{c.arch}</td>
              <td class="w-install">
                {#if running && job}
                  <div class="prog" role="group" aria-label="Installing {c.name}">
                    <div class="progress" role="progressbar" aria-valuemin="0" aria-valuemax="100" aria-valuenow={Math.round(job.percent)} aria-label="{c.name} install progress"><span style:width="{job.percent}%"></span></div>
                    <div class="small"><span>{job.step || "Starting"}</span> <span class="mono muted">{Math.round(job.percent)}%</span></div>
                  </div>
                {:else if c.installed || job?.state === "done"}
                  <span class="badge running"><span class="dot"></span>Installed</span>
                {:else if !c.supported}
                  <button class="btn sm" disabled aria-describedby="u-{c.id}">Install</button>
                  <div class="hint" id="u-{c.id}">{unsupportedNote(c)}</div>
                {:else}
                  <button class="btn sm primary" onclick={() => installRuntime(c)}><Icon name="download" size={14} /> {job?.state === "failed" ? "Retry install" : "Install"}</button>
                  {#if job?.state === "failed"}<div class="err">{job.error || "Install failed."}</div>{/if}
                {/if}
              </td>
            </tr>
          {/each}
        </tbody>
      </table>
    </div>
  {/if}
  {#if missingKinds.length}
    <div class="notice hintbox">
      <Icon name="alert" />
      <span>
        {#each missingKinds as k, i (k)}<b>{k}</b>{i < missingKinds.length - 1 ? ", " : ""}{/each}
        {missingKinds.length === 1 ? "has" : "have"} no download in the catalog. Use <b>Detect on this PC</b> or <b>Register by path</b> to add
        {missingKinds.length === 1 ? "it" : "them"}.
      </span>
    </div>
  {/if}
</section>

{#if registering}
  <Modal title="Register a runtime by path" onclose={() => (registering = false)}>
    <form id="reg-form" class="dlg-body stack" onsubmit={submitRegister} novalidate>
      {#if rForm}<div class="notice bad" role="alert"><Icon name="alert" /><span>{rForm}</span></div>{/if}
      <div class="field"><label>
        <span class="label">Kind</span>
        <select bind:value={rKind}>{#each RUNTIME_KINDS as k (k)}<option value={k}>{k}</option>{/each}</select></label>
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

{#if removing}
  <ConfirmDialog
    title="{removing.source === 'installed' ? 'Remove' : 'Forget'} {removing.name}?"
    message={removing.source === "installed"
      ? "Zero2Dev installed this runtime, so its folder will be deleted. Services that use it must be changed first."
      : "This only removes the entry from Zero2Dev. The program on disk is not touched."}
    confirmLabel={removing.source === "installed" ? "Remove runtime" : "Forget runtime"}
    busy={removeBusy}
    onconfirm={confirmRemove}
    oncancel={() => (removing = null)}
  />
{/if}

<style>
  .sect {
    margin-top: 28px;
  }
  .sect-head {
    display: flex;
    align-items: baseline;
    margin-bottom: 10px;
  }
  .w-install {
    width: 300px;
  }
  .prog {
    display: grid;
    gap: 4px;
  }
  .prog .small {
    display: flex;
    justify-content: space-between;
  }
  .hintbox {
    margin-top: 12px;
  }
  .hintbox :global(svg) {
    flex: none;
    margin-top: 3px;
  }
</style>
