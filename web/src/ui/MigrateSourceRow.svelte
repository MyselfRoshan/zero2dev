<script lang="ts">
  // One program in the Import list: its icon, what it holds, whether it is imported, whether it
  // is worth it, a tick for importing several at once, and the button that opens its plan. On a
  // narrow screen the plan is rendered right under the row (children).
  import type { Snippet } from "svelte";
  import {
    adviceFor,
    modeAllowed,
    sourceIconKind,
    sourceSummary,
    type SourceStatus,
  } from "../lib/migrate";
  import { lists } from "../lib/store.svelte";
  import type { MigrateSource } from "../lib/types";
  import AdviceChip from "./AdviceChip.svelte";
  import Icon from "./Icon.svelte";
  import KindIcon from "./KindIcon.svelte";

  type Props = {
    s: MigrateSource;
    status: SourceStatus;
    open: boolean;
    /** The tick for a batch: shown only when the row can be imported with the others. */
    pickable: boolean;
    picked: boolean;
    onopen: () => void;
    onpick: (on: boolean) => void;
    openLabel: string;
    children?: Snippet;
  };
  let { s, status, open, pickable, picked, onopen, onpick, openLabel, children }: Props = $props();
  const advice = $derived(adviceFor(s));
</script>

<li class="src" class:open data-testid="migrate-source" data-source={s.id} data-status={status.key}>
  <div class="src-head">
    <span class="pick">
      {#if pickable}
        <input type="checkbox" checked={picked} onchange={(e) => onpick(e.currentTarget.checked)} aria-label="Select {s.label} to import with others" data-testid="migrate-pick" />
      {/if}
    </span>
    {#if s.program === "database"}<span class="db-ico"><Icon name="database" size={20} /></span>{:else}<KindIcon kind={sourceIconKind(s, lists.runtimes)} size={22} />{/if}
    <div class="grow src-name">
      <span class="src-title">{s.label}
        <span class="badge" class:running={status.tone === "ok"} class:plain={status.tone !== "ok"} data-testid="migrate-status"><span class="dot"></span>{status.text}</span>
        {#if status.key === "new" && s.program !== "database"}<AdviceChip level={advice.level} />{/if}
      </span>
      <span class="muted small">{sourceSummary(s)}{s.program !== "database" && !modeAllowed(s, "move") ? " · copy only" : ""}</span>
      {#if status.key === "blocked"}<span class="small why" data-testid="migrate-why">{s.blockers[0]}.</span>{/if}
    </div>
    <button class="btn sm" type="button" aria-expanded={open} aria-controls="plan-{s.id}" onclick={onopen} data-testid="migrate-open">
      {open ? "Close" : openLabel}
    </button>
  </div>
  {@render children?.()}
</li>

<style>
  .src {
    border-top: 1px solid var(--line);
    padding: 12px 0;
    min-width: 0;
  }
  .src:first-child {
    border-top: 0;
    padding-top: 0;
  }
  .src.open > .src-head {
    background: var(--accent-soft);
    margin: -6px -8px;
    padding: 6px 8px;
    border-radius: var(--radius);
  }
  .src-head {
    display: flex;
    align-items: center;
    gap: 12px;
    min-width: 0;
  }
  .pick {
    display: inline-flex;
    width: 16px;
    flex: none;
    justify-content: center;
  }
  .db-ico {
    display: inline-flex;
    color: var(--accent);
  }
  .src-name {
    display: grid;
    gap: 2px;
    min-width: 0;
  }
  .src-title {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 4px 8px;
    font-weight: 600;
  }
  .why {
    color: var(--muted);
  }
  /* Narrow: the name gets the line, and the button drops under it, in line with the name. */
  @media (max-width: 560px) {
    .src-head {
      flex-wrap: wrap;
    }
    .src-name {
      flex: 1 1 calc(100% - 70px);
    }
    .src-head > :global(.btn) {
      flex: none;
      margin-left: 62px;
    }
  }
</style>
