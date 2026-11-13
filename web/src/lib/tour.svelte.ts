// The running tutorial (Help): which tour, which step, and what existed when it began.
// Progress is kept in localStorage, so a reload continues where it was.
import { lists, toast } from "./store.svelte";
import { navigate } from "./router.svelte";
import {
  baseOf,
  completedTours,
  loadProgress,
  markCompleted,
  saveProgress,
  tourById,
  type TourProgress,
} from "./tours";

export const tour = $state<{ active: TourProgress | null; completed: string[] }>({
  active: loadProgress(),
  completed: completedTours(),
});

export function startTour(id: string): void {
  const t = tourById(id);
  if (!t) return;
  tour.active = { id, step: 0, base: baseOf(lists) };
  saveProgress(tour.active);
  const first = t.steps[0];
  if (first.route && !location.hash.startsWith(first.route)) navigate(first.route);
}

/** Moves to step i. With `follow` (Next and Back pressed by the user), the guide also
 * goes to the step's page; steps reached by doing the thing stay where the user is. */
export function goToStep(i: number, follow = false): void {
  const a = tour.active;
  const t = a && tourById(a.id);
  if (!a || !t) return;
  if (i >= t.steps.length) return finishTour();
  tour.active = { ...a, step: Math.max(0, i) };
  saveProgress(tour.active);
  const route = t.steps[tour.active.step].route;
  if (follow && route && !location.hash.startsWith(route)) navigate(route);
}

export function finishTour(): void {
  const a = tour.active;
  if (!a) return;
  tour.completed = markCompleted(a.id);
  const title = tourById(a.id)?.title ?? "Tutorial";
  tour.active = null;
  saveProgress(null);
  toast("success", `Tutorial finished: ${title}.`);
}

export function exitTour(): void {
  tour.active = null;
  saveProgress(null);
}
