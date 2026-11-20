<script lang="ts">
  // "Recommended" / "Optional" / "Not worth it": whether an item is worth importing.
  import { adviceLabel, type AdviceLevel } from "../lib/migrate";

  let { level }: { level: AdviceLevel } = $props();
</script>

<span class="chip {level}" data-advice={level}>{adviceLabel(level)}</span>

<style>
  .chip {
    display: inline-flex;
    align-items: center;
    height: 20px;
    padding: 0 6px;
    border: 1px solid var(--line-strong);
    border-radius: var(--radius-sm);
    font-size: 12px;
    font-weight: 400;
    line-height: 18px;
    background: var(--surface-2);
    color: var(--muted);
    white-space: nowrap;
  }
  .chip.recommended {
    background: var(--accent-soft);
    border-color: var(--accent-border);
    color: var(--accent-text);
  }
</style>
