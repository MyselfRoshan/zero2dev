// "Take a snapshot first?": offered before a risky action (switching a runtime's default version,
// resetting a database, importing a dump). The dialog is ui/SnapshotOffer.svelte, mounted once in
// App.svelte; callers only use offerSnapshot.
import { openOffer } from "./snapshotOffer.svelte";
import type { SnapshotScope } from "./types";

export interface SnapshotOfferRequest {
  scope: SnapshotScope;
  /** The project or the database service to snapshot. */
  ownerId: string;
  /** Shown in the dialog, e.g. "before importing shop.sql into shop". */
  reason: string;
}

/**
 * "taken": a snapshot was taken (and finished) and the action may go ahead; "skipped": the user
 * chose to go ahead without one; "cancelled": the user cancelled the action itself.
 */
export type SnapshotOfferResult = "taken" | "skipped" | "cancelled";

/**
 * Asks "Take a snapshot first?". Running services of the owner are stopped for the snapshot and
 * started again (the dialog says so). Resolves "taken" only once the snapshot job is done.
 */
export async function offerSnapshot(req: SnapshotOfferRequest): Promise<SnapshotOfferResult> {
  return openOffer(req);
}
