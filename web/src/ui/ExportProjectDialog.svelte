<script lang="ts">
  // Export a project to a zero2dev.json (docs/API.md, 0.0.6 "Project file"): the Save as dialog of the
  // daemon's PC, the result in words (what was left out), and an explicit Overwrite when the file exists.
  import { api } from "../lib/api";
  import { baseName, dirOf } from "../lib/databases";
  import { PROJECT_FILE, PROJECT_FILE_FILTERS, exportPathError, exportSummary, isExistsError, projectFolder, suggestExportPath } from "../lib/projectfile";
  import { app, errMessage, lists, toast } from "../lib/store.svelte";
  import type { Project } from "../lib/types";
  import Icon from "./Icon.svelte";
  import Modal from "./Modal.svelte";

  type Props = { project: Project; onclose: () => void };
  let { project, onclose }: Props = $props();

  // The suggestion is read once, when the dialog opens.
  // svelte-ignore state_referenced_locally
  let path = $state(suggestExportPath(projectFolder(project, lists.services)));
  let busy = $state(false);
  let browsing = $state(false);
  let error = $state("");
  let exists = $state("");
  let result = $state<{ saved: string; warnings: string[] } | null>(null);

  const canBrowse = $derived(!!app.status?.folderPicker);
  const pathError = $derived(path.trim() ? exportPathError(path) : "");

  async function browse() {
    browsing = true;
    try {
      const r = await api.pickFile({ title: `Save ${project.name} as`, start: dirOf(path), save: true, defaultName: baseName(path) || PROJECT_FILE, filters: PROJECT_FILE_FILTERS });
      if (!r.cancelled && r.path) {
        path = r.path;
        exists = "";
      }
    } catch (e) {
      toast("error", errMessage(e));
    } finally {
      browsing = false;
    }
  }

  async function save(overwrite: boolean) {
    const bad = exportPathError(path);
    if (bad) {
      error = bad;
      return;
    }
    busy = true;
    error = "";
    try {
      const r = await api.exportProject(project.id, { path: path.trim(), overwrite });
      result = exportSummary(r);
      exists = "";
    } catch (e) {
      if (isExistsError(e) && !overwrite) exists = e.message;
      else error = errMessage(e);
    } finally {
      busy = false;
    }
  }
</script>

<Modal title="Export {project.name}" narrow onclose={onclose}>
  <div class="dlg-body stack" data-testid="export-dialog">
    {#if result}
      <div class="row"><Icon name="circleCheck" size={20} /><p data-testid="export-saved"><b>{result.saved}</b></p></div>
      {#if result.warnings.length}
        <div class="notice warn" data-testid="export-warnings">
          <Icon name="alert" />
          <div class="grow">
            <b>Left out of the file</b>
            <ul class="plain">{#each result.warnings as w (w)}<li data-testid="export-warning">{w}</li>{/each}</ul>
          </div>
        </div>
      {/if}
      <p class="hint">Put the file next to the project's code and share it with the project. Whoever imports it sets any left-out secret again.</p>
    {:else}
      <p>Writes a small file, <code>{PROJECT_FILE}</code>, that describes this project's services, sites and runtimes, so it can be set up again on another PC.</p>
      <div class="field">
        <label>
          <span class="label">Save as</span>
          <span class="path-row">
            <input type="text" class="mono" bind:value={path} oninput={() => (exists = "")} aria-invalid={!!pathError} aria-describedby="exp-help" placeholder="D:\work\shop\{PROJECT_FILE}" spellcheck="false" autocomplete="off" data-testid="export-path" />
            {#if canBrowse}
              <button class="btn" type="button" onclick={browse} disabled={browsing} aria-label="Browse for where to save the project file">
                {#if browsing}<span class="spin" aria-hidden="true"></span> Waiting...{:else}<Icon name="folder" size={14} /> Browse...{/if}
              </button>
            {/if}
          </span>
        </label>
        {#if pathError}<span class="err" id="exp-help">{pathError}</span>{:else}<span class="hint" id="exp-help">Save it next to the project's code: every folder in the file is written relative to this file's folder. A folder name also works.</span>{/if}
      </div>
      <p class="hint">Left out on purpose: passwords, variables whose name looks like a secret, the contents of <code>.env</code>, and absolute paths.</p>
      {#if exists}
        <div class="notice warn" role="alert" data-testid="export-exists">
          <Icon name="alert" />
          <span class="grow">{exists}. Overwrite replaces that file with the new one. Nothing else is changed.</span>
        </div>
      {/if}
      {#if error}<div class="notice bad" role="alert" data-testid="export-error"><Icon name="alert" /><span>{error}</span></div>{/if}
    {/if}
  </div>
  {#snippet footer()}
    {#if result}
      <button class="btn primary" type="button" onclick={onclose} data-testid="export-close">Done</button>
    {:else}
      <button class="btn" type="button" onclick={onclose}>Cancel</button>
      {#if exists}
        <button class="btn solid danger" type="button" disabled={busy} onclick={() => save(true)} data-testid="export-overwrite">{#if busy}<span class="spin" aria-hidden="true"></span> Saving...{:else}Overwrite{/if}</button>
      {:else}
        <button class="btn primary" type="button" disabled={busy || !path.trim() || !!pathError} onclick={() => save(false)} data-testid="export-go">{#if busy}<span class="spin" aria-hidden="true"></span> Saving...{:else}Export{/if}</button>
      {/if}
    {/if}
  {/snippet}
</Modal>

<style>
  .stack {
    display: grid;
    gap: 12px;
  }
  .stack p {
    margin: 0;
  }
  .path-row {
    display: flex;
    gap: 8px;
  }
  .path-row input {
    flex: 1;
    min-width: 0;
  }
  .plain {
    list-style: none;
    margin: 4px 0 0;
    padding: 0;
    display: grid;
    gap: 4px;
    overflow-wrap: anywhere;
  }
  [data-testid="export-saved"] {
    overflow-wrap: anywhere;
  }
  .row > :global(svg) {
    color: var(--ok-fill);
    flex: none;
  }
</style>
