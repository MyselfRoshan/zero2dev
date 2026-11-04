<script lang="ts">
  import type { ServiceState } from "../lib/types";
  let { state }: { state: ServiceState } = $props();
</script>

<span class="badge {state}" data-state={state}><span class="dot"></span>{state}</span>
