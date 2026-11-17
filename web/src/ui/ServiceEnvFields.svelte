<script lang="ts">
  // The .env file field of the service form. Any kind can have one: its variables are added to
  // the service's environment (below the user's own variables). The path is relative to the folder.
  import type { ServiceKind } from "../lib/types";

  type Props = {
    /** The .env file, relative to the folder, or "" (bindable). */
    envFile: string;
    /** The service's folder, to show where the file is looked for. */
    cwd: string;
    kind: ServiceKind | "";
    /** A message about envFile, for example "Use a path inside the folder." */
    error?: string;
  };
  let { envFile = $bindable(), cwd, kind, error = "" }: Props = $props();
</script>

<div class="field" data-testid="env-file-field">
  <label>
    <span class="label">.env file <span class="muted">(optional)</span></span>
    <input type="text" class="mono" bind:value={envFile} placeholder=".env" spellcheck="false" aria-invalid={!!error} data-kind={kind} />
  </label>
  {#if error}<span class="err">{error}</span>{:else}<span class="hint">Path relative to {cwd || "the project folder"}, for example <code>.env</code> or <code>config/app.env</code>. Its KEY=VALUE lines are added to the service's environment; your own variables win. A missing file is only a note, not an error.</span>{/if}
</div>
