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
