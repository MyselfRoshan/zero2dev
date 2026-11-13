import { describe, expect, it } from "vite-plus/test";
import { lastLine, overview, recentActivity, statusSentence } from "./dashboard";
import type { Service } from "./types";

const svc = (o: Partial<Service>): Service =>
  ({
    id: "s",
    name: "s",
    kind: "node",
    command: "",
    args: [],
    settings: {},
    customArgs: false,
    cwd: "",
    env: {},
    port: 0,
    healthPath: "",
    runtimeId: "",
    autostart: false,
    state: "stopped",
    pid: 0,
    startedAt: null,
    lastError: "",
    ...o,
  }) as Service;

describe("dashboard overview", () => {
  const services = [
    svc({ id: "a", name: "API", state: "running" }),
    svc({ id: "b", name: "DB", kind: "postgres", state: "running" }),
    svc({ id: "c", name: "Worker", state: "failed" }),
    svc({ id: "d", name: "Caddy", kind: "caddy", state: "stopped" }),
  ];
  const sites = [
    { id: "1", enabled: true },
    { id: "2", enabled: false },
  ] as never[];
  const runtimes = [{ kind: "node" }, { kind: "node" }, { kind: "caddy" }] as never[];
  it("counts what runs", () => {
    const o = overview({ services, sites, runtimes, catalog: [] }, { siteServer: "caddy" });
    expect(o.services).toMatchObject({ total: 4, running: 2, failed: 1 });
    expect(o.databases).toEqual({ total: 1, running: 1 });
    expect(o.sites).toMatchObject({
      total: 2,
      enabled: 1,
      server: "Caddy",
      serverRunning: false,
      serverInstalled: true,
    });
    expect(o.runtimes).toMatchObject({ kinds: 2, versions: 3, updates: 0 });
  });
  it("sums it up in one line", () => {
    const o = overview({ services, sites, runtimes, catalog: [] }, null);
    expect(statusSentence(o)).toBe("2 of 4 services running · 1 failed · sites served by Caddy (stopped)");
    expect(statusSentence(overview({ services: [], sites: [], runtimes: [], catalog: [] }, null))).toBe(
      "No services yet",
    );
  });
});

describe("recent activity", () => {
  it("merges services, newest first, and skips unknown ones", () => {
    const by = {
      a: [
        { ts: "2026-11-13T10:00:00Z", stream: "stdout", text: "a1", seq: 1 },
        { ts: "2026-11-13T10:00:03Z", stream: "stderr", text: "a2", seq: 4 },
      ],
      b: [{ ts: "2026-11-13T10:00:02Z", stream: "system", text: "b1", seq: 3 }],
      gone: [{ ts: "2026-11-13T10:00:09Z", stream: "stdout", text: "x", seq: 9 }],
    } as never;
    const r = recentActivity(
      by,
      [
        { id: "a", name: "API" },
        { id: "b", name: "DB" },
      ],
      2,
    );
    expect(r.map((l) => `${l.serviceName}:${l.text}`)).toEqual(["API:a2", "DB:b1"]);
  });
  it("finds the last non-empty line", () => {
    expect(
      lastLine([
        { ts: "", stream: "stdout", text: "ready" },
        { ts: "", stream: "stdout", text: "  " },
      ])?.text,
    ).toBe("ready");
    expect(lastLine(undefined)).toBeUndefined();
  });
});
