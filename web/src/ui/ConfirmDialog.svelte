<script lang="ts">
  import Modal from "./Modal.svelte";
  type Props = {
    title: string;
    message: string;
    confirmLabel: string;
    busy?: boolean;
    /** Red button for destructive actions (default); false for a normal primary action. */
    danger?: boolean;
    onconfirm: () => void;
    oncancel: () => void;
  };
  let { title, message, confirmLabel, busy = false, danger = true, onconfirm, oncancel }: Props = $props();
</script>

<Modal {title} narrow onclose={oncancel}>
  <div class="dlg-body"><p>{message}</p></div>
  {#snippet footer()}
    <button class="btn" type="button" onclick={oncancel}>Cancel</button>
    <button class="btn solid" class:danger class:primary={!danger} type="button" disabled={busy} onclick={onconfirm}>{confirmLabel}</button>
  {/snippet}
</Modal>
