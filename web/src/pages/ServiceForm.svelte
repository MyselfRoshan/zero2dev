<script lang="ts">
  // The service editor (a page, see ServiceEditor.svelte). It is drawn from the type's profile
  // (GET /api/kinds): only the fields that type uses, its common settings up front and the rest
  // under "More settings", and beside it the exact command line the daemon will run.
  import { api, ApiError } from "../lib/api";
  import { app, lists, toast, upsertService, loadStatus, loadKinds } from "../lib/store.svelte";
  import type { CommandPreview, Service, ServiceInput, ServiceKind } from "../lib/types";
  import { serviceKindLabel } from "../lib/ux";
  import { defaultOptionLabel, defaultRuntime, RUNTIME_GROUPS, runtimeOptionLabel, SERVICE_GROUP_IDS } from "../lib/catalog";
  import { envRowsToMap, hasErrors, isAbsolutePath, parsePort, runtimeKindsFor, validateService, type ServiceField } from "../lib/validate";
  import {
    changedKeys,
    creatableKinds,
    effective,
    fixedFor,
    profileFor,
    resetKeys,
    settingErrors,
    settingsForSave,
    splitCommon,
    versionAtLeast,
    type KindOption,
    type Settings,
  } from "../lib/kinds";
  import Icon from "../ui/Icon.svelte";
  import KindIcon from "../ui/KindIcon.svelte";
  import TypePicker from "../ui/TypePicker.svelte";
  import CommandPanel from "../ui/CommandPanel.svelte";
  import FolderInput from "../ui/FolderInput.svelte";
  import OptionField from "../ui/OptionField.svelte";
  import ExtensionPicker from "../ui/ExtensionPicker.svelte";

  type Props = { service?: Service; onsaved: (s: Service) => void; oncancel: () => void };
  let { service, onsaved, oncancel }: Props = $props();

  // The form edits a copy; later changes to the prop are intentionally ignored.
  // svelte-ignore state_referenced_locally
  const init = service;
  let name = $state(init?.name ?? "");
  // A new service starts with no type: the type cards are the first step.
  let kind = $state<ServiceKind | "">(init?.kind ?? "");
  let typeOpen = $state(!init);
  let moreOpen = $state(false);
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
  const split = $derived(profile ? splitCommon(profile, settings, optErrors) : { common: [], more: [] });
  const moreCount = $derived(split.more.reduce((n, g) => n + g.options.length, 0));
  const changed = $derived(customArgs ? [] : changedKeys(profile, settings));
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

  const runtimeOptions = $derived(
    kind ? lists.runtimes.filter((r) => runtimeKindsFor(kind as ServiceKind).includes(r.kind as never)) : [],
  );
  const defaultLabel = $derived(defaultOptionLabel(lists.runtimes, (kind && runtimeKindsFor(kind)[0]) || "other"));
  /** The runtime the service will use: the chosen one, else the default of its kind. */
  const runtime = $derived(
    !kind || kind === "custom" ? undefined : (runtimeOptions.find((r) => r.id === runtimeId) ?? defaultRuntime(lists.runtimes, kind as never)),
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
    typeOpen = false;
    if (value === kind) return;
    const oldDefault = portDefault(kind);
    if (kind === "custom" || value === "custom") {
      command = "";
      args = [];
      customArgs = false;
    }
    kind = value;
    settings = {}; // the new type's defaults
    moreOpen = false;
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
    const k = (kind || "custom") as ServiceKind;
    const { env: envMap, error } = envRowsToMap(env);
    const port = portUse === "hidden" ? (init?.port ?? 0) : parsePort(portText === "" ? "0" : portText);
    return {
      envError: error,
      input: {
        name: name.trim(),
        kind: k,
        command: k === "custom" || customArgs ? command.trim() : "",
        args: args.map((a) => a.v).filter((v) => v !== ""),
        settings: settingsForSave(profile, eff),
        customArgs: k !== "custom" && customArgs,
        cwd: folderUse === "hidden" ? (init?.cwd ?? "") : cwd.trim(),
        env: envMap,
        port: Number.isNaN(port) ? -1 : port,
        healthPath: profile && !profile.health ? "" : healthPath.trim(),
        runtimeId: k === "custom" ? "" : runtimeId,
        autostart,
      },
    };
  }

  // Live preview of the command line (debounced).
  let preview = $state<CommandPreview | null>(null);
  let previewError = $state("");
  $effect(() => {
    const { input } = build();
    if (!kind) {
      preview = null;
      return;
    }
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
    if (!kind) {
      typeOpen = true;
      formError = "Choose what you want to run first.";
      return;
    }
    if (hasErrors(errors) || Object.keys(optErrors).length) {
      queueMicrotask(() => {
        const el = document.querySelector<HTMLElement>("#svc-form [aria-invalid='true']");
        el?.scrollIntoView({ block: "center" });
        el?.focus();
      });
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

  function resetOne(key: string) {
    settings = resetKeys(settings, [key]);
    revalidate();
  }
  function resetAll() {
    settings = {};
    revalidate();
  }
  const folderLabel = $derived(profile?.folder.label ?? "Project folder");
  const advancedSummary = $derived.by(() => {
    const parts: string[] = [];
    if (kind && kind !== "custom") parts.push(customArgs ? "your own command line" : (profile?.extraArgs || "extra arguments").toLowerCase());
    if (profile?.health !== false) parts.push("health check");
    parts.push("environment variables");
    const t = parts.join(", ");
    return t.charAt(0).toUpperCase() + t.slice(1);
  });
</script>

<div class="editor">
  <form id="svc-form" class="col" onsubmit={save} oninput={revalidate} novalidate>
    {#if formError}<div class="notice bad" role="alert"><Icon name="alert" /><span>{formError}</span></div>{/if}

    <!-- 1. Type: cards until one is chosen, then a line at the top of Basics -->
    {#if typeOpen || !kind}
      <section class="section-card" aria-labelledby="sec-type" data-tour="type-picker">
        <header><h2 id="sec-type">{kind ? "Change the type" : "What do you want to run?"}</h2>{#if kind && init}<p>The settings go back to the new type's defaults.</p>{/if}</header>
        <div class="body">
          <TypePicker groups={KIND_GROUPS} profiles={lists.kinds} value={kind} onpick={setKind} label={serviceKindLabel} />
          {#if kind}<div><button type="button" class="btn sm ghost" onclick={() => (typeOpen = false)}>Keep {serviceKindLabel(kind)}</button></div>{/if}
        </div>
      </section>
    {/if}

    {#if kind}
      <!-- 2. Basics -->
      <section class="section-card" aria-labelledby="sec-basics" data-tour="basics">
        <div class="chosen" data-testid="chosen-type">
          <KindIcon {kind} size={28} />
          <div class="grow">
            <h2 id="sec-basics" class="chosen-name">{serviceKindLabel(kind)}</h2>
            <div class="muted small">{profile?.summary ?? ""}</div>
          </div>
          {#if !typeOpen}<button type="button" class="btn sm" onclick={() => (typeOpen = true)} aria-label="Change type">Change type</button>{/if}
        </div>
        <div class="body">
          {#if profile?.note}<div class="notice"><Icon name="info" /><span>{profile.note}</span></div>{/if}
          <div class="field"><label>
            <span class="label">Name</span>
            <input type="text" bind:value={name} maxlength="64" aria-invalid={!!errors.name} aria-describedby="e-name" placeholder={kind === "custom" ? "My tool" : `My ${serviceKindLabel(kind)} app`} /></label>
            {#if errors.name}<span class="err" id="e-name">{errors.name}</span>{/if}
          </div>

          {#if folderUse === "required"}
            <div class="field">
              <span class="label" aria-hidden="true">{folderLabel}</span>
              <FolderInput bind:value={cwd} label={folderLabel} title="Choose the {folderLabel.toLowerCase()}" placeholder="C:\work\my-app" invalid={!!errors.cwd} onpicked={revalidate} />
              {#if errors.cwd}<span class="err">{errors.cwd}</span>{:else}<span class="hint">{profile?.folder.hint ?? "The folder your app lives in. The app runs from here."}</span>{/if}
            </div>
          {:else if profile?.dataDir === "service"}
            <div class="field" data-testid="data-folder">
              <span class="label">Data folder</span>
              <span class="mono path">{dataDirText || "Chosen by Zero2Dev"}</span>
              <span class="hint">Zero2Dev keeps this service's files here, so there is no project folder to choose. Deleting the service keeps them.</span>
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
                    <span class="hint">No {serviceKindLabel(kind)} installed. <a href="#/runtimes">Install one</a>, or Zero2Dev uses the one on PATH.</span>
                  {/if}
                </div>
              {/if}
            </div>
          {/if}

          <div class="switch-line compact">
            <label for="autostart" class="grow">Start automatically when Zero2Dev starts</label>
            <input id="autostart" type="checkbox" role="switch" class="switch" bind:checked={autostart} />
          </div>
        </div>
      </section>

      <!-- 3. Settings -->
      {#if profile && kind !== "custom" && profile.options.length}
        <section class="section-card" aria-labelledby="sec-settings">
          <header><h2 id="sec-settings">{profile.label} settings</h2>{#if !customArgs}<p>Already set to values that work. Change only what you need.</p>{/if}</header>
          <div class="body">
            {#if customArgs}
              <div class="notice"><Icon name="info" /><span>The settings are off: this service runs the command line written under Advanced.</span></div>
            {:else}
              <div class="opts">
                {#each split.common as o (o.key)}{@render option(o)}{/each}
              </div>
              {#if moreCount}
                <button type="button" class="more" aria-expanded={moreOpen} onclick={() => (moreOpen = !moreOpen)}>
                  <span class="caret" aria-hidden="true">{moreOpen ? "▾" : "▸"}</span>
                  {moreOpen ? "Fewer settings" : `More settings (${moreCount})`}
                  {#if !moreOpen}<span class="muted small">{split.more.map((g) => g.name).join(", ")}</span>{/if}
                </button>
                {#if moreOpen}
                  {#each split.more as g (g.name)}
                    <h3 class="subhead">{g.name}</h3>
                    <div class="opts">{#each g.options as o (o.key)}{@render option(o)}{/each}</div>
                  {/each}
                {/if}
              {/if}
            {/if}
          </div>
        </section>
      {/if}

      <!-- 4. Advanced -->
      <details class="section-card adv" bind:open={advancedOpen}>
        <summary>
          <span class="adv-title">Advanced</span>
          <span class="muted small">{advancedSummary}</span>
        </summary>
        <div class="body">
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
            <div class="switch-line">
              <label for="custom-args" class="grow"><span class="label">Write my own command line</span><span class="hint">Turns the settings off: you write every argument yourself, starting from the generated ones.</span></label>
              <input id="custom-args" type="checkbox" role="switch" class="switch" checked={customArgs} onchange={(e) => toggleCustomArgs(e.currentTarget.checked)} data-testid="custom-args" />
            </div>
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
  </form>

  <div class="side">
    {#if kind}
      <CommandPanel {preview} error={previewError} fixed={profile ? fixedFor(profile, windows) : []} fixedEnv={profile?.env ?? []}
        {changed} onresetall={changed.length ? resetAll : undefined} custom={kind === "custom" || customArgs}
        formId="svc-form" saveLabel={service ? "Save changes" : "Create service"} {saving} {oncancel} />
    {:else}
      <aside class="placeholder">
        <h2>What will run</h2>
        <p class="muted small">Choose a type to see the exact command line Zero2Dev runs for it.</p>
        <div class="actions"><button class="btn" type="button" onclick={oncancel}>Cancel</button></div>
      </aside>
    {/if}
  </div>

  <!-- Narrow screens: Save stays in reach while the panel is below the form. -->
  <div class="mobile-bar">
    <button class="btn" type="button" onclick={oncancel}>Cancel</button>
    <button class="btn primary" type="submit" form="svc-form" disabled={saving || !kind}>{service ? "Save changes" : "Create service"}</button>
  </div>
</div>

{#snippet option(o: KindOption)}
  {#if o.emit?.special === "phpExtensions"}
    <ExtensionPicker runtimeId={runtime?.id ?? ""} bind:value={() => eff[o.key] ?? "", (v) => (settings[o.key] = v)} help={o.help} onchange={revalidate} />
  {:else}
    <OptionField option={o} bind:value={() => eff[o.key] ?? "", (v) => (settings[o.key] = v)} error={optErrors[o.key]}
      disabledNote={versionNote(o.minVersion)} suggestions={o.suggest === "npmScripts" ? npmScripts : []} onchange={revalidate} onreset={() => resetOne(o.key)} />
  {/if}
{/snippet}

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
  .editor {
    display: grid;
    grid-template-columns: minmax(0, 1fr) 340px;
    gap: 20px;
    align-items: start;
    max-width: 1180px;
  }
  .col {
    display: grid;
    gap: 16px;
    min-width: 0;
  }
  .side {
    position: sticky;
    top: 0;
  }
  .opt {
    font-weight: 400;
  }
  .opts {
    display: grid;
    gap: 18px;
  }
  .chosen {
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 14px 18px 12px;
    border-bottom: 1px solid var(--line);
  }
  .chosen-name {
    font-size: 16px;
    margin: 0;
  }
  .compact label {
    font-size: 14px;
  }
  .switch-line {
    display: flex;
    align-items: flex-start;
    gap: 12px;
  }
  .switch-line label {
    display: grid;
    gap: 3px;
    cursor: pointer;
  }
  .more {
    display: flex;
    align-items: center;
    gap: 8px;
    width: 100%;
    padding: 9px 12px;
    border: 1px dashed var(--line-strong);
    border-radius: var(--radius);
    background: var(--surface-2);
    color: var(--text);
    font: inherit;
    font-weight: 600;
    font-size: 14px;
    cursor: pointer;
    text-align: left;
  }
  .more:hover {
    border-color: var(--accent);
  }
  .more .small {
    font-weight: 400;
    margin-left: 4px;
  }
  .caret {
    width: 12px;
  }
  .adv > summary {
    display: flex;
    align-items: baseline;
    gap: 10px;
    padding: 14px 18px;
    cursor: pointer;
  }
  .adv[open] > summary {
    padding-bottom: 0;
  }
  .adv-title {
    font-weight: 600;
    font-size: 16px;
  }
  .placeholder {
    background: var(--surface);
    border: 1px dashed var(--line-strong);
    border-radius: 8px;
    padding: 14px 16px;
  }
  .placeholder h2 {
    font-size: 14px;
    margin: 0 0 6px;
  }
  .placeholder .actions {
    margin-top: 12px;
  }
  .mobile-bar {
    display: none;
  }
  @media (max-width: 1100px) {
    .editor {
      grid-template-columns: minmax(0, 1fr);
    }
    .side {
      position: static;
    }
    .mobile-bar {
      display: flex;
      justify-content: flex-end;
      gap: 8px;
      position: sticky;
      bottom: 0;
      padding: 12px 0;
      background: var(--bg);
      border-top: 1px solid var(--line);
      z-index: 2;
    }
  }
</style>
