import { afterEach, expect, it, vi } from "vite-plus/test";

afterEach(() => {
  vi.unstubAllGlobals();
  vi.resetModules();
});

it("keeps Settings and its hash when a navigation guard declines, then allows leaving", async () => {
  const location = { hash: "#/settings", pathname: "/", search: "" };
  let onHashChange = () => {};
  const replaceState = vi.fn((_state: unknown, _title: string, url: string) => {
    location.hash = url.slice(url.indexOf("#"));
  });
  vi.stubGlobal("location", location);
  vi.stubGlobal("history", { replaceState });
  vi.stubGlobal("window", {
    addEventListener: (_event: string, handler: () => void) => {
      onHashChange = handler;
    },
  });
  const { router, beforeNavigate, navigate } = await import("./router.svelte");
  const confirm = vi.fn(() => false);
  const remove = beforeNavigate((next) => next.name === "settings" || confirm());
  navigate("#/runtimes");
  onHashChange();
  expect(router.route.name).toBe("settings");
  expect(location.hash).toBe("#/settings");
  expect(confirm).toHaveBeenCalledOnce();
  confirm.mockReturnValue(true);
  navigate("#/runtimes");
  onHashChange();
  expect(router.route.name).toBe("runtimes");
  remove();
  navigate("#/help");
  onHashChange();
  expect(router.route.name).toBe("help");
  expect(confirm).toHaveBeenCalledTimes(2);
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
