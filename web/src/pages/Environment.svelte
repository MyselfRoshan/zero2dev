<script lang="ts">
  // The Environment page (#/environment): which folders go on the Windows user PATH and which
  // user variables get set, item by item. Nothing is on by default and nothing changes in
  // Windows until Apply; the review bar lists exactly what Apply does.
  import { tick, untrack } from "svelte";
  import { ApiError, api } from "../lib/api";
  import { runtimeTitle } from "../lib/catalog";
  import {
    changeCount,
    changeList,
    cloneConfig,
    dirError,
    doneText,
    moveItem,
    newItemId,
    nowSummary,
    ownsAnything,
    pathCommands,
    pathDir,
    pathNote,
    pathStatus,
    pathTitle,
    quickSetup,
    runtimesWithoutPath,
    sameConfig,
    setAllEnabled,
    shadowText,
    shadowed,
    suggestions,
    tryCommand,
    varSourceLabel,
    varStatus,
    varValue,
    type Change,
    type RowStatus,
  } from "../lib/environment";
  import { attempt, envSignal, errMessage, lists, toast } from "../lib/store.svelte";
  import type { EnvConfig, EnvPath, EnvVar, Environment } from "../lib/types";
  import ConfirmDialog from "../ui/ConfirmDialog.svelte";
  import EnvReviewBar from "../ui/EnvReviewBar.svelte";
  import EnvVarEditor from "../ui/EnvVarEditor.svelte";
  import FolderInput from "../ui/FolderInput.svelte";
  import Icon from "../ui/Icon.svelte";
  import KindIcon from "../ui/KindIcon.svelte";
  import PageHead from "../ui/PageHead.svelte";

  let saved = $state<Environment | null>(null);
  let draft = $state<EnvConfig>({ path: [], vars: [] });
  let loadError = $state("");
  let saving = $state(false);
  let applyError = $state("");
  /** What the last Apply did, shown until dismissed or the next change. */
  let result = $state<{ changes: Change[]; cmd: string } | null>(null);
  let adding = $state<null | "runtime" | "folder">(null);
  let customDir = $state("");
  let pathErr = $state("");
  /** The variable editor: undefined = closed, null = adding, else the item being edited. */
  let editing = $state<EnvVar | null | undefined>(undefined);
  let confirmUndo = $state(false);

  const rts = $derived(lists.runtimes);
  const report = $derived(saved?.report);
  const supported = $derived(report?.supported ?? true);
  const savedPath = $derived(new Map((saved?.config.path ?? []).map((p) => [p.id, p])));
  const savedVars = $derived(new Map((saved?.config.vars ?? []).map((v) => [v.id, v])));
  const pathSt = $derived(new Map((report?.path ?? []).map((p) => [p.id, p])));
  const varSt = $derived(new Map((report?.vars ?? []).map((v) => [v.id, v])));
  const changes = $derived(saved ? changeList(draft, saved.config, saved.report, rts) : []);
  const count = $derived(changeCount(changes));
  const dirty = $derived(!!saved && !sameConfig(draft, saved.config));

  const dirOf = (p: EnvPath) => pathDir(p, pathSt.get(p.id), rts, report?.shimsDir ?? "");
  const cmdsOf = (p: EnvPath) => pathCommands(p, pathSt.get(p.id), rts);
  const titleOf = (p: EnvPath) => pathTitle(p, rts);
  const lost = $derived(shadowed(draft.path, cmdsOf, titleOf));
  /** Ids of the folders that are on, in PATH order (the place numbers). */
  const onPath = $derived(draft.path.filter((p) => p.enabled).map((p) => p.id));
  const onCount = (items: { enabled: boolean }[]) => `${items.filter((i) => i.enabled).length} of ${items.length} on`;
  const showQuick = $derived(
    !!saved && supported && !ownsAnything(saved.report) && !draft.path.some((p) => p.enabled) && !draft.vars.some((v) => v.enabled),
  );
  const quickVars = $derived(["java", "go"].filter((k) => rts.some((r) => r.kind === k)).map((k) => (k === "java" ? "JAVA_HOME" : "GOROOT")));
  const chips = $derived(suggestions(rts, draft.vars));
  const freeRuntimes = $derived(runtimesWithoutPath(rts, draft.path));

  /** Reads the daemon's view. Edits that are not applied yet are kept. */
  async function load() {
    try {
      const e = await api.getEnvironment();
      const keep = dirty;
      saved = e;
      if (!keep) draft = cloneConfig(e.config);
      loadError = "";
    } catch (err) {
      loadError = errMessage(err);
    }
  }
  // Initially, and whenever the daemon says the environment changed (a runtime was installed,
  // removed or made default).
  $effect(() => {
    void envSignal.n;
    untrack(() => void load());
  });

  async function focusRow(id: string) {
    await tick();
    document.querySelector<HTMLInputElement>(`[data-item="${id}"] input.switch`)?.focus();
  }

  // ---- folders ----
  function addRuntime(id: string) {
    const item: EnvPath = { id: newItemId("envp_"), source: "runtime", runtimeId: id, enabled: true };
    draft.path.push(item);
    adding = null;
    void focusRow(item.id);
  }
  function addFolder() {
    pathErr = dirError(customDir, draft.path.map(dirOf).filter(Boolean));
    if (pathErr) return;
    const item: EnvPath = { id: newItemId("envp_"), source: "custom", dir: customDir.trim(), enabled: true };
    draft.path.push(item);
    customDir = "";
    adding = null;
    void focusRow(item.id);
  }
  const move = (id: string, by: -1 | 1) => (draft.path = moveItem(draft.path, id, by));
  const removePath = (id: string) => (draft.path = draft.path.filter((p) => p.id !== id));

  // ---- variables ----
  function saveVar(v: Omit<EnvVar, "id" | "enabled">) {
    const ed = editing;
    if (ed) {
      const at = draft.vars.findIndex((x) => x.id === ed.id);
      if (at >= 0) draft.vars[at] = { ...v, id: ed.id, enabled: draft.vars[at].enabled };
      editing = undefined;
      void focusRow(ed.id);
      return;
    }
    const item: EnvVar = { ...v, id: newItemId("envv_"), enabled: true };
    draft.vars.push(item);
    editing = undefined;
    void focusRow(item.id);
  }
  function addSuggested(name: string, kind: string) {
    const item: EnvVar = { id: newItemId("envv_"), name, source: "default", kind, enabled: true };
    draft.vars.push(item);
    void focusRow(item.id);
  }
  const removeVar = (id: string) => {
    draft.vars = draft.vars.filter((v) => v.id !== id);
    if (editing && editing.id === id) editing = undefined;
  };

  // ---- apply ----
  async function put(cfg: EnvConfig, okText?: string): Promise<Environment | undefined> {
    applyError = "";
    saving = true;
    const res = await attempt(async () => {
      try {
        return await api.putEnvironment(cfg);
      } catch (err) {
        // 409: Windows refused (or this is not Windows); nothing was saved.
        if (err instanceof ApiError && err.status === 409) applyError = errMessage(err);
        throw err;
      }
    }, okText);
    saving = false;
    return res;
  }
  async function apply(e?: SubmitEvent) {
    e?.preventDefault();
    const done = changes;
    // No toast: the result notice at the top says what happened, and gets the focus.
    const res = await put(draft);
    if (!res) return;
    saved = res;
    draft = cloneConfig(res.config);
    editing = undefined;
    result = { changes: done, cmd: tryCommand(res.config, res.report, rts) };
    await tick();
    const el = document.querySelector<HTMLElement>('[data-testid="env-result"]');
    el?.scrollIntoView({ block: "nearest", behavior: "smooth" });
    el?.focus({ preventScroll: true });
  }
  /** "Add it back" and friends: apply the saved config again, keeping any edits on screen. */
  async function reapply() {
    if (!saved) return;
    const res = await put(saved.config, "Applied again.");
    if (!res) return;
    const keep = dirty;
    saved = res;
    if (!keep) draft = cloneConfig(res.config);
  }
  async function undoAll() {
    confirmUndo = false;
    if (!saved) return;
    draft = setAllEnabled(setAllEnabled(draft, "path", false), "vars", false);
    await apply();
  }
  function discard() {
    if (saved) draft = cloneConfig(saved.config);
    applyError = "";
    editing = undefined;
    adding = null;
  }
  function quick() {
    draft = quickSetup(draft, rts);
    result = null;
    queueMicrotask(() => document.getElementById("env-path-h")?.scrollIntoView({ block: "start", behavior: "smooth" }));
  }
  async function copy(text: string) {
    try {
      await navigator.clipboard.writeText(text);
      toast("success", "Copied.");
    } catch {
      toast("error", "Could not copy to the clipboard in this browser context.");
    }
  }
</script>

{#snippet status(s: RowStatus, id: string)}
  <span class="status s-{s.tone}" id="st-{id}" data-testid="env-status">
    <span class="dot" aria-hidden="true"></span>
    <span>{s.text}</span>
    {#if s.fix?.href}<a href={s.fix.href} class="fix">{s.fix.label}</a>{:else if s.fix?.reapply}<button type="button" class="linkish fix" onclick={reapply} disabled={saving}>{s.fix.label}</button>{/if}
  </span>
{/snippet}

<PageHead sub="Make the runtimes Zero2Dev installed work in every terminal, not only inside Zero2Dev.">
  <button class="btn" type="button" onclick={() => untrack(() => void load())} disabled={saving} data-testid="env-refresh"><Icon name="restart" size={14} /> Check again</button>
</PageHead>

{#if loadError}
  <div class="notice bad" role="alert" data-testid="env-load-error">
    <Icon name="alert" /><span class="grow">Could not read the environment: {loadError}</span>
    <button class="btn sm" type="button" onclick={() => void load()}>Try again</button>
  </div>
{:else if !saved || !report}
  <p class="muted">Loading...</p>
{:else}
  <div class="stack">
    <div class="now" data-testid="env-now">
      <p class="now-line">{nowSummary(report, saved.config)}</p>
      <details class="how">
        <summary class="linkish">What does this change in Windows?</summary>
        <ul>
          <li>Only your own user environment (the <code>HKCU\Environment</code> part of the registry). No administrator rights, nothing for other users.</li>
          <li>A variable you already have is never overwritten, and a folder you already had on PATH is never removed.</li>
          <li>Switching an item off takes back exactly what Zero2Dev added. The PATH you had before is kept in <code>{report.shimsDir}\.registry-backup.json</code>.</li>
          <li>Terminals you open afterwards see the change. Open terminals keep what they had.</li>
        </ul>
      </details>
    </div>

    {#if !supported}
      <div class="notice warn" data-testid="env-unsupported">
        <Icon name="alert" />
        <span>This system is not Windows, so the user environment cannot be changed here. You can still use the activate scripts in Settings, Command line.</span>
      </div>
    {/if}
    {#if report.error}
      <div class="notice bad" role="alert"><Icon name="alert" /><span>{report.error}</span></div>
    {/if}
    {#if applyError}
      <div class="notice bad" role="alert" data-testid="env-error">
        <Icon name="alert" />
        <span>Windows did not accept the change: {applyError} Nothing was saved.</span>
      </div>
    {/if}
    {#if result && !dirty}
      <div class="notice ok" role="status" tabindex="-1" data-testid="env-result">
        <Icon name="circleCheck" />
        <div class="grow stack-s">
          <b>Done.</b>
          {#if result.changes.length}
            <ul class="plain">{#each result.changes as c, i (i)}<li>{doneText(c)}</li>{/each}</ul>
          {/if}
          {#if result.cmd}
            <div class="row wrap">
              <span>Open a <b>new</b> terminal and try</span>
              <code class="chip">{result.cmd}</code>
              <button class="btn sm" type="button" onclick={() => copy(result!.cmd)}><Icon name="copy" size={14} /> Copy</button>
            </div>
          {/if}
        </div>
        <button class="btn sm ghost icon" type="button" aria-label="Dismiss" onclick={() => (result = null)}><Icon name="x" size={14} /></button>
      </div>
    {/if}

    {#if showQuick}
      <section class="section-card" aria-labelledby="env-quick-h" data-testid="env-quick">
        <header>
          <h2 id="env-quick-h">Quick start</h2>
          <p>What most people want: <code>node</code>, <code>php</code>, <code>java</code>, <code>go</code> ... in every terminal, always the version marked Default.</p>
        </header>
        <div class="body">
          <div class="row wrap">
            <button class="btn" type="button" onclick={quick} data-testid="env-quick-setup"><Icon name="sparkles" size={14} /> Use the default versions everywhere</button>
            <span class="hint">Turns on <b>Default versions</b>{quickVars.length ? ` and ${quickVars.join(" and ")}` : ""}. You review the change before anything happens.</span>
          </div>
        </div>
      </section>
    {/if}

    <form id="env-form" class="stack" onsubmit={apply} novalidate>
      <section class="section-card" aria-labelledby="env-path-h" data-testid="env-path-card">
        <header>
          <h2 id="env-path-h">Folders on your PATH</h2>
          <p>A terminal looks for a command in these folders from the top down, so a folder higher on the list wins.</p>
        </header>
        <div class="body">
          {#if draft.path.length > 1}<div class="bulk">
            <span class="muted small" data-testid="env-path-count">{onCount(draft.path)}</span>
            <span class="spacer"></span>
            <button class="btn sm ghost" type="button" onclick={() => (draft = setAllEnabled(draft, "path", true))} disabled={draft.path.length === 0} data-testid="env-path-all-on">Turn all on</button>
            <button class="btn sm ghost" type="button" onclick={() => (draft = setAllEnabled(draft, "path", false))} disabled={draft.path.length === 0} data-testid="env-path-all-off">Turn all off</button>
          </div>{/if}
          {#if draft.path.length === 0}
            <p class="muted">No folders in the list. Add one below.</p>
          {:else}
            <ol class="items">
              {#each draft.path as p, i (p.id)}
                {@const title = titleOf(p)}
                {@const s = pathStatus(p, savedPath.get(p.id), pathSt.get(p.id), { supported, draftList: draft.path, savedList: saved.config.path })}
                {@const cmds = cmdsOf(p)}
                {@const shadow = lost.get(p.id)}
                <li class="item" class:off={!p.enabled} data-testid="env-path-row" data-item={p.id}>
                  <input type="checkbox" role="switch" class="switch" bind:checked={p.enabled} aria-label="{title} on your PATH" aria-describedby="st-{p.id}" />
                  <div class="what">
                    <div class="title">{#if onPath.length > 1 && p.enabled}<span class="pos" aria-hidden="true">{onPath.indexOf(p.id) + 1}</span><span class="sr-only">Place {onPath.indexOf(p.id) + 1} on PATH: </span>{/if}{title}</div>
                    {#if dirOf(p)}<code class="dir">{dirOf(p)}</code>{/if}
                    <span class="hint">{pathNote(p)}</span>
                    {#if cmds.length}
                      <span class="hint cmds">Gives you {#each cmds as c (c)}<code class="chip-code">{c}</code>{/each}</span>
                    {/if}
                    {#if shadow}<span class="hint shadow" data-testid="env-shadow"><Icon name="info" size={14} /> {shadowText(shadow)}</span>{/if}
                    {@render status(s, p.id)}
                  </div>
                  <div class="acts">{#if draft.path.length > 1}
                    <button class="btn sm ghost icon" type="button" onclick={() => move(p.id, -1)} disabled={i === 0} aria-label="Move {title} up" title="Move up"><Icon name="chevronUp" size={14} /></button>
                    <button class="btn sm ghost icon" type="button" onclick={() => move(p.id, 1)} disabled={i === draft.path.length - 1} aria-label="Move {title} down" title="Move down"><Icon name="chevronDown" size={14} /></button>{/if}
                    <button class="btn sm ghost icon danger" type="button" onclick={() => removePath(p.id)} aria-label="Remove {title} from the list" title="Remove from the list"><Icon name="trash" size={14} /></button>
                  </div>
                </li>
              {/each}
            </ol>
          {/if}

          {#if adding === "runtime"}
            <div class="panel" role="group" aria-labelledby="env-add-rt-h" data-testid="env-add-runtime">
              <h3 class="panel-title" id="env-add-rt-h">Add one runtime version</h3>
              <span class="hint">Its folder goes on your PATH, so that version runs even when it is not the default.</span>
              <ul class="picks">
                {#each rts as r (r.id)}
                  {@const used = !freeRuntimes.includes(r)}
                  <li>
                    <button class="pick" type="button" disabled={used} onclick={() => addRuntime(r.id)}>
                      <KindIcon kind={r.kind} size={18} />
                      <span class="grow">{runtimeTitle(r)}{r.default ? " (default)" : ""}</span>
                      <span class="muted small">{used ? "In the list" : "Add"}</span>
                    </button>
                  </li>
                {/each}
              </ul>
              <div><button class="btn ghost" type="button" onclick={() => (adding = null)}>Cancel</button></div>
            </div>
          {:else if adding === "folder"}
            <div class="panel" role="group" aria-labelledby="env-add-dir-h" data-testid="env-add-folder">
              <h3 class="panel-title" id="env-add-dir-h">Add your own folder</h3>
              <div class="field">
                <FolderInput bind:value={customDir} label="Folder to put on PATH" title="Choose a folder for your PATH" placeholder="C:\tools\bin" invalid={!!pathErr} />
                {#if pathErr}<span class="err" role="alert">{pathErr}</span>{:else}<span class="hint">A full path, such as <code>C:\tools\bin</code>.</span>{/if}
              </div>
              <div class="row">
                <button class="btn" type="button" onclick={addFolder} data-testid="env-path-add">Add folder</button>
                <button class="btn ghost" type="button" onclick={() => ((adding = null), (pathErr = ""))}>Cancel</button>
              </div>
            </div>
          {:else}
            <div class="row wrap">
              <button class="btn" type="button" onclick={() => (adding = "runtime")} disabled={rts.length === 0} data-testid="env-add-runtime-open"><Icon name="plus" size={14} /> Add a runtime version</button>
              <button class="btn" type="button" onclick={() => (adding = "folder")} data-testid="env-add-folder-open"><Icon name="folder" size={14} /> Add your own folder</button>
              {#if rts.length === 0}<span class="hint">No runtime is installed yet. <a href="#/runtimes">Install one</a>.</span>{/if}
            </div>
          {/if}
        </div>
      </section>

      <section class="section-card" aria-labelledby="env-vars-h" data-testid="env-vars-card">
        <header>
          <h2 id="env-vars-h">Variables</h2>
          <p>Some tools find a runtime through a variable, such as Maven through <code>JAVA_HOME</code>. Zero2Dev only sets a variable you do not have yet.</p>
        </header>
        <div class="body">
          {#if draft.vars.length > 1}<div class="bulk">
            <span class="muted small" data-testid="env-vars-count">{onCount(draft.vars)}</span>
            <span class="spacer"></span>
            <button class="btn sm ghost" type="button" onclick={() => (draft = setAllEnabled(draft, "vars", true))} disabled={draft.vars.length === 0} data-testid="env-vars-all-on">Turn all on</button>
            <button class="btn sm ghost" type="button" onclick={() => (draft = setAllEnabled(draft, "vars", false))} disabled={draft.vars.length === 0} data-testid="env-vars-all-off">Turn all off</button>
          </div>{/if}
          {#if draft.vars.length === 0}
            <p class="muted">No variables in the list. Add one below.</p>
          {:else}
            <ul class="items">
              {#each draft.vars as v (v.id)}
                {@const value = varValue(v, varSt.get(v.id), rts)}
                {@const s = varStatus(v, savedVars.get(v.id), varSt.get(v.id), value, supported)}
                <li class="item" class:off={!v.enabled} data-testid="env-var-row" data-item={v.id} data-name={v.name}>
                  <input type="checkbox" role="switch" class="switch" bind:checked={v.enabled} aria-label="Set {v.name}" aria-describedby="st-{v.id}" />
                  <div class="what">
                    <div class="title"><code>{v.name}</code></div>
                    {#if value}<code class="dir">{value}</code>{/if}
                    <span class="hint">{varSourceLabel(v, rts)}</span>
                    {@render status(s, v.id)}
                  </div>
                  <div class="acts">
                    <button class="btn sm ghost icon" type="button" onclick={() => (editing = v)} aria-label="Edit {v.name}" title="Edit"><Icon name="edit" size={14} /></button>
                    <button class="btn sm ghost icon danger" type="button" onclick={() => removeVar(v.id)} aria-label="Remove {v.name} from the list" title="Remove from the list"><Icon name="trash" size={14} /></button>
                  </div>
                </li>
              {/each}
            </ul>
          {/if}

          {#if editing !== undefined}
            {#key editing?.id ?? "new"}
              <EnvVarEditor item={editing} runtimes={rts} others={draft.vars.filter((x) => x.id !== editing?.id).map((x) => x.name)} onsave={saveVar} oncancel={() => (editing = undefined)} />
            {/key}
          {:else}
            <div class="row wrap">
              <button class="btn" type="button" onclick={() => (editing = null)} data-testid="env-var-add"><Icon name="plus" size={14} /> Add a variable</button>
              {#each chips as c (c.name)}
                <button class="btn sm chip-btn" type="button" onclick={() => addSuggested(c.name, c.kind)} data-testid="env-var-suggest"><Icon name="plus" size={14} /> {c.label}</button>
              {/each}
            </div>
          {/if}
        </div>
      </section>

      {#if ownsAnything(report)}
        <div class="danger-zone" data-testid="env-undo">
          <div class="grow">
            <div class="label">Remove everything Zero2Dev added</div>
            <span class="hint">Takes Zero2Dev's folders off your PATH and deletes the variables it set. The list stays, with every item off.</span>
          </div>
          <button class="btn danger" type="button" onclick={() => (confirmUndo = true)} disabled={saving}><Icon name="undo" size={14} /> Remove everything</button>
        </div>
      {/if}
    </form>

    {#if changes.length}
      <EnvReviewBar formId="env-form" {changes} {count} {saving} ondiscard={discard} />
    {/if}
  </div>
{/if}

{#if confirmUndo}
  <ConfirmDialog
    title="Remove everything Zero2Dev added?"
    message="Zero2Dev takes its folders off your PATH and deletes the variables it set. Anything you set yourself stays as it is. You can switch items on again at any time."
    confirmLabel="Remove everything"
    busy={saving}
    onconfirm={undoAll}
    oncancel={() => (confirmUndo = false)}
  />
{/if}

<style>
  .stack {
    display: grid;
    gap: 16px;
    max-width: 860px;
    min-width: 0;
  }
  .stack-s {
    display: grid;
    gap: 6px;
    min-width: 0;
  }
  .now {
    display: grid;
    gap: 4px;
  }
  .now-line {
    margin: 0;
    font-weight: 600;
  }
  .how summary {
    width: fit-content;
    cursor: pointer;
  }
  .how ul,
  .plain {
    margin: 6px 0 0;
    padding-left: 20px;
    display: grid;
    gap: 4px;
    color: var(--muted);
  }
  .plain {
    margin: 0;
    color: var(--text);
    overflow-wrap: anywhere;
  }
  .notice.ok {
    border-color: var(--ok-border);
    background: var(--ok-soft);
  }
  .notice.ok > :global(svg) {
    color: var(--ok-fill);
  }
  .chip {
    padding: 0 6px;
    border: 1px solid var(--line-strong);
    border-radius: var(--radius-sm);
    background: var(--surface);
  }
  .bulk {
    display: flex;
    align-items: center;
    gap: 4px;
  }
  .items {
    list-style: none;
    margin: 0;
    padding: 0;
    display: grid;
  }
  .item {
    display: grid;
    grid-template-columns: auto minmax(0, 1fr) auto;
    align-items: start;
    gap: 4px 12px;
    padding: 12px 0;
    border-top: 1px solid var(--line);
  }
  .item:first-child {
    border-top: 0;
    padding-top: 0;
  }
  .item .switch {
    margin-top: 1px;
  }
  .what {
    display: grid;
    gap: 4px;
    min-width: 0;
    justify-items: start;
  }
  .pos {
    display: inline-grid;
    place-items: center;
    min-width: 20px;
    height: 20px;
    margin-right: 8px;
    padding: 0 4px;
    border-radius: var(--radius-sm);
    background: var(--accent-soft);
    color: var(--accent-text);
    font-size: 12px;
  }
  .title {
    font-weight: 600;
  }
  .item.off .title,
  .item.off .dir {
    color: var(--muted);
  }
  .dir {
    padding: 2px 8px;
    border: 1px solid var(--line);
    border-radius: var(--radius-sm);
    background: var(--sunken);
    overflow-wrap: anywhere;
    max-width: 100%;
  }
  .cmds {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 4px;
  }
  .shadow {
    display: flex;
    align-items: flex-start;
    gap: 6px;
  }
  .shadow :global(svg) {
    flex: none;
    margin-top: 3px;
  }
  .status {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 4px 8px;
    font-size: 14px;
  }
  .status .dot {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    flex: none;
  }
  .s-ok {
    color: var(--ok);
  }
  .s-ok .dot {
    background: var(--ok-fill);
  }
  .s-pending {
    color: var(--accent-text);
  }
  .s-pending .dot {
    background: var(--accent);
  }
  .s-warn {
    color: var(--warn);
  }
  .s-warn .dot {
    background: var(--warn-fill);
  }
  .s-muted {
    color: var(--muted);
  }
  .s-muted .dot {
    border: 1px solid var(--subtle);
  }
  .fix {
    font-weight: 600;
  }
  .acts {
    display: flex;
    gap: 2px;
  }
  .panel {
    display: grid;
    gap: 12px;
    padding: 16px;
    border: 1px solid var(--line);
    border-radius: var(--radius-lg);
    background: var(--surface-2);
  }
  .panel-title {
    font-size: 14px;
    font-weight: 600;
    margin: 0;
  }
  .picks {
    list-style: none;
    margin: 0;
    padding: 0;
    display: grid;
    gap: 4px;
  }
  .pick {
    display: flex;
    align-items: center;
    gap: 10px;
    width: 100%;
    min-height: 40px;
    padding: 6px 12px;
    border: 1px solid var(--line-strong);
    border-radius: var(--radius);
    background: var(--surface);
    color: var(--text);
    font: inherit;
    text-align: left;
    cursor: pointer;
  }
  .pick:hover:not(:disabled) {
    border-color: var(--accent);
  }
  .pick:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 2px;
  }
  .pick:disabled {
    cursor: default;
    color: var(--muted);
    background: var(--sunken);
  }
  .danger-zone {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 10px;
    padding: 12px 16px;
    border: 1px solid var(--bad-border);
    border-radius: var(--radius-lg);
    background: var(--surface);
  }
  .danger-zone .grow {
    display: grid;
    gap: 2px;
    flex: 1 1 240px;
  }
  @media (max-width: 560px) {
    .item {
      grid-template-columns: auto minmax(0, 1fr);
    }
    .acts {
      grid-column: 2;
    }
    .bulk {
      flex-wrap: wrap;
    }
  }
</style>
