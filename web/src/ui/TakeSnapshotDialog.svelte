<script lang="ts">
  // Take a snapshot of a project or a database service: a name, and when services run, the choice
  // to stop them, take it and start them again (restart: true). Shows the job's progress.
  import { api } from "../lib/api";
  import { waitForJob } from "../lib/snapshotJobs";
  import { defaultSnapshotName, isBusyState, joinNames, ownerServices, validateSnapshotName } from "../lib/snapshots";
  import { errMessage, lists, toast } from "../lib/store.svelte";
  import type { Job, SnapshotScope } from "../lib/types";
  import Icon from "./Icon.svelte";
  import JobProgress from "./JobProgress.svelte";
  import Modal from "./Modal.svelte";

  type Props = { scope: SnapshotScope; ownerId: string; ownerName: string; ondone: () => void; onclose: () => void };
  let { scope, ownerId, ownerName, ondone, onclose }: Props = $props();

  let name = $state(defaultSnapshotName());
  let restart = $state(true);
  let busy = $state(false);
  let job = $state<Job | null>(null);
  let error = $state("");
  let touched = $state(false);

  const members = $derived(ownerServices(scope, ownerId, lists.services, lists.projects));
  const running = $derived(members.filter(isBusyState));
  const nameError = $derived(validateSnapshotName(name));
  const blocked = $derived(running.length > 0 && !restart);

  async function take(e: Event) {
    e.preventDefault();
    touched = true;
    if (nameError || blocked || busy) return;
    busy = true;
    error = "";
    try {
      const started = await api.takeSnapshot(scope, ownerId, { name: name.trim(), restart: running.length > 0 && restart });
      const end = await waitForJob(started, (j) => (job = j));
      if (end.state === "done") {
        toast("success", `Snapshot "${name.trim()}" taken.`);
        ondone();
      } else error = end.error || "The snapshot failed.";
    } catch (err) {
      error = errMessage(err);
    } finally {
      busy = false;
    }
  }
</script>

<Modal title="Take a snapshot of {ownerName}" onclose={() => { if (!busy) onclose(); }}>
  <form class="dlg-body take" id="take-snapshot-form" onsubmit={take} data-testid="take-snapshot-dialog">
    <p class="muted">
      {#if scope === "project"}
        Saves the settings of {members.length === 1 ? "its service" : `its ${members.length} services`}, the runtime version each one uses, its sites, its php.ini files and the data of its databases. The code in the project folders is not copied.
      {:else}
        Saves the settings of {ownerName} and its data folder.
      {/if}
    </p>
    <label class="field">
      <span class="label">Name</span>
      <input type="text" bind:value={name} maxlength="64" disabled={busy} aria-invalid={touched && !!nameError} aria-describedby="take-name-hint" />
      {#if touched && nameError}<span class="err" id="take-name-hint">{nameError}</span>{:else}<span class="hint" id="take-name-hint">Say why you take it, such as "Before the PHP 8.4 upgrade".</span>{/if}
    </label>
    {#if running.length}
      <div class="notice warn" data-testid="take-running">
        <Icon name="alert" />
        <div class="grow">
          <p>{joinNames(running.map((s) => s.name))} {running.length === 1 ? "is" : "are"} running. A snapshot needs stopped services, so the data is not changing while it is copied.</p>
          <label class="check"><input type="checkbox" bind:checked={restart} disabled={busy} /> Stop {running.length === 1 ? "it" : "them"}, take the snapshot, and start {running.length === 1 ? "it" : "them"} again</label>
        </div>
      </div>
    {/if}
    {#if job}<JobProgress {job} label="Snapshot" />{/if}
    {#if error}<div class="notice bad" role="alert"><Icon name="alert" /><span>{error}</span></div>{/if}
  </form>
  {#snippet footer()}
    <button class="btn" type="button" disabled={busy} onclick={onclose}>Cancel</button>
    <button class="btn primary" type="submit" form="take-snapshot-form" disabled={busy || blocked}>{#if busy}<span class="spin" aria-hidden="true"></span>{/if} Take snapshot</button>
  {/snippet}
</Modal>

<style>
  .take {
    display: grid;
    gap: 16px;
  }
  .take p {
    margin: 0;
  }
  .notice .grow {
    display: grid;
    gap: 4px;
  }
</style>
