<script lang="ts">
  // The bar pinned to the bottom of the Environment page while changes are staged: how many,
  // the exact list of what Apply does in Windows, Discard, and the page's one primary button.
  import { beforeNavigate, router } from "../lib/router.svelte";
  import type { Change } from "../lib/environment";
  import Icon from "./Icon.svelte";

  type Props = { formId: string; changes: Change[]; count: number; saving: boolean; ondiscard: () => void };
  let { formId, changes, count, saving, ondiscard }: Props = $props();
  let open = $state(true);

  // Leaving or reloading the page asks first.
  $effect(() => {
    const warn = (e: BeforeUnloadEvent) => e.preventDefault();
    window.addEventListener("beforeunload", warn);
    const unguard = beforeNavigate(
      (to) => router.route.name !== "environment" || to.name === "environment" || window.confirm("Discard the changes you have not applied?"),
    );
    return () => {
      unguard();
      window.removeEventListener("beforeunload", warn);
    };
  });

  const ICON: Record<Change["kind"], string> = { add: "plus", remove: "x", move: "reorder", set: "check", unset: "x", keep: "info", list: "list" };
  const label = $derived(count === 1 ? "Apply 1 change" : `Apply ${count} changes`);
</script>

<div class="bar" role="region" aria-label="Changes to apply" data-testid="env-review">
  <div class="head">
    <span class="dot" aria-hidden="true"></span>
    <button class="linkish grow toggle" type="button" aria-expanded={open} aria-controls="env-changes" onclick={() => (open = !open)}>
      {count === 1 ? "1 change" : `${count} changes`} not applied yet · {open ? "Hide" : "Show"} what changes
    </button>
    <button class="btn" type="button" onclick={ondiscard} disabled={saving}>Discard</button>
    <button class="btn primary" type="submit" form={formId} disabled={saving}>{saving ? "Applying..." : label}</button>
  </div>
  {#if open}
    <ul class="changes" id="env-changes" data-testid="env-changes">
      {#each changes as c, i (i)}
        <li class="c-{c.kind}"><Icon name={ICON[c.kind]} size={14} /><span>{c.text}</span></li>
      {/each}
    </ul>
    <p class="hint">Only your own user environment changes (no administrator rights). Terminals you open afterwards see it; open ones keep what they had.</p>
  {/if}
</div>

<style>
  .bar {
    position: sticky;
    bottom: 0;
    z-index: 3;
    display: grid;
    gap: 8px;
    margin-top: 16px;
    padding: 12px 16px;
    border: 1px solid var(--line);
    border-radius: var(--radius-lg);
    background: var(--elevated);
    box-shadow: var(--shadow);
    max-width: 860px;
  }
  .head {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 8px 12px;
  }
  .dot {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: var(--accent);
    flex: none;
  }
  .toggle {
    text-align: left;
    min-width: 0;
  }
  .changes {
    list-style: none;
    margin: 0;
    padding: 0;
    display: grid;
    gap: 4px;
    max-height: 30vh;
    overflow: auto;
  }
  .changes li {
    display: flex;
    align-items: flex-start;
    gap: 8px;
    overflow-wrap: anywhere;
    font-size: 14px;
  }
  .changes li :global(svg) {
    flex: none;
    margin-top: 4px;
  }
  .c-add :global(svg),
  .c-set :global(svg) {
    color: var(--ok);
  }
  .c-remove :global(svg),
  .c-unset :global(svg) {
    color: var(--bad);
  }
  .c-move :global(svg),
  .c-keep :global(svg),
  .c-list :global(svg) {
    color: var(--muted);
  }
  .hint {
    margin: 0;
  }
</style>
