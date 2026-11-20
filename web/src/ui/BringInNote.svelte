<script module lang="ts">
  // One shared, quiet read of what Import found, for every Runtimes card at once. Errors (an
  // older daemon without Import, the daemon measuring folders slowly) just hide the note.
  import { api } from "../lib/api";
  import type { MigrateSource, Migration } from "../lib/types";

  const found = $state<{ sources: MigrateSource[]; migrations: Migration[]; at: number }>({
    sources: [],
    migrations: [],
    at: 0,
  });
  let inflight: Promise<void> | null = null;
  const FRESH_MS = 60_000;

  function refresh(force = false): void {
    if (inflight || (!force && found.at && Date.now() - found.at < FRESH_MS)) return;
    inflight = (async () => {
      try {
        const [s, m] = await Promise.all([api.migrationSources(), api.listMigrations()]);
        found.sources = s;
        found.migrations = m;
      } catch {
        found.sources = [];
        found.migrations = [];
      } finally {
        found.at = Date.now();
        inflight = null;
      }
    })();
  }

  /** Called by the Import page after a migration or a revert, so the notes follow. */
  export function invalidateBringIn(): void {
    found.at = 0;
  }
</script>

<script lang="ts">
  import { noteFor } from "../lib/migrate";
  import { lists } from "../lib/store.svelte";
  import Icon from "./Icon.svelte";

  let { kind }: { kind: string } = $props();

  $effect(() => {
    // Again when the runtimes change (an install removes a blocker, an adoption ends a note).
    void lists.runtimes.length;
    refresh(found.at === 0);
  });
  const note = $derived(noteFor(kind, found.sources, found.migrations, lists.runtimes));
</script>

{#if note}
  <p class="bring-in" data-testid="bring-in-note" data-kind={kind}>
    <Icon name="info" size={14} />
    <span>{note.text} <a href={note.href}>{note.link}</a></span>
  </p>
{/if}

<style>
  .bring-in {
    display: flex;
    align-items: flex-start;
    gap: 6px;
    margin: 0;
    padding: 0 16px 12px 56px;
    color: var(--muted);
    font-size: 12px;
    line-height: 20px;
  }
  .bring-in :global(svg) {
    flex: none;
    margin-top: 3px;
    color: var(--accent);
  }
  .bring-in a {
    font-weight: 600;
    white-space: nowrap;
  }
  :global(.tile) > .bring-in {
    padding: 0 12px 12px;
  }
  @media (max-width: 560px) {
    .bring-in {
      padding-left: 16px;
    }
  }
</style>
