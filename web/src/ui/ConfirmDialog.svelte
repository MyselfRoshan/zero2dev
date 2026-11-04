<script lang="ts">
  import Modal from "./Modal.svelte";
  type Props = {
    title: string;
    message: string;
    confirmLabel: string;
    busy?: boolean;
    onconfirm: () => void;
    oncancel: () => void;
  };
  let { title, message, confirmLabel, busy = false, onconfirm, oncancel }: Props = $props();
</script>

<Modal {title} narrow onclose={oncancel}>
  <div class="dlg-body"><p>{message}</p></div>
  {#snippet footer()}
    <button class="btn" type="button" onclick={oncancel}>Cancel</button>
    <button class="btn danger solid" type="button" disabled={busy} onclick={onconfirm}>{confirmLabel}</button>
  {/snippet}
</Modal>
