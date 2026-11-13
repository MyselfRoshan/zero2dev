<script lang="ts">
  import { isAntivirusError, isNetworkError, splitJobError } from "../lib/installer";
  import type { CatalogEntry, Job } from "../lib/types";
  import Icon from "./Icon.svelte";
  import InstallerLog from "./InstallerLog.svelte";

  let { entry, job, onimport }: { entry: CatalogEntry; job: Job; onimport: () => void } = $props();
  const parts = $derived(splitJobError(job.error));
</script>

<div class="notice bad fail" role="alert" data-failed={entry.id}>
  <Icon name="alert" />
  <div class="stack-s">
    <b>{entry.name} could not be installed{parts.step ? ` (step: ${parts.step})` : ""}.</b>
    <div class="errtext mono" data-testid="job-error">{job.error || "No error text was reported."}</div>
    {#if isNetworkError(job.error)}
      <p>
        This looks like a network problem. In <a href="#/settings">Settings</a> set a download proxy, or download
        <code>{entry.fileName}</code> yourself{#if entry.homepage}{" "}from the <a href={entry.homepage} target="_blank" rel="noreferrer">download page</a>{/if} and use
        <button class="linklike" type="button" onclick={onimport}>Import file</button>.
      </p>
    {:else if isAntivirusError(job.error)}
      <p>Antivirus software may be scanning the new files. Wait a moment and choose Retry install.</p>
    {/if}
    <InstallerLog />
  </div>
</div>

<style>
  .fail {
    align-items: flex-start;
  }
  .stack-s {
    display: grid;
    gap: 8px;
    min-width: 0;
    flex: 1;
  }
  .errtext {
    white-space: pre-wrap;
    overflow-wrap: anywhere;
    padding: 8px 10px;
    border-radius: var(--radius);
    background: var(--surface);
    color: var(--text);
    border: 1px solid var(--line-strong);
    font-size: 12px;
    user-select: all;
  }
  .linklike {
    all: unset;
    color: var(--accent-text);
    text-decoration: underline;
    cursor: pointer;
  }
  .linklike:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 2px;
  }
</style>
