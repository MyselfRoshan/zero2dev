<script lang="ts">
  // "Make <runtime> the default?" when services follow the kind's default (0.0.5, snapshot-ui):
  // lists the projects whose services switch versions and offers a snapshot of each first.
  import { api } from "../lib/api";
  import { defaultRuntime, kindLabel, runtimeTitle } from "../lib/catalog";
  import { waitForJob } from "../lib/snapshotJobs";
  import { defaultFollowers, isBusyState, joinNames } from "../lib/snapshots";
  import { errMessage, lists, makeDefaultRuntime, toast } from "../lib/store.svelte";
  import type { Job, Runtime } from "../lib/types";
  import Icon from "./Icon.svelte";
  import JobProgress from "./JobProgress.svelte";
  import Modal from "./Modal.svelte";

  let { runtime, onclose }: { runtime: Runtime; onclose: () => void } = $props();

  const followers = $derived(defaultFollowers(runtime.kind, lists.services, lists.projects));
  const current = $derived(defaultRuntime(lists.runtimes, runtime.kind));
  const label = $derived(kindLabel(runtime.kind));
  const snapName = $derived(`Before ${runtimeTitle(runtime)} became the default`.slice(0, 64));

  // Project id -> take a snapshot first (on by default).
  let skip = $state<Record<string, boolean>>({});
  let busy = $state(false);
  let jobsBy = $state<Record<string, Job>>({});
  let error = $state("");
  const chosen = $derived(followers.projects.filter((g) => !skip[g.project.id]));

  const runningOf = (ids: string[]) => lists.services.filter((s) => ids.includes(s.id) && isBusyState(s));

  async function confirm() {
    busy = true;
    error = "";
    try {
      for (const g of chosen) {
        if (jobsBy[g.project.id]?.state === "done") continue; // already taken on an earlier try
        const restart = runningOf(g.project.serviceIds).length > 0;
        const started = await api.takeSnapshot("project", g.project.id, { name: snapName, restart });
        const end = await waitForJob(started, (j) => (jobsBy = { ...jobsBy, [g.project.id]: j }));
        if (end.state !== "done") {
          error = `The snapshot of ${g.project.name} failed: ${end.error || "unknown error"}. The default was not changed.`;
          return;
        }
      }
      if (chosen.length) toast("success", `${chosen.length === 1 ? "Snapshot" : `${chosen.length} snapshots`} taken.`);
      await makeDefaultRuntime(runtime.id);
      onclose();
    } catch (e) {
      error = `${errMessage(e)} The default was not changed.`;
    } finally {
      busy = false;
    }
  }
</script>

<Modal title="Make {runtimeTitle(runtime)} the default?" onclose={() => { if (!busy) onclose(); }}>
  <div class="dlg-body md" data-testid="make-default-dialog">
    <p>
      {followers.services.length === 1 ? "One service uses" : `${followers.services.length} services use`} the default {label}{current ? ` (now ${current.version})` : ""}.
      From {followers.services.length === 1 ? "its" : "their"} next start {followers.services.length === 1 ? "it runs" : "they run"} {runtime.version} instead. Services that picked a version of their own do not change.
    </p>
    {#if followers.projects.length}
      <fieldset class="projects">
        <legend class="label">Take a snapshot first, so you can go back</legend>
        {#each followers.projects as g (g.project.id)}
          {@const running = runningOf(g.project.serviceIds)}
          {@const j = jobsBy[g.project.id]}
          <div class="proj" data-project={g.project.name}>
            <label class="check"><input type="checkbox" checked={!skip[g.project.id]} disabled={busy} onchange={(e) => (skip = { ...skip, [g.project.id]: !(e.currentTarget as HTMLInputElement).checked })} /> Snapshot of <b>{g.project.name}</b></label>
            <div class="small muted sub">Uses the default: {joinNames(g.services.map((s) => s.name))}</div>
            {#if running.length && !skip[g.project.id]}<div class="small muted sub">{joinNames(running.map((s) => s.name))} {running.length === 1 ? "is" : "are"} stopped for the snapshot and started again.</div>{/if}
            {#if j && !skip[g.project.id]}<div class="sub"><JobProgress job={j} label="Snapshot of {g.project.name}" /></div>{/if}
          </div>
        {/each}
      </fieldset>
    {/if}
    {#if followers.loose.length}
      <p class="small muted">Not in a project, so not in a snapshot: {joinNames(followers.loose.map((s) => s.name))}.</p>
    {/if}
    {#if error}<div class="notice bad" role="alert"><Icon name="alert" /><span>{error}</span></div>{/if}
  </div>
  {#snippet footer()}
    <button class="btn" type="button" disabled={busy} onclick={onclose}>Cancel</button>
    <button class="btn primary" type="button" disabled={busy} onclick={confirm} data-testid="make-default-confirm">{#if busy}<span class="spin" aria-hidden="true"></span>{/if} {chosen.length ? (chosen.length === 1 ? "Take snapshot and make default" : "Take snapshots and make default") : "Make default"}</button>
  {/snippet}
</Modal>

<style>
  .md {
    display: grid;
    gap: 12px;
  }
  .md p {
    margin: 0;
  }
  .projects {
    border: 1px solid var(--line);
    border-radius: var(--radius-lg);
    margin: 0;
    padding: 8px 12px 12px;
    display: grid;
    gap: 8px;
  }
  .projects legend {
    padding: 0 4px;
    font-weight: 600;
  }
  .proj {
    display: grid;
    gap: 2px;
  }
  .sub {
    padding-left: 24px;
  }
</style>
