<script lang="ts">
  // The Projects page (#/projects): groups of services and sites that start and stop together.
  import { api } from "../lib/api";
  import { startAll, stopAll } from "../lib/actions";
  import { deleteProjectMessage, projectStatus, servicesLine, sitesLine, stateLabel } from "../lib/projects";
  import { navigate } from "../lib/router.svelte";
  import { app, attempt, lists, loadProjects, loadSites } from "../lib/store.svelte";
  import type { Project } from "../lib/types";
  import ConfirmDialog from "../ui/ConfirmDialog.svelte";
  import ExportProjectDialog from "../ui/ExportProjectDialog.svelte";
  import Icon from "../ui/Icon.svelte";
  import PageHead from "../ui/PageHead.svelte";

  let deleting = $state<Project | null>(null);
  let deleteBusy = $state(false);
  let exporting = $state<Project | null>(null);
  // The project whose Start or Stop is in flight; a project start waits until its services are up.
  let busyId = $state<string | null>(null);

  async function run(p: Project, start: boolean) {
    busyId = p.id;
    try {
      if (start) {
        await startAll(p.id);
        void loadSites(); // a project start also switches its sites on
      } else await stopAll(p.id);
    } finally {
      busyId = null;
    }
  }

  async function confirmDelete() {
    if (!deleting) return;
    const p = deleting;
    deleteBusy = true;
    const ok = await attempt(async () => {
      await api.deleteProject(p.id);
      return true;
    }, `${p.name} deleted.`);
    deleteBusy = false;
    deleting = null;
    if (ok) await loadProjects();
  }
</script>

<PageHead sub="A project groups the services and sites of one piece of work, so you can start and stop them together.">
  <button class="btn" type="button" onclick={() => navigate("#/create")} data-tour="create-project" data-testid="projects-from-framework"><Icon name="rocket" /> New project from a framework</button>
  <button class="btn" type="button" onclick={() => navigate("#/projects/import")} data-tour="import-project" data-testid="projects-import" title="Set a project up from a zero2dev.json file, or from the folder of a cloned repository"><Icon name="download" /> Import project</button>
  <button class="btn primary" type="button" onclick={() => navigate("#/projects/new")} data-tour="new-project"><Icon name="plus" /> New project</button>
</PageHead>

{#if app.loaded.projects && lists.projects.length === 0}
  <div class="card empty" data-testid="projects-empty">
    <h2>No projects yet</h2>
    <p>Group the services and sites that belong together, such as an API, its database and its local address. One click then starts the whole set.</p>
    <div class="row wrap">
      <button class="btn primary" type="button" onclick={() => navigate("#/projects/new")}><Icon name="plus" /> Create your first project</button>
      <button class="btn" type="button" onclick={() => navigate("#/create")} data-testid="projects-empty-from-framework"><Icon name="rocket" /> Start from a framework</button>
      <button class="btn" type="button" onclick={() => navigate("#/projects/import")} data-testid="projects-empty-import"><Icon name="download" /> Import a project</button>
    </div>
  </div>
{:else}
  <div class="card t-wrap">
    <table class="t">
      <thead><tr><th>Project</th><th>Status</th><th>Services</th><th>Sites</th><th><span class="sr-only">Actions</span></th></tr></thead>
      <tbody>
        {#each lists.projects as p (p.id)}
          {@const st = projectStatus(p, lists.services, lists.sites)}
          <tr data-project={p.name}>
            <td><a class="cell-title" href="#/projects/{encodeURIComponent(p.id)}/edit" title="Edit {p.name}">{p.name}</a></td>
            <td>
              <span class="badge {st.state === 'running' ? 'running' : st.state === 'stopped' ? 'stopped' : st.state === 'partial' ? 'accent partial' : 'plain'}" data-state={st.state}>
                <span class="dot"></span>{stateLabel(st.state)}
              </span>
              {#if st.failed}<span class="badge failed" title="{st.failed} of its services failed"><span class="dot"></span>{st.failed} failed</span>{/if}
            </td>
            <td data-testid="project-services-count">{servicesLine(st)}</td>
            <td data-testid="project-sites-count">{sitesLine(st)}</td>
            <td class="actions">
              <button class="btn sm" type="button" disabled={busyId !== null || !st.canStart} onclick={() => run(p, true)} aria-label="Start {p.name}" title="Starts the project's services, switches its sites on and runs the site server"><Icon name="play" size={14} /> Start</button>
              <button class="btn sm" type="button" disabled={busyId !== null || !st.canStop} onclick={() => run(p, false)} aria-label="Stop {p.name}" title="Stops the project's services. Sites and other services keep running"><Icon name="stop" size={14} /> Stop</button>
              <a class="btn sm" href="#/projects/{encodeURIComponent(p.id)}/snapshots" aria-label="Snapshots of {p.name}" title="Take, restore or clone snapshots of this project" data-testid="project-snapshots-link"><Icon name="history" size={14} /> Snapshots</a>
              <button class="btn sm" type="button" onclick={() => (exporting = p)} aria-label="Export {p.name}" title="Write a zero2dev.json that describes this project, to share it with its code" data-testid="project-export"><Icon name="upload" size={14} /> Export</button>
              <button class="btn sm ghost icon" type="button" onclick={() => navigate(`#/projects/${encodeURIComponent(p.id)}/edit`)} aria-label="Edit {p.name}" title="Edit"><Icon name="edit" size={14} /></button>
              <button class="btn sm ghost icon danger" type="button" onclick={() => (deleting = p)} aria-label="Delete {p.name}" title="Delete"><Icon name="trash" size={14} /></button>
            </td>
          </tr>
        {/each}
      </tbody>
    </table>
  </div>
{/if}

{#if exporting}
  <ExportProjectDialog project={exporting} onclose={() => (exporting = null)} />
{/if}

{#if deleting}
  <ConfirmDialog title="Delete {deleting.name}?" message={deleteProjectMessage(deleting.name)} confirmLabel="Delete project" busy={deleteBusy} onconfirm={confirmDelete} oncancel={() => (deleting = null)} />
{/if}

<style>
  .badge.partial .dot {
    background: var(--accent);
  }
</style>
