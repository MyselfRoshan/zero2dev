<script lang="ts">
  // The plan of one program: what to import (each item with whether it is worth it), Copy or Move,
  // the free space, and Review. It is the side panel on a wide screen and sits under the row on a
  // narrow one. Nothing starts here: Review opens the dialog on the page.
  import type { Snippet } from "svelte";
  import { ago } from "../lib/format";
  import {
    adviceFor,
    blockerAction,
    fmtSize,
    modeAllowed,
    targetText,
    totalSize,
    type FreeSpace,
  } from "../lib/migrate";
  import type { MigrateMode, MigrateSource, Migration } from "../lib/types";
  import AdviceChip from "./AdviceChip.svelte";
  import Icon from "./Icon.svelte";

  type Props = {
    s: MigrateSource;
    /** The migration that holds this program now, if any. */
    active: Migration | undefined;
    mode: MigrateMode;
    checked: string[];
    free: FreeSpace;
    planError: string;
    /** Review can start: nothing blocks it. */
    canReview: boolean;
    busy: boolean;
    checking: boolean;
    primary: boolean;
    onreview: () => void;
    onclose: () => void;
    onrecheck: () => void;
    /** The database form, for the "database" program. */
    dbform?: Snippet;
  };
  let {
    s,
    active,
    mode = $bindable(),
    checked = $bindable(),
    free,
    planError,
    canReview,
    busy,
    checking,
    primary,
    onreview,
    onclose,
    onrecheck,
    dbform,
  }: Props = $props();

  const picked = $derived(s.items.filter((i) => i.exists && checked.includes(i.label)));
  const moveOk = $derived(modeAllowed(s, "move"));
  const modeHint = (md: MigrateMode) =>
    md === "move"
      ? `The originals are saved, then removed from their old place. The installed ${s.label} starts fresh; Revert puts them back.`
      : `The originals stay where they are, and the installed ${s.label} keeps working as before.`;
</script>

<div class="plan" role="region" aria-labelledby="plan-h-{s.id}" id="plan-{s.id}" data-testid="migrate-plan" data-plan={s.id}>
  <div class="head">
    <h3 id="plan-h-{s.id}" tabindex="-1">{s.label}</h3>
    <button class="btn sm ghost icon" type="button" aria-label="Close the plan" onclick={onclose}><Icon name="x" size={14} /></button>
  </div>

  {#each s.blockers as b (b)}
    {@const act = blockerAction(b)}
    <div class="notice warn" role="status" data-testid="migrate-blocker">
      <Icon name="alert" />
      <span class="grow">{b}.</span>
      {#if act.type === "link"}<a class="btn sm" href={act.href}>{act.label}</a>
      {:else if act.type === "recheck"}<button class="btn sm" type="button" onclick={onrecheck} disabled={checking} data-testid="migrate-recheck"><Icon name="restart" size={14} /> {act.label}</button>{/if}
    </div>
  {/each}
  {#if active && s.program !== "database"}
    <div class="notice" data-testid="migrate-already">
      <Icon name="info" />
      <span>Imported {ago(active.createdAt)}. Revert it in History to import it again.</span>
    </div>
  {/if}

  {#if s.program === "database"}
    {@render dbform?.()}
  {:else}
    <fieldset class="items" data-testid="migrate-items">
      <legend class="label">What to import</legend>
      {#each s.items as it (it.label)}
        {@const adv = adviceFor(s, it)}
        <label class="item" class:off={!it.exists}>
          <input type="checkbox" value={it.label} bind:group={checked} disabled={!it.exists || s.items.length === 1} />
          <span class="what">
            <span class="it-title">{it.label} <span class="muted small">{it.exists ? fmtSize(it.sizeBytes) : "not on this PC"}</span>{#if it.exists}<AdviceChip level={adv.level} />{/if}</span>
            {#if it.exists && adv.reason}<span class="muted small" data-testid="migrate-advice">{adv.reason}</span>{/if}
            <span class="fromto small"><code>{it.from}</code> <Icon name="arrowRight" size={12} /><span class="sr-only">to</span> <code>{it.to}</code></span>
          </span>
        </label>
      {/each}
    </fieldset>

    <div class="field">
      <span class="label" id="mode-l-{s.id}">How</span>
      <div class="modes" role="radiogroup" aria-labelledby="mode-l-{s.id}" data-testid="migrate-mode">
        <label class="mode" class:on={mode === "copy"}>
          <input type="radio" name="mode-{s.id}" value="copy" bind:group={mode} disabled={!modeAllowed(s, "copy")} />
          <span class="what"><span class="it-title">Copy</span><span class="hint">{modeHint("copy")}</span></span>
        </label>
        <label class="mode" class:on={mode === "move"} class:off={!moveOk} title={moveOk ? undefined : s.copyOnlyReason}>
          <input type="radio" name="mode-{s.id}" value="move" bind:group={mode} disabled={!moveOk} aria-describedby={moveOk ? undefined : `copy-only-${s.id}`} />
          <span class="what">
            <span class="it-title">Move</span>
            {#if moveOk}<span class="hint">{modeHint("move")}</span>
            {:else}<span class="hint" id="copy-only-{s.id}" data-testid="migrate-copy-only">Not offered: {s.copyOnlyReason}</span>{/if}
          </span>
        </label>
      </div>
    </div>
  {/if}

  {#if free.text}
    <p class="free {free.state}" data-testid="migrate-free-space" role={free.state === "short" ? "alert" : undefined}>
      <Icon name={free.state === "ok" ? "circleCheck" : "alert"} size={14} /> {free.text}
    </p>
  {/if}
  {#if targetText(s)}
    <div class="notice warn" data-testid="migrate-target-warning"><Icon name="alert" /><span>{targetText(s)}</span></div>
  {/if}
  {#if s.note}<p class="hint note" data-testid="migrate-note"><Icon name="info" size={14} /> {s.note}</p>{/if}
  {#if planError}
    <div class="notice bad" role="alert" data-testid="migrate-error"><Icon name="alert" /><span>{planError}</span></div>
  {/if}

  <div class="row wrap actions">
    <button class="btn" class:primary type="button" onclick={onreview} disabled={!canReview} data-testid="migrate-review">
      Review{s.program === "database" ? " the copy" : ""}{s.program !== "database" && picked.length ? ` (${fmtSize(totalSize(picked))})` : ""}
    </button>
    <button class="btn ghost" type="button" onclick={onclose}>Cancel</button>
    {#if busy}<span class="hint">Wait until the running job is done.</span>{/if}
  </div>
</div>

<style>
  .plan {
    display: grid;
    gap: 16px;
    padding: 16px;
    border: 1px solid var(--line);
    border-radius: var(--radius-lg);
    background: var(--surface);
    min-width: 0;
  }
  .head {
    display: flex;
    align-items: center;
    gap: 8px;
  }
  .head h3 {
    flex: 1;
    margin: 0;
    font-size: 16px;
  }
  /* The heading only takes focus from a link (?source=) so a screen reader reads the plan: no ring on text. */
  .head h3:focus {
    outline: none;
  }
  .items {
    display: grid;
    gap: 8px;
    margin: 0;
    padding: 0;
    border: 0;
    min-width: 0;
  }
  .items legend {
    padding: 0;
    margin-bottom: 4px;
  }
  .item,
  .mode {
    display: flex;
    align-items: flex-start;
    gap: 10px;
    padding: 8px 12px;
    border: 1px solid var(--line);
    border-radius: var(--radius);
    background: var(--surface);
    cursor: pointer;
    min-width: 0;
  }
  .item input,
  .mode input {
    margin-top: 3px;
    flex: none;
  }
  .item.off,
  .mode.off {
    color: var(--muted);
    cursor: not-allowed;
  }
  .modes {
    display: grid;
    gap: 8px;
  }
  .mode.on {
    border-color: var(--accent);
    background: var(--accent-soft);
  }
  .what {
    display: grid;
    gap: 2px;
    min-width: 0;
  }
  .it-title {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 2px 8px;
    font-weight: 600;
  }
  .fromto {
    display: inline-flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 4px;
    color: var(--muted);
    min-width: 0;
  }
  .fromto code {
    font-size: 12px;
    overflow-wrap: anywhere;
  }
  .free {
    display: flex;
    align-items: flex-start;
    gap: 6px;
    margin: 0;
    font-size: 13px;
    color: var(--muted);
  }
  .free :global(svg) {
    flex: none;
    margin-top: 3px;
  }
  .free.tight {
    color: var(--warn);
  }
  .free.short {
    color: var(--bad);
  }
  .note {
    display: flex;
    align-items: flex-start;
    gap: 6px;
    margin: 0;
  }
  .note :global(svg) {
    flex: none;
    margin-top: 3px;
  }
  @media (max-width: 560px) {
    .plan {
      padding: 12px;
    }
  }
</style>
