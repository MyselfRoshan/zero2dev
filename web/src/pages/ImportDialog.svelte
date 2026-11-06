<script lang="ts">
  import { ApiError } from "../lib/api";
  import { importRuntime, toast } from "../lib/store.svelte";
  import type { CatalogEntry } from "../lib/types";
  import { hasErrors, validateImport, type ImportField } from "../lib/validate";
  import Icon from "../ui/Icon.svelte";
  import Modal from "../ui/Modal.svelte";

  let { entry, onclose }: { entry: CatalogEntry; onclose: () => void } = $props();
  let accept = $state(false);

  let filePath = $state("");
  let sha = $state("");
  let errors = $state<Partial<Record<ImportField, string>>>({});
  let formError = $state("");
  let busy = $state(false);
  let submitted = $state(false);

  function revalidate() {
    if (submitted) errors = validateImport({ filePath, sha256: sha });
  }

  async function submit(e: SubmitEvent) {
    e.preventDefault();
    submitted = true;
    formError = "";
    errors = validateImport({ filePath, sha256: sha });
    if (entry.unverified && !sha.trim() && !accept) {
      errors = { ...errors, sha256: "Paste the checksum from the download page, or tick the box below to import without a checksum." };
    }
    if (hasErrors(errors)) return;
    busy = true;
    try {
      await importRuntime(entry, filePath.trim(), sha, {
        acceptUnverified: entry.unverified && !sha.trim() && accept,
      });
      toast("info", `Importing ${entry.fileName}.`);
      onclose();
    } catch (err) {
      formError = err instanceof ApiError ? err.message : String(err);
    } finally {
      busy = false;
    }
  }
</script>

<Modal title="Import {entry.name} from a file" {onclose}>
  <form id="imp-form" class="dlg-body stack" onsubmit={submit} oninput={revalidate} novalidate>
    {#if formError}<div class="notice bad" role="alert"><Icon name="alert" /><span>{formError}</span></div>{/if}
    <p>
      Download <code>{entry.fileName}</code> in your browser{#if entry.homepage}{" "}from the <a href={entry.homepage} target="_blank" rel="noreferrer">download page <Icon name="external" size={12} /></a>{/if}, then enter where you saved it.
    </p>
    <div class="field">
      <label>
        <span class="label">Path of the downloaded file</span>
        <input type="text" class="mono" bind:value={filePath} aria-invalid={!!errors.filePath} aria-describedby="imp-path-help" placeholder={"C:\\Users\\me\\Downloads\\" + entry.fileName} spellcheck="false" />
      </label>
      {#if errors.filePath}<span class="err">{errors.filePath}</span>{:else}<span class="hint" id="imp-path-help">Absolute path. The file name should be {entry.fileName}.</span>{/if}
    </div>
    <div class="field">
      <label>
        <span class="label">Checksum, SHA-256, SHA-512 or MD5 (optional)</span>
        <input type="text" class="mono" bind:value={sha} aria-invalid={!!errors.sha256} aria-describedby="imp-sha-help" placeholder="32, 64 or 128 hexadecimal characters" spellcheck="false" autocomplete="off" />
      </label>
      {#if errors.sha256}<span class="err">{errors.sha256}</span>{:else}<span class="hint" id="imp-sha-help">Leave empty to check the file against the published checksum. If the network blocks that too, paste the hash from the download page.</span>{/if}
    </div>
    {#if entry.unverified}
      <label class="check"><input type="checkbox" bind:checked={accept} /> <span>Import without a checksum. The publisher does not publish one, so only a pasted checksum can verify this file.</span></label>
    {/if}
    <div class="notice"><Icon name="check" /><span>Zero2Dev works on a copy of this file inside its own data folder. Your downloaded file is never modified or deleted.</span></div>
  </form>
  {#snippet footer()}
    <button class="btn" type="button" onclick={onclose}>Cancel</button>
    <button class="btn primary" type="submit" form="imp-form" disabled={busy}>Import file</button>
  {/snippet}
</Modal>
