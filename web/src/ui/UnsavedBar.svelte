<script lang="ts">
  import { beforeNavigate, router } from "../lib/router.svelte";

  // A bar pinned to the bottom of the page while a form has unsaved changes: Discard or Save.
  type Props = {
    formId: string;
    saving: boolean;
    ondiscard: () => void;
    label?: string;
    /** The route (name) of the page the form is on; leaving it asks first. */
    page?: string;
    /** What the confirmation says is lost. */
    what?: string;
  };
  let { formId, saving, ondiscard, label = "Save settings", page = "settings", what = "settings" }: Props = $props();

  // Leaving or reloading the page asks first.
  $effect(() => {
    const warn = (e: BeforeUnloadEvent) => e.preventDefault();
    window.addEventListener("beforeunload", warn);
    const unguard = beforeNavigate((to) =>
      router.route.name !== page || to.name === page || window.confirm(`Discard unsaved ${what} changes?`),
    );
    return () => {
      unguard();
      window.removeEventListener("beforeunload", warn);
    };
  });
</script>

<div class="bar" role="region" aria-label="Unsaved changes">
  <span class="dot" aria-hidden="true"></span>
  <span class="grow">Unsaved changes</span>
  <button class="btn" type="button" onclick={ondiscard} disabled={saving}>Discard</button>
  <button class="btn primary" type="submit" form={formId} disabled={saving}>{saving ? "Saving..." : label}</button>
</div>

<style>
  .bar {
    position: sticky;
    bottom: 0;
    z-index: 3;
    display: flex;
    align-items: center;
    gap: 12px;
    margin-top: 16px;
    padding: 12px 16px;
    border: 1px solid var(--line);
    border-radius: var(--radius-lg);
    background: var(--elevated);
    box-shadow: var(--shadow);
  }
  .dot {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: var(--warn-fill);
  }
  .grow {
    font-weight: 600;
    font-size: 14px;
    white-space: nowrap;
  }
  @media (max-width: 560px) {
    .bar {
      gap: 8px;
      padding: 10px 12px;
    }
    .dot {
      display: none;
    }
  }
</style>
