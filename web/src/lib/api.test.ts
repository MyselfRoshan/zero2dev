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
