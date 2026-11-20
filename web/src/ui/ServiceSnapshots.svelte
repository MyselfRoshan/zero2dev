<script lang="ts">
  // Snapshots of a database service on its page: take, restore, delete (0.0.5). Nothing for other kinds.
  import { isDatabaseKind } from "../lib/snapshots";
  import type { Service } from "../lib/types";
  import SnapshotManager from "./SnapshotManager.svelte";

  let { service }: { service: Service } = $props();
</script>

{#if isDatabaseKind(service.kind)}
  <section class="section-card svc-snaps" data-testid="service-snapshots" aria-labelledby="svc-snaps-h">
    <header>
      <h2 id="svc-snaps-h">Snapshots</h2>
      <p>A saved copy of {service.name}'s settings and data. Take one before an import or a reset, and restore it to go back.</p>
    </header>
    <div class="body">
      <SnapshotManager scope="service" ownerId={service.id} ownerName={service.name} />
    </div>
  </section>
{/if}

<style>
  .svc-snaps {
    margin-top: 16px;
  }
</style>
