<script lang="ts">
  import type { Snippet } from "svelte";
  let { title, sub, children }: { title?: string; sub?: string; children?: Snippet } = $props();
</script>

<div class="page-head">
  <div class="grow">
    {#if title}<h2 class="page-title">{title}</h2>{/if}
    {#if sub}<p class={title ? "muted" : "lede"}>{sub}</p>{/if}
  </div>
  {#if children}<div class="row wrap">{@render children()}</div>{/if}
</div>

<style>
  .page-head {
    display: flex;
    align-items: flex-end;
    gap: 16px;
    margin-bottom: 14px;
  }
  .lede {
    color: var(--muted);
    font-size: 14px;
  }
  .page-title {
    font-size: 15px;
  }
</style>
