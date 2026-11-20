<script lang="ts">
  // The strip at the top of Import: what was found, what is worth importing first, what is
  // already in and what the backups take. "Select recommended" ticks the programs worth importing
  // in so they can be imported together.
  import { fmtSize, type Recommended, type Summary } from "../lib/migrate";
  import Icon from "./Icon.svelte";

  type Props = {
    sum: Summary;
    rec: Recommended[];
    /** Every recommended program is ticked already. */
    allPicked: boolean;
    /** This is the one primary button of the view (nothing else is). */
    primary: boolean;
    onselect: () => void;
    onbackups: () => void;
  };
  let { sum, rec, allPicked, primary, onselect, onbackups }: Props = $props();
</script>

<section class="sum" aria-label="Summary" data-testid="migrate-summary">
  <dl class="tiles">
    <div class="tile">
      <dt>Found on this PC</dt>
      <dd data-testid="migrate-sum-found">{sum.found}</dd>
      <span class="muted small">{sum.found === 1 ? "program" : "programs"} with something to import</span>
    </div>
    <div class="tile">
      <dt>Can come in</dt>
      <dd data-testid="migrate-sum-size">{fmtSize(sum.bringable)}</dd>
      <span class="muted small">not imported yet</span>
    </div>
    <div class="tile">
      <dt>In Zero2Dev</dt>
      <dd data-testid="migrate-sum-in">{sum.broughtIn}</dd>
      <span class="muted small">{sum.broughtIn === 1 ? "migration" : "migrations"} in use</span>
    </div>
    <div class="tile">
      <dt>Backups</dt>
      <dd data-testid="migrate-sum-backups">{sum.backups > 0 ? fmtSize(sum.backups) : "None"}</dd>
      <span class="small">{#if sum.backups > 0}<button class="linkbtn" type="button" onclick={onbackups}>See and free up</button>{:else}<span class="muted">nothing saved</span>{/if}</span>
    </div>
  </dl>

  {#if rec.length}
    <div class="rec" data-testid="migrate-recommended">
      <Icon name="sparkles" size={18} />
      <p class="grow">
        <b>Worth importing first:</b>
        {#each rec as r, i (r.source.id)}{i ? ", " : " "}{r.source.label} <span class="muted">({fmtSize(r.bytes)})</span>{/each}.
        <span class="muted">Settings, extensions and global tools you would miss. Caches that fill up again are left out.</span>
      </p>
      <button class="btn" class:primary type="button" onclick={onselect} data-testid="migrate-select">
        {allPicked ? "Clear selection" : `Select recommended (${rec.length})`}
      </button>
    </div>
  {/if}
</section>

<style>
  .sum {
    display: grid;
    gap: 12px;
  }
  .tiles {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(150px, 1fr));
    gap: 12px;
    margin: 0;
  }
  .tile {
    display: grid;
    gap: 2px;
    padding: 12px 16px;
    border: 1px solid var(--line);
    border-radius: var(--radius-lg);
    background: var(--surface);
    min-width: 0;
  }
  .tile dt {
    color: var(--muted);
    font-size: 13px;
  }
  .tile dd {
    margin: 0;
    font-size: 24px;
    line-height: 32px;
    font-weight: 600;
  }
  .rec {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 8px 12px;
    padding: 12px 16px;
    border: 1px solid var(--accent-border);
    border-radius: var(--radius-lg);
    background: var(--accent-soft);
  }
  .rec > :global(svg) {
    color: var(--accent-text);
    flex: none;
  }
  .rec p {
    margin: 0;
    flex: 1 1 260px;
    min-width: 0;
  }
  .linkbtn {
    all: unset;
    cursor: pointer;
    color: var(--accent-text);
    text-decoration: underline;
  }
  .linkbtn:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 2px;
  }
</style>
