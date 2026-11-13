<script lang="ts">
  import { tick } from "svelte";
  import { fmtTime } from "../lib/format";
  import { toast, type LogEntry } from "../lib/store.svelte";
  import Icon from "./Icon.svelte";

  type Props = {
    lines: LogEntry[];
    names?: Record<string, string>;
    onclear: () => void;
    emptyText?: string;
    height?: string;
  };
  let { lines, names, onclear, emptyText = "No output yet.", height = "100%" }: Props = $props();

  let filter = $state("");
  let paused = $state(false);
  let frozen = $state.raw<LogEntry[]>([]);
  let stick = $state(true);
  let box: HTMLDivElement;

  const source = $derived(paused ? frozen : lines);
  const shown = $derived.by(() => {
    const f = filter.trim().toLowerCase();
    return f ? source.filter((l) => l.text.toLowerCase().includes(f)) : source;
  });
  const hidden = $derived(paused ? Math.max(0, lines.length - frozen.length) : 0);

  function togglePause() {
    if (!paused) frozen = lines;
    paused = !paused;
    if (!paused) stick = true;
  }

  $effect(() => {
    void shown;
    if (stick) {
      tick().then(() => {
        if (box) box.scrollTop = box.scrollHeight;
      });
    }
  });

  function onscroll() {
    if (!box) return;
    stick = box.scrollHeight - box.scrollTop - box.clientHeight < 24;
  }
  function jump() {
    if (!box) return;
    stick = true;
    box.scrollTop = box.scrollHeight;
  }
  function clear() {
    frozen = [];
    onclear();
  }
  async function copy() {
    const text = shown.map((l) => `${l.ts} ${l.stream} ${names?.[l.serviceId] ? names[l.serviceId] + " " : ""}${l.text}`).join("\n");
    try {
      await navigator.clipboard.writeText(text);
      toast("success", `Copied ${shown.length} lines.`);
    } catch {
      const ta = document.createElement("textarea");
      ta.value = text;
      document.body.appendChild(ta);
      ta.select();
      const ok = document.execCommand("copy");
      ta.remove();
      toast(ok ? "success" : "error", ok ? `Copied ${shown.length} lines.` : "Could not copy. Select the text and copy it manually.");
    }
  }
</script>

<div class="viewer" style:height>
  <div class="bar">
    <label class="find">
      <Icon name="search" size={14} />
      <input type="search" placeholder="Filter lines" aria-label="Filter log lines" bind:value={filter} />
    </label>
    <span class="count mono">{shown.length}{filter ? ` / ${source.length}` : ""} lines</span>
    <span class="spacer"></span>
    <button class="btn sm" aria-pressed={paused} onclick={togglePause}>
      <Icon name={paused ? "play" : "pause"} size={14} />{paused ? `Resume${hidden ? ` (${hidden} new)` : ""}` : "Pause"}
    </button>
    <button class="btn sm" onclick={copy} disabled={!shown.length}><Icon name="copy" size={14} /> Copy</button>
    <button class="btn sm" onclick={clear} disabled={!lines.length && !frozen.length}><Icon name="broom" size={14} /> Clear</button>
  </div>
  <div class="scroller">
    <!-- svelte-ignore a11y_no_noninteractive_tabindex -->
    <div class="lines mono" bind:this={box} {onscroll} role="log" aria-label="Log output" tabindex="0">
      {#each shown as l (l.seq)}
        <div class="ln {l.stream}">
          <span class="ts">{fmtTime(l.ts)}</span>
          {#if names}<span class="svc">{names[l.serviceId] ?? l.serviceId}</span>{/if}
          <span class="tx">{l.text}</span>
        </div>
      {:else}
        <div class="none">{filter ? "No lines match the filter." : emptyText}</div>
      {/each}
    </div>
    {#if !stick}
      <button class="btn sm jump" onclick={jump}><Icon name="arrowDown" size={14} /> Jump to latest</button>
    {/if}
  </div>
</div>

<style>
  .viewer {
    display: flex;
    flex-direction: column;
    min-height: 220px;
    border: 1px solid var(--line-strong);
    border-radius: 8px;
    overflow: hidden;
    background: var(--log-bg);
  }
  .bar {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 8px 10px;
    background: var(--surface);
    border-bottom: 1px solid var(--line);
    flex-wrap: wrap;
  }
  .find {
    display: flex;
    align-items: center;
    gap: 6px;
    width: 220px;
    padding-left: 8px;
    border: 1px solid var(--line-strong);
    border-radius: var(--radius);
    background: var(--surface);
    color: var(--muted);
  }
  .find input {
    border: 0;
    height: 28px;
    background: transparent;
    padding: 0 6px 0 0;
  }
  .find input:focus-visible {
    outline: none;
  }
  .find:focus-within {
    outline: 2px solid var(--accent);
  }
  .count {
    color: var(--muted);
    font-size: 12px;
  }
  .scroller {
    position: relative;
    flex: 1;
    min-height: 0;
  }
  .lines {
    position: absolute;
    inset: 0;
    overflow: auto;
    padding: 8px 0;
    color: var(--log-text);
    font-size: 12px;
    line-height: 1.55;
  }
  .ln {
    display: flex;
    gap: 12px;
    padding: 0 14px;
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }
  .ln:hover {
    background: rgb(255 255 255 / 0.04);
  }
  .ts {
    flex: none;
    color: var(--log-dim);
  }
  .svc {
    flex: none;
    min-width: 7ch;
    max-width: 16ch;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    color: var(--log-sys);
  }
  .ln.stderr .tx {
    color: var(--log-err);
  }
  .ln.stderr {
    background: rgb(255 120 105 / 0.07);
    box-shadow: inset 2px 0 0 var(--log-err);
  }
  .ln.system .tx {
    color: var(--log-sys);
    font-style: italic;
  }
  .none {
    padding: 14px;
    color: var(--log-dim);
    font-family: var(--sans);
  }
  .jump {
    position: absolute;
    right: 18px;
    bottom: 14px;
    box-shadow: var(--shadow);
  }
</style>
