<script lang="ts">
  // The "Databases from another server" form of Import: the server (type, host, port, user,
  // password), List databases, then the databases to copy and the Zero2Dev service they go into.
  // The page reads it through `ready` and the exported helpers.
  import { tick } from "svelte";
  import { api } from "../lib/api";
  import {
    DB_KINDS,
    dbTargets,
    defaultPort,
    defaultUser,
    fmtSize,
    validateDbCopy,
    validateDbForm,
    type DbField,
    type DbKind,
    type ReviewDb,
  } from "../lib/migrate";
  import { errMessage, lists } from "../lib/store.svelte";
  import type { DbDatabase, MigrateDatabase } from "../lib/types";
  import Icon from "./Icon.svelte";

  let { ready = $bindable(false) }: { ready?: boolean } = $props();

  let kind = $state<DbKind>("mysql");
  let host = $state("127.0.0.1");
  let port = $state("3306");
  let user = $state("root");
  let pass = $state("");
  let err = $state<Partial<Record<DbField, string>>>({});
  let connError = $state("");
  let list = $state<DbDatabase[] | null>(null);
  let picked = $state<string[]>([]);
  let target = $state("");
  let listing = $state(false);
  const targets = $derived(dbTargets(lists.services, kind));
  const targetSvc = $derived(targets.find((t) => t.id === target));
  const pickedDbs = $derived((list ?? []).filter((d) => picked.includes(d.name)));

  $effect(() => {
    ready = !!list;
  });

  // The only target: preselected.
  $effect(() => {
    if (!target && targets.length === 1) target = targets[0].id;
  });

  function pickKind(k: DbKind) {
    if (port === String(defaultPort(kind))) port = String(defaultPort(k));
    if (user === defaultUser(kind)) user = defaultUser(k);
    kind = k;
    list = null;
    picked = [];
    const t = dbTargets(lists.services, k);
    target = t.length === 1 ? t[0].id : "";
  }
  function connChanged() {
    list = null;
    picked = [];
    connError = "";
  }
  const focusInvalid = async () => {
    await tick();
    document.querySelector<HTMLElement>('[data-testid="migrate-db-form"] [aria-invalid="true"]')?.focus();
  };
  async function listDatabases() {
    connError = "";
    err = validateDbForm({ kind, host, port, user }, lists.services);
    if (Object.keys(err).length) return focusInvalid();
    listing = true;
    try {
      const r = await api.remoteDatabases({ kind, host: host.trim(), port: Number(port), user: user.trim(), password: pass });
      list = r.databases;
      picked = [];
    } catch (e) {
      connError = errMessage(e);
      list = null;
    } finally {
      listing = false;
    }
  }

  /** Checks the choice before the review; focuses the first problem. */
  export async function validate(): Promise<boolean> {
    err = validateDbCopy(picked, target, targets);
    if (Object.keys(err).length) {
      await focusInvalid();
      return false;
    }
    return true;
  }
  export function review(): ReviewDb {
    return { databases: pickedDbs, host: host.trim(), port: Number(port), target: targetSvc?.name ?? "" };
  }
  export function request(): MigrateDatabase {
    return { kind, host: host.trim(), port: Number(port), user: user.trim(), password: pass, databases: [...picked], targetServiceId: target };
  }
</script>

<form class="db stack" data-testid="migrate-db-form" novalidate onsubmit={(e) => { e.preventDefault(); void listDatabases(); }}>
  <div class="field">
    <span class="label" id="db-kind-l">Server type</span>
    <div class="seg" role="radiogroup" aria-labelledby="db-kind-l">
      {#each DB_KINDS as k (k.id)}
        <label><input type="radio" name="db-kind" value={k.id} checked={kind === k.id} onchange={() => pickKind(k.id)} />{k.label}</label>
      {/each}
    </div>
  </div>
  <div class="grid2">
    <div class="field">
      <label for="db-host" class="label">Host</label>
      <input id="db-host" type="text" class="mono" bind:value={host} oninput={connChanged} aria-invalid={!!err.host} aria-describedby="db-host-m" spellcheck="false" />
      {#if err.host}<span class="err" id="db-host-m">{err.host}</span>{:else}<span class="hint" id="db-host-m">127.0.0.1 for a server installed on this PC.</span>{/if}
    </div>
    <div class="field">
      <label for="db-port" class="label">Port</label>
      <input id="db-port" type="text" inputmode="numeric" class="mono" bind:value={port} oninput={connChanged} aria-invalid={!!err.port} aria-describedby="db-port-m" />
      {#if err.port}<span class="err" id="db-port-m">{err.port}</span>{:else}<span class="hint" id="db-port-m">Usually {defaultPort(kind)}.</span>{/if}
    </div>
    <div class="field">
      <label for="db-user" class="label">User</label>
      <input id="db-user" type="text" bind:value={user} oninput={connChanged} aria-invalid={!!err.user} aria-describedby="db-user-m" spellcheck="false" autocomplete="off" />
      {#if err.user}<span class="err" id="db-user-m">{err.user}</span>{/if}
    </div>
    <div class="field">
      <label for="db-pass" class="label">Password</label>
      <input id="db-pass" type="password" bind:value={pass} oninput={connChanged} autocomplete="off" aria-describedby="db-pass-m" />
      <span class="hint" id="db-pass-m">Used for this copy only; Zero2Dev does not store it.</span>
    </div>
  </div>
  <div class="row wrap">
    <button class="btn" type="submit" disabled={listing} data-testid="migrate-db-list">
      {#if listing}<span class="spin" aria-hidden="true"></span> Connecting...{:else}<Icon name="database" size={14} /> List databases{/if}
    </button>
  </div>
  {#if connError}
    <div class="notice bad" role="alert" data-testid="migrate-db-error"><Icon name="alert" /><span>Could not list the databases: {connError}</span></div>
  {/if}
  {#if list}
    <fieldset class="dbs" aria-describedby="db-dbs-m" data-testid="migrate-db-list-result">
      <legend class="label">Databases on {host.trim()}:{port}</legend>
      {#if list.length === 0}
        <p class="muted">That server has no databases of its own.</p>
      {:else}
        {#each list as d (d.name)}
          <label class="check"><input type="checkbox" value={d.name} bind:group={picked} onchange={() => (err = { ...err, databases: undefined })} /><span class="mono">{d.name}</span>{#if d.sizeBytes}<span class="muted small">{fmtSize(d.sizeBytes)}</span>{/if}</label>
        {/each}
      {/if}
      {#if err.databases}<span class="err" id="db-dbs-m">{err.databases}</span>{/if}
    </fieldset>
    <div class="field">
      <label for="db-target" class="label">Copy into</label>
      <select id="db-target" bind:value={target} onchange={() => (err = { ...err, target: undefined })} aria-invalid={!!err.target} aria-describedby="db-target-m" data-testid="migrate-db-target">
        <option value="">Pick a Zero2Dev service</option>
        {#each targets as t (t.id)}<option value={t.id}>{t.name} ({t.kind}, port {t.port})</option>{/each}
      </select>
      {#if err.target}<span class="err" id="db-target-m">{err.target}</span>
      {:else if targets.length === 0}<span class="hint" id="db-target-m">No Zero2Dev service can take a {kind === "postgres" ? "PostgreSQL" : "MySQL or MariaDB"} dump. <a href="#/services/new">Create one</a>.</span>
      {:else if targetSvc && targetSvc.state !== "running"}<span class="hint" id="db-target-m">{targetSvc.name} is stopped. <a href="#/services/{targetSvc.id}">Start it</a> first: the copy imports into the running server.</span>
      {:else}<span class="hint" id="db-target-m">A database that exists there already is replaced.</span>{/if}
    </div>
  {/if}
</form>

<style>
  .dbs {
    display: grid;
    gap: 8px;
    margin: 0;
    padding: 0;
    border: 0;
    min-width: 0;
  }
  .dbs legend {
    padding: 0;
    margin-bottom: 4px;
  }
</style>
