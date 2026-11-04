import { describe, expect, it, vi } from "vite-plus/test";
import { BACKOFF_MAX_MS, EventConnection, backoffDelay, type EventSourceLike, type StreamState } from "./events";

class FakeSource implements EventSourceLike {
  onopen: ((ev: unknown) => void) | null = null;
  onerror: ((ev: unknown) => void) | null = null;
  closed = false;
  listeners = new Map<string, (ev: { data: string }) => void>();
  constructor(public url: string) {}
  addEventListener(type: string, l: (ev: { data: string }) => void) {
    this.listeners.set(type, l);
  }
  close() {
    this.closed = true;
  }
  emit(type: string, data: unknown) {
    this.listeners.get(type)?.({ data: JSON.stringify(data) });
  }
}

function harness(opts: { ticketFail?: (n: number) => unknown } = {}) {
  const sources: FakeSource[] = [];
  const timers: { fn: () => void; ms: number }[] = [];
  let n = 0;
  const events: [string, unknown][] = [];
  const states: StreamState[] = [];
  const onOpen = vi.fn();
  const conn = new EventConnection({
    getTicket: async () => {
      n++;
      const f = opts.ticketFail?.(n);
      if (f) throw f;
      return `t${n}`;
    },
    createSource: (u) => {
      const s = new FakeSource(u);
      sources.push(s);
      return s;
    },
    onEvent: (t, d) => events.push([t, d]),
    onOpen,
    onState: (s) => states.push(s),
    isAuthError: (e) => e === "auth",
    setTimer: (fn, ms) => {
      timers.push({ fn, ms });
      return timers.length;
    },
    clearTimer: () => {},
  });
  const flush = () => new Promise((r) => setTimeout(r, 0));
  return { conn, sources, timers, events, states, onOpen, flush };
}

describe("backoffDelay", () => {
  it("doubles from 1 s and caps at 15 s", () => {
    expect([0, 1, 2, 3, 4, 5, 20].map(backoffDelay)).toEqual([
      1000,
      2000,
      4000,
      8000,
      BACKOFF_MAX_MS,
      BACKOFF_MAX_MS,
      BACKOFF_MAX_MS,
    ]);
  });
});

describe("EventConnection", () => {
  it("connects with a ticket and dispatches parsed events", async () => {
    const h = harness();
    h.conn.start();
    await h.flush();
    expect(h.sources[0].url).toBe("/api/events?ticket=t1");
    h.sources[0].onopen?.({});
    expect(h.onOpen).toHaveBeenCalledWith(false);
    h.sources[0].emit("service.status", { id: "svc_1", state: "running" });
    h.sources[0].emit("log.line", { serviceId: "svc_1", text: "hi" });
    expect(h.events).toEqual([
      ["service.status", { id: "svc_1", state: "running" }],
      ["log.line", { serviceId: "svc_1", text: "hi" }],
    ]);
  });

  it("reconnects with a fresh ticket and growing backoff, then resets after success", async () => {
    const h = harness();
    h.conn.start();
    await h.flush();
    h.sources[0].onopen?.({});

    h.sources[0].onerror?.({});
    expect(h.sources[0].closed).toBe(true);
    expect(h.timers.at(-1)?.ms).toBe(1000);
    h.timers.at(-1)!.fn();
    await h.flush();
    expect(h.sources[1].url).toBe("/api/events?ticket=t2");

    h.sources[1].onerror?.({}); // never opened: attempt counter keeps growing
    expect(h.timers.at(-1)?.ms).toBe(2000);
    h.timers.at(-1)!.fn();
    await h.flush();
    h.sources[2].onerror?.({});
    expect(h.timers.at(-1)?.ms).toBe(4000);
    h.timers.at(-1)!.fn();
    await h.flush();

    h.sources[3].onopen?.({});
    expect(h.onOpen).toHaveBeenLastCalledWith(true); // reconnect -> caller refetches lists
    h.sources[3].onerror?.({});
    expect(h.timers.at(-1)?.ms).toBe(1000); // reset after a successful open
  });

  it("retries when the ticket request itself fails (daemon down)", async () => {
    const h = harness({ ticketFail: (n) => (n < 3 ? new Error("network") : null) });
    h.conn.start();
    await h.flush();
    expect(h.sources).toHaveLength(0);
    expect(h.timers.at(-1)?.ms).toBe(1000);
    h.timers.at(-1)!.fn();
    await h.flush();
    expect(h.timers.at(-1)?.ms).toBe(2000);
    h.timers.at(-1)!.fn();
    await h.flush();
    expect(h.sources).toHaveLength(1);
    expect(h.sources[0].url).toContain("ticket=t3");
  });

  it("stops retrying on an auth error", async () => {
    const h = harness({ ticketFail: () => "auth" });
    h.conn.start();
    await h.flush();
    expect(h.states.at(-1)).toBe("unauthorized");
    expect(h.timers).toHaveLength(0);
  });

  it("ignores events from a superseded source and from malformed frames", async () => {
    const h = harness();
    h.conn.start();
    await h.flush();
    const first = h.sources[0];
    first.onopen?.({});
    first.listeners.get("log.line")?.({ data: "not json" });
    expect(h.events).toHaveLength(0);
    h.conn.stop();
    first.emit("service.status", { id: "x" });
    expect(h.events).toHaveLength(0);
    expect(first.closed).toBe(true);
  });
});
