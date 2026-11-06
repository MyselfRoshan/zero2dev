<script lang="ts">
  // One setting of a service type, drawn from its profile (GET /api/kinds): the control,
  // what it writes on the command line or in php.ini, and a plain explanation.
  import { splitList, writesLabel, type KindOption } from "../lib/kinds";

  type Props = {
    option: KindOption;
    value: string;
    error?: string;
    /** Set when the selected runtime is too old for this option. */
    disabledNote?: string;
    /** Suggested values (npm scripts from package.json). */
    suggestions?: string[];
    onchange?: () => void;
  };
  let { option: o, value = $bindable(), error = "", disabledNote = "", suggestions = [], onchange }: Props = $props();

  const id = $derived(`opt-${o.key}`);
  const writes = $derived(writesLabel(o) === o.label ? "" : writesLabel(o));
  const disabled = $derived(!!disabledNote);

  function toggleMulti(v: string, on: boolean) {
    const cur = splitList(value);
    value = (on ? [...cur.filter((x) => x !== v), v] : cur.filter((x) => x !== v)).join(",");
    onchange?.();
  }
</script>

<div class="field opt" data-option={o.key}>
  {#if o.type === "bool"}
    <label class="check">
      <input type="checkbox" checked={value === "true"} {disabled} aria-describedby="{id}-h"
        onchange={(e) => { value = e.currentTarget.checked ? "true" : "false"; onchange?.(); }} />
      <span>{o.label}</span>
      {#if writes}<code class="writes">{writes}</code>{/if}
    </label>
  {:else if o.type === "multi" && o.choices}
    <fieldset class="multi" aria-describedby="{id}-h">
      <legend class="label">{o.label} {#if writes}<code class="writes">{writes}</code>{/if}</legend>
      {#each o.choices as c (c.value)}
        <label class="check"><input type="checkbox" checked={splitList(value).includes(c.value)} {disabled}
          onchange={(e) => toggleMulti(c.value, e.currentTarget.checked)} /> <span>{c.label}</span></label>
      {/each}
    </fieldset>
  {:else}
    <label for={id} class="label">{o.label}{#if !o.required && o.type !== "select" && o.default === ""} <span class="muted opt-tag">(optional)</span>{/if}
      {#if writes}<code class="writes">{writes}</code>{/if}</label>
    {#if o.type === "select"}
      <select {id} bind:value {disabled} aria-describedby="{id}-h" aria-invalid={!!error} onchange={() => onchange?.()}>
        {#each o.choices ?? [] as c (c.value)}<option value={c.value}>{c.label}</option>{/each}
      </select>
    {:else if o.type === "lines"}
      <textarea {id} class="mono" rows="3" bind:value {disabled} placeholder={o.placeholder ?? ""} spellcheck="false"
        aria-describedby="{id}-h" aria-invalid={!!error} oninput={() => onchange?.()}></textarea>
    {:else}
      <input {id} type="text" class="mono" bind:value {disabled} placeholder={o.placeholder ?? o.default}
        inputmode={o.type === "number" ? "numeric" : undefined} spellcheck="false" list={suggestions.length ? `${id}-list` : undefined}
        aria-describedby="{id}-h" aria-invalid={!!error} oninput={() => onchange?.()} />
      {#if suggestions.length}<datalist id="{id}-list">{#each suggestions as s (s)}<option value={s}></option>{/each}</datalist>{/if}
    {/if}
  {/if}
  {#if error}<span class="err">{error}</span>{/if}
  <span class="hint" id="{id}-h">{#if disabledNote}<strong>{disabledNote}.</strong> {/if}{o.help}{#if o.default !== "" && (o.type === "text" || o.type === "number" || o.type === "size")}{" "}Default: <code>{o.default}</code>.{/if}</span>
</div>

<style>
  /* .field > label is a grid in app.css; these labels keep their parts on one line */
  .opt > label.label {
    display: block;
  }
  .opt > label.check {
    display: flex;
  }
  .writes {
    font-size: 11px;
    font-weight: 400;
    color: var(--muted, #6b7280);
    margin-left: 6px;
  }
  .opt-tag {
    font-weight: 400;
  }
  .multi {
    border: 0;
    margin: 0;
    padding: 0;
    display: grid;
    gap: 4px;
  }
  .multi legend {
    padding: 0;
    margin-bottom: 4px;
  }
  textarea {
    width: 100%;
    resize: vertical;
  }
</style>
