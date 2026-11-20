<script lang="ts">
  // The progress of one snapshot, restore or clone job: a bar, its step and the percent.
  import type { Job } from "../lib/types";

  let { job, label }: { job: Job; label: string } = $props();
  const pct = $derived(Math.max(0, Math.min(100, Math.round(job.percent || 0))));
</script>

<div class="jp" role="status" data-testid="job-progress" data-state={job.state}>
  <div class="progress" class:done={job.state === "done"} class:failed={job.state === "failed"} role="progressbar" aria-valuemin="0" aria-valuemax="100" aria-valuenow={pct} aria-label="{label} progress"><span style:width="{pct}%"></span></div>
  <div class="small row"><span class="grow">{job.state === "done" ? "Done" : job.state === "failed" ? "Failed" : job.step || "Starting"}</span><span class="mono muted">{pct}%</span></div>
</div>

<style>
  .jp {
    display: grid;
    gap: 4px;
  }
</style>
