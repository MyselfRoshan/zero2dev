<script lang="ts">
  // The History tab of Import: every migration with its details (items, where they went, how long it
  // took, what was backed up) and what can be done with it: Revert, remove the old files after a copy,
  // delete the backups, remove it from the list. Jobs (revert, clean-up) are tracked by the page.
  import { api } from "../lib/api";
  import { ago, fmtDateTime } from "../lib/format";
  import {
    HISTORY_FILTERS,
    backupsText,
    backupTotal,
    canRevert,
    cleanupText,
    defaultRevert,
    filterHistory,
    fmtSize,
    noCleanupReason,
    revertLabel,
    revertText,
    stateLabel,
    tookText,
    type HistoryFilter,
  } from "../lib/migrate";
  import { errMessage, lists } from "../lib/store.svelte";
  import type { Job, MigrateMode, MigrateProgram, Migration, RevertHow } from "../lib/types";
  import ConfirmDialog from "./ConfirmDialog.svelte";
  import Icon from "./Icon.svelte";

  type Started = { kind: "revert" | "cleanup"; label: string; program: MigrateProgram; mode: MigrateMode; migrationId: string };
  type Props = {
    history: Migration[] | null;
    error: string;
    /** A job runs: nothing here can start another. */
    busy: boolean;
    onjob: (job: Job, t: Started) => void;
    onchanged: () => void;
  };
  let { history, error, busy, onjob, onchanged }: Props = $props();

  let filter = $state<HistoryFilter>("all");
  let program = $state("");
  let detailId = $state("");
  let revertId = $state("");
  let how = $state<RevertHow | undefined>(undefined);
  let confirm = $state<"" | "revert" | "cleanup" | "backups" | "remove">("");
  let working = $state(false);
  let actionError = $state("");

  const rows = $derived(filterHistory(history ?? [], filter, program));
  const programs = $derived([...new Set((history ?? []).map((m) => m.program))]);
  const revertMig = $derived(history?.find((m) => m.id === revertId));
  const serviceName = (id?: string) => lists.services.find((s) => s.id === id)?.name ?? "the Zero2Dev service";
  const programLabel = (p: string) => history?.find((m) => m.program === p)?.label ?? p;

  function toggleDetails(m: Migration) {
    detailId = detailId === m.id ? "" : m.id;
    revertId = "";
    actionError = "";
  }
  function openRevert(m: Migration) {
    detailId = m.id;
    revertId = revertId === m.id ? "" : m.id;
    how = defaultRevert(m);
    actionError = "";
  }

  async function run(fn: () => Promise<unknown>) {
    working = true;
    actionError = "";
    try {
      await fn();
    } catch (e) {
      actionError = errMessage(e);
    } finally {
      working = false;
      confirm = "";
    }
  }
  const doRevert = () =>
    run(async () => {
      const m = revertMig;
      if (!m || !how) return;
      const job = await api.revertMigration(m.id, how);
      revertId = "";
      onjob(job, { kind: "revert", label: m.label, program: m.program, mode: m.mode, migrationId: m.id });
    });
  const doCleanup = () =>
    run(async () => {
      const m = history?.find((x) => x.id === detailId);
      if (!m) return;
      const job = await api.cleanupMigration(m.id);
      onjob(job, { kind: "cleanup", label: m.label, program: m.program, mode: m.mode, migrationId: m.id });
    });
  const doBackups = () =>
    run(async () => {
      if (!detailId) return;
      await api.deleteMigrationBackups(detailId);
      onchanged();
    });
  const doRemove = () =>
    run(async () => {
      if (!detailId) return;
      await api.deleteMigration(detailId);
      detailId = "";
      onchanged();
    });
  const cur = $derived(history?.find((m) => m.id === detailId));
</script>

<section class="section-card" aria-labelledby="mg-history" data-testid="migrate-history">
  <header>
    <h2 id="mg-history"><Icon name="history" size={16} /> History</h2>
    <p>Everything you imported, newest first. Revert puts things back as they were.{#if history && backupTotal(history) > 0} Backups take <b data-testid="migrate-backups-total">{fmtSize(backupTotal(history))}</b>.{/if}</p>
  </header>
  <div class="body">
    {#if error}
      <div class="notice bad" role="alert"><Icon name="alert" /><span>Could not read the history: {error}</span></div>
    {:else if !history}
      <p class="muted">Loading...</p>
    {:else if history.length === 0}
      <p class="muted" data-testid="migrate-history-empty">Nothing imported yet.</p>
    {:else}
      <div class="filters row wrap">
        <div class="seg" role="radiogroup" aria-label="Show" data-testid="migrate-history-filter">
          {#each HISTORY_FILTERS as f (f.id)}
            <label><input type="radio" name="hist-filter" value={f.id} bind:group={filter} />{f.label}</label>
          {/each}
        </div>
        {#if programs.length > 1}
          <select bind:value={program} aria-label="Program" data-testid="migrate-history-program">
            <option value="">All programs</option>
            {#each programs as p (p)}<option value={p}>{programLabel(p)}</option>{/each}
          </select>
        {/if}
      </div>
      {#if actionError}<div class="notice bad" role="alert" data-testid="migrate-history-error"><Icon name="alert" /><span>{actionError}</span></div>{/if}
      {#if rows.length === 0}
        <p class="muted" data-testid="migrate-history-none">Nothing matches.</p>
      {/if}
      <ul class="hist">
        {#each rows as m (m.id)}
          {@const st = stateLabel(m)}
          {@const open = detailId === m.id}
          <li class="h-row" data-testid="migrate-history-row" data-migration={m.id} data-source={m.source} data-state={m.state}>
            <div class="h-main">
              <div class="h-title">
                <b>{m.label}</b>
                <span class="badge {st.tone === 'ok' ? 'running' : st.tone === 'bad' ? 'failed' : 'stopped'}"><span class="dot"></span>{st.text}</span>
                {#if m.backupBytes > 0}<span class="badge plain" title="What Zero2Dev saved before changing anything">Backup {fmtSize(m.backupBytes)}</span>{/if}
              </div>
              <span>{m.description}</span>
              <span class="muted small">
                <span title={fmtDateTime(m.createdAt)}>{m.mode === "move" ? "Moved" : "Copied"} {ago(m.createdAt)}</span>{#if m.revertedAt}<span title={fmtDateTime(m.revertedAt)}>, reverted {ago(m.revertedAt)}</span>{/if}
              </span>
            </div>
            <div class="h-actions row wrap">
              {#if canRevert(m)}
                <button class="btn sm" type="button" aria-expanded={revertId === m.id} aria-controls="rv-{m.id}" onclick={() => openRevert(m)} disabled={busy} data-testid="migrate-revert" aria-label="Revert {m.label}">
                  <Icon name="undo" size={14} /> {revertId === m.id ? "Close" : "Revert…"}
                </button>
              {/if}
              <button class="btn sm ghost" type="button" aria-expanded={open} aria-controls="dt-{m.id}" onclick={() => toggleDetails(m)} data-testid="migrate-details" aria-label="Details of {m.label}">
                <Icon name={open ? "chevronUp" : "chevronDown"} size={14} /> Details
              </button>
            </div>

            {#if open}
              <div class="detail" id="dt-{m.id}" data-testid="migrate-history-detail">
                <table class="items-t">
                  <caption class="sr-only">Items of {m.label}</caption>
                  <thead><tr><th scope="col">Item</th><th scope="col">From</th><th scope="col">To</th><th scope="col" class="num">Size</th></tr></thead>
                  <tbody>
                    {#each m.items as it (it.label)}
                      <tr><th scope="row">{it.label}</th><td><code>{it.from}</code></td><td><code>{it.to}</code></td><td class="num">{fmtSize(it.sizeBytes)}</td></tr>
                    {/each}
                  </tbody>
                </table>
                <dl class="facts small">
                  <div><dt>Started</dt><dd>{fmtDateTime(m.createdAt)} {tookText(m)}</dd></div>
                  {#if m.revertedAt}<div><dt>Reverted</dt><dd>{fmtDateTime(m.revertedAt)}</dd></div>{/if}
                  <div><dt>Backups</dt><dd>{m.backupBytes > 0 ? fmtSize(m.backupBytes) : "none"}</dd></div>
                  <div><dt>Id</dt><dd><code>{m.id}</code></dd></div>
                </dl>
                <div class="row wrap">
                  {#if m.canCleanup}
                    <button class="btn sm" type="button" disabled={busy || working} onclick={() => (confirm = "cleanup")} data-testid="migrate-cleanup">
                      <Icon name="trash" size={14} /> Remove the old files ({fmtSize(m.originalsBytes)})…
                    </button>
                  {/if}
                  {#if m.backupBytes > 0}
                    <button class="btn sm" type="button" disabled={busy || working} onclick={() => (confirm = "backups")} data-testid="migrate-backups-delete">
                      <Icon name="trash" size={14} /> Delete backups ({fmtSize(m.backupBytes)})…
                    </button>
                  {/if}
                  {#if m.state !== "done" && m.revertOptions.length === 0}
                    <button class="btn sm" type="button" disabled={busy || working} onclick={() => (confirm = "remove")} data-testid="migrate-remove">
                      <Icon name="x" size={14} /> Remove from history…
                    </button>
                  {/if}
                </div>
                {#if noCleanupReason(m)}<p class="hint" data-testid="migrate-no-cleanup">Old files are kept: {noCleanupReason(m)}</p>{/if}
              </div>
            {/if}

            {#if revertId === m.id}
              <div class="rv" id="rv-{m.id}" role="group" aria-labelledby="rv-h-{m.id}" data-testid="migrate-revert-panel">
                <span class="label" id="rv-h-{m.id}">How to revert {m.label}</span>
                <div class="hows" role="radiogroup" aria-labelledby="rv-h-{m.id}" data-testid="migrate-revert-how">
                  {#each m.revertOptions as o (o)}
                    <label class="how" class:on={how === o}>
                      <input type="radio" name="how-{m.id}" value={o} bind:group={how} />
                      <span class="what"><span class="it-title">{revertLabel(o)}</span><span class="hint">{revertText(o, m, serviceName(m.serviceId))}</span></span>
                    </label>
                  {/each}
                </div>
                <div class="row wrap">
                  <button class="btn danger" type="button" disabled={!how || busy} onclick={() => (confirm = "revert")} data-testid="migrate-revert-go"><Icon name="undo" size={14} /> {how ? revertLabel(how) : "Revert"}</button>
                  <button class="btn ghost" type="button" onclick={() => (revertId = "")}>Cancel</button>
                </div>
              </div>
            {/if}
          </li>
        {/each}
      </ul>
    {/if}
  </div>
</section>

{#if confirm === "revert" && revertMig && how}
  <ConfirmDialog
    title="{revertLabel(how)}: {revertMig.label}?"
    message={revertText(how, revertMig, serviceName(revertMig.serviceId))}
    confirmLabel={revertLabel(how)}
    busy={working}
    onconfirm={doRevert}
    oncancel={() => (confirm = "")}
  />
{:else if confirm === "cleanup" && cur}
  <ConfirmDialog title="Remove the old {cur.label} files?" message={cleanupText(cur)} confirmLabel="Remove the old files" busy={working} onconfirm={doCleanup} oncancel={() => (confirm = "")} />
{:else if confirm === "backups" && cur}
  <ConfirmDialog title="Delete the backups of {cur.label}?" message={backupsText(cur)} confirmLabel="Delete backups" busy={working} onconfirm={doBackups} oncancel={() => (confirm = "")} />
{:else if confirm === "remove" && cur}
  <ConfirmDialog
    title="Remove {cur.label} from the history?"
    message="Its record and any backups it still holds are deleted. Your data is not touched."
    confirmLabel="Remove from history"
    busy={working}
    onconfirm={doRemove}
    oncancel={() => (confirm = "")}
  />
{/if}

<style>
  .hist {
    list-style: none;
    margin: 0;
    padding: 0;
    display: grid;
  }
  .filters {
    margin-bottom: 12px;
    gap: 8px 12px;
    align-items: center;
  }
  .filters select {
    width: auto;
    min-width: 180px;
  }
  .h-row {
    display: grid;
    grid-template-columns: minmax(0, 1fr) auto;
    gap: 8px 12px;
    align-items: start;
    border-top: 1px solid var(--line);
    padding: 12px 0;
    min-width: 0;
  }
  .h-row:first-child {
    border-top: 0;
    padding-top: 0;
  }
  .h-main {
    display: grid;
    gap: 2px;
    min-width: 0;
    overflow-wrap: anywhere;
  }
  .h-title {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 4px 8px;
    font-weight: 600;
  }
  .detail,
  .rv {
    grid-column: 1 / -1;
    display: grid;
    gap: 12px;
    padding: 16px;
    border: 1px solid var(--line);
    border-radius: var(--radius-lg);
    background: var(--surface-2);
    min-width: 0;
  }
  .items-t {
    width: 100%;
    border-collapse: collapse;
    font-size: 13px;
    display: block;
    overflow-x: auto;
  }
  .items-t th,
  .items-t td {
    text-align: left;
    padding: 4px 12px 4px 0;
    vertical-align: top;
    font-weight: 400;
  }
  .items-t thead th {
    color: var(--muted);
  }
  .items-t tbody th {
    font-weight: 600;
    white-space: nowrap;
  }
  .items-t code {
    font-size: 12px;
    overflow-wrap: anywhere;
  }
  .num {
    text-align: right;
    white-space: nowrap;
  }
  .facts {
    display: flex;
    flex-wrap: wrap;
    gap: 4px 24px;
    margin: 0;
  }
  .facts div {
    display: flex;
    gap: 6px;
  }
  .facts dt {
    color: var(--muted);
  }
  .facts dd {
    margin: 0;
  }
  .hows {
    display: grid;
    gap: 8px;
  }
  .how {
    display: flex;
    align-items: flex-start;
    gap: 10px;
    padding: 8px 12px;
    border: 1px solid var(--line);
    border-radius: var(--radius);
    background: var(--surface);
    cursor: pointer;
  }
  .how input {
    margin-top: 3px;
    flex: none;
  }
  .how.on {
    border-color: var(--accent);
  }
  .what {
    display: grid;
    gap: 2px;
    min-width: 0;
  }
  .it-title {
    font-weight: 600;
  }
  #mg-history {
    display: inline-flex;
    align-items: center;
    gap: 8px;
  }
  @media (max-width: 560px) {
    .h-row {
      grid-template-columns: minmax(0, 1fr);
    }
    .detail,
    .rv {
      padding: 12px;
    }
  }
</style>
