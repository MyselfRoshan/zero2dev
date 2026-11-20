<script lang="ts">
  // Import several programs at once. The ticked rows show a bar; Review lists every program with
  // what is imported (the recommended items); Start runs them one after another. A failure does not
  // stop the others, and what was there before is kept (each is its own migration in History).
  import { onDestroy } from "svelte";
  import { api } from "../lib/api";
  import {
    batchBusy,
    batchSummary,
    buildBatch,
    fmtSize,
    freeSpace,
    nextWaiting,
    type BatchEntry,
  } from "../lib/migrate";
  import { errMessage } from "../lib/store.svelte";
  import type { MigrateSource, Migration } from "../lib/types";
  import Icon from "./Icon.svelte";
  import Modal from "./Modal.svelte";

  type Props = {
    sources: MigrateSource[];
    history: Migration[] | null;
    picked: string[];
    /** The view's one primary button, when nothing else wants it. */
    primary: boolean;
    /** True while a batch runs (the page waits for it). */
    running?: boolean;
    onclear: () => void;
    /** After the batch: reload the lists. */
    onfinished: () => void;
  };
  let { sources, history, picked, primary, running = $bindable(false), onclear, onfinished }: Props = $props();

  let open = $state(false);
  let preferMove = $state(false);
  let entries = $state<BatchEntry[]>([]);
  let started = $state(false);
  let cancelled = false;
  onDestroy(() => (cancelled = true));

  const draft = $derived(buildBatch(sources, history, picked, preferMove));
  const list = $derived(started ? entries : draft);
  const bytes = $derived(list.reduce((n, e) => n + e.bytes, 0));
  const free = $derived(freeSpace(sources.find((x) => x.id === list[0]?.sourceId) ?? sources[0], bytes));
  const anyMove = $derived(
    picked.some((id) => sources.find((x) => x.id === id)?.modes.includes("move")),
  );
  const finished = $derived(started && !batchBusy(entries));

  const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms));

  async function waitFor(id: string, e: BatchEntry) {
    while (!cancelled) {
      const j = await api.getJob(id);
      e.percent = j.percent;
      if (j.state !== "running") {
        if (j.state === "done") e.state = "done";
        else {
          e.state = "failed";
          e.error = j.error || "Unknown error.";
        }
        return;
      }
      await sleep(500);
    }
  }

  async function start() {
    entries = draft.map((e) => ({ ...e }));
    started = true;
    running = true;
    for (let e = nextWaiting(entries); e && !cancelled; e = nextWaiting(entries)) {
      e.state = "running";
      try {
        const job = await api.startMigration({ source: e.sourceId, mode: e.mode, items: e.items });
        await waitFor(job.id, e);
      } catch (err) {
        e.state = "failed";
        e.error = errMessage(err);
      }
    }
    running = false;
    onfinished();
  }

  function close() {
    if (running) return;
    open = false;
    if (started) {
      started = false;
      entries = [];
      onclear();
    }
  }
</script>

{#if picked.length > 0 && !started}
  <div class="bar" role="region" aria-label="Several at once" data-testid="migrate-batch-bar">
    <span class="grow"><b>{picked.length}</b> selected · {fmtSize(draft.reduce((n, e) => n + e.bytes, 0))}</span>
    <button class="btn ghost" type="button" onclick={onclear}>Clear</button>
    <button class="btn" class:primary type="button" onclick={() => (open = true)} disabled={!draft.length || running} data-testid="migrate-batch-review">Review {picked.length}…</button>
  </div>
{/if}

{#if open}
  <Modal title={started ? "Importing them" : `Import ${list.length} ${list.length === 1 ? "program" : "programs"}?`} onclose={close}>
    <div class="dlg-body" data-testid="migrate-batch-dialog">
      {#if !started}
        <p>These are imported one after another. Only the items worth it are included; a failure does not stop the rest, and what was there before is saved first.</p>
        {#if anyMove}
          <div class="field">
            <span class="label" id="batch-how">How</span>
            <div class="seg" role="radiogroup" aria-labelledby="batch-how">
              <label><input type="radio" name="batch-mode" checked={!preferMove} onchange={() => (preferMove = false)} />Copy all</label>
              <label><input type="radio" name="batch-mode" checked={preferMove} onchange={() => (preferMove = true)} />Move where possible</label>
            </div>
            <span class="hint">{preferMove ? "Programs that can only be copied stay copies. A move removes the originals after saving them." : "The originals stay where they are."}</span>
          </div>
        {/if}
      {/if}
      <ul class="list">
        {#each list as e (e.sourceId)}
          <li data-testid="migrate-batch-entry" data-source={e.sourceId} data-state={e.state}>
            <span class="state" aria-hidden="true">
              {#if e.state === "running"}<span class="spin"></span>
              {:else if e.state === "done"}<Icon name="circleCheck" size={16} />
              {:else if e.state === "failed"}<Icon name="alert" size={16} />
              {:else}<Icon name="clock" size={16} />{/if}
            </span>
            <span class="grow">
              <b>{e.label}</b> <span class="muted small">{e.mode === "move" ? "move" : "copy"} · {fmtSize(e.bytes)}</span>
              {#if e.state === "running"}<span class="muted small"> · {Math.round(e.percent)}%</span>{/if}
              {#if e.state === "failed"}<span class="err small">{e.error}</span>{/if}
            </span>
            <span class="sr-only">{e.state}</span>
          </li>
        {/each}
      </ul>
      {#if !started && free.text}
        <p class="free {free.state}" data-testid="migrate-batch-free"><Icon name={free.state === "ok" ? "circleCheck" : "alert"} size={14} /> {free.text}</p>
      {/if}
      {#if finished}
        <div class="notice" class:ok={!entries.some((e) => e.state === "failed")} class:bad={entries.some((e) => e.state === "failed")} role="status" data-testid="migrate-batch-result">
          <Icon name="circleCheck" /><span>{batchSummary(entries)} Each one is in History, where it can be reverted.</span>
        </div>
      {/if}
    </div>
    {#snippet footer()}
      {#if finished}
        <a class="btn" href="#/migrate?tab=history" onclick={close}>Open History</a>
        <button class="btn primary" type="button" onclick={close} data-testid="migrate-batch-done">Close</button>
      {:else if started}
        <button class="btn" type="button" disabled>Working…</button>
      {:else}
        <button class="btn" type="button" onclick={close}>Cancel</button>
        <button class="btn primary" type="button" disabled={!list.length || free.state === "short"} onclick={start} data-testid="migrate-batch-start">Start ({list.length})</button>
      {/if}
    {/snippet}
  </Modal>
{/if}

<style>
  .bar {
    position: sticky;
    bottom: calc(12px + env(safe-area-inset-bottom, 0px));
    z-index: 5;
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 8px 12px;
    padding: 10px 16px;
    border: 1px solid var(--accent-border);
    border-radius: var(--radius-lg);
    background: var(--surface);
    box-shadow: var(--shadow);
  }
  .list {
    list-style: none;
    margin: 0 0 12px;
    padding: 0;
    display: grid;
    gap: 8px;
  }
  .list li {
    display: flex;
    align-items: flex-start;
    gap: 10px;
    padding: 8px 12px;
    border: 1px solid var(--line);
    border-radius: var(--radius);
    background: var(--surface-2);
    min-width: 0;
  }
  .state {
    display: inline-flex;
    margin-top: 2px;
    color: var(--muted);
  }
  li[data-state="done"] .state {
    color: var(--ok);
  }
  li[data-state="failed"] .state {
    color: var(--bad);
  }
  .free {
    display: flex;
    gap: 6px;
    margin: 0;
    font-size: 13px;
    color: var(--muted);
  }
  .free.tight {
    color: var(--warn);
  }
  .free.short {
    color: var(--bad);
  }
  .dlg-body p {
    margin: 0 0 12px;
  }
  .notice.ok {
    border-color: var(--ok-border);
    background: var(--ok-soft);
  }
</style>
