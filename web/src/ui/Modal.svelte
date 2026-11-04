<script lang="ts">
  import type { Snippet } from "svelte";
  import Icon from "./Icon.svelte";

  type Props = {
    title: string;
    onclose: () => void;
    narrow?: boolean;
    children: Snippet;
    footer?: Snippet;
  };
  let { title, onclose, narrow = false, children, footer }: Props = $props();
  let dlg: HTMLDialogElement;
  const titleId = `dlg-${Math.random().toString(36).slice(2, 8)}`;

  $effect(() => {
    if (!dlg.open) dlg.showModal();
    return () => {
      if (dlg.open) dlg.close();
    };
  });
</script>

<dialog bind:this={dlg} class:narrow aria-labelledby={titleId} onclose={onclose} oncancel={(e) => { e.preventDefault(); onclose(); }}>
  <div class="dlg-head">
    <h2 id={titleId} class="grow">{title}</h2>
    <button class="btn ghost icon sm" type="button" onclick={onclose} aria-label="Close"><Icon name="x" /></button>
  </div>
  {@render children()}
  {#if footer}<div class="dlg-foot">{@render footer()}</div>{/if}
</dialog>
