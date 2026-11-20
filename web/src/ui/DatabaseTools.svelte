<script lang="ts" module>
  import type { DbJobRef } from "../lib/databases";
  import type { Job } from "../lib/types";
  import { api as moduleApi } from "../lib/api";

  // The import or export last started on each service. Kept outside the component, so leaving the
  // page and coming back still shows the progress (or the result) of the job.
  type Tracked = DbJobRef & { last?: Job; refreshed?: boolean; dismissed?: boolean };
  const tracked = $state<Record<string, Tracked>>({});

  /** Safety net next to the job.progress / job.done events: asks the daemon until the job ends. */
  function poll(serviceId: string, jobId: string) {
    const tick = async () => {
      const t = tracked[serviceId];
      if (!t || t.id !== jobId) return;
      try {
        const j = await moduleApi.getJob(jobId);
        if (tracked[serviceId]?.id !== jobId) return;
        tracked[serviceId].last = j;
        if (j.state === "running") setTimeout(tick, 1000);
      } catch {
        setTimeout(tick, 2000);
      }
    };
    setTimeout(tick, 800);
  }
</script>

<script lang="ts">
  // The database tools of a database service (0.0.5): its databases with their sizes, Create
  // database, Create user, Import a dump, Export, the administrator password and Reset.
  // MariaDB, MySQL and PostgreSQL get everything; Redis gets the password and reset.
  import { untrack } from "svelte";
  import { api, ApiError } from "../lib/api";
  import {
    actionState,
    adminLabel,
    baseName,
    databaseNameError,
    dirOf,
    DUMP_FILTERS,
    dumpPathError,
    exportFileName,
    fmtSize,
    generatePassword,
    isDbService,
    isFileExists,
    isGzipPath,
    jobNoun,
    jobText,
    passwordError,
    resetMessage,
    stateNote,
    supportsFor,
    userDatabases,
    userNameError,
    type DbAction,
  } from "../lib/databases";
  import { dbDataPath } from "../lib/installer";
  import { offerSnapshot } from "../lib/snapshotOffer";
  import { app, attempt, errMessage, jobs, toast, upsertService } from "../lib/store.svelte";
  import type { DatabaseInfo, Service } from "../lib/types";
  import ConfirmDialog from "./ConfirmDialog.svelte";
  import Icon from "./Icon.svelte";
  import Modal from "./Modal.svelte";

  let { service }: { service: Service } = $props();

  const isDb = $derived(isDbService(service.kind));
  const sql = $derived(service.kind !== "redis");

  // ---- what the server has (GET .../database) ----
  let info = $state<DatabaseInfo | null>(null);
  let infoError = $state("");
  let loadSeq = 0;
  // The databases seen while it last ran: the reset confirmation names them while it is stopped.
  let lastKnown = $state<string[]>([]);
  async function load() {
    if (!isDbService(service.kind)) return;
    const seq = ++loadSeq;
    const sid = service.id;
    try {
      const i = await api.databaseInfo(sid);
      if (seq !== loadSeq) return;
      info = i;
      infoError = "";
      if (i.running) lastKnown = userDatabases(i).map((d) => d.name);
    } catch (e) {
      if (seq !== loadSeq) return;
      infoError = errMessage(e);
    }
  }
  // Again whenever the server starts or stops, or the password changes.
  $effect(() => {
    const st = service.state;
    void service.id;
    void service.adminPassword;
    if (st === "starting" || st === "stopping") return;
    untrack(() => void load());
  });

  const supports = $derived(info?.supports ?? supportsFor(service.kind));
  const running = $derived(service.state === "running");
  const dbs = $derived(running && info?.running ? userDatabases(info) : []);
  const users = $derived(running && info?.running ? (info?.users ?? []) : []);
  const admin = $derived(adminLabel(service.kind, info?.user));
  const hasPassword = $derived(!!service.adminPassword || !!info?.hasPassword);
  const dataDir = $derived(info?.dataDir || dbDataPath(app.status?.homeDir ?? "<data folder>", service.kind, service.id));

  // ---- the running import or export ----
  const ref = $derived(tracked[service.id]);
  const job = $derived.by(() => {
    if (!ref) return undefined;
    const live = jobs[ref.id];
    const last = ref.last;
    if (live && live.state !== "running") return live;
    if (last && last.state !== "running") return last;
    if (live && last) return live.percent >= last.percent ? live : last;
    return live ?? last;
  });
  const jobRunning = $derived(!!ref && (!job || job.state === "running"));
  const busy = $derived(jobRunning && ref ? jobNoun(ref.type) : "");
  // A finished job changes the list: read it again once.
  $effect(() => {
    if (ref && job && job.state !== "running" && !ref.refreshed) {
      ref.refreshed = true;
      untrack(() => void load());
    }
  });

  const can = (a: DbAction) => actionState(a, service, busy, supports);
  const toolsState = $derived(can("create"));
  const passwordState = $derived(can("password"));
  const resetState = $derived(can("reset"));
  const note = $derived(stateNote(service));

  // ---- dialogs ----
  type DialogKind = "create" | "user" | "import" | "export" | "password";
  let dialog = $state<DialogKind | null>(null);
  let formError = $state("");
  let errors = $state<Record<string, string>>({});
  let saving = $state(false);

  // field values
  let newDb = $state("");
  let userName = $state("");
  let userPw = $state("");
  let userGrant = $state("");
  let showUserPw = $state(false);
  const NEW = "\u0000new";
  let importTarget = $state("");
  let importNew = $state("");
  let importPath = $state("");
  let exportDb = $state("");
  let exportPath = $state("");
  let adminPw = $state("");
  let showAdminPw = $state(false);

  function open(kind: DialogKind, database = "") {
    formError = "";
    errors = {};
    saving = false;
    if (kind === "create") newDb = "";
    if (kind === "user") {
      userName = "";
      userPw = "";
      userGrant = database;
      showUserPw = false;
    }
    if (kind === "import") {
      importTarget = database || dbs[0]?.name || NEW;
      importNew = "";
    }
    if (kind === "export") {
      exportDb = database || dbs[0]?.name || "";
      if (exportPath && exportDb) exportPath = joinDir(dirOf(exportPath), exportFileName(exportDb, isGzipPath(exportPath)));
    }
    if (kind === "password") {
      adminPw = "";
      showAdminPw = false;
    }
    dialog = kind;
  }
  function close() {
    if (saving) return;
    dialog = null;
  }
  function joinDir(dir: string, name: string): string {
    return dir ? `${dir.replace(/[\\/]+$/, "")}\\${name}` : "";
  }
  function fail(e: unknown) {
    formError = e instanceof ApiError ? e.message : errMessage(e);
  }

  async function submitCreate(e: SubmitEvent) {
    e.preventDefault();
    errors = { name: databaseNameError(newDb) };
    if (errors.name) return;
    saving = true;
    formError = "";
    try {
      const r = await api.createDatabase(service.id, newDb.trim());
      toast("success", `Created the database ${r.name}.`);
      saving = false;
      dialog = null;
      void load();
    } catch (err) {
      fail(err);
      saving = false;
    }
  }

  async function submitUser(e: SubmitEvent) {
    e.preventDefault();
    errors = { user: userNameError(userName), password: passwordError(userPw) };
    if (errors.user || errors.password) return;
    saving = true;
    formError = "";
    try {
      const body: { user: string; password: string; database?: string } = { user: userName.trim(), password: userPw };
      if (userGrant) body.database = userGrant;
      const r = await api.createDatabaseUser(service.id, body);
      toast(
        "success",
        r.database ? `Created the user ${r.user} with all privileges on ${r.database}.` : `Created the user ${r.user}.`,
      );
      saving = false;
      dialog = null;
      void load();
    } catch (err) {
      fail(err);
      saving = false;
    }
  }

  async function submitImport(e: SubmitEvent) {
    e.preventDefault();
    const creating = importTarget === NEW;
    const database = creating ? importNew.trim() : importTarget;
    errors = {
      database: creating ? databaseNameError(importNew) : database ? "" : "Pick a database.",
      file: dumpPathError(importPath),
    };
    if (errors.database || errors.file) return;
    saving = true;
    formError = "";
    const filePath = importPath.trim();
    const offer = await offerSnapshot({
      scope: "service",
      ownerId: service.id,
      reason: `before importing ${baseName(filePath)} into ${database}`,
    });
    if (offer === "cancelled") {
      saving = false;
      return;
    }
    try {
      const j = await api.importDatabase(service.id, { database, filePath, create: creating });
      track(j, "db-import", database, filePath);
      saving = false;
      dialog = null;
    } catch (err) {
      fail(err);
      saving = false;
    }
  }

  let overwriteAsk = $state(false);
  async function submitExport(e?: SubmitEvent, overwrite = false) {
    e?.preventDefault();
    errors = { database: exportDb ? "" : "Pick a database.", file: dumpPathError(exportPath, "C:\\dumps\\shop.sql") };
    if (errors.database || errors.file) return;
    saving = true;
    formError = "";
    const filePath = exportPath.trim();
    try {
      const j = await api.exportDatabase(service.id, { database: exportDb, filePath, overwrite });
      track(j, "db-export", exportDb, filePath);
      overwriteAsk = false;
      saving = false;
      dialog = null;
    } catch (err) {
      saving = false;
      if (!overwrite && isFileExists(err)) overwriteAsk = true;
      else {
        overwriteAsk = false;
        fail(err);
      }
    }
  }

  function track(j: Job, type: "db-import" | "db-export", database: string, file: string) {
    tracked[service.id] = { id: j.id, type, database, file, last: j };
    poll(service.id, j.id);
  }
  function dismissJob() {
    delete tracked[service.id];
  }

  // ---- Browse (the native dialog on the daemon's PC) ----
  const canBrowse = $derived(!!app.status?.folderPicker);
  let browsing = $state(false);
  async function browse(save: boolean) {
    browsing = true;
    try {
      const current = save ? exportPath : importPath;
      const r = await api.pickFile({
        title: save ? `Save the dump of ${exportDb || "the database"} as` : "Choose a dump to import",
        start: dirOf(current),
        save,
        defaultName: save ? (baseName(current) || exportFileName(exportDb || "dump")) : undefined,
        filters: DUMP_FILTERS,
      });
      if (!r.cancelled && r.path) {
        if (save) exportPath = r.path;
        else importPath = r.path;
      }
    } catch (err) {
      toast("error", errMessage(err));
    } finally {
      browsing = false;
    }
  }

  // ---- password ----
  async function submitPassword(e: SubmitEvent) {
    e.preventDefault();
    errors = { password: passwordError(adminPw) };
    if (errors.password) return;
    saving = true;
    formError = "";
    try {
      const saved = await api.setDatabasePassword(service.id, adminPw);
      upsertService(saved);
      toast("success", `${admin} has a new password. Restart the apps that use ${service.name} so they get it.`);
      saving = false;
      dialog = null;
      void load();
    } catch (err) {
      fail(err);
      saving = false;
    }
  }
  let removeAsk = $state(false);
  let removing = $state(false);
  async function removePassword() {
    removing = true;
    const saved = await attempt(() => api.setDatabasePassword(service.id, ""), `${admin} has no password now.`);
    removing = false;
    removeAsk = false;
    if (saved) {
      upsertService(saved);
      void load();
    }
  }

  // ---- reset ----
  let resetAsk = $state(false);
  let resetting = $state(false);
  async function confirmReset() {
    resetAsk = false;
    const offer = await offerSnapshot({ scope: "service", ownerId: service.id, reason: `before resetting ${service.name}` });
    if (offer === "cancelled") return;
    resetting = true;
    const saved = await attempt(
      () => api.resetDatabase(service.id),
      `${service.name} was reset. The next start sets it up again, empty.`,
    );
    resetting = false;
    if (saved) {
      upsertService(saved);
      lastKnown = [];
      void load();
    }
  }
</script>

{#if isDb}
  <section class="section-card db-tools" aria-labelledby="db-tools-h" data-testid="db-tools">
    <header>
      <h2 id="db-tools-h"><Icon name="database" size={16} /> {sql ? "Databases" : "Password and reset"}</h2>
      <p>
        {#if sql}The databases on this server, and tools to create them, add users, load a dump into one or save one to a file.{:else}The password apps send to Redis, and a reset that empties it.{/if}
      </p>
    </header>
    <div class="body">
      {#if note}
        <div class="notice" role="status" data-testid="db-state-note"><Icon name="info" /><span>{note}</span></div>
      {/if}
      {#if infoError && service.state !== "starting" && service.state !== "stopping"}
        <div class="notice warn" role="alert" data-testid="db-info-error"><Icon name="alert" /><span>Could not read the databases: {infoError}</span></div>
      {/if}

      {#if ref && !ref.dismissed}
        {@const pct = job?.state === "done" ? 100 : Math.round(job?.percent ?? 0)}
        <div class="job" data-testid="db-job" data-state={job?.state ?? "running"}>
          <div class="row">
            {#if job?.state === "failed"}<span class="ic bad"><Icon name="alert" size={14} /></span>{:else if job?.state === "done"}<span class="ic ok"><Icon name="circleCheck" size={14} /></span>{:else}<span class="spin" aria-hidden="true"></span>{/if}
            <span class="grow" role="status" aria-live="polite" data-testid="db-job-text">{jobText(ref, job)}</span>
            {#if !jobRunning}<button class="btn sm ghost icon" type="button" onclick={dismissJob} aria-label="Dismiss" data-testid="db-job-dismiss"><Icon name="x" size={14} /></button>{/if}
          </div>
          <div class="progress" class:done={job?.state === "done"} class:failed={job?.state === "failed"} role="progressbar" aria-label={ref.type === "db-import" ? "Import progress" : "Export progress"} aria-valuemin={0} aria-valuemax={100} aria-valuenow={pct}><span style:width="{pct}%"></span></div>
        </div>
      {/if}

      {#if sql}
        <div class="row wrap tools" data-testid="db-actions">
          <button class="btn" type="button" disabled={!toolsState.allowed} aria-describedby={toolsState.allowed ? undefined : "db-why"} onclick={() => open("create")} data-testid="db-create"><Icon name="plus" size={14} /> Create database</button>
          <button class="btn" type="button" disabled={!toolsState.allowed} aria-describedby={toolsState.allowed ? undefined : "db-why"} onclick={() => open("user")} data-testid="db-user"><Icon name="userPlus" size={14} /> Create user</button>
          <button class="btn" type="button" disabled={!can("import").allowed} aria-describedby={can("import").allowed ? undefined : "db-why"} onclick={() => open("import")} data-testid="db-import"><Icon name="upload" size={14} /> Import a dump</button>
          <button class="btn" type="button" disabled={!can("export").allowed || !dbs.length} aria-describedby={can("export").allowed && dbs.length ? undefined : "db-why"} onclick={() => open("export")} data-testid="db-export"><Icon name="download" size={14} /> Export</button>
        </div>
        {#if !toolsState.allowed}
          <p class="hint why" id="db-why" data-testid="db-why">{toolsState.reason}</p>
        {:else if !dbs.length}
          <p class="hint why" id="db-why">Export needs a database: create one or import a dump first.</p>
        {/if}

        {#if running && info?.running}
          {#if dbs.length}
            <div class="t-wrap list">
              <table class="t" data-testid="db-list">
                <caption class="sr-only">Databases on {service.name}</caption>
                <thead><tr><th scope="col">Database</th><th scope="col" class="num">Size</th><th scope="col"><span class="sr-only">Actions</span></th></tr></thead>
                <tbody>
                  {#each dbs as d (d.name)}
                    <tr data-testid="db-row" data-db={d.name}>
                      <td class="mono">{d.name}</td>
                      <td class="num" title={d.sizeBytes ? `${d.sizeBytes} bytes` : "The server did not report a size"}>{fmtSize(d.sizeBytes)}</td>
                      <td class="actions">
                        <button class="btn sm ghost" type="button" disabled={!can("import").allowed} onclick={() => open("import", d.name)} aria-label="Import a dump into {d.name}"><Icon name="upload" size={14} /> Import</button>
                        <button class="btn sm ghost" type="button" disabled={!can("export").allowed} onclick={() => open("export", d.name)} aria-label="Export {d.name}"><Icon name="download" size={14} /> Export</button>
                      </td>
                    </tr>
                  {/each}
                </tbody>
              </table>
            </div>
          {:else}
            <p class="muted empty-list" data-testid="db-empty">No databases yet. Create one, or import a dump into a new database.</p>
          {/if}
          {#if users.length}
            <p class="small muted" data-testid="db-users">Users: <span class="mono">{users.join(", ")}</span></p>
          {/if}
        {/if}
      {/if}

      {#if supports.password}
        <div class="part" data-testid="db-password">
          <h3 class="subhead">Administrator password</h3>
          <p class="muted" data-testid="db-password-state">
            {#if hasPassword}{admin} has a password. Apps that use {service.name} get it in their connection variables.{:else}{admin} has no password. That is fine while only programs on this PC connect; set one for apps and tools that ask for it.{/if}
          </p>
          <div class="row wrap">
            <button class="btn" type="button" disabled={!passwordState.allowed} aria-describedby={passwordState.allowed ? undefined : "db-password-why"} onclick={() => open("password")} data-testid="db-password-set"><Icon name="key" size={14} /> {hasPassword ? "Change password" : "Set password"}</button>
            {#if hasPassword}
              <button class="btn" type="button" disabled={!passwordState.allowed} aria-describedby={passwordState.allowed ? undefined : "db-password-why"} onclick={() => (removeAsk = true)} data-testid="db-password-remove"><Icon name="x" size={14} /> Remove password</button>
            {/if}
          </div>
          {#if !passwordState.allowed}<p class="hint" id="db-password-why" data-testid="db-password-why">{passwordState.reason}</p>{/if}
        </div>
      {/if}

      {#if supports.reset}
        <div class="part" data-testid="db-reset-part">
          <h3 class="subhead">Reset</h3>
          <p class="muted">Deletes everything {service.name} stores, by deleting its data folder <code class="mono">{dataDir}</code>. The next start sets it up again as on its first start: empty, without a password.</p>
          <div class="row wrap">
            <button class="btn danger" type="button" disabled={!resetState.allowed || resetting} aria-describedby={resetState.allowed ? undefined : "db-reset-why"} onclick={() => (resetAsk = true)} data-testid="db-reset"><Icon name="trash" size={14} /> {resetting ? "Resetting..." : `Reset ${sql ? "database" : "Redis"}...`}</button>
          </div>
          {#if !resetState.allowed}<p class="hint" id="db-reset-why" data-testid="db-reset-why">{resetState.reason}</p>{/if}
        </div>
      {/if}
    </div>
  </section>

  {#if dialog === "create"}
    <Modal title="Create a database on {service.name}" narrow onclose={close}>
      <form id="db-form-create" class="dlg-body stack" onsubmit={submitCreate} novalidate data-testid="db-form-create">
        {#if formError}<div class="notice bad" role="alert"><Icon name="alert" /><span>{formError}</span></div>{/if}
        <div class="field">
          <label><span class="label">Database name</span>
            <input type="text" class="mono" bind:value={newDb} aria-invalid={!!errors.name} aria-describedby="db-new-help" placeholder="shop" spellcheck="false" autocomplete="off" />
          </label>
          {#if errors.name}<span class="err" id="db-new-help">{errors.name}</span>{:else}<span class="hint" id="db-new-help">1-64 letters, digits or _. It starts empty.</span>{/if}
        </div>
      </form>
      {#snippet footer()}
        <button class="btn" type="button" onclick={close}>Cancel</button>
        <button class="btn primary" type="submit" form="db-form-create" disabled={saving}>Create database</button>
      {/snippet}
    </Modal>
  {:else if dialog === "user"}
    <Modal title="Create a user on {service.name}" narrow onclose={close}>
      <form id="db-form-user" class="dlg-body stack" onsubmit={submitUser} novalidate data-testid="db-form-user">
        {#if formError}<div class="notice bad" role="alert"><Icon name="alert" /><span>{formError}</span></div>{/if}
        <div class="field">
          <label><span class="label">User name</span>
            <input type="text" class="mono" bind:value={userName} aria-invalid={!!errors.user} aria-describedby="db-user-help" placeholder="shop" spellcheck="false" autocomplete="off" />
          </label>
          {#if errors.user}<span class="err" id="db-user-help">{errors.user}</span>{:else}<span class="hint" id="db-user-help">1-64 letters, digits or _.</span>{/if}
        </div>
        <div class="field">
          <label for="db-user-pw"><span class="label">Password</span></label>
          <div class="row">
            <input id="db-user-pw" class="mono grow" type={showUserPw ? "text" : "password"} bind:value={userPw} aria-invalid={!!errors.password} aria-describedby="db-user-pw-help" autocomplete="new-password" spellcheck="false" />
            <button class="btn" type="button" aria-pressed={showUserPw} onclick={() => (showUserPw = !showUserPw)} aria-label={showUserPw ? "Hide the password" : "Show the password"}><Icon name={showUserPw ? "eyeOff" : "eye"} size={14} /></button>
            <button class="btn" type="button" onclick={() => { userPw = generatePassword(); showUserPw = true; }}>Generate</button>
          </div>
          {#if errors.password}<span class="err" id="db-user-pw-help">{errors.password}</span>{:else}<span class="hint" id="db-user-pw-help">Your app logs in with this user and password.</span>{/if}
        </div>
        <div class="field">
          <label><span class="label">Access to</span>
            <select bind:value={userGrant} aria-describedby="db-grant-help">
              <option value="">No database yet</option>
              {#each dbs as d (d.name)}<option value={d.name}>{d.name}</option>{/each}
            </select>
          </label>
          <span class="hint" id="db-grant-help">The user gets all privileges on the database you pick, and nothing else.</span>
        </div>
      </form>
      {#snippet footer()}
        <button class="btn" type="button" onclick={close}>Cancel</button>
        <button class="btn primary" type="submit" form="db-form-user" disabled={saving}>Create user</button>
      {/snippet}
    </Modal>
  {:else if dialog === "import"}
    <Modal title="Import a dump into {service.name}" onclose={close}>
      <form id="db-form-import" class="dlg-body stack" onsubmit={submitImport} novalidate data-testid="db-form-import">
        {#if formError}<div class="notice bad" role="alert"><Icon name="alert" /><span>{formError}</span></div>{/if}
        <div class="grid2">
          <div class="field">
            <label><span class="label">Into the database</span>
              <select bind:value={importTarget} aria-invalid={!!errors.database && importTarget !== NEW}>
                {#each dbs as d (d.name)}<option value={d.name}>{d.name}</option>{/each}
                <option value={NEW}>A new database...</option>
              </select>
            </label>
          </div>
          {#if importTarget === NEW}
            <div class="field">
              <label><span class="label">New database name</span>
                <input type="text" class="mono" bind:value={importNew} aria-invalid={!!errors.database} aria-describedby="db-imp-new-help" placeholder="shop" spellcheck="false" autocomplete="off" />
              </label>
              {#if errors.database}<span class="err" id="db-imp-new-help">{errors.database}</span>{:else}<span class="hint" id="db-imp-new-help">Created first, then filled from the dump.</span>{/if}
            </div>
          {/if}
        </div>
        <div class="field">
          <label for="db-imp-file"><span class="label">Dump file</span></label>
          <div class="row">
            <input id="db-imp-file" type="text" class="mono grow" bind:value={importPath} aria-invalid={!!errors.file} aria-describedby="db-imp-file-help" placeholder={"C:\\dumps\\shop.sql"} spellcheck="false" readonly={browsing} />
            {#if canBrowse}<button class="btn" type="button" onclick={() => browse(false)} disabled={browsing} aria-label="Browse for the dump file">{#if browsing}<span class="spin" aria-hidden="true"></span> Waiting...{:else}<Icon name="folder" size={14} /> Browse...{/if}</button>{/if}
          </div>
          {#if errors.file}<span class="err" id="db-imp-file-help">{errors.file}</span>{:else}<span class="hint" id="db-imp-file-help">A .sql dump, or a compressed .sql.gz. The file is only read, never changed.</span>{/if}
          {#if browsing}<span class="hint" role="status">A Windows file dialog is open. Choose the file there, or close it to cancel.</span>{/if}
        </div>
        <div class="notice warn"><Icon name="alert" /><span>The statements in the dump run in {importTarget === NEW ? (importNew.trim() || "the new database") : importTarget}. Tables the dump drops and creates again are replaced. If a statement fails, the import stops and what came before it stays.</span></div>
      </form>
      {#snippet footer()}
        <button class="btn" type="button" onclick={close}>Cancel</button>
        <button class="btn primary" type="submit" form="db-form-import" disabled={saving || browsing}>{saving ? "Starting..." : "Import"}</button>
      {/snippet}
    </Modal>
  {:else if dialog === "export"}
    <Modal title="Export a database of {service.name}" onclose={close}>
      <form id="db-form-export" class="dlg-body stack" onsubmit={submitExport} novalidate data-testid="db-form-export">
        {#if formError}<div class="notice bad" role="alert"><Icon name="alert" /><span>{formError}</span></div>{/if}
        <div class="field">
          <label><span class="label">Database</span>
            <select bind:value={exportDb} aria-invalid={!!errors.database}>
              {#each dbs as d (d.name)}<option value={d.name}>{d.name}</option>{/each}
            </select>
          </label>
          {#if errors.database}<span class="err">{errors.database}</span>{/if}
        </div>
        <div class="field">
          <label for="db-exp-file"><span class="label">Save as</span></label>
          <div class="row">
            <input id="db-exp-file" type="text" class="mono grow" bind:value={exportPath} aria-invalid={!!errors.file} aria-describedby="db-exp-file-help" placeholder={"C:\\dumps\\" + exportFileName(exportDb || "shop")} spellcheck="false" readonly={browsing} />
            {#if canBrowse}<button class="btn" type="button" onclick={() => browse(true)} disabled={browsing} aria-label="Browse for where to save the dump">{#if browsing}<span class="spin" aria-hidden="true"></span> Waiting...{:else}<Icon name="folder" size={14} /> Browse...{/if}</button>{/if}
          </div>
          {#if errors.file}<span class="err" id="db-exp-file-help">{errors.file}</span>{:else}<span class="hint" id="db-exp-file-help">{isGzipPath(exportPath) ? "Ends in .gz, so the dump is compressed." : "A plain .sql file. End the name in .gz to compress it."} The file appears only when the export has finished.</span>{/if}
          {#if browsing}<span class="hint" role="status">A Windows file dialog is open. Choose where to save there, or close it to cancel.</span>{/if}
        </div>
      </form>
      {#snippet footer()}
        <button class="btn" type="button" onclick={close}>Cancel</button>
        <button class="btn primary" type="submit" form="db-form-export" disabled={saving || browsing}>{saving ? "Starting..." : "Export"}</button>
      {/snippet}
    </Modal>
  {:else if dialog === "password"}
    <Modal title="{hasPassword ? 'Change' : 'Set'} the password of {admin}" narrow onclose={close}>
      <form id="db-form-password" class="dlg-body stack" onsubmit={submitPassword} novalidate data-testid="db-form-password">
        {#if formError}<div class="notice bad" role="alert"><Icon name="alert" /><span>{formError}</span></div>{/if}
        <div class="field">
          <label for="db-admin-pw"><span class="label">New password</span></label>
          <div class="row">
            <input id="db-admin-pw" class="mono grow" type={showAdminPw ? "text" : "password"} bind:value={adminPw} aria-invalid={!!errors.password} aria-describedby="db-admin-pw-help" autocomplete="new-password" spellcheck="false" />
            <button class="btn" type="button" aria-pressed={showAdminPw} onclick={() => (showAdminPw = !showAdminPw)} aria-label={showAdminPw ? "Hide the password" : "Show the password"}><Icon name={showAdminPw ? "eyeOff" : "eye"} size={14} /></button>
            <button class="btn" type="button" onclick={() => { adminPw = generatePassword(); showAdminPw = true; }}>Generate</button>
          </div>
          {#if errors.password}<span class="err" id="db-admin-pw-help">{errors.password}</span>{:else}<span class="hint" id="db-admin-pw-help">Zero2Dev keeps it, uses it to stop the server cleanly, and gives it to the apps that use {service.name} (restart them to pick it up). You can see it again under Connection.</span>{/if}
        </div>
        {#if service.kind === "postgres"}<div class="notice"><Icon name="info" /><span>PostgreSQL still lets programs on this PC connect without the password. It is there for apps and tools that ask for one.</span></div>{/if}
      </form>
      {#snippet footer()}
        <button class="btn" type="button" onclick={close}>Cancel</button>
        <button class="btn primary" type="submit" form="db-form-password" disabled={saving}>{hasPassword ? "Change password" : "Set password"}</button>
      {/snippet}
    </Modal>
  {/if}

  {#if overwriteAsk}
    <ConfirmDialog
      title="Replace {baseName(exportPath)}?"
      message="{exportPath.trim()} already exists. Exporting replaces it with a new dump of {exportDb}."
      confirmLabel="Replace file"
      busy={saving}
      onconfirm={() => submitExport(undefined, true)}
      oncancel={() => (overwriteAsk = false)}
    />
  {/if}
  {#if removeAsk}
    <ConfirmDialog
      title="Remove the password of {admin}?"
      message="{admin} will have no password again. Apps that use {service.name} get an empty password from their next start; restart them after this."
      confirmLabel="Remove password"
      danger={false}
      busy={removing}
      onconfirm={removePassword}
      oncancel={() => (removeAsk = false)}
    />
  {/if}
  {#if resetAsk}
    <ConfirmDialog
      title="Reset {service.name}?"
      message={resetMessage(service, dataDir, lastKnown)}
      confirmLabel={sql ? "Reset database" : "Reset Redis"}
      busy={resetting}
      onconfirm={confirmReset}
      oncancel={() => (resetAsk = false)}
    />
  {/if}
{/if}

<style>
  .db-tools {
    margin-top: 16px;
  }
  .db-tools h2 {
    display: inline-flex;
    align-items: center;
    gap: 8px;
  }
  .tools .btn {
    flex: none;
  }
  .why {
    margin: -8px 0 0;
  }
  .list {
    border: 1px solid var(--line);
    border-radius: var(--radius-lg);
  }
  .num {
    text-align: right;
    white-space: nowrap;
  }
  .empty-list {
    margin: 0;
  }
  .part {
    display: grid;
    gap: 8px;
  }
  .part p {
    margin: 0;
  }
  .part .subhead {
    margin: 0;
  }
  .job {
    display: grid;
    gap: 8px;
    padding: 12px 16px;
    border: 1px solid var(--line);
    border-radius: var(--radius-lg);
    background: var(--surface-2);
  }
  .job .row {
    align-items: flex-start;
  }
  .job [role="status"] {
    overflow-wrap: anywhere;
  }
  .ic {
    display: inline-flex;
    margin-top: 4px;
  }
  .ic.ok {
    color: var(--ok);
  }
  .ic.bad {
    color: var(--bad);
  }
  code {
    overflow-wrap: anywhere;
  }
  @media (max-width: 560px) {
    .tools .btn {
      flex: 1 1 calc(50% - 8px);
    }
  }
</style>
