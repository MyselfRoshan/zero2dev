<script lang="ts">
  // The service type, as cards grouped like the Runtimes page (Languages, Databases, Other).
  // Each card is a radio button named after the type, with its one-line summary as description.
  import type { KindProfile } from "../lib/kinds";
  import type { ServiceKind } from "../lib/types";
  import KindIcon from "./KindIcon.svelte";

  type Props = {
    groups: { label: string; kinds: ServiceKind[] }[];
    profiles: KindProfile[];
    value: ServiceKind | "";
    onpick: (k: ServiceKind) => void;
    label: (k: string) => string;
  };
  let { groups, profiles, value, onpick, label }: Props = $props();
  const summary = (k: string) => profiles.find((p) => p.kind === k)?.summary ?? "";
</script>

<fieldset class="types">
  <legend class="sr-only">Type</legend>
  {#each groups as g (g.label)}
    <div class="group">
      <h3 class="glabel">{g.label}</h3>
      <div class="grid">
        {#each g.kinds as k (k)}
          <label class="card-opt" class:on={value === k}>
            <input type="radio" name="svc-type" value={k} checked={value === k} onchange={() => onpick(k)}
              aria-labelledby="t-{k}" aria-describedby="t-{k}-d" />
            <KindIcon kind={k} size={22} />
            <span class="txt">
              <span class="nm" id="t-{k}">{label(k)}</span>
              <span class="sm" id="t-{k}-d">{summary(k)}</span>
            </span>
          </label>
        {/each}
      </div>
    </div>
  {/each}
</fieldset>

<style>
  .types {
    border: 0;
    margin: 0;
    padding: 0;
    display: grid;
    gap: 12px;
    min-width: 0;
  }
  .glabel {
    font-size: 12px;
    font-weight: 600;
    color: var(--muted);
    margin: 0 0 6px;
  }
  .grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(190px, 1fr));
    gap: 8px;
  }
  .card-opt {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 10px 12px;
    border: 1px solid var(--line);
    border-radius: var(--radius);
    background: var(--surface);
    cursor: pointer;
    min-width: 0;
    position: relative;
  }
  .card-opt:hover {
    border-color: var(--line-strong);
  }
  .card-opt.on {
    border-color: var(--accent);
    background: var(--accent-soft);
    box-shadow: 0 0 0 1px var(--accent) inset;
  }
  .card-opt:has(input:focus-visible) {
    outline: 2px solid var(--accent);
    outline-offset: 2px;
  }
  .card-opt input {
    position: absolute;
    inset: 0;
    opacity: 0;
    margin: 0;
    cursor: pointer;
  }
  .txt {
    display: grid;
    gap: 1px;
    min-width: 0;
  }
  .nm {
    font-weight: 600;
    font-size: 14px;
  }
  .sm {
    font-size: 12px;
    color: var(--muted);
    line-height: 1.3;
  }
</style>
