<script lang="ts">
  import { dismissToast, toasts } from "../lib/store.svelte";
  import Icon from "./Icon.svelte";
</script>

<div class="toasts" role="region" aria-label="Notifications">
  <div aria-live="polite" aria-atomic="false">
    {#each toasts.filter((t) => t.kind !== "error") as t (t.id)}
      <div class="toast {t.kind}">
        <Icon name="check" size={16} />
        <span class="grow">{t.text}</span>
        <button class="btn ghost icon sm" onclick={() => dismissToast(t.id)} aria-label="Dismiss"><Icon name="x" size={14} /></button>
      </div>
    {/each}
  </div>
  <div role="alert" aria-live="assertive" aria-atomic="false">
    {#each toasts.filter((t) => t.kind === "error") as t (t.id)}
      <div class="toast error">
        <Icon name="alert" size={16} />
        <span class="grow">{t.text}</span>
        <button class="btn ghost icon sm" onclick={() => dismissToast(t.id)} aria-label="Dismiss"><Icon name="x" size={14} /></button>
      </div>
    {/each}
  </div>
</div>

<style>
  .toasts {
    position: fixed;
    right: 16px;
    bottom: 16px;
    z-index: 100;
    display: grid;
    gap: 8px;
    width: min(420px, calc(100vw - 32px));
    pointer-events: none;
  }
  .toasts > div {
    display: grid;
    gap: 8px;
  }
  .toast {
    pointer-events: auto;
    display: flex;
    align-items: flex-start;
    gap: 12px;
    padding: 12px 12px 12px 16px;
    border-radius: var(--radius-lg);
    background: var(--elevated);
    border: 1px solid var(--line);
    box-shadow: var(--shadow);
    word-break: break-word;
  }
  .toast :global(svg) {
    margin-top: 3px;
    flex: none;
  }
  .toast.success > :global(svg) {
    color: var(--ok-fill);
  }
  .toast.error > :global(svg) {
    color: var(--bad-fill);
  }
</style>
