// "Take a snapshot first?": offered before a risky action (switching a runtime's default version,
// resetting a database, importing a dump). The snapshot-ui work (0.0.5) replaces this stub with the
// real dialog (ui/SnapshotOffer.svelte); callers only use offerSnapshot.
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

/** Wave 0 stub: always goes ahead without a snapshot. */
export async function offerSnapshot(req: SnapshotOfferRequest): Promise<SnapshotOfferResult> {
  void req;
  return "skipped";
}
