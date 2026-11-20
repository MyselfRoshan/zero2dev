<script lang="ts">
  // "Take a snapshot first?" before a risky action (lib/snapshotOffer.ts). Mounted once in App.
  // Take snapshot and continue / Continue without / Cancel; "taken" only when the job is done.
  import { api } from "../lib/api";
  import { waitForJob } from "../lib/snapshotJobs";
  import { closeOffer, offerState } from "../lib/snapshotOffer.svelte";
  import { isBusyState, joinNames, nameFromReason, ownerServices, validateSnapshotName } from "../lib/snapshots";
  import { errMessage, lists, toast } from "../lib/store.svelte";
  import type { Job } from "../lib/types";
  import Icon from "./Icon.svelte";
  import JobProgress from "./JobProgress.svelte";
  import Modal from "./Modal.svelte";

  const cur = $derived(offerState.current);
  let name = $state("");
  let job = $state<Job | null>(null);
  let error = $state("");
  let busy = $state(false);
  let shownSeq = 0;

  // A new offer starts fresh, with a name made from its reason.
  $effect(() => {
    const c = offerState.current;
    if (c && c.seq !== shownSeq) {
      shownSeq = c.seq;
      name = nameFromReason(c.req.reason);
      job = null;
      error = "";
      busy = false;
    }
  });

  const ownerName = $derived.by(() => {
    if (!cur) return "";
    const { scope, ownerId } = cur.req;
    return (scope === "project" ? lists.projects.find((p) => p.id === ownerId)?.name : lists.services.find((s) => s.id === ownerId)?.name) ?? ownerId;
  });
  const running = $derived(cur ? ownerServices(cur.req.scope, cur.req.ownerId, lists.services, lists.projects).filter(isBusyState) : []);
  const nameError = $derived(validateSnapshotName(name));

  async function take() {
    const c = cur;
    if (!c || nameError) return;
    busy = true;
    error = "";
    try {
      const started = await api.takeSnapshot(c.req.scope, c.req.ownerId, { name: name.trim(), restart: running.length > 0 });
      const end = await waitForJob(started, (j) => (job = j));
      if (offerState.current?.seq !== c.seq) return; // replaced meanwhile
      if (end.state === "done") {
        toast("success", `Snapshot "${name.trim()}" taken.`);
        closeOffer("taken");
      } else error = end.error || "The snapshot failed.";
    } catch (e) {
      error = errMessage(e);
    } finally {
      busy = false;
    }
  }
</script>

{#if cur}
  <Modal title="Take a snapshot first?" onclose={() => { if (!busy) closeOffer("cancelled"); }}>
    <div class="dlg-body offer" data-testid="snapshot-offer">
      <p>Zero2Dev can save a snapshot of <b>{ownerName}</b> {cur.req.reason}. If something goes wrong, restore it from {cur.req.scope === "project" ? "the project's Snapshots page" : "the service page"}.</p>
      <label class="field">
        <span class="label">Snapshot name</span>
        <input type="text" bind:value={name} maxlength="64" disabled={busy} aria-invalid={!!nameError} aria-describedby="offer-name-err" />
        {#if nameError}<span class="err" id="offer-name-err">{nameError}</span>{/if}
      </label>
      {#if running.length}
        <div class="notice warn" data-testid="offer-running"><Icon name="alert" /><span>{joinNames(running.map((s) => s.name))} {running.length === 1 ? "is" : "are"} running. Taking the snapshot stops {running.length === 1 ? "it" : "them"}, takes it, and starts {running.length === 1 ? "it" : "them"} again.</span></div>
      {/if}
      {#if job}<JobProgress {job} label="Snapshot" />{/if}
      {#if error}<div class="notice bad" role="alert"><Icon name="alert" /><span>{error}</span></div>{/if}
    </div>
    {#snippet footer()}
      <button class="btn" type="button" disabled={busy} onclick={() => closeOffer("cancelled")}>Cancel</button>
      <button class="btn" type="button" disabled={busy} onclick={() => closeOffer("skipped")}>Continue without</button>
      <button class="btn primary" type="button" disabled={busy || !!nameError} onclick={take}>{#if busy}<span class="spin" aria-hidden="true"></span>{/if} Take snapshot and continue</button>
    {/snippet}
  </Modal>
{/if}

<style>
  .offer {
    display: grid;
    gap: 16px;
  }
  .offer p {
    margin: 0;
  }
  :global(dialog:has([data-testid="snapshot-offer"]) .dlg-foot) {
    flex-wrap: wrap;
  }
</style>
