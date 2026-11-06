<script lang="ts">
  import { api, ApiError } from "../lib/api";
  import { lists, toast, upsertService, loadStatus } from "../lib/store.svelte";
  import type { Service, ServiceInput, ServiceKind } from "../lib/types";
  import { DB_PORTS, defaultArgsText, needsArgs, serviceKindLabel } from "../lib/ux";
  import { defaultOptionLabel, RUNTIME_GROUPS, runtimeOptionLabel, SERVICE_GROUP_IDS } from "../lib/catalog";
  import { envRowsToMap, hasErrors, parsePort, runtimeKindsFor, validateService, type ServiceField } from "../lib/validate";
  import Icon from "../ui/Icon.svelte";
  import Modal from "../ui/Modal.svelte";
  import FolderInput from "../ui/FolderInput.svelte";

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

  let errors = $state<Partial<Record<ServiceField, string>>>({});
  let formError = $state("");
  let saving = $state(false);
  let submitted = $state(false);

  const defaultLabel = $derived(defaultOptionLabel(lists.runtimes, runtimeKindsFor(kind)[0] ?? "other"));
  const runtimeOptions = $derived(lists.runtimes.filter((r) => runtimeKindsFor(kind).includes(r.kind as never)));
  // Same categories as the Runtimes page, the generic kind last.
  const KIND_GROUPS: { label: string; kinds: ServiceKind[] }[] = [
    ...RUNTIME_GROUPS.filter((g) => SERVICE_GROUP_IDS.includes(g.id)).map((g) => ({ label: g.title, kinds: g.kinds as ServiceKind[] })),
    { label: "Other", kinds: ["custom"] },
  ];
  const defaultArgsHint = $derived(defaultArgsText(kind));
  // Databases get their usual port while the field is empty or still holds another database's default.
  $effect(() => {
    const def = DB_PORTS[kind];
    const cur = portText.trim();
    if (def && (cur === "" || cur === "0" || Object.values(DB_PORTS).includes(Number(cur)))) portText = String(def);
  });
  // Rarely needed fields live under "Advanced"; it starts open when they hold something.
  // svelte-ignore state_referenced_locally
  let advancedOpen = $state(
    !!(init && ((init.kind !== "custom" && init.command) || init.healthPath || Object.keys(init.env ?? {}).length)),
  );
  $effect(() => {
    if (errors.healthPath || errors.env || (kind !== "custom" && errors.command)) advancedOpen = true;
  });
  // A program typed for a custom service stays when the type changes and then overrides the
  // selected version, so keep it visible under Advanced.
  $effect(() => {
    if (kind !== "custom" && command.trim()) advancedOpen = true;
  });

  $effect(() => {
    // Drop a runtime choice that no longer matches the selected kind.
    if (runtimeId && !runtimeOptions.some((r) => r.id === runtimeId)) runtimeId = "";
  });

  function setKind(value: ServiceKind) {
    if (kind === "custom" && value !== "custom") command = "";
    kind = value;
  }

  function build(): { input: ServiceInput; envError?: string } {
    const { env: envMap, error } = envRowsToMap(env);
    const port = parsePort(portText);
    return {
      envError: error,
      input: {
        name: name.trim(),
        kind,
        command: command.trim(),
        args: args.map((a) => a.v).filter((v) => v !== ""),
        cwd: cwd.trim(),
        env: envMap,
        port: Number.isNaN(port) ? -1 : port,
        healthPath: healthPath.trim(),
        runtimeId: kind === "custom" ? "" : runtimeId,
        autostart,
      },
    };
  }

  function revalidate() {
    if (!submitted) return;
    const { input, envError } = build();
    errors = validateService(input);
    if (envError) errors.env = envError;
  }

  async function save(e: SubmitEvent) {
    e.preventDefault();
    submitted = true;
    formError = "";
    const { input, envError } = build();
    errors = validateService(input);
    if (envError) errors.env = envError;
    if (hasErrors(errors)) {
      queueMicrotask(() => document.querySelector<HTMLElement>("dialog [aria-invalid='true']")?.focus());
      return;
    }
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

    <div class="field">
      <span class="label" aria-hidden="true">Project folder</span>
      <FolderInput bind:value={cwd} label="Project folder" title="Choose the project folder" placeholder="C:\work\my-api" invalid={!!errors.cwd} onpicked={revalidate} />
      {#if errors.cwd}<span class="err">{errors.cwd}</span>{:else}<span class="hint">The folder your app lives in. The app runs from here.</span>{/if}
    </div>

    {#if kind === "custom"}
      <div class="field"><label>
        <span class="label">Program</span>
        <input type="text" class="mono" bind:value={command} aria-invalid={!!errors.command} placeholder="C:\tools\app.exe" spellcheck="false" /></label>
        {#if errors.command}<span class="err">{errors.command}</span>{:else}<span class="hint">A full path, or a program name found on PATH.</span>{/if}
      </div>
    {/if}

    <div class="field" role="group" aria-labelledby="args-l">
      <span class="label" id="args-l">Arguments {#if !needsArgs(kind)}<span class="muted opt">(optional)</span>{/if}</span>
      {#each args as a, i (a.id)}
        <div class="list-row">
          <input type="text" class="mono" bind:value={a.v} aria-label="Argument {i + 1}" spellcheck="false" placeholder={i === 0 && kind === "node" ? "server.js" : ""} />
          <button type="button" class="btn ghost icon" aria-label="Remove argument {i + 1}" onclick={() => { args.splice(i, 1); revalidate(); }}><Icon name="x" /></button>
        </div>
      {/each}
      <div><button type="button" class="btn sm" onclick={() => args.push({ id: nextId++, v: "" })}><Icon name="plus" size={14} /> Add argument</button></div>
      {#if errors.args}<span class="err">{errors.args}</span>{:else}<span class="hint">{defaultArgsHint}</span>{/if}
    </div>

    <div class="grid2">
      <div class="field"><label>
        <span class="label">Port <span class="muted opt">(optional)</span></span>
        <input type="text" inputmode="numeric" class="mono" bind:value={portText} aria-invalid={!!errors.port} placeholder="3000" /></label>
        {#if errors.port}<span class="err">{errors.port}</span>{:else}<span class="hint">The port your app listens on. Your app gets it as PORT.</span>{/if}
      </div>
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

    <label class="check"><input type="checkbox" bind:checked={autostart} /> <span>Start automatically when Zero2Dev starts</span></label>

    <details class="advanced" bind:open={advancedOpen}>
      <summary>Advanced: {kind === "custom" ? "" : "program, "}health check, environment variables</summary>
      <div class="adv-body stack">
        {#if kind !== "custom"}
          <div class="field"><label>
            <span class="label">Program</span>
            <input type="text" class="mono" bind:value={command} aria-invalid={!!errors.command} placeholder="Use the {serviceKindLabel(kind)} version above" spellcheck="false" /></label>
            {#if errors.command}<span class="err">{errors.command}</span>{:else}<span class="hint">Only to run something other than the selected version, for example npm.cmd.</span>{/if}
          </div>
        {/if}
        <div class="field"><label>
          <span class="label">Health check path</span>
          <input type="text" class="mono" bind:value={healthPath} aria-invalid={!!errors.healthPath} placeholder="/health" spellcheck="false" /></label>
          {#if errors.healthPath}<span class="err">{errors.healthPath}</span>{:else}<span class="hint">A page that answers when the app is ready. Without it, Zero2Dev waits until the port accepts connections.</span>{/if}
        </div>
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
          {#if errors.env}<span class="err">{errors.env}</span>{/if}
        </div>
      </div>
    </details>
  </form>
  {#snippet footer()}
    <button class="btn" type="button" onclick={oncancel}>Cancel</button>
    <button class="btn primary" type="submit" form="svc-form" disabled={saving}>{service ? "Save changes" : "Create service"}</button>
  {/snippet}
</Modal>

<style>
  .opt {
    font-weight: 400;
  }
</style>
