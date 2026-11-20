<script lang="ts">
  // Snapshots of a project (#/projects/{id}/snapshots): take, restore, clone, delete (0.0.5).
  import { app, lists } from "../lib/store.svelte";
  import Icon from "../ui/Icon.svelte";
  import PageHead from "../ui/PageHead.svelte";
  import SnapshotManager from "../ui/SnapshotManager.svelte";

  let { id }: { id: string } = $props();
  const project = $derived(lists.projects.find((p) => p.id === id));
</script>

<PageHead sub="Named copies of this project's services, settings, runtime versions, sites and database data. Restore one to go back, or clone it into a new project." />

<div class="card card-pad page" data-testid="project-snapshots" data-project-id={id}>
  <a class="back small" href="#/projects"><Icon name="back" size={14} /> All projects</a>
  {#if app.loaded.projects && !project}
    <div class="notice warn" data-testid="project-gone"><Icon name="alert" /><span>This project was deleted. Its snapshots are kept: restoring one brings the project back.</span></div>
  {/if}
  {#if app.loaded.projects}
    <SnapshotManager scope="project" ownerId={id} ownerName={project?.name ?? "the project"} exists={!!project} primary />
  {/if}
</div>

<style>
  .page {
    display: grid;
    gap: 16px;
  }
  .back {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    justify-self: start;
    text-decoration: none;
  }
</style>
