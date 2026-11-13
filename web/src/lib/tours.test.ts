import { describe, expect, it } from "vite-plus/test";
import {
  baseOf,
  completedTours,
  loadProgress,
  markCompleted,
  onStepPage,
  saveProgress,
  TOURS,
  tourById,
  type TourState,
} from "./tours";

const state = (o: Partial<TourState> = {}): TourState => ({
  route: "#/dashboard",
  runtimes: [],
  services: [],
  sites: [],
  has: () => false,
  ...o,
});

function memStore() {
  const m = new Map<string, string>();
  return {
    getItem: (k: string) => m.get(k) ?? null,
    setItem: (k: string, v: string) => void m.set(k, v),
    removeItem: (k: string) => void m.delete(k),
  };
}

describe("tour definitions", () => {
  it("cover the major tasks, each with steps that say what to do", () => {
    expect(TOURS.map((t) => t.id)).toEqual(["runtime", "service", "site", "database", "editor", "tour"]);
    for (const t of TOURS) {
      expect(t.title && t.summary && t.minutes > 0, t.id).toBeTruthy();
      expect(t.steps.length, t.id).toBeGreaterThan(1);
      for (const s of t.steps) {
        expect(s.title && s.text, `${t.id}: ${s.title}`).toBeTruthy();
        // brackets and quotes balance (no DOM in unit tests to parse it)
        if (s.target) expect(s.target.split("[").length, s.target).toBe(s.target.split("]").length);
      }
    }
  });
});

describe("step checks", () => {
  const svcTour = tourById("service")!;
  const base = baseOf(state({ services: [{ id: "old", kind: "node", state: "running" }] }));
  it("advance on navigation", () => {
    const open = svcTour.steps[0];
    expect(open.done!(state({ route: "#/dashboard" }), base)).toBe(false);
    expect(open.done!(state({ route: "#/services" }), base)).toBe(true);
  });
  it("advance only when something new happened", () => {
    const create = svcTour.steps.find((s) => s.title === "Create it")!;
    const start = svcTour.steps.find((s) => s.title === "Start it")!;
    const before = state({ services: [{ id: "old", kind: "node", state: "running" }] });
    expect(create.done!(before, base)).toBe(false);
    const created = state({
      services: [
        { id: "old", kind: "node", state: "running" },
        { id: "new", kind: "node", state: "stopped" },
      ],
    });
    expect(create.done!(created, base)).toBe(true);
    expect(start.done!(created, base), "the old one was already running").toBe(false);
    const started = state({
      services: [
        { id: "old", kind: "node", state: "running" },
        { id: "new", kind: "node", state: "running" },
      ],
    });
    expect(start.done!(started, base)).toBe(true);
  });
  it("installing finishes when the runtime exists", () => {
    const install = tourById("runtime")!.steps[2];
    expect(install.done!(state(), base)).toBe(false);
    expect(install.done!(state({ runtimes: [{ kind: "node" }] }), base)).toBe(true);
  });
  it("knows the page of a step", () => {
    expect(onStepPage({ title: "x", text: "y", route: "#/services" }, "#/services/new")).toBe(true);
    expect(onStepPage({ title: "x", text: "y", route: "#/sites" }, "#/services")).toBe(false);
    expect(onStepPage({ title: "x", text: "y" }, "#/anything")).toBe(true);
  });
});

describe("tour progress", () => {
  it("survives a reload and rejects stale data", () => {
    const st = memStore();
    expect(loadProgress(st)).toBeNull();
    const p = { id: "service", step: 3, base: baseOf(state()) };
    saveProgress(p, st);
    expect(loadProgress(st)).toEqual(p);
    saveProgress({ ...p, step: 99 }, st);
    expect(loadProgress(st)).toBeNull();
    st.setItem("zero2dev.tour", "{not json");
    expect(loadProgress(st)).toBeNull();
    saveProgress(null, st);
    expect(st.getItem("zero2dev.tour")).toBeNull();
  });
  it("remembers finished tours", () => {
    const st = memStore();
    expect(completedTours(st)).toEqual([]);
    markCompleted("site", st);
    expect(markCompleted("site", st)).toEqual(["site"]);
  });
});
