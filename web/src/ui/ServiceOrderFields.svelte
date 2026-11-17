<script lang="ts">
  // "Start order" fields of the service form: the services this one needs running first and,
  // for apps, the database it uses (which is a dependency too, and gives it DATABASE_URL & co).
  // WORKSTREAM B owns this file; the props below are the contract with ServiceForm.svelte.
  import { lists } from "../lib/store.svelte";
  import { isDatabase } from "../lib/catalog";
  import type { ServiceKind } from "../lib/types";

  type Props = {
    /** Ids of the services that must be running first (bindable). */
    dependsOn: string[];
    /** Id of the database service this one uses, or "" (bindable). */
    databaseId: string;
    /** The service's type: databases themselves have no database to pick. */
    kind: ServiceKind | "";
    /** The service being edited (excluded from the choices); undefined when creating. */
    selfId?: string;
    /** A message from the daemon about these fields, for example a dependency loop. */
    error?: string;
  };
  let { dependsOn = $bindable(), databaseId = $bindable(), kind, selfId, error = "" }: Props = $props();

  const others = $derived(lists.services.filter((s) => s.id !== selfId));
  const databases = $derived(others.filter((s) => isDatabase(s.kind)));

  function toggle(id: string, on: boolean) {
    dependsOn = on ? [...dependsOn.filter((x) => x !== id), id] : dependsOn.filter((x) => x !== id);
  }
</script>

{#if kind && !isDatabase(kind)}
  <div class="field" data-testid="order-fields">
    <label>
      <span class="label">Database <span class="muted opt">(optional)</span></span>
      <select bind:value={databaseId} data-testid="database-select">
        <option value="">None</option>
        {#each databases as d (d.id)}<option value={d.id}>{d.name}</option>{/each}
      </select>
    </label>
    {#if databases.length === 0}
      <span class="hint">There is no database service yet. Add one under Services to pick it here.</span>
    {:else}
      <span class="hint">The database this app uses. It is started first and stopped last, and the app gets its connection as DATABASE_URL, DB_HOST, DB_PORT and similar variables.</span>
    {/if}
  </div>
{/if}
<div class="field" role="group" aria-labelledby="dep-l" data-testid="depends-on">
  <span class="label" id="dep-l">Starts after <span class="muted opt">(optional)</span></span>
  {#if others.length === 0}
    <span class="hint">There are no other services yet.</span>
  {:else}
    <div class="deps" class:invalid={!!error}>
      {#each others as s (s.id)}
        <label class="check">
          <input type="checkbox" checked={dependsOn.includes(s.id)} onchange={(e) => toggle(s.id, e.currentTarget.checked)} data-testid="dep-{s.id}" />
          <span>{s.name}</span>
          {#if s.id === databaseId}<span class="muted small">(its database)</span>{/if}
        </label>
      {/each}
    </div>
    {#if !error}<span class="hint">Zero2Dev starts these first and waits until they are running. When you stop everything, they stop last.</span>{/if}
  {/if}
  {#if error}<span class="err">{error}</span>{/if}
</div>

<style>
  .deps {
    display: grid;
    max-height: 176px;
    overflow-y: auto;
    padding: 2px 12px;
    border: 1px solid var(--line-strong);
    border-radius: var(--radius);
    background: var(--surface);
  }
  .deps.invalid {
    border-color: var(--bad);
  }
  .check span {
    overflow-wrap: anywhere;
  }
</style>
