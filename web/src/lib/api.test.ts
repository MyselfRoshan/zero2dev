import { afterEach, beforeEach, describe, expect, it, vi } from "vite-plus/test";
import { ApiError, TOKEN_KEY, api, consumeTokenFromHash, onAuthFailure, request, setFetch } from "./api";

function memStorage() {
  const m = new Map<string, string>();
  return {
    getItem: (k: string) => m.get(k) ?? null,
    setItem: (k: string, v: string) => void m.set(k, v),
    removeItem: (k: string) => void m.delete(k),
  };
}

function res(status: number, body?: unknown): Response {
  return new Response(body === undefined ? null : JSON.stringify(body), {
    status,
    headers: body === undefined ? {} : { "Content-Type": "application/json" },
  });
}

const fails = (p: Promise<unknown>) =>
  p.then(
    () => {
      throw new Error("expected a rejection");
    },
    (e: unknown) => e as ApiError,
  );

beforeEach(() => {
  vi.stubGlobal("localStorage", memStorage());
});
afterEach(() => {
  setFetch(null);
  onAuthFailure(null);
  vi.unstubAllGlobals();
});

describe("runtime endpoints", () => {
  it("makes a runtime the default with POST and no body", async () => {
    const f = vi.fn().mockResolvedValue(res(200, { id: "rt 1", default: true }));
    setFetch(f as never);
    const r = await api.setDefaultRuntime("rt 1");
    expect(f.mock.calls[0][0]).toBe("/api/runtimes/rt%201/default");
    expect(f.mock.calls[0][1].method).toBe("POST");
    expect(f.mock.calls[0][1].body).toBeUndefined();
    expect(r.default).toBe(true);
  });
  it("sends only contract fields when installing", async () => {
    const f = vi.fn().mockResolvedValue(res(202, { id: "job" }));
    setFetch(f as never);
    await api.installRuntime("php-7.1.33", { acceptUnverified: true });
    expect(JSON.parse(f.mock.calls[0][1].body)).toEqual({ catalogId: "php-7.1.33", acceptUnverified: true });
  });
});

describe("request", () => {
  it("sends the bearer token and JSON body", async () => {
    localStorage.setItem(TOKEN_KEY, "abc123");
    const f = vi.fn().mockResolvedValue(res(200, { ok: 1 }));
    setFetch(f as never);
    await request("POST", "/api/x", { a: 1 });
    const [path, init] = f.mock.calls[0];
    expect(path).toBe("/api/x");
    expect(init.method).toBe("POST");
    expect(init.headers.Authorization).toBe("Bearer abc123");
    expect(init.headers["Content-Type"]).toBe("application/json");
    expect(init.body).toBe('{"a":1}');
  });

  it("omits Authorization when there is no token and Content-Type without a body", async () => {
    const f = vi.fn().mockResolvedValue(res(200, []));
    setFetch(f as never);
    await api.listServices();
    expect(f.mock.calls[0][1].headers.Authorization).toBeUndefined();
    expect(f.mock.calls[0][1].headers["Content-Type"]).toBeUndefined();
  });

  it("returns undefined for 204", async () => {
    setFetch((async () => res(204)) as never);
    await expect(api.deleteService("svc_1")).resolves.toBeUndefined();
  });

  it("maps the contract error body to ApiError", async () => {
    setFetch((async () => res(409, { error: { code: "conflict", message: "port 3000 is already in use" } })) as never);
    const e = await fails(api.startService("svc_1"));
    expect(e).toBeInstanceOf(ApiError);
    expect(e.status).toBe(409);
    expect(e.code).toBe("conflict");
    expect(e.message).toBe("port 3000 is already in use");
  });

  it("falls back to status-derived codes when the body is not the contract shape", async () => {
    setFetch((async () => new Response("<html>bad gateway</html>", { status: 502 })) as never);
    const e = await fails(request("GET", "/api/x"));
    expect(e.code).toBe("internal");
    expect(e.message).toContain("502");
  });

  it("treats 401 as auth failure and notifies the handler", async () => {
    const h = vi.fn();
    onAuthFailure(h);
    setFetch((async () => res(401, { error: { code: "unauthorized", message: "no" } })) as never);
    const e = await fails(api.status());
    expect(e.isAuth).toBe(true);
    expect(h).toHaveBeenCalledOnce();
  });

  it("maps fetch rejection to a network error", async () => {
    setFetch((async () => {
      throw new TypeError("Failed to fetch");
    }) as never);
    const e = await fails(api.status());
    expect(e.isNetwork).toBe(true);
    expect(e.code).toBe("network");
  });

  it("encodes ids in paths", async () => {
    const f = vi.fn().mockResolvedValue(res(200, {}));
    setFetch(f as never);
    await api.getService("a/b");
    expect(f.mock.calls[0][0]).toBe("/api/services/a%2Fb");
  });
});

describe("consumeTokenFromHash", () => {
  it("stores the token and strips the fragment", () => {
    const replaceState = vi.fn();
    const loc = { hash: "#token=deadbeef", pathname: "/", search: "" } as Location;
    const t = consumeTokenFromHash(loc, { replaceState } as unknown as History);
    expect(t).toBe("deadbeef");
    expect(localStorage.getItem(TOKEN_KEY)).toBe("deadbeef");
    expect(replaceState).toHaveBeenCalledWith(null, "", "/");
  });
  it("ignores other fragments", () => {
    const replaceState = vi.fn();
    const loc = { hash: "#/services", pathname: "/", search: "" } as Location;
    expect(consumeTokenFromHash(loc, { replaceState } as unknown as History)).toBeNull();
    expect(replaceState).not.toHaveBeenCalled();
  });
});

describe("bulk, project and terminal endpoints", () => {
  const calls = async (fn: () => Promise<unknown>, body?: unknown) => {
    const f = vi.fn().mockResolvedValue(res(200, body ?? {}));
    setFetch(f as never);
    await fn();
    const [url, init] = f.mock.calls[0] as [string, RequestInit];
    return { url, method: init.method, body: init.body };
  };

  it("start-all and stop-all name the project only when given one", async () => {
    expect(await calls(() => api.startAllServices())).toMatchObject({ url: "/api/services/start-all", method: "POST" });
    expect(await calls(() => api.startAllServices("prj 1"))).toMatchObject({
      url: "/api/services/start-all?project=prj%201",
    });
    expect(await calls(() => api.stopAllServices())).toMatchObject({ url: "/api/services/stop-all" });
    expect(await calls(() => api.stopAllServices("prj_1"))).toMatchObject({
      url: "/api/services/stop-all?project=prj_1",
    });
  });
  it("enables and disables all sites", async () => {
    expect(await calls(() => api.enableAllSites())).toMatchObject({ url: "/api/sites/enable-all", method: "POST" });
    expect(await calls(() => api.disableAllSites("prj_1"))).toMatchObject({
      url: "/api/sites/disable-all?project=prj_1",
    });
  });
  it("manages projects", async () => {
    const input = { name: "Shop", serviceIds: ["svc_1"], siteIds: [] };
    expect(await calls(() => api.listProjects())).toMatchObject({ url: "/api/projects", method: "GET" });
    expect(await calls(() => api.createProject(input))).toMatchObject({ method: "POST", body: JSON.stringify(input) });
    expect(await calls(() => api.updateProject("prj_1", input))).toMatchObject({
      url: "/api/projects/prj_1",
      method: "PUT",
    });
    const f = vi.fn().mockResolvedValue(res(204));
    setFetch(f as never);
    await api.deleteProject("prj_1");
    expect(f.mock.calls[0][0]).toBe("/api/projects/prj_1");
    expect((f.mock.calls[0][1] as RequestInit).method).toBe("DELETE");
  });
  it("opens a terminal with POST and no body", async () => {
    const f = vi.fn().mockResolvedValue(res(204));
    setFetch(f as never);
    await api.openTerminal("svc 1");
    expect(f.mock.calls[0][0]).toBe("/api/services/svc%201/terminal");
    expect((f.mock.calls[0][1] as RequestInit).body).toBeUndefined();
  });
});
