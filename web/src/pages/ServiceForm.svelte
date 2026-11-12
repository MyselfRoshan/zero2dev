<script lang="ts">
  // The service form is drawn from the type's profile (GET /api/kinds): only the fields that
  // type uses, its settings with their defaults and explanations, and a live preview of the
  // exact command line the daemon will run.
  import { api, ApiError } from "../lib/api";
  import { app, lists, toast, upsertService, loadStatus, loadKinds } from "../lib/store.svelte";
  import type { CommandPreview, Service, ServiceInput, ServiceKind } from "../lib/types";
  import { serviceKindLabel } from "../lib/ux";
  import { defaultOptionLabel, defaultRuntime, RUNTIME_GROUPS, runtimeOptionLabel, SERVICE_GROUP_IDS } from "../lib/catalog";
  import { envRowsToMap, hasErrors, isAbsolutePath, parsePort, runtimeKindsFor, validateService, type ServiceField } from "../lib/validate";
  import {
    commandText,
    creatableKinds,
    effective,
    fixedFor,
    friendlyArgs,
    groupedOptions,
    profileFor,
    settingErrors,
    settingsForSave,
    versionAtLeast,
    type Settings,
  } from "../lib/kinds";
  import Icon from "../ui/Icon.svelte";
  import Modal from "../ui/Modal.svelte";
  import FolderInput from "../ui/FolderInput.svelte";
  import OptionField from "../ui/OptionField.svelte";
  import ExtensionPicker from "../ui/ExtensionPicker.svelte";

  type Props = { service?: Service; onsaved: (s: Service) => void; oncancel: () => void };
  let { service, onsaved, oncancel }: Props = $props();

  // The form edits a copy; later changes to the prop are intentionally ignored.
  // svelte-ignore state_referenced_locally
  const init = service;
  let name = $state(init?.name ?? "");
  let kind = $state<ServiceKind>(init?.kind ?? "node");
  let command = $state(init?.command ?? "");
  let args = $state<{ id: number; v: string }[]>((init?.args ?? []).map((v, i) => ({ id: i, v })));
  let nextId = 1000;
  let cwd = $state(init?.cwd ?? "");
  let env = $state<{ id: number; key: string; value: string }[]>(
    Object.entries(init?.env ?? {}).map(([key, value], i) => ({ id: i, key, value })),
  );
  let portText = $state(init ? String(init.port) : "");
  let healthPath = $state(init?.healthPath ?? "");
  let runtimeId = $state(init?.runtimeId ?? "");
  let autostart = $state(init?.autostart ?? false);
  let settings = $state<Settings>({ ...init?.settings });
  let customArgs = $state(init?.kind !== "custom" && !!init?.customArgs);

  let errors = $state<Partial<Record<ServiceField, string>>>({});
  let optErrors = $state<Record<string, string>>({});
  let formError = $state("");
  let saving = $state(false);
  let submitted = $state(false);

  $effect(() => {
    if (lists.kinds.length === 0) void loadKinds();
  });

  const profile = $derived(profileFor(lists.kinds, kind));
  const eff = $derived(effective(profile, settings));
  const groups = $derived(profile ? groupedOptions(profile, eff) : []);
  const windows = $derived((app.status?.os ?? "windows") === "windows");
  const folderUse = $derived(profile?.folder.use ?? "required");
  const portUse = $derived(profile?.port.use ?? "optional");

  // Types offered: those Zero2Dev does not create itself, plus the one being edited.
  const offered = $derived(new Set([...creatableKinds(lists.kinds), ...(init ? [init.kind] : [])]));
  const KIND_GROUPS = $derived(
    [
      ...RUNTIME_GROUPS.filter((g) => SERVICE_GROUP_IDS.includes(g.id)).map((g) => ({ label: g.title, kinds: g.kinds as ServiceKind[] })),
      { label: "Other", kinds: ["custom"] as ServiceKind[] },
    ]
      .map((g) => ({ ...g, kinds: lists.kinds.length ? g.kinds.filter((k) => offered.has(k)) : g.kinds }))
      .filter((g) => g.kinds.length),
  );

  const runtimeOptions = $derived(lists.runtimes.filter((r) => runtimeKindsFor(kind).includes(r.kind as never)));
  const defaultLabel = $derived(defaultOptionLabel(lists.runtimes, runtimeKindsFor(kind)[0] ?? "other"));
  /** The runtime the service will use: the chosen one, else the default of its kind. */
  const runtime = $derived(
    kind === "custom" ? undefined : (runtimeOptions.find((r) => r.id === runtimeId) ?? defaultRuntime(lists.runtimes, kind as never)),
  );

  $effect(() => {
    // Drop a runtime choice that no longer matches the selected kind.
    if (runtimeId && !runtimeOptions.some((r) => r.id === runtimeId)) runtimeId = "";
  });

  function portDefault(k: string): number {
    return profileFor(lists.kinds, k)?.port.default ?? 0;
  }
  // A new service starts with its type's usual port.
  $effect(() => {
    if (!init && portText === "" && profile && profile.port.use !== "hidden" && (profile.port.default ?? 0) > 0) {
      portText = String(profile.port.default);
    }
  });

  function setKind(value: ServiceKind) {
    if (value === kind) return;
    const oldDefault = portDefault(kind);
    if (kind === "custom" || value === "custom") {
      command = "";
      args = [];
      customArgs = false;
    }
    kind = value;
    settings = {}; // the new type's defaults
    const cur = portText.trim();
    if (cur === "" || cur === "0" || Number(cur) === oldDefault) {
      const d = portDefault(value);
      portText = d > 0 ? String(d) : "";
    }
    revalidate();
  }

  // Rarely needed fields live under "Advanced"; it starts open when they hold something.
  // svelte-ignore state_referenced_locally
  let advancedOpen = $state(
    !!(init && (init.customArgs || (init.kind !== "custom" && init.args.length) || init.healthPath || Object.keys(init.env ?? {}).length)),
  );
  $effect(() => {
    if (errors.healthPath || errors.env || (kind !== "custom" && errors.command)) advancedOpen = true;
  });

  // npm scripts of the project, as suggestions.
  let npmScripts = $state<string[]>([]);
  $effect(() => {
    const dir = cwd.trim();
    if (kind !== "node" || !isAbsolutePath(dir)) {
      npmScripts = [];
      return;
    }
    const t = setTimeout(() => {
      api.projectScripts(dir).then((r) => (npmScripts = r.scripts), () => (npmScripts = []));
    }, 300);
    return () => clearTimeout(t);
  });

  function build(): { input: ServiceInput; envError?: string } {
    const { env: envMap, error } = envRowsToMap(env);
    const port = portUse === "hidden" ? (init?.port ?? 0) : parsePort(portText === "" ? "0" : portText);
    return {
      envError: error,
      input: {
        name: name.trim(),
        kind,
        command: kind === "custom" || customArgs ? command.trim() : "",
        args: args.map((a) => a.v).filter((v) => v !== ""),
        settings: settingsForSave(profile, eff),
        customArgs: kind !== "custom" && customArgs,
        cwd: folderUse === "hidden" ? (init?.cwd ?? "") : cwd.trim(),
        env: envMap,
        port: Number.isNaN(port) ? -1 : port,
        healthPath: profile && !profile.health ? "" : healthPath.trim(),
        runtimeId: kind === "custom" ? "" : runtimeId,
        autostart,
      },
    };
  }

  // Live preview of the command line (debounced).
  let preview = $state<CommandPreview | null>(null);
  let previewError = $state("");
  $effect(() => {
    const { input } = build();
    const body = { ...input, id: init?.id };
    const t = setTimeout(() => {
      api.previewService(body).then(
        (p) => ((preview = p), (previewError = "")),
        (e) => (previewError = e instanceof ApiError ? e.message : String(e)),
      );
    }, 300);
    return () => clearTimeout(t);
  });

  function toggleCustomArgs(on: boolean) {
    customArgs = on;
    if (on && preview) {
      // Start from the generated arguments, so writing your own is an edit.
      args = preview.args.map((v) => ({ id: nextId++, v }));
      if (!command.trim() && preview.program !== runtime?.binary) command = preview.program;
    } else if (!on) {
      args = [];
      command = "";
    }
    advancedOpen = true;
  }

  function checkAll() {
    const { input, envError } = build();
    errors = validateService(input, { folderRequired: folderUse === "required", portRequired: portUse === "required" });
    if (envError) errors.env = envError;
    optErrors = customArgs ? {} : settingErrors(profile, eff);
  }

  function revalidate() {
    if (submitted) checkAll();
  }

  async function save(e: SubmitEvent) {
    e.preventDefault();
    submitted = true;
    formError = "";
    checkAll();
    if (hasErrors(errors) || Object.keys(optErrors).length) {
      queueMicrotask(() => document.querySelector<HTMLElement>("dialog [aria-invalid='true']")?.focus());
      return;
    }
    const { input } = build();
    saving = true;
    try {
      const saved = service ? await api.updateService(service.id, input) : await api.createService(input);
      upsertService(saved);
      void loadStatus();
      toast("success", service ? `${saved.name} saved.` : `${saved.name} created.`);
      onsaved(saved);
    } catch (err) {
      formError = err instanceof ApiError ? err.message : String(err);
    } finally {
      saving = false;
    }
  }

  function versionNote(minVersion: string | undefined): string {
    if (!minVersion || !runtime || versionAtLeast(runtime.version, minVersion)) return "";
    return `Needs ${profile?.label ?? kind} ${minVersion} or newer (this is ${runtime.version})`;
  }

  const dataDirText = $derived(preview?.dataDir || "");
</script>

<Modal title={service ? `Edit ${service.name}` : "New service"} onclose={oncancel}>
  <form id="svc-form" class="dlg-body stack" onsubmit={save} oninput={revalidate} novalidate>
    {#if formError}<div class="notice bad" role="alert"><Icon name="alert" /><span>{formError}</span></div>{/if}
    <div class="grid2">
      <div class="field"><label>
        <span class="label">Name</span>
        <input type="text" bind:value={name} maxlength="64" aria-invalid={!!errors.name} aria-describedby="e-name" placeholder="My API" /></label>
        {#if errors.name}<span class="err" id="e-name">{errors.name}</span>{/if}
      </div>
      <div class="field"><label>
        <span class="label">Type</span>
        <select bind:value={() => kind, setKind} aria-invalid={!!errors.kind}>
          {#each KIND_GROUPS as g (g.label)}
            <optgroup label={g.label}>{#each g.kinds as k (k)}<option value={k}>{serviceKindLabel(k)}</option>{/each}</optgroup>
          {/each}
        </select></label>
      </div>
    </div>

    {#if profile?.note}<div class="notice"><Icon name="info" /><span>{profile.note}</span></div>{/if}

    {#if folderUse === "required"}
      <div class="field">
        <span class="label" aria-hidden="true">{profile?.folder.label ?? "Project folder"}</span>
        <FolderInput bind:value={cwd} label={profile?.folder.label ?? "Project folder"} title="Choose the {(profile?.folder.label ?? 'project folder').toLowerCase()}" placeholder="C:\work\my-app" invalid={!!errors.cwd} onpicked={revalidate} />
        {#if errors.cwd}<span class="err">{errors.cwd}</span>{:else}<span class="hint">{profile?.folder.hint ?? "The folder your app lives in. The app runs from here."}</span>{/if}
      </div>
    {:else if profile?.dataDir === "service"}
      <div class="field" data-testid="data-folder">
        <span class="label">Data folder</span>
        <span class="mono path">{dataDirText || "Chosen by Zero2Dev"}</span>
        <span class="hint">Zero2Dev keeps this service's files here, inside its own data folder. There is no project folder to choose. Deleting the service keeps them.</span>
      </div>
    {/if}

    {#if kind === "custom"}
      <div class="field"><label>
        <span class="label">Program</span>
        <input type="text" class="mono" bind:value={command} aria-invalid={!!errors.command} placeholder="C:\tools\app.exe" spellcheck="false" /></label>
        {#if errors.command}<span class="err">{errors.command}</span>{:else}<span class="hint">A full path, or a program name found on PATH.</span>{/if}
      </div>
      {@render argList("Arguments", profile?.extraArgsHint ?? "One value per row. {port} is replaced with the port.")}
    {/if}

    {#if portUse !== "hidden" || kind !== "custom"}
      <div class="grid2">
        {#if portUse !== "hidden"}
          <div class="field"><label>
            <span class="label">Port {#if portUse === "optional"}<span class="muted opt">(optional)</span>{/if}</span>
            <input type="text" inputmode="numeric" class="mono" bind:value={portText} aria-invalid={!!errors.port} placeholder={String(profile?.port.default || 3000)} /></label>
            {#if errors.port}<span class="err">{errors.port}</span>{:else}<span class="hint">{profile?.port.hint ?? "The port your app listens on."}</span>{/if}
          </div>
        {/if}
        {#if kind !== "custom"}
          <div class="field"><label>
            <span class="label">Version</span>
            <select bind:value={runtimeId}>
              <option value="">{defaultLabel}</option>
              {#each runtimeOptions as r (r.id)}<option value={r.id}>{runtimeOptionLabel(r)}</option>{/each}
            </select></label>
            {#if runtimeOptions.length === 0}
              <span class="hint">No {serviceKindLabel(kind)} installed. <a href="#/runtimes" onclick={oncancel}>Install one</a>, or Zero2Dev uses the one on PATH.</span>
            {/if}
          </div>
        {/if}
      </div>
    {/if}

    {#if profile && kind !== "custom"}
      {#if customArgs}
        <div class="notice"><Icon name="info" /><span>The settings are off: this service runs the command line written under Advanced.</span></div>
      {:else if groups.length}
        <section class="settings" aria-label="{profile.label} settings">
          {#each groups as g (g.name)}
            <div class="group">
              <h3 class="group-title">{g.name}</h3>
              <div class="stack">
                {#each g.options as o (o.key)}
                  {#if o.emit?.special === "phpExtensions"}
                    <ExtensionPicker runtimeId={runtime?.id ?? ""} bind:value={() => eff[o.key] ?? "", (v) => (settings[o.key] = v)} help={o.help} onchange={revalidate} />
                  {:else}
                    <OptionField option={o} bind:value={() => eff[o.key] ?? "", (v) => (settings[o.key] = v)} error={optErrors[o.key]}
                      disabledNote={versionNote(o.minVersion)} suggestions={o.suggest === "npmScripts" ? npmScripts : []} onchange={revalidate} />
                  {/if}
                {/each}
              </div>
            </div>
          {/each}
        </section>
      {/if}
      {#if !customArgs && (fixedFor(profile, windows).length || profile.env?.length)}
        <details class="advanced" data-testid="always-added">
          <summary>Always added ({fixedFor(profile, windows).length + (profile.env?.length ?? 0)})</summary>
          <ul class="adv-body fixed">
            {#each fixedFor(profile, windows) as f (f.args.join(" "))}
              <li><code>{friendlyArgs(f.args)}</code> <span class="muted">{f.help}</span></li>
            {/each}
            {#each profile.env ?? [] as v (v.name)}
              <li><code>{v.name}={friendlyArgs([v.value])}</code> <span class="muted">{v.help}</span></li>
            {/each}
          </ul>
        </details>
      {/if}
    {/if}

    <label class="check"><input type="checkbox" bind:checked={autostart} /> <span>Start automatically when Zero2Dev starts</span></label>

    {#if kind !== "custom" || profile?.health !== false}
      <details class="advanced" bind:open={advancedOpen}>
        <summary>Advanced: {kind === "custom" ? "" : customArgs ? "your own command line, " : `${(profile?.extraArgs || "extra arguments").toLowerCase()}, `}{profile?.health === false ? "" : "health check, "}environment variables</summary>
        <div class="adv-body stack">
          {#if kind !== "custom"}
            {#if customArgs}
              <div class="field"><label>
                <span class="label">Program <span class="muted opt">(optional)</span></span>
                <input type="text" class="mono" bind:value={command} aria-invalid={!!errors.command} placeholder="Use the {serviceKindLabel(kind)} version above" spellcheck="false" /></label>
                {#if errors.command}<span class="err">{errors.command}</span>{:else}<span class="hint">Only to run something other than the selected version, for example npm.cmd.</span>{/if}
              </div>
              {@render argList("Arguments (the whole command line)", "These replace everything Zero2Dev would generate. {port}, {home} and {data} are replaced.")}
            {:else}
              {@render argList(profile?.extraArgs || "Extra arguments", profile?.extraArgsHint ?? "")}
            {/if}
            <label class="check"><input type="checkbox" checked={customArgs} onchange={(e) => toggleCustomArgs(e.currentTarget.checked)} data-testid="custom-args" />
              <span>Write my own command line instead of the settings</span></label>
          {/if}
          {#if profile?.health !== false}
            <div class="field"><label>
              <span class="label">Health check path</span>
              <input type="text" class="mono" bind:value={healthPath} aria-invalid={!!errors.healthPath} placeholder="/health" spellcheck="false" /></label>
              {#if errors.healthPath}<span class="err">{errors.healthPath}</span>{:else}<span class="hint">A page that answers when the app is ready. Without it, Zero2Dev waits until the port accepts connections.</span>{/if}
            </div>
          {/if}
          <div class="field" role="group" aria-labelledby="env-l">
            <span class="label" id="env-l">Environment variables</span>
            {#each env as r, i (r.id)}
              <div class="kv-row">
                <input type="text" class="mono" bind:value={r.key} aria-label="Variable name {i + 1}" placeholder="NODE_ENV" spellcheck="false" />
                <input type="text" class="mono" bind:value={r.value} aria-label="Variable value {i + 1}" placeholder="development" spellcheck="false" />
                <button type="button" class="btn ghost icon" aria-label="Remove variable {i + 1}" onclick={() => { env.splice(i, 1); revalidate(); }}><Icon name="x" /></button>
              </div>
            {/each}
            <div><button type="button" class="btn sm" onclick={() => env.push({ id: nextId++, key: "", value: "" })}><Icon name="plus" size={14} /> Add variable</button></div>
            {#if errors.env}<span class="err">{errors.env}</span>{:else}<span class="hint">Your variables win over the ones the settings set.</span>{/if}
          </div>
        </div>
      </details>
    {/if}

    <div class="field preview" aria-live="polite">
      <span class="label">Command line</span>
      {#if preview}
        <code class="cmd" data-testid="preview-command">{commandText(preview.program, preview.args)}</code>
        {#if preview.env.length}<span class="hint mono small">With {preview.env.join("  ")}</span>{/if}
        {#if preview.files.length}<span class="hint">Writes {preview.files.join(", ")} before each start.</span>{/if}
        {#each preview.warnings as w (w)}<span class="hint warn">{w}</span>{/each}
      {:else if previewError}
        <span class="err">{previewError}</span>
      {:else}
        <span class="hint">Working it out...</span>
      {/if}
    </div>
  </form>
  {#snippet footer()}
    <button class="btn" type="button" onclick={oncancel}>Cancel</button>
    <button class="btn primary" type="submit" form="svc-form" disabled={saving}>{service ? "Save changes" : "Create service"}</button>
  {/snippet}
</Modal>

{#snippet argList(label: string, hint: string)}
  <div class="field" role="group" aria-labelledby="args-l">
    <span class="label" id="args-l">{label} {#if kind !== "custom"}<span class="muted opt">(optional)</span>{/if}</span>
    {#each args as a, i (a.id)}
      <div class="list-row">
        <input type="text" class="mono" bind:value={a.v} aria-label="Argument {i + 1}" spellcheck="false" />
        <button type="button" class="btn ghost icon" aria-label="Remove argument {i + 1}" onclick={() => { args.splice(i, 1); revalidate(); }}><Icon name="x" /></button>
      </div>
    {/each}
    <div><button type="button" class="btn sm" onclick={() => args.push({ id: nextId++, v: "" })}><Icon name="plus" size={14} /> Add argument</button></div>
    {#if errors.args}<span class="err">{errors.args}</span>{:else if hint}<span class="hint">{hint}</span>{/if}
  </div>
{/snippet}

<style>
  .opt {
    font-weight: 400;
  }
  .settings {
    display: grid;
    gap: 14px;
    border-top: 1px solid var(--border, #e5e7eb);
    padding-top: 12px;
  }
  .group-title {
    font-size: 12px;
    text-transform: uppercase;
    letter-spacing: 0.04em;
    color: var(--muted, #6b7280);
    margin: 0 0 6px;
  }
  .fixed {
    margin: 0;
    list-style: none;
    display: grid;
    gap: 6px;
  }
  .fixed code {
    font-size: 12px;
  }
  .preview .cmd {
    display: block;
    padding: 8px 10px;
    background: var(--sunken, #f3f4f6);
    border-radius: 6px;
    font-size: 12px;
    white-space: pre-wrap;
    word-break: break-all;
  }
  .warn {
    color: var(--warn, #b45309);
  }
</style>
