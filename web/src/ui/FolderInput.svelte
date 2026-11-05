<script lang="ts">
  // A folder path field with a "Browse..." button that opens the native Windows folder dialog
  // through the daemon (the browser's own picker never reveals the full path). Typing the path
  // still works; Browse is only shown when the daemon can open the dialog.
  import { api, ApiError } from "../lib/api";
  import { app, toast } from "../lib/store.svelte";
  import Icon from "./Icon.svelte";

  type Props = {
    value: string;
    label: string;
    /** Title of the native dialog. */
    title?: string;
    placeholder?: string;
    invalid?: boolean;
    /** Called after Browse filled in a folder (programmatic changes fire no input event). */
    onpicked?: (path: string) => void;
  };
  let { value = $bindable(), label, title, placeholder, invalid = false, onpicked }: Props = $props();

  const canBrowse = $derived(!!app.status?.folderPicker);
  let waiting = $state(false);

  async function browse() {
    waiting = true;
    try {
      const r = await api.pickFolder(title ?? label, value.trim());
      if (!r.cancelled && r.path) {
        value = r.path;
        onpicked?.(r.path);
      }
    } catch (err) {
      toast("error", err instanceof ApiError ? err.message : String(err));
    } finally {
      waiting = false;
    }
  }
</script>

<div class="folder-input">
  <input type="text" class="mono" bind:value aria-label={label} aria-invalid={invalid} {placeholder} spellcheck="false" readonly={waiting} />
  {#if canBrowse}
    <button type="button" class="btn" onclick={browse} disabled={waiting} aria-label="Browse for {label}" title="Choose the folder in a Windows dialog">
      {#if waiting}<span class="spin" aria-hidden="true"></span> Waiting...{:else}<Icon name="folder" size={14} /> Browse...{/if}
    </button>
  {/if}
</div>
{#if waiting}<span class="hint" role="status">A Windows folder dialog is open. Choose a folder there, or close it to cancel.</span>{/if}

<style>
  .folder-input {
    display: flex;
    gap: 8px;
  }
  .folder-input input {
    flex: 1;
    min-width: 0;
  }
  .folder-input .btn {
    flex: none;
  }
</style>
