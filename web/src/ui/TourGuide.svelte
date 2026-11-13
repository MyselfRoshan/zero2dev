<script lang="ts">
  // The tutorial guide: a small panel that says what to do next, a ring around the thing to
  // press, and automatic progress when the step really happened. The page stays usable.
  import { onMount } from "svelte";
  import { lists } from "../lib/store.svelte";
  import { navigate } from "../lib/router.svelte";
  import { exitTour, finishTour, goToStep, tour } from "../lib/tour.svelte";
  import { onStepPage, tourById, type TourState } from "../lib/tours";
  import Icon from "./Icon.svelte";

  let hash = $state(location.hash);
  let tick = $state(0); // bumps when the page changes (DOM, scroll, size)
  let root = $state<HTMLDivElement | null>(null);

  const active = $derived(tour.active);
  const def = $derived(active ? tourById(active.id) : undefined);
  const step = $derived(def && active ? def.steps[active.step] : undefined);
  const last = $derived(!!def && !!active && active.step === def.steps.length - 1);
  const here = $derived(step ? onStepPage(step, hash) : true);

  function visible(el: Element): boolean {
    const r = el.getBoundingClientRect();
    return r.width > 0 && r.height > 0 && getComputedStyle(el).visibility !== "hidden";
  }
  function findTarget(sel: string | undefined): HTMLElement | null {
    if (!sel) return null;
    try {
      for (const el of document.querySelectorAll<HTMLElement>(sel)) if (!root?.contains(el) && visible(el)) return el;
    } catch {
      /* a bad selector shows no ring */
    }
    return null;
  }
  const now = $derived.by<TourState>(() => {
    void tick;
    return {
      route: hash,
      runtimes: lists.runtimes,
      services: lists.services,
      sites: lists.sites,
      has: (sel) => !!findTarget(sel),
    };
  });
  const target = $derived.by(() => {
    void tick;
    return here ? findTarget(step?.target) : null;
  });
  const rect = $derived.by(() => {
    void tick;
    return target?.getBoundingClientRect() ?? null;
  });
  const finished = $derived(!!step?.done && !!active && step.done(now, active.base));

  // Finished steps move on by themselves, after a short moment so the tick is seen.
  $effect(() => {
    if (!finished || !active) return;
    const at = active.step;
    const t = setTimeout(() => {
      if (tour.active?.step === at) goToStep(at + 1);
    }, 700);
    return () => clearTimeout(t);
  });

  // Bring the target into view once per step.
  let shownFor = "";
  $effect(() => {
    const key = active ? `${active.id}:${active.step}` : "";
    if (target && key !== shownFor) {
      shownFor = key;
      const r = target.getBoundingClientRect();
      if (r.top < 60 || r.bottom > innerHeight - 40) target.scrollIntoView({ block: "center", behavior: "smooth" });
    }
  });

  // A modal dialog makes the rest of the page inert: the guide then lives inside it.
  $effect(() => {
    void tick;
    if (!root) return;
    const dlg = [...document.querySelectorAll<HTMLDialogElement>("dialog[open]")].pop();
    const host = dlg ?? document.body;
    if (root.parentElement !== host) host.appendChild(root);
  });

  // The panel sits next to the highlighted thing: below it, else above, right or left,
  // whichever fits; with an arrow pointing at it. Without a target it waits bottom right.
  let pw = $state(360);
  let ph = $state(220);
  let vw = $state(innerWidth);
  let vh = $state(innerHeight);
  const GAP = 14;
  const M = 12; // margin to the window edge
  type Place = { top: number; left: number; side: "below" | "above" | "right" | "left"; arrow: number } | null;
  const place = $derived.by<Place>(() => {
    if (!rect || vw <= 560) return null; // phones: a bottom sheet
    const clampX = (x: number) => Math.min(Math.max(x, M), vw - pw - M);
    const clampY = (y: number) => Math.min(Math.max(y, M), vh - ph - M);
    const cx = rect.left + rect.width / 2;
    const cy = rect.top + rect.height / 2;
    if (rect.bottom + GAP + ph <= vh - M) {
      const left = clampX(cx - pw / 2);
      return { side: "below", top: rect.bottom + GAP, left, arrow: Math.min(Math.max(cx - left, 18), pw - 18) };
    }
    if (rect.top - GAP - ph >= M) {
      const left = clampX(cx - pw / 2);
      return { side: "above", top: rect.top - GAP - ph, left, arrow: Math.min(Math.max(cx - left, 18), pw - 18) };
    }
    if (rect.right + GAP + pw <= vw - M) {
      const top = clampY(cy - ph / 2);
      return { side: "right", top, left: rect.right + GAP, arrow: Math.min(Math.max(cy - top, 18), ph - 18) };
    }
    if (rect.left - GAP - pw >= M) {
      const top = clampY(cy - ph / 2);
      return { side: "left", top, left: rect.left - GAP - pw, arrow: Math.min(Math.max(cy - top, 18), ph - 18) };
    }
    return null; // the target fills the screen: keep the corner
  });

  let panel = $state<HTMLElement | null>(null);
  $effect(() => {
    if (active && panel) {
      void active.step;
      panel.focus({ preventScroll: true });
    }
  });

  onMount(() => {
    const onHash = () => (hash = location.hash);
    let raf = 0;
    const bump = () => {
      if (!raf) raf = requestAnimationFrame(() => ((raf = 0), tick++));
    };
    const mo = new MutationObserver(bump);
    mo.observe(document.body, { childList: true, subtree: true, attributes: true, attributeFilter: ["class", "open", "aria-expanded"] });
    addEventListener("hashchange", onHash);
    addEventListener("scroll", bump, true);
    const onResize = () => ((vw = innerWidth), (vh = innerHeight), bump());
    addEventListener("resize", onResize);
    const iv = setInterval(bump, 1000);
    return () => {
      mo.disconnect();
      removeEventListener("hashchange", onHash);
      removeEventListener("scroll", bump, true);
      removeEventListener("resize", onResize);
      clearInterval(iv);
      if (raf) cancelAnimationFrame(raf);
      root?.remove();
    };
  });

  function showMe() {
    if (!here && step?.route) navigate(step.route);
    else target?.scrollIntoView({ block: "center", behavior: "smooth" });
    tick++;
  }
  function onKey(e: KeyboardEvent) {
    if (e.key === "Escape") {
      e.stopPropagation();
      exitTour();
    }
  }
</script>

<div class="tour-root" bind:this={root}>
  {#if active && def && step}
    {#if rect}
      <!-- The rest of the page is dimmed around the target; clicks still go through. -->
      <div class="shade" style:top="{rect.top - 6}px" style:left="{rect.left - 6}px" style:width="{rect.width + 12}px" style:height="{rect.height + 12}px" aria-hidden="true"></div>
      <div class="ring" style:top="{rect.top - 6}px" style:left="{rect.left - 6}px" style:width="{rect.width + 12}px" style:height="{rect.height + 12}px" aria-hidden="true"></div>
    {/if}
    <!-- svelte-ignore a11y_no_noninteractive_element_interactions (Esc closes the guide) -->
    <div class="guide" class:placed={!!place} class:corner={!place} data-side={place?.side} role="dialog" aria-modal="false" aria-labelledby="tour-t" tabindex="-1"
      bind:this={panel} bind:offsetWidth={pw} bind:offsetHeight={ph} onkeydown={onKey} data-testid="tour-guide"
      style:top={place ? `${place.top}px` : undefined} style:left={place ? `${place.left}px` : undefined}
      style:--arrow={place ? `${place.arrow}px` : undefined}>
      <header>
        <Icon name="lightbulb" size={15} />
        <span class="name">{def.title}</span>
        <span class="count">Step {active.step + 1} of {def.steps.length}</span>
        <button type="button" class="x" onclick={exitTour} aria-label="Exit the tutorial"><Icon name="x" size={14} /></button>
      </header>
      <div class="bar" aria-hidden="true"><span style:width="{((active.step + (finished ? 1 : 0)) / def.steps.length) * 100}%"></span></div>
      <div class="body" aria-live="polite">
        <h2 id="tour-t">{#if finished}<Icon name="circleCheck" size={16} />{/if} {step.title}</h2>
        <p>{step.text}</p>
        {#if !here && step.route}
          <p class="note">This happens on another page.</p>
        {:else if step.target && !target}
          <p class="note">It is not on the screen right now{step.done ? "; it may already be done" : ""}. You can skip this step.</p>
        {:else if step.done && !finished && step.waiting}
          <p class="wait"><span class="spin" aria-hidden="true"></span> {step.waiting}...</p>
        {/if}
      </div>
      <footer>
        {#if active.step > 0}<button type="button" class="btn sm ghost" onclick={() => goToStep(active.step - 1, true)}>Back</button>{/if}
        <span class="spacer"></span>
        {#if !here && step.route}
          <button type="button" class="btn sm primary" onclick={showMe}>Go there <Icon name="arrowRight" size={14} /></button>
        {:else if step.target && target}
          <button type="button" class="btn sm" onclick={showMe}>Show me</button>
        {/if}
        {#if last}
          <button type="button" class="btn sm primary" onclick={finishTour}>Finish</button>
        {:else if step.done && !finished}
          <button type="button" class="btn sm ghost" onclick={() => goToStep(active.step + 1)}>Skip</button>
        {:else if here}
          <button type="button" class="btn sm primary" onclick={() => goToStep(active.step + 1, true)}>Next</button>
        {/if}
      </footer>
    </div>
  {/if}
</div>

<style>
  .tour-root {
    display: contents;
  }
  .shade {
    position: fixed;
    z-index: 2147482999;
    border-radius: var(--radius-lg);
    box-shadow: 0 0 0 200vmax rgba(0, 0, 0, 0.45); /* antd colorBgMask */
    pointer-events: none;
    transition:
      top var(--motion-mid) var(--ease-in-out),
      left var(--motion-mid) var(--ease-in-out),
      width var(--motion-mid) var(--ease-in-out),
      height var(--motion-mid) var(--ease-in-out);
  }
  .ring {
    position: fixed;
    z-index: 2147483000;
    border: 2px solid var(--accent);
    border-radius: var(--radius-lg);
    box-shadow:
      0 0 0 4px color-mix(in srgb, var(--accent) 25%, transparent),
      0 0 24px color-mix(in srgb, var(--accent) 45%, transparent);
    pointer-events: none;
    transition:
      top var(--motion-mid) var(--ease-in-out),
      left var(--motion-mid) var(--ease-in-out),
      width var(--motion-mid) var(--ease-in-out),
      height var(--motion-mid) var(--ease-in-out);
    animation: pulse 1.6s var(--ease-in-out)-in-out infinite;
  }
  @keyframes pulse {
    50% {
      box-shadow:
        0 0 0 8px color-mix(in srgb, var(--accent) 12%, transparent),
        0 0 28px color-mix(in srgb, var(--accent) 35%, transparent);
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .ring,
    .shade,
    .guide {
      animation: none;
      transition: none;
    }
  }
  .guide {
    position: fixed;
    z-index: 2147483001;
    bottom: 20px;
    width: min(360px, calc(100vw - 32px));
    background: var(--elevated);
    color: var(--text);
    border: 1px solid var(--line);
    border-radius: var(--radius-lg);
    box-shadow: var(--shadow);
    outline: none;
    font-size: 14px;
  }
  .guide.corner {
    right: 20px;
  }
  .guide.placed {
    bottom: auto;
    transition:
      top var(--motion-mid) var(--ease-in-out),
      left var(--motion-mid) var(--ease-in-out);
  }
  /* the arrow points at the highlighted thing */
  .guide.placed::before {
    content: "";
    position: absolute;
    width: 12px;
    height: 12px;
    background: var(--elevated);
    border: 1px solid var(--line);
    transform: rotate(45deg);
  }
  .guide[data-side="below"]::before {
    top: -7px;
    left: calc(var(--arrow) - 6px);
    border-right: 0;
    border-bottom: 0;
  }
  .guide[data-side="above"]::before {
    bottom: -7px;
    left: calc(var(--arrow) - 6px);
    border-left: 0;
    border-top: 0;
  }
  .guide[data-side="right"]::before {
    left: -7px;
    top: calc(var(--arrow) - 6px);
    border-top: 0;
    border-right: 0;
  }
  .guide[data-side="left"]::before {
    right: -7px;
    top: calc(var(--arrow) - 6px);
    border-bottom: 0;
    border-left: 0;
  }
  .guide:focus-visible {
    outline: 2px solid var(--accent);
  }
  header {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 10px 10px 8px 14px;
    color: var(--accent-text);
  }
  .name {
    font-weight: 600;
    font-size: 12px;
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .count {
    font-size: 12px;
    color: var(--muted);
  }
  .x {
    border: 0;
    background: none;
    color: var(--muted);
    cursor: pointer;
    padding: 4px;
    border-radius: 6px;
    display: inline-flex;
  }
  .x:hover {
    background: var(--sunken);
    color: var(--text);
  }
  .bar {
    height: 3px;
    background: var(--sunken);
    margin: 0 14px;
    border-radius: 2px;
    overflow: hidden;
  }
  .bar span {
    display: block;
    height: 100%;
    background: var(--accent);
    transition: width var(--motion-slow) var(--ease-in-out);
  }
  .body {
    padding: 10px 14px 4px;
  }
  .body h2 {
    display: flex;
    align-items: center;
    gap: 6px;
    font-size: 16px;
    margin: 0 0 6px;
  }
  .body h2 :global(svg) {
    color: var(--ok);
  }
  .body p {
    margin: 0 0 8px;
    line-height: 1.45;
  }
  .note {
    color: var(--muted);
    font-size: 12px;
  }
  .wait {
    display: flex;
    align-items: center;
    gap: 8px;
    color: var(--accent-text);
    font-size: 12px;
  }
  footer {
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 8px 12px 12px;
  }
  @media (max-width: 560px) {
    .guide,
    .guide.corner,
    .guide.placed {
      left: 16px;
      right: 16px;
      width: auto;
      bottom: 12px;
    }
  }
</style>
