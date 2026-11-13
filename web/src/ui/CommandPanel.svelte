<script lang="ts">
  // The "What will run" side panel of the service editor: the exact command line from the
  // daemon, what Zero2Dev adds and why, what was changed from the defaults, and Save.
  import { commandText, friendlyArgs, panelNotes } from "../lib/kinds";
  import { serviceKindLabel } from "../lib/ux";
  import type { CommandPreview } from "../lib/types";
  import Icon from "./Icon.svelte";

  type Props = {
    preview: CommandPreview | null;
    error: string;
    fixed: { args: string[]; help: string }[];
    fixedEnv: { name: string; value: string; help: string }[];
    changed: string[];
    onresetall?: () => void;
    custom: boolean;
    formId: string;
    saveLabel: string;
    saving: boolean;
    oncancel: () => void;
  };
  let { preview, error, fixed, fixedEnv, changed, onresetall, custom, formId, saveLabel, saving, oncancel }: Props = $props();
  let copied = $state(false);

  const text = $derived(preview ? commandText(preview.program || "<program>", preview.args) : "");
  const notes = $derived(preview ? panelNotes(preview.warnings, serviceKindLabel) : []);
  async function copy() {
    try {
      await navigator.clipboard.writeText(text);
      copied = true;
      setTimeout(() => (copied = false), 1500);
    } catch {
      /* clipboard not available: the text is still selectable */
    }
  }
</script>

<aside class="panel" aria-labelledby="run-h" data-tour="cmd-panel">
  <div class="top">
    <h2 id="run-h">What will run</h2>
    {#if preview}<button type="button" class="btn sm ghost" onclick={copy} aria-label="Copy the command line"><Icon name="copy" size={13} /> {copied ? "Copied" : "Copy"}</button>{/if}
  </div>
  <div class="body" aria-live="polite">
    {#if preview}
      <code class="cmd" data-testid="preview-command">{text}</code>
      {#if preview.env.length}
        <div class="sub">
          <span class="k">Environment</span>
          <ul class="envs">{#each preview.env as e (e)}<li class="mono">{e}</li>{/each}</ul>
        </div>
      {/if}
      {#if preview.files.length}<p class="small muted">Writes <span class="mono">{preview.files.join(", ")}</span> before each start.</p>{/if}
      {#each notes as n (n.text)}<div class="note small {n.level}" role="note"><Icon name={n.level === "warn" ? "alert" : "info"} size={13} /> <span>{n.text}</span></div>{/each}
    {:else if error}
      <span class="err">{error}</span>
    {:else}
      <span class="hint">Working it out...</span>
    {/if}

    {#if !custom}
      <div class="changed" data-testid="changed-summary">
        {#if changed.length}
          <span><b>{changed.length}</b> setting{changed.length === 1 ? "" : "s"} changed from the defaults</span>
          {#if onresetall}<button type="button" class="linkish" onclick={onresetall}>Reset all</button>{/if}
        {:else}
          <span class="muted">All settings are at their defaults.</span>
        {/if}
      </div>
    {/if}

    {#if !custom && (fixed.length || fixedEnv.length)}
      <details class="always" data-testid="always-added">
        <summary>Always added ({fixed.length + fixedEnv.length})</summary>
        <ul>
          {#each fixed as f (f.args.join(" "))}<li><code>{friendlyArgs(f.args)}</code><span>{f.help}</span></li>{/each}
          {#each fixedEnv as v (v.name)}<li><code>{v.name}={friendlyArgs([v.value])}</code><span>{v.help}</span></li>{/each}
        </ul>
      </details>
    {/if}
  </div>
  <div class="actions">
    <button class="btn" type="button" onclick={oncancel}>Cancel</button>
    <button class="btn primary" type="submit" form={formId} disabled={saving} data-tour="create-service">{saveLabel}</button>
  </div>
</aside>

<style>
  .panel {
    background: var(--surface);
    border: 1px solid var(--line);
    border-radius: 8px;
    display: flex;
    flex-direction: column;
    max-height: calc(100vh - 52px - 48px);
  }
  .top {
    display: flex;
    align-items: center;
    padding: 12px 16px 0;
  }
  .top h2 {
    font-size: 14px;
    margin: 0;
    flex: 1;
  }
  .body {
    padding: 10px 16px 14px;
    display: grid;
    gap: 10px;
    overflow: auto;
    min-height: 0;
  }
  .cmd {
    display: block;
    padding: 10px 12px;
    background: var(--log-bg);
    color: var(--log-text);
    border: 1px solid var(--line-strong);
    border-radius: 6px;
    font-size: 12px;
    line-height: 1.5;
    white-space: pre-wrap;
    word-break: break-all;
  }
  .sub {
    display: grid;
    gap: 3px;
  }
  .k {
    font-size: 12px;
    font-weight: 600;
    color: var(--muted);
  }
  .envs {
    margin: 0;
    padding: 0;
    list-style: none;
    font-size: 11.5px;
    display: grid;
    gap: 1px;
    word-break: break-all;
  }
  .note {
    display: flex;
    gap: 6px;
    align-items: flex-start;
  }
  .note :global(svg) {
    flex: none;
    margin-top: 2px;
  }
  .note.warn {
    color: var(--warn);
  }
  .note.todo {
    color: var(--muted);
  }
  .changed {
    display: flex;
    gap: 10px;
    align-items: baseline;
    flex-wrap: wrap;
    font-size: 12px;
    padding-top: 8px;
    border-top: 1px solid var(--line);
  }
  .always summary {
    cursor: pointer;
    font-size: 12px;
    font-weight: 600;
  }
  .always ul {
    margin: 8px 0 0;
    padding: 0;
    list-style: none;
    display: grid;
    gap: 8px;
  }
  .always li {
    display: grid;
    gap: 2px;
    font-size: 12px;
  }
  .always li span {
    color: var(--muted);
  }
  .always code {
    font-size: 11.5px;
    word-break: break-all;
  }
  .actions {
    display: flex;
    justify-content: flex-end;
    gap: 8px;
    padding: 12px 16px;
    border-top: 1px solid var(--line);
    background: var(--surface-2);
    border-radius: 0 0 8px 8px;
  }
</style>
