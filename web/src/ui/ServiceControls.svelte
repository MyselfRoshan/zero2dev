<script lang="ts">
  import { isActive, restartService, startService, stopService } from "../lib/actions";
  import type { Service } from "../lib/types";
  import Icon from "./Icon.svelte";

  let { service, showRestart = false }: { service: Service; showRestart?: boolean } = $props();
  let busy = $state(false);
  const transitional = $derived(service.state === "stopping");

  async function run(fn: (s: Service) => Promise<void>) {
    busy = true;
    try {
      await fn(service);
    } finally {
      busy = false;
    }
  }
</script>

{#if isActive(service)}
  <button class="btn sm" disabled={busy || transitional} onclick={() => run(stopService)} aria-label="Stop {service.name}"><Icon name="stop" size={14} /> Stop</button>
{:else}
  <button class="btn sm" disabled={busy || transitional} onclick={() => run(startService)} aria-label="Start {service.name}"><Icon name="play" size={14} /> Start</button>
{/if}
{#if showRestart && isActive(service)}
  <button class="btn sm" disabled={busy || transitional} onclick={() => run(restartService)} aria-label="Restart {service.name}"><Icon name="restart" size={14} /> Restart</button>
{/if}
