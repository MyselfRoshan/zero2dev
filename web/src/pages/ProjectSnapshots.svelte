<script lang="ts">
  // Snapshots of a project (#/projects/{id}/snapshots): take, restore, clone, delete (0.0.5,
  // snapshot-ui). Wave 0 stub.
  import PageHead from "../ui/PageHead.svelte";

  let { id }: { id: string } = $props();
</script>

<PageHead sub="Named copies of this project's services, settings, runtime versions, sites and database data." />
<div class="card empty" data-testid="project-snapshots" data-project-id={id}><p class="muted">Snapshots are coming in this version.</p></div>
