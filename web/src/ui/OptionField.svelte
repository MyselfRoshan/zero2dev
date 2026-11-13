<script lang="ts">
  // One setting of a service type, drawn from its profile (GET /api/kinds): its label, the
  // control, one line of help ending in what it writes (--max-connections, php.ini memory_limit),
  // and, once changed, an "Edited" mark with a Reset link back to the default.
  import { isChanged, splitList, writesLabel, type KindOption } from "../lib/kinds";

  type Props = {
    option: KindOption;
    value: string;
    error?: string;
    /** Set when the selected runtime is too old for this option. */
    disabledNote?: string;
    /** Suggested values (npm scripts from package.json). */
    suggestions?: string[];
    onchange?: () => void;
    onreset?: () => void;
  };
  let { option: o, value = $bindable(), error = "", disabledNote = "", suggestions = [], onchange, onreset }: Props = $props();

  const id = $derived(`opt-${o.key}`);
  const writes = $derived(writesLabel(o));
  const disabled = $derived(!!disabledNote);
  const changed = $derived(isChanged(o, value));

  function set(v: string) {
    value = v;
    onchange?.();
  }
  function toggleMulti(v: string, on: boolean) {
    const cur = splitList(value);
    set((on ? [...cur.filter((x) => x !== v), v] : cur.filter((x) => x !== v)).join(","));
  }
  function reset() {
    if (onreset) onreset();
    else set(o.default);
  }
</script>

{#snippet meta()}
  {#if changed && !disabled}
    <span class="edited" title="Changed from the default ({o.default === '' ? 'empty' : o.default})"><span class="dot" aria-hidden="true"></span>Edited</span>
    <button type="button" class="linkish reset" onclick={reset} aria-label="Reset {o.label} to its default">Reset</button>
  {/if}
{/snippet}

{#snippet help()}
  <span class="hint" id="{id}-h">
    {#if disabledNote}<strong>{disabledNote}.</strong>{" "}{/if}{o.help}
    {#if writes}{" "}<code class="chip-code">{writes}</code>{/if}
  </span>
{/snippet}

<div class="opt" class:changed data-option={o.key}>
  {#if o.type === "bool"}
    <div class="switch-row">
      <div class="grow">
        <label class="name" for={id}>{o.label}</label>
        {@render help()}
      </div>
      {@render meta()}
      <input {id} type="checkbox" role="switch" class="switch" checked={value === "true"} {disabled} aria-describedby="{id}-h"
        onchange={(e) => set(e.currentTarget.checked ? "true" : "false")} />
    </div>
  {:else if o.type === "multi" && o.choices}
    <fieldset class="multi" aria-describedby="{id}-h">
      <legend class="head"><span class="name">{o.label}</span>{@render meta()}</legend>
      <div class="choices">
        {#each o.choices as c (c.value)}
          <label class="check"><input type="checkbox" checked={splitList(value).includes(c.value)} {disabled}
            onchange={(e) => toggleMulti(c.value, e.currentTarget.checked)} /> <span>{c.label}</span></label>
        {/each}
      </div>
      {@render help()}
    </fieldset>
  {:else}
    <div class="head">
      <label for={id} class="name">{o.label}{#if !o.required && o.type !== "select" && o.default === ""}{" "}<span class="muted opt-tag">(optional)</span>{/if}</label>
      {@render meta()}
    </div>
    {#if o.type === "select"}
      <select {id} bind:value {disabled} aria-describedby="{id}-h" aria-invalid={!!error} onchange={() => onchange?.()}>
        {#each o.choices ?? [] as c (c.value)}<option value={c.value}>{c.label}</option>{/each}
      </select>
    {:else if o.type === "lines"}
      <textarea {id} class="mono" rows="3" bind:value {disabled} placeholder={o.placeholder ?? ""} spellcheck="false"
        aria-describedby="{id}-h" aria-invalid={!!error} oninput={() => onchange?.()}></textarea>
    {:else}
      <input {id} type="text" class="mono" bind:value {disabled} placeholder={o.default || o.placeholder || ""}
        inputmode={o.type === "number" ? "numeric" : undefined} spellcheck="false" list={suggestions.length ? `${id}-list` : undefined}
        aria-describedby="{id}-h" aria-invalid={!!error} oninput={() => onchange?.()} />
      {#if suggestions.length}<datalist id="{id}-list">{#each suggestions as s (s)}<option value={s}></option>{/each}</datalist>{/if}
    {/if}
    {#if error}<span class="err">{error}</span>{/if}
    {@render help()}
  {/if}
</div>

<style>
  .opt {
    display: grid;
    gap: 5px;
    min-width: 0;
  }
  .head {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 0;
  }
  .name {
    font-weight: 600;
    font-size: 14px;
  }
  .head .name {
    flex: 1;
  }
  .opt-tag {
    font-weight: 400;
  }
  .switch-row {
    display: flex;
    align-items: flex-start;
    gap: 12px;
  }
  .switch-row .grow {
    display: grid;
    gap: 3px;
  }
  .switch-row .switch {
    margin-top: 1px;
  }
  .edited {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    font-size: 11.5px;
    color: var(--accent-text);
    white-space: nowrap;
  }
  .edited .dot {
    width: 6px;
    height: 6px;
    border-radius: 50%;
    background: var(--accent);
  }
  .reset {
    font-size: 12px;
  }
  .multi {
    border: 0;
    margin: 0;
    padding: 0;
    display: grid;
    gap: 5px;
    min-width: 0;
  }
  .multi legend {
    padding: 0;
    margin-bottom: 2px;
    width: 100%;
  }
  .choices {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(200px, 1fr));
    gap: 0 14px;
  }
  .choices .check {
    min-height: 28px;
  }
  textarea {
    width: 100%;
    resize: vertical;
  }
</style>
