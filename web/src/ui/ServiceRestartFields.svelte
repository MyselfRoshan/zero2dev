<script lang="ts">
  // "Restart on crash" fields of the service form: a switch and the number of tries.
  // WORKSTREAM C owns this file; the props below are the contract with ServiceForm.svelte.
  type Props = {
    /** Start the service again when it exits unexpectedly (bindable). */
    restartOnCrash: boolean;
    /** Most restarts in a row, 1-20 (bindable). */
    restartLimit: number;
    /** A message about restartLimit, for example "Use a number from 1 to 20." */
    error?: string;
  };
  let { restartOnCrash = $bindable(), restartLimit = $bindable(), error = "" }: Props = $props();
</script>

<div class="field" data-testid="restart-fields">
  <div class="switch-line">
    <label for="restart-on-crash" class="grow">
      <span class="label">Restart if it crashes</span>
      <span class="hint">When it stops by itself, Zero2Dev starts it again after 1 s, then 2 s, 4 s and so on (never more than 60 s apart). Stopping it yourself never restarts it.</span>
    </label>
    <input id="restart-on-crash" type="checkbox" role="switch" class="switch" bind:checked={restartOnCrash} data-testid="restart-on-crash" />
  </div>
  {#if restartOnCrash}
    <div class="field limit">
      <label>
        <span class="label">Give up after (attempts)</span>
        <input type="number" min="1" max="20" step="1" bind:value={restartLimit} aria-invalid={!!error} data-testid="restart-limit" />
      </label>
      {#if error}<span class="err">{error}</span>{:else}<span class="hint">From 1 to 20. If it crashes this many times in a row, it stays stopped and you get a notice. The count starts again after it has run for a minute.</span>{/if}
    </div>
  {/if}
</div>

<style>
  .switch-line {
    display: flex;
    align-items: flex-start;
    gap: 12px;
  }
  .switch-line label {
    display: grid;
    gap: 3px;
    cursor: pointer;
  }
  .limit {
    margin-top: 8px;
  }
  .limit input {
    width: 120px;
  }
</style>
