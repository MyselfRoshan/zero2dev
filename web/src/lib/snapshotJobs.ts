// Runs a snapshot, restore or clone job to its end (0.0.5, snapshot-ui). Progress comes from the
// job.progress events the store merges into `jobs`; GET /api/jobs/{id} is polled as a safety net
// and gives the final job with its fields (snapshotId, projectId).
import { api } from "./api";
import { jobs } from "./store.svelte";
import type { Job } from "./types";

const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms));

/** Waits until the job is done or failed, calling onUpdate with its progress. Never throws. */
export async function waitForJob(started: Job, onUpdate: (j: Job) => void = () => {}): Promise<Job> {
  let last: Job = { ...started };
  onUpdate(last);
  let polled = 0;
  while (last.state === "running") {
    await sleep(200);
    const live = jobs[started.id];
    if (live) last = { ...last, percent: live.percent, step: live.step || last.step };
    polled += 200;
    // The event stream may miss the end (or say it before the fields are set): ask the daemon.
    if (polled >= 1000 || (live && live.state !== "running")) {
      polled = 0;
      try {
        last = await api.getJob(started.id);
      } catch {
        /* try again on the next round */
      }
    }
    onUpdate(last);
  }
  return last;
}
