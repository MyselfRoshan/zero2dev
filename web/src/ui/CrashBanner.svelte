<script lang="ts">
  // A warning for services that crashed (the `service.crashed` event), shown until the service is
  // started again by hand or the warning is dismissed. WORKSTREAM C owns this file.
  import { crashes, dismissCrash, lists } from "../lib/store.svelte";
  import type { ServiceCrashedEvent } from "../lib/types";
  import Icon from "./Icon.svelte";

  /** Show only this service's crash (the service page); without it, every crash (the Dashboard). */
  let { serviceId }: { serviceId?: string } = $props();
  const items = $derived(Object.values(crashes).filter((c) => !serviceId || c.id === serviceId));

  /** The headline: what happened and what Zero2Dev does about it. */
  function headline(c: ServiceCrashedEvent): string {
    if (c.gaveUp) return `${c.name} gave up after ${c.attempt} ${c.attempt === 1 ? "attempt" : "attempts"}`;
    if (c.limit <= 0) return `${c.name} stopped unexpectedly`;
    const state = lists.services.find((s) => s.id === c.id)?.state;
    // The restart has happened since: do not keep promising one.
    if (state === "starting" || state === "running") return `${c.name} stopped unexpectedly and was restarted (attempt ${c.attempt} of ${c.limit})`;
    return `${c.name} stopped unexpectedly — restarting in ${c.retryInSec} s (attempt ${c.attempt} of ${c.limit})`;
  }
</script>

{#each items as c (c.id)}
  <div class="notice warn crash" role="alert" data-testid="crash-banner">
    <Icon name="alert" />
    <span class="grow">
      <b>{headline(c)}</b>{#if c.error}<span class="err-text">: {c.error}</span>{/if}
    </span>
    {#if !serviceId}<a class="btn sm ghost" href="#/logs?service={c.id}" data-testid="crash-logs">See logs</a>{/if}
    <button class="btn sm ghost" type="button" onclick={() => dismissCrash(c.id)} data-testid="crash-dismiss">Dismiss</button>
  </div>
{/each}

<style>
  .crash {
    align-items: center;
    margin-bottom: 12px;
  }
  .err-text {
    overflow-wrap: anywhere;
  }
</style>
