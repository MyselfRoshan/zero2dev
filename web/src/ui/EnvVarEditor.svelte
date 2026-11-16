<script lang="ts">
  // Add or edit one variable of the Environment page: a name, where its value comes from
  // (segmented), and a live preview of the exact line that will be set.
  import { kindLabel, runtimeOptionLabel } from "../lib/catalog";
  import { ENV_KINDS, defaultOf, nameError, runtimeValue } from "../lib/environment";
  import type { EnvVar, Runtime } from "../lib/types";

  type Props = {
    /** The variable being edited, or null to add one. */
    item: EnvVar | null;
    runtimes: Runtime[];
    /** Names of the other variables, to refuse duplicates. */
    others: string[];
    onsave: (v: Omit<EnvVar, "id" | "enabled">) => void;
    oncancel: () => void;
  };
  let { item, runtimes, others, onsave, oncancel }: Props = $props();

  // The form starts from the item being edited (read once: the editor is re-created for another item).
  const start = (() => item)();
  let name = $state(start?.name ?? "");
  let source = $state<EnvVar["source"]>(start?.source ?? "default");
  let kind = $state(start?.kind ?? (ENV_KINDS.find((k) => defaultOf(runtimes, k)) ?? "java"));
  let runtimeId = $state(start?.runtimeId ?? runtimes[0]?.id ?? "");
  let value = $state(start?.value ?? "");
  let error = $state("");

  const preview = $derived.by(() => {
    if (source === "custom") return value;
    const r = source === "default" ? defaultOf(runtimes, kind) : runtimes.find((x) => x.id === runtimeId);
    return r ? runtimeValue(r) : "";
  });
  const previewNote = $derived(
    source === "default" && !defaultOf(runtimes, kind) ? `No ${kindLabel(kind)} runtime is installed yet, so nothing would be set.` : "",
  );

  function save() {
    error = nameError(name, others);
    if (!error && source === "runtime" && !runtimeId) error = "Choose a version.";
    if (!error && source === "custom" && !value.trim()) error = "Enter a value.";
    if (error) return;
    onsave({
      name: name.trim(),
      source,
      ...(source === "default" ? { kind } : source === "runtime" ? { runtimeId } : { value }),
    });
  }
  function enter(e: KeyboardEvent) {
    if (e.key !== "Enter") return;
    e.preventDefault();
    save();
  }
  // Focus the name when the editor opens.
  let nameEl = $state<HTMLInputElement>();
  $effect(() => nameEl?.focus());
</script>

<div class="editor" role="group" aria-labelledby="env-ed-h" data-testid="env-var-editor">
  <h3 id="env-ed-h" class="panel-title">{item ? `Edit ${item.name}` : "Add a variable"}</h3>
  <div class="field">
    <label for="env-var-name" class="label">Name</label>
    <input id="env-var-name" bind:this={nameEl} type="text" class="mono" list="env-names" bind:value={name} spellcheck="false" autocomplete="off" aria-invalid={!!error && !!nameError(name, others)} data-testid="env-var-name" onkeydown={enter} />
    <datalist id="env-names">
      {#each ["JAVA_HOME", "GOROOT", "NODE_HOME", "PHP_HOME", "GOPATH", "CATALINA_HOME"] as n (n)}<option value={n}></option>{/each}
    </datalist>
  </div>
  <div class="field" role="radiogroup" aria-labelledby="env-src-l">
    <span class="label" id="env-src-l">Value</span>
    <div class="seg" data-testid="env-var-source">
      <label><input type="radio" name="env-src" value="default" bind:group={source} /> Default version of a runtime</label>
      <label><input type="radio" name="env-src" value="runtime" bind:group={source} /> One version</label>
      <label><input type="radio" name="env-src" value="custom" bind:group={source} /> Your own value</label>
    </div>
  </div>
  {#if source === "default"}
    <div class="field">
      <label for="env-var-kind" class="label">Runtime</label>
      <select id="env-var-kind" bind:value={kind} data-testid="env-var-kind">
        {#each ENV_KINDS as k (k)}<option value={k}>{kindLabel(k)}{defaultOf(runtimes, k) ? "" : " (not installed)"}</option>{/each}
      </select>
      <span class="hint">It moves along when you mark another version as Default on the Runtimes page.</span>
    </div>
  {:else if source === "runtime"}
    <div class="field">
      <label for="env-var-runtime" class="label">Version</label>
      {#if runtimes.length}
        <select id="env-var-runtime" bind:value={runtimeId} data-testid="env-var-runtime">
          {#each runtimes as r (r.id)}<option value={r.id}>{runtimeOptionLabel(r)}</option>{/each}
        </select>
      {:else}
        <span class="hint">No runtime is installed yet. <a href="#/runtimes">Install one on the Runtimes page.</a></span>
      {/if}
    </div>
  {:else}
    <div class="field">
      <label for="env-var-value" class="label">Your value</label>
      <input id="env-var-value" type="text" class="mono" bind:value spellcheck="false" autocomplete="off" data-testid="env-var-value" onkeydown={enter} />
    </div>
  {/if}
  <div class="preview" aria-live="polite" data-testid="env-var-preview">
    <span class="muted small">Will be set as</span>
    <code>{name.trim() || "NAME"} = {preview || "(nothing)"}</code>
    {#if previewNote}<span class="hint">{previewNote}</span>{/if}
  </div>
  {#if error}<span class="err" role="alert">{error}</span>{/if}
  <div class="row">
    <button class="btn" type="button" onclick={save} data-testid="env-var-save">{item ? "Save variable" : "Add variable"}</button>
    <button class="btn ghost" type="button" onclick={oncancel}>Cancel</button>
  </div>
</div>

<style>
  .editor {
    display: grid;
    gap: 12px;
    padding: 16px;
    border: 1px solid var(--line);
    border-radius: var(--radius-lg);
    background: var(--surface-2);
  }
  .panel-title {
    font-size: 14px;
    font-weight: 600;
    margin: 0;
  }
  .preview {
    display: grid;
    gap: 4px;
  }
  .preview code {
    padding: 4px 8px;
    border: 1px solid var(--line);
    border-radius: var(--radius-sm);
    background: var(--sunken);
    overflow-wrap: anywhere;
    width: fit-content;
    max-width: 100%;
  }
</style>
