<script lang="ts">
  // Create (#/projects/new) and edit (#/projects/{id}/edit) a project: a name, the services that
  // start and stop together, and the sites that go on with them.
  import { api, ApiError } from "../lib/api";
  import { navigate } from "../lib/router.svelte";
  import { tick } from "svelte";
  import { moveId, orderRows, sequenceLine, toggleId, validateProjectForm } from "../lib/projects";
  import { app, lists, loadProjects, toast } from "../lib/store.svelte";
  import { serviceKindLabel } from "../lib/ux";
  import Icon from "../ui/Icon.svelte";
  import KindIcon from "../ui/KindIcon.svelte";
  import PageHead from "../ui/PageHead.svelte";

  let { id }: { id?: string } = $props();
  const project = $derived(id ? lists.projects.find((p) => p.id === id) : undefined);
  // Once the list has loaded, an id that is not in it is a deleted project.
  const missing = $derived(!!id && !project && app.loaded.projects);

  let name = $state("");
  let serviceIds = $state<string[]>([]);
  let siteIds = $state<string[]>([]);
  let nameError = $state("");
  let formError = $state("");
  let saving = $state(false);

  // The form edits a copy. A direct link may arrive before the list has loaded, so it is filled
  // once, when the project first shows up; members that were deleted since are left out.
  let seeded = false;
  $effect(() => {
    if (seeded || !project) return;
    seeded = true;
    name = project.name;
    serviceIds = project.serviceIds.filter((sid) => lists.services.some((s) => s.id === sid));
    siteIds = project.siteIds.filter((sid) => lists.sites.some((s) => s.id === sid));
  });

  // The ticked services in start order: a newly ticked one goes to the end of the list.
  const rows = $derived(orderRows(serviceIds, lists.services));
  const sequence = $derived(sequenceLine(serviceIds, lists.services));
  let moved = $state("");

  async function move(sid: string, by: -1 | 1) {
    serviceIds = moveId(serviceIds, sid, by);
    const at = serviceIds.indexOf(sid);
    const svc = lists.services.find((s) => s.id === sid);
    moved = `${svc?.name ?? "Service"} moved to place ${at + 1} of ${serviceIds.length}.`;
    // Keep the keyboard on the row that moved; at either end, on its other button.
    await tick();
    const edge = (by === -1 && at === 0) || (by === 1 && at === serviceIds.length - 1);
    const dir = edge ? (by === -1 ? "down" : "up") : by === -1 ? "up" : "down";
    document.querySelector<HTMLButtonElement>(`[data-move="${sid}:${dir}"]`)?.focus();
  }

  async function save(e: SubmitEvent) {
    e.preventDefault();
    formError = "";
    nameError = validateProjectForm(name).name ?? "";
    if (nameError) {
      document.getElementById("project-name")?.focus();
      return;
    }
    saving = true;
    try {
      const input = { name: name.trim(), serviceIds, siteIds };
      const saved = id ? await api.updateProject(id, input) : await api.createProject(input);
      await loadProjects();
      toast("success", id ? `${saved.name} saved.` : `${saved.name} created.`);
      navigate("#/projects");
    } catch (err) {
      formError = err instanceof ApiError ? err.message : String(err);
    } finally {
      saving = false;
    }
  }
</script>

<a class="back muted" href="#/projects"><Icon name="back" size={14} /> All projects</a>

{#if id && !project}
  <div class="card empty">
    {#if missing}
      <h2>Project not found</h2>
      <p class="muted">It may have been deleted.</p>
      <a class="btn" href="#/projects">Back to projects</a>
    {:else}
      <p class="muted">Loading...</p>
    {/if}
  </div>
{:else}
  <PageHead title={project ? `Edit ${project.name}` : "New project"} sub="Choose what belongs together. Start and Stop then act on all of it at once." />

  <form id="project-form" class="col" onsubmit={save} novalidate>
    {#if formError}<div class="notice bad" role="alert"><Icon name="alert" /><span>{formError}</span></div>{/if}

    <section class="section-card" aria-labelledby="sec-name">
      <header><h2 id="sec-name">Name</h2></header>
      <div class="body">
        <div class="field"><label>
          <span class="label">Project name</span>
          <input id="project-name" type="text" bind:value={name} maxlength="64" aria-invalid={!!nameError} oninput={() => nameError && (nameError = validateProjectForm(name).name ?? "")} placeholder="My shop" /></label>
          {#if nameError}<span class="err">{nameError}</span>{/if}
        </div>
      </div>
    </section>

    <section class="section-card" aria-labelledby="sec-services" data-testid="project-services">
      <header><h2 id="sec-services">Services</h2><p>Start runs these, along with anything they depend on. Stop stops only these.</p></header>
      <div class="body">
        {#if lists.services.length === 0}
          <p class="muted">No services yet. <a href="#/services/new">Add a service</a> first.</p>
        {:else}
          <ul class="picks">
            {#each lists.services as s (s.id)}
              <li><label class="check">
                <input type="checkbox" checked={serviceIds.includes(s.id)} onchange={() => (serviceIds = toggleId(serviceIds, s.id))} />
                <KindIcon kind={s.kind} size={16} />
                <span class="pick-name">{s.name}</span>
                <span class="muted small">{serviceKindLabel(s.kind)}</span>
              </label></li>
            {/each}
          </ul>
        {/if}
      </div>
    </section>

    {#if rows.length > 1}
      <section class="section-card" aria-labelledby="sec-order" data-testid="project-order">
        <header>
          <h2 id="sec-order">Start order</h2>
          <p>Start goes down this list and waits for each service to be running before it starts the next one. Stop goes back up.</p>
        </header>
        <div class="body">
          <ol class="order">
            {#each rows as r, i (r.service.id)}
              <li data-testid="project-order-row" data-service={r.service.name}>
                <span class="place" aria-hidden="true">{i + 1}</span>
                <KindIcon kind={r.service.kind} size={16} />
                <span class="what">
                  <span class="pick-name">{r.service.name}</span>
                  <span class="muted small">{serviceKindLabel(r.service.kind)}</span>
                  {#if r.after.length}<span class="note small">Starts after {r.after.join(" and ")}, which it needs.</span>{/if}
                  {#if r.outside.length}<span class="note small muted">Also starts {r.outside.join(" and ")} first, which it needs.</span>{/if}
                </span>
                <span class="moves">
                  <button class="btn sm ghost icon" type="button" data-move="{r.service.id}:up" disabled={i === 0} onclick={() => move(r.service.id, -1)} aria-label="Move {r.service.name} up" title="Move up"><Icon name="chevronUp" size={14} /></button>
                  <button class="btn sm ghost icon" type="button" data-move="{r.service.id}:down" disabled={i === rows.length - 1} onclick={() => move(r.service.id, 1)} aria-label="Move {r.service.name} down" title="Move down"><Icon name="chevronDown" size={14} /></button>
                </span>
              </li>
            {/each}
          </ol>
          <p class="muted small sequence" data-testid="project-sequence">Start runs {sequence}.</p>
          <p class="sr-only" aria-live="polite">{moved}</p>
        </div>
      </section>
    {/if}

    <section class="section-card" aria-labelledby="sec-sites" data-testid="project-sites">
      <header><h2 id="sec-sites">Sites</h2><p>Start switches these sites on and makes sure the site server runs. Stop leaves them as they are.</p></header>
      <div class="body">
        {#if lists.sites.length === 0}
          <p class="muted">No sites yet. <a href="#/sites?new">Add a site</a> to include it here.</p>
        {:else}
          <ul class="picks">
            {#each lists.sites as s (s.id)}
              <li><label class="check">
                <input type="checkbox" checked={siteIds.includes(s.id)} onchange={() => (siteIds = toggleId(siteIds, s.id))} />
                <span class="pick-name">{s.name}</span>
                <span class="muted small mono">{s.domain}</span>
              </label></li>
            {/each}
          </ul>
        {/if}
      </div>
    </section>

    <div class="foot">
      <button class="btn" type="button" onclick={() => navigate("#/projects")}>Cancel</button>
      <button class="btn primary" type="submit" disabled={saving}>{id ? "Save changes" : "Create project"}</button>
    </div>
  </form>
{/if}

<style>
  .back {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    margin-bottom: 14px;
    font-size: 14px;
  }
  .col {
    display: grid;
    gap: 16px;
    max-width: 760px;
  }
  .picks {
    list-style: none;
    margin: 0;
    padding: 0;
    border: 1px solid var(--line);
    border-radius: var(--radius);
    max-height: 280px;
    overflow: auto;
  }
  .picks li + li {
    border-top: 1px solid var(--line);
  }
  .picks .check {
    padding: 4px 12px;
  }
  .picks .check:hover {
    background: var(--surface-2);
  }
  .pick-name {
    font-weight: 600;
    min-width: 0;
    overflow-wrap: anywhere;
  }
  .order {
    list-style: none;
    margin: 0;
    padding: 0;
    border: 1px solid var(--line);
    border-radius: var(--radius);
  }
  .order li {
    display: flex;
    align-items: flex-start;
    gap: 8px;
    padding: 8px 12px;
  }
  .order li + li {
    border-top: 1px solid var(--line);
  }
  .order :global(svg) {
    flex: none;
    margin-top: 2px;
  }
  .place {
    flex: none;
    width: 20px;
    height: 20px;
    border-radius: 50%;
    display: inline-grid;
    place-items: center;
    font-size: 12px;
    font-weight: 600;
    background: var(--surface-2);
    color: var(--muted);
  }
  .what {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-wrap: wrap;
    align-items: baseline;
    column-gap: 8px;
    row-gap: 2px;
  }
  .what .note {
    flex-basis: 100%;
  }
  .moves {
    flex: none;
    display: flex;
    gap: 4px;
  }
  .sequence {
    margin: 8px 0 0;
  }
  .foot {
    display: flex;
    justify-content: flex-end;
    gap: 8px;
  }
</style>
