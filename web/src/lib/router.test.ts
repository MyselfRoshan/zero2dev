import { afterEach, expect, it, vi } from "vite-plus/test";

afterEach(() => {
  vi.unstubAllGlobals();
  vi.resetModules();
});

it("keeps Settings mounted on declined hash navigation and allows confirmed navigation", async () => {
  const location = { hash: "#/settings", pathname: "/", search: "" };
  let change: () => void = () => {};
  vi.stubGlobal("location", location);
  vi.stubGlobal("window", {
    addEventListener: (_: string, listener: () => void) => {
      change = listener;
    },
  });
  vi.stubGlobal("history", {
    replaceState: (_: unknown, __: string, url: string) => {
      location.hash = url.slice(url.indexOf("#"));
    },
  });
  const { router, beforeNavigate, navigate } = await import("./router.svelte");
  let confirmed = false;
  const remove = beforeNavigate((to) => to.name === "settings" || confirmed);
  navigate("#/runtimes");
  change();
  expect(location.hash).toBe("#/settings");
  expect(router.route.name).toBe("settings");
  confirmed = true;
  navigate("#/runtimes");
  change();
  expect(router.route.name).toBe("runtimes");
  remove();
  navigate("#/settings");
  change();
  confirmed = false;
  navigate("#/help");
  change();
  expect(router.route.name).toBe("help");
});
