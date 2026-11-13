<script lang="ts">
  // Top bar: the theme button opens a popover with the theme (System / Light / Dark) and the
  // accent colour (a preset or any colour). Changes apply and save at once, with Undo.
  // See DESIGN.md (Theming).
  import { app, appearance, currentAppearance, setAppearance } from "../lib/store.svelte";
  import { ACCENT_PRESETS, contrast, isHex, type ThemeMode } from "../lib/theme";
  import Icon from "./Icon.svelte";

  const MODES: { id: ThemeMode; label: string; icon: string }[] = [
    { id: "system", label: "System", icon: "monitor" },
    { id: "light", label: "Light", icon: "sun" },
    { id: "dark", label: "Dark", icon: "moon" },
  ];

  const now = $derived(currentAppearance());
  const mode = $derived(MODES.find((m) => m.id === now.theme) ?? MODES[0]);
  const preset = $derived(ACCENT_PRESETS.find((p) => p.color === now.accent));

  let open = $state(false);
  let root = $state<HTMLElement | null>(null);
  let trigger = $state<HTMLButtonElement | null>(null);
  let pop = $state<HTMLElement | null>(null);

  function close(refocus = true) {
    open = false;
    if (refocus) trigger?.focus();
  }

  // Outside click closes; focus goes to the selected theme when it opens.
  $effect(() => {
    if (!open) return;
    queueMicrotask(() => pop?.querySelector<HTMLElement>("input[name=theme-mode]:checked")?.focus());
    const onDown = (e: PointerEvent) => {
      if (root && !root.contains(e.target as Node)) close(false);
    };
    document.addEventListener("pointerdown", onDown);
    return () => document.removeEventListener("pointerdown", onDown);
  });

  // The hex field follows the saved accent until the user types in it.
  let hex = $state("");
  let editing = $state(false);
  let hexError = $state("");
  $effect(() => {
    if (!editing) hex = now.accent;
  });

  // Undo: the values before the last change.
  let undo = $state<{ theme: ThemeMode; accent: string } | null>(null);
  let saved = $state("");
  let savedTimer: ReturnType<typeof setTimeout> | undefined;

  async function change(c: { theme?: ThemeMode; accent?: string }, what: string, isUndo = false) {
    const before = currentAppearance();
    if ((c.theme ?? before.theme) === before.theme && (c.accent ?? before.accent) === before.accent) return;
    const ok = await setAppearance(c);
    if (!ok) return;
    undo = isUndo ? null : before;
    saved = isUndo ? "Change undone." : `${what} saved.`;
    clearTimeout(savedTimer);
    savedTimer = setTimeout(() => ((saved = ""), (undo = null)), 8000);
  }
  const pickTheme = (t: ThemeMode) => change({ theme: t }, `Theme ${MODES.find((m) => m.id === t)?.label}`);
  const pickAccent = (color: string) => {
    hexError = "";
    const label = ACCENT_PRESETS.find((p) => p.color === color)?.label ?? color;
    return change({ accent: color.toLowerCase() }, `Accent ${label}`);
  };
  function doUndo() {
    if (undo) void change(undo, "", true);
  }

  function commitHex() {
    editing = false;
    let v = hex.trim();
    if (/^[0-9a-f]{6}$/i.test(v)) v = "#" + v;
    if (v.toLowerCase() === now.accent) return;
    if (!isHex(v)) {
      hexError = "Use a colour like #1677ff (# and six hex digits).";
      return;
    }
    hex = v.toLowerCase();
    void pickAccent(hex);
  }

  // Swatches are one radio group: Tab enters at the selected one, arrows move and select.
  const focusIdx = $derived(Math.max(0, ACCENT_PRESETS.findIndex((p) => p.color === now.accent)));
  function onSwatchKey(e: KeyboardEvent) {
    const keys: Record<string, number> = { ArrowRight: 1, ArrowDown: 1, ArrowLeft: -1, ArrowUp: -1 };
    let i: number;
    if (e.key in keys) i = (focusIdx + keys[e.key] + ACCENT_PRESETS.length) % ACCENT_PRESETS.length;
    else if (e.key === "Home") i = 0;
    else if (e.key === "End") i = ACCENT_PRESETS.length - 1;
    else return;
    e.preventDefault();
    void pickAccent(ACCENT_PRESETS[i].color);
    (e.currentTarget as HTMLElement).querySelectorAll<HTMLElement>("[role=radio]")[i]?.focus();
  }
  // The tick on a swatch: white or dark, whichever reads on that colour.
  const tick = (c: string) => (contrast(c, "#ffffff") >= 3 ? "#ffffff" : "#1f1f1f");

  function onPopKey(e: KeyboardEvent) {
    if (e.key === "Escape" && !(e.target as HTMLElement).matches(".hex")) {
      e.preventDefault();
      close();
    }
  }
</script>

<div class="theme-menu" bind:this={root}>
  <button
    type="button"
    class="btn sm ghost icon"
    class:on={open}
    bind:this={trigger}
    onclick={() => (open ? close() : (open = true))}
    disabled={!app.settings}
    aria-haspopup="dialog"
    aria-expanded={open}
    aria-controls="theme-pop"
    aria-label="Theme and accent colour (theme: {mode.label})"
    title="Theme and accent colour"
    data-testid="theme-toggle"
  >
    <Icon name={mode.icon} size={15} />
  </button>

  {#if open}
    <!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
    <div id="theme-pop" class="pop" role="dialog" aria-label="Theme and accent colour" tabindex="-1" bind:this={pop} onkeydown={onPopKey} data-testid="theme-popover">
      <div class="sec">
        <span class="label" id="theme-l">Theme</span>
        <div class="seg full" role="radiogroup" aria-labelledby="theme-l">
          {#each MODES as m (m.id)}
            <label>
              <input type="radio" name="theme-mode" value={m.id} checked={now.theme === m.id} onchange={() => pickTheme(m.id)} />
              <Icon name={m.icon} size={14} />
              {m.label}
            </label>
          {/each}
        </div>
        {#if now.theme === "system"}
          <span class="hint">Follows Windows: dark is {appearance.prefersDark ? "on" : "off"} right now.</span>
        {/if}
      </div>

      <div class="sec">
        <span class="label" id="accent-l">Accent colour <span class="which">{preset ? preset.label : "Custom"}</span></span>
        <div class="swatches" role="radiogroup" aria-labelledby="accent-l" tabindex="-1" onkeydown={onSwatchKey}>
          {#each ACCENT_PRESETS as p, i (p.id)}
            <button
              type="button"
              role="radio"
              class="swatch"
              aria-checked={now.accent === p.color}
              aria-label={p.label}
              title={p.label}
              tabindex={focusIdx === i ? 0 : -1}
              data-accent={p.id}
              style:--sw={p.color}
              style:color={tick(p.color)}
              onclick={() => pickAccent(p.color)}
            >
              {#if now.accent === p.color}<Icon name="check" size={14} />{/if}
            </button>
          {/each}
        </div>

        <div class="custom">
          <label class="color-pick" title="Pick any colour">
            <input type="color" value={now.accent} onchange={(e) => pickAccent((e.currentTarget as HTMLInputElement).value)} aria-label="Pick a custom accent colour" />
          </label>
          <input
            type="text"
            class="mono hex"
            bind:value={hex}
            onfocus={() => (editing = true)}
            oninput={() => (hexError = "")}
            onblur={commitHex}
            onkeydown={(e) => {
              if (e.key === "Enter") {
                e.preventDefault();
                commitHex();
              } else if (e.key === "Escape") {
                e.preventDefault();
                editing = false;
                hexError = "";
                hex = now.accent;
              }
            }}
            aria-label="Custom accent colour (hex)"
            aria-invalid={!!hexError}
            aria-describedby="hex-help"
            spellcheck="false"
            autocomplete="off"
            maxlength="7"
          />
        </div>
        {#if hexError}
          <span class="err" id="hex-help">{hexError}</span>
        {:else}
          <span class="hint" id="hex-help">Any colour works: buttons and links get a shade that stays readable.</span>
        {/if}
      </div>

      <div class="saved" role="status">
        {#if saved}
          <Icon name="check" size={14} />
          <span>{saved}</span>
          {#if undo}<button type="button" class="linkish small" onclick={doUndo}>Undo</button>{/if}
        {:else}
          <span class="muted small">Saved for every browser on this PC.</span>
        {/if}
      </div>
    </div>
  {/if}
</div>

<style>
  .theme-menu {
    position: relative;
  }
  .on {
    background: var(--fill);
  }
  .pop {
    position: absolute;
    top: calc(100% + 8px);
    right: 0;
    z-index: 60;
    width: min(296px, calc(100vw - 24px));
    display: grid;
    gap: 16px;
    padding: 16px;
    background: var(--elevated);
    color: var(--text);
    border: 1px solid var(--line);
    border-radius: var(--radius-lg, 8px);
    box-shadow: var(--shadow);
    animation: pop-in var(--motion-mid, 0.2s) var(--ease-out, ease-out);
    transform-origin: top right;
  }
  .pop:focus {
    outline: none;
  }
  @keyframes pop-in {
    from {
      opacity: 0;
      transform: scale(0.96) translateY(-4px);
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .pop {
      animation: none;
    }
  }
  .sec {
    display: grid;
    gap: 8px;
  }
  .label {
    font-weight: 600;
    display: flex;
    align-items: baseline;
    gap: 8px;
  }
  .which {
    font-weight: 400;
    color: var(--muted);
    font-size: 12px;
  }
  .seg.full {
    display: grid;
    grid-template-columns: repeat(3, 1fr);
  }
  .seg.full :global(label) {
    justify-content: center;
  }
  .swatches {
    display: grid;
    grid-template-columns: repeat(8, 1fr);
    gap: 6px;
    outline: none;
  }
  .swatch {
    display: grid;
    place-items: center;
    aspect-ratio: 1;
    width: 100%;
    padding: 0;
    border: 0;
    border-radius: 50%;
    background: var(--sw);
    cursor: pointer;
    box-shadow: inset 0 0 0 1px rgba(0, 0, 0, 0.08);
    transition: transform var(--motion-fast, 0.1s) var(--ease-in-out, ease);
  }
  .swatch:hover {
    transform: scale(1.08);
  }
  .swatch[aria-checked="true"] {
    box-shadow:
      0 0 0 2px var(--elevated),
      0 0 0 4px var(--sw);
  }
  .custom {
    display: flex;
    align-items: center;
    gap: 8px;
  }
  .color-pick {
    display: inline-flex;
    flex: none;
    width: 32px;
    height: 32px;
    border: 1px solid var(--line-strong);
    border-radius: var(--radius, 6px);
    overflow: hidden;
    cursor: pointer;
  }
  .color-pick input {
    width: 44px;
    height: 44px;
    margin: -6px;
    border: 0;
    padding: 0;
    cursor: pointer;
  }
  .hex {
    flex: 1;
  }
  .saved {
    display: flex;
    align-items: center;
    gap: 6px;
    min-height: 22px;
    padding-top: 12px;
    border-top: 1px solid var(--line);
    color: var(--ok);
  }
</style>
