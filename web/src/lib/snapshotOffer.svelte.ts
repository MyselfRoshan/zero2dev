// The open "Take a snapshot first?" offer, shown by ui/SnapshotOffer.svelte (mounted once in App).
import type { SnapshotOfferRequest, SnapshotOfferResult } from "./snapshotOffer";

interface OpenOffer {
  seq: number;
  req: SnapshotOfferRequest;
  resolve: (r: SnapshotOfferResult) => void;
}

export const offerState = $state({ current: null as OpenOffer | null });
let seq = 0;

/** Shows the offer; a second offer while one is open cancels the first. */
export function openOffer(req: SnapshotOfferRequest): Promise<SnapshotOfferResult> {
  offerState.current?.resolve("cancelled");
  return new Promise((resolve) => {
    offerState.current = { seq: ++seq, req, resolve };
  });
}

/** Closes the offer with this answer. */
export function closeOffer(result: SnapshotOfferResult): void {
  const cur = offerState.current;
  if (!cur) return;
  offerState.current = null;
  cur.resolve(result);
}
