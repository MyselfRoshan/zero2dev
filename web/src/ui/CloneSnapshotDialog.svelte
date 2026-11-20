<script lang="ts">
  // Clone a project snapshot into a new project: a name, the domain suffix with a preview of the
  // new domains, and the note that both projects share the same code folder.
  import { untrack } from "svelte";
  import { api } from "../lib/api";
  import { waitForJob } from "../lib/snapshotJobs";
  import { clonePreview, sharedFolders, validateDomainSuffix } from "../lib/snapshots";
  import { errMessage, lists, loadProjects, loadServices, loadSites, toast } from "../lib/store.svelte";
  import type { Job, Snapshot } from "../lib/types";
  import Icon from "./Icon.svelte";
  import JobProgress from "./JobProgress.svelte";
  import Modal from "./Modal.svelte";

  type Props = { snap: Snapshot; ondone: (projectId: string) => void; onclose: () => void };
  let { snap, ondone, onclose }: Props = $props();

  let name = $state(untrack(() => `${snap.project?.name ?? "Project"} copy`.slice(0, 64)));
  let suffix = $state("-copy");
  let busy = $state(false);
  let job = $state<Job | null>(null);
  let error = $state("");

  const nameError = $derived(!name.trim() ? "Give the new project a name." : name.trim().length > 64 ? "Use at most 64 characters." : "");
  const suffixError = $derived(validateDomainSuffix(suffix));
  const preview = $derived(clonePreview(snap.sites, suffix, lists.sites.map((s) => s.domain)));
  const folders = $derived(sharedFolders(snap));

  async function clone(e: Event) {
    e.preventDefault();
    if (nameError || suffixError || busy) return;
    busy = true;
    error = "";
    try {
      const started = await api.cloneSnapshot(snap.id, { name: name.trim(), domainSuffix: suffix });
      const end = await waitForJob(started, (j) => (job = j));
      if (end.state === "done") {
        await Promise.all([loadProjects(), loadServices(), loadSites()]);
        toast("success", `${name.trim()} created from "${snap.name}".`);
        ondone(end.projectId ?? "");
      } else error = end.error || "The clone failed.";
    } catch (err) {
      error = errMessage(err);
    } finally {
      busy = false;
    }
  }
</script>

<Modal title="Clone {snap.name} into a new project" onclose={() => { if (!busy) onclose(); }}>
  <form class="dlg-body clone" id="clone-snapshot-form" onsubmit={clone} data-testid="clone-dialog">
    <p class="muted">Makes a new project with copies of the snapshot's services, sites and database data. Each copy gets a free port of its own.</p>
    <div class="grid2">
      <label class="field">
        <span class="label">New project name</span>
        <input type="text" bind:value={name} maxlength="64" disabled={busy} aria-invalid={!!nameError} aria-describedby="clone-name-hint" />
        {#if nameError}<span class="err" id="clone-name-hint">{nameError}</span>{/if}
      </label>
      <label class="field">
        <span class="label">Domain suffix</span>
        <input type="text" class="mono-input" bind:value={suffix} maxlength="30" disabled={busy} aria-invalid={!!suffixError} aria-describedby="clone-suffix-hint" />
        {#if suffixError}<span class="err" id="clone-suffix-hint">{suffixError}</span>{:else}<span class="hint" id="clone-suffix-hint">Added to the first part of each site's domain.</span>{/if}
      </label>
    </div>
    {#if preview.length && !suffixError}
      <div class="field">
        <span class="label">New addresses</span>
        <ul class="preview" data-testid="clone-preview">
          {#each preview as p, i (i)}<li><span class="mono">{p.from}</span> <Icon name="arrowRight" size={14} /> <span class="mono" data-testid="clone-domain">{p.to}</span></li>{/each}
        </ul>
      </div>
    {/if}
    {#if folders.length}
      <div class="notice" data-testid="clone-shared"><Icon name="info" /><span>The code is not copied: both projects use the same folder{folders.length === 1 ? "" : "s"} (<span class="mono">{folders.join(", ")}</span>). Changes to the code show up in both.</span></div>
    {/if}
    {#if job}<JobProgress {job} label="Clone" />{/if}
    {#if error}<div class="notice bad" role="alert"><Icon name="alert" /><span>{error}</span></div>{/if}
  </form>
  {#snippet footer()}
    <button class="btn" type="button" disabled={busy} onclick={onclose}>Cancel</button>
    <button class="btn primary" type="submit" form="clone-snapshot-form" disabled={busy || !!nameError || !!suffixError}>{#if busy}<span class="spin" aria-hidden="true"></span>{/if} Clone</button>
  {/snippet}
</Modal>

<style>
  .clone {
    display: grid;
    gap: 16px;
  }
  .clone p {
    margin: 0;
  }
  .preview {
    list-style: none;
    margin: 0;
    padding: 0;
    display: grid;
    gap: 2px;
  }
  .preview li {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 6px;
    overflow-wrap: anywhere;
  }
</style>
