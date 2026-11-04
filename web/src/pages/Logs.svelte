<script lang="ts">
  import { onMount } from "svelte";
  import { clearLogs, lists, logs, seedLogs, app } from "../lib/store.svelte";
  import type { LogEntry } from "../lib/store.svelte";
  import { MAX_LOG_LINES } from "../lib/store.svelte";
  import LogViewer from "../ui/LogViewer.svelte";
  import PageHead from "../ui/PageHead.svelte";

  let serviceId = $state("");
  const names = $derived(Object.fromEntries(lists.services.map((s) => [s.id, s.name])));

  onMount(() => {
    for (const s of lists.services) if (!logs.byService[s.id]) void seedLogs(s.id, 200);
  });
  let seeded = new Set<string>();
  $effect(() => {
    // Services that appear after mount (first load) also get their tail.
    for (const s of lists.services) {
      if (!seeded.has(s.id)) {
        seeded.add(s.id);
        if (!logs.byService[s.id]) void seedLogs(s.id, 200);
      }
    }
  });
  let last = app.reconnects;
  $effect(() => {
    if (app.reconnects !== last) {
      last = app.reconnects;
      for (const s of lists.services) void seedLogs(s.id, 200);
    }
  });

  const merged = $derived.by(() => {
    const src = serviceId ? (logs.byService[serviceId] ?? []) : Object.values(logs.byService).flat();
    const all: LogEntry[] = serviceId ? src : [...src].sort((a, b) => (a.ts < b.ts ? -1 : a.ts > b.ts ? 1 : a.seq - b.seq));
    return all.length > MAX_LOG_LINES ? all.slice(all.length - MAX_LOG_LINES) : all;
  });
</script>

<PageHead title="Combined logs" sub="Output from every service, live.">
  <label class="row">
    <span class="muted">Service</span>
    <select bind:value={serviceId} style="width: 220px">
      <option value="">All services</option>
      {#each lists.services as s (s.id)}<option value={s.id}>{s.name}</option>{/each}
    </select>
  </label>
</PageHead>

<div class="vh">
  <LogViewer lines={merged} {names} onclear={() => clearLogs(serviceId || undefined)} emptyText="No output yet. Start a service and its lines appear here." />
</div>

<style>
  .vh {
    height: calc(100vh - 52px - 24px - 40px - 70px);
    min-height: 320px;
  }
</style>
