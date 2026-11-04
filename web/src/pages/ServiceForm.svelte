<script lang="ts">
  import { api, ApiError } from "../lib/api";
  import { lists, toast, upsertService, loadStatus } from "../lib/store.svelte";
  import { SERVICE_KINDS, type Service, type ServiceInput, type ServiceKind } from "../lib/types";
  import { envRowsToMap, hasErrors, parsePort, runtimeKindsFor, validateService, type ServiceField } from "../lib/validate";
  import Icon from "../ui/Icon.svelte";
  import Modal from "../ui/Modal.svelte";

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

  const runtimeOptions = $derived(lists.runtimes.filter((r) => runtimeKindsFor(kind).includes(r.kind as never)));
  const defaultArgsHint = $derived(
    kind === "php"
      ? "Empty means -S 127.0.0.1:{port} -t <cwd>"
      : kind === "go"
        ? "Empty means run ."
        : kind === "caddy"
          ? "Empty means run with the generated Caddyfile"
          : kind === "node" || kind === "java"
            ? "Required for this kind. Use {port} to pass the port."
            : "One value per row. {port} is replaced with the port.",
  );

  $effect(() => {
    // Drop a runtime choice that no longer matches the selected kind.
    if (runtimeId && !runtimeOptions.some((r) => r.id === runtimeId)) runtimeId = "";
  });

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
        <span class="label">Kind</span>
        <select bind:value={kind} aria-invalid={!!errors.kind}>
          {#each SERVICE_KINDS as k (k)}<option value={k}>{k}</option>{/each}
        </select></label>
      </div>
      <div class="field"><label>
        <span class="label">Runtime</span>
        <select bind:value={runtimeId} disabled={kind === "custom"}>
          <option value="">{kind === "custom" ? "Not used for custom services" : "Automatic (first registered, or PATH)"}</option>
          {#each runtimeOptions as r (r.id)}<option value={r.id}>{r.name}</option>{/each}
        </select></label>
        {#if kind !== "custom" && runtimeOptions.length === 0}
          <span class="hint">No {kind} runtime yet. <a href="#/runtimes" onclick={oncancel}>Install or register one</a>, or rely on PATH.</span>
        {/if}
      </div>
      <div class="field"><label>
        <span class="label">Command {kind === "custom" ? "" : "(optional)"}</span>
        <input type="text" class="mono" bind:value={command} aria-invalid={!!errors.command} placeholder={kind === "custom" ? "C:\\tools\\app.exe" : "Use the runtime binary"} spellcheck="false" /></label>
        {#if errors.command}<span class="err">{errors.command}</span>{/if}
      </div>
    </div>

    <div class="field" role="group" aria-labelledby="args-l">
      <span class="label" id="args-l">Arguments</span>
      {#each args as a, i (a.id)}
        <div class="list-row">
          <input type="text" class="mono" bind:value={a.v} aria-label="Argument {i + 1}" spellcheck="false" placeholder={i === 0 && kind === "node" ? "server.js" : ""} />
          <button type="button" class="btn ghost icon" aria-label="Remove argument {i + 1}" onclick={() => { args.splice(i, 1); revalidate(); }}><Icon name="x" /></button>
        </div>
      {/each}
      <div><button type="button" class="btn sm" onclick={() => args.push({ id: nextId++, v: "" })}><Icon name="plus" size={14} /> Add argument</button></div>
      <span class="hint">{defaultArgsHint}</span>
      {#if errors.args}<span class="err">{errors.args}</span>{/if}
    </div>

    <div class="field"><label>
      <span class="label">Working directory</span>
      <input type="text" class="mono" bind:value={cwd} aria-invalid={!!errors.cwd} placeholder="C:\work\my-api" spellcheck="false" /></label>
      {#if errors.cwd}<span class="err">{errors.cwd}</span>{:else}<span class="hint">Absolute path of an existing folder.</span>{/if}
    </div>

    <div class="grid2">
      <div class="field"><label>
        <span class="label">Port</span>
        <input type="text" inputmode="numeric" class="mono" bind:value={portText} aria-invalid={!!errors.port} placeholder="0" /></label>
        {#if errors.port}<span class="err">{errors.port}</span>{:else}<span class="hint">Leave empty or 0 for no port. Passed as PORT and {"{port}"}.</span>{/if}
      </div>
      <div class="field"><label>
        <span class="label">Health path</span>
        <input type="text" class="mono" bind:value={healthPath} aria-invalid={!!errors.healthPath} placeholder="/health" spellcheck="false" /></label>
        {#if errors.healthPath}<span class="err">{errors.healthPath}</span>{:else}<span class="hint">Optional. Without it Zero2Dev checks that the port accepts connections.</span>{/if}
      </div>
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

    <label class="check"><input type="checkbox" bind:checked={autostart} /> <span>Start automatically when Zero2Dev starts</span></label>
  </form>
  {#snippet footer()}
    <button class="btn" type="button" onclick={oncancel}>Cancel</button>
    <button class="btn primary" type="submit" form="svc-form" disabled={saving}>{service ? "Save changes" : "Create service"}</button>
  {/snippet}
</Modal>
