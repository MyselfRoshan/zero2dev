export type Route =
  | { name: "dashboard" }
  | { name: "services" }
  | { name: "service"; id: string }
  | { name: "runtimes" }
  | { name: "sites" }
  | { name: "logs" }
  | { name: "settings" };

export function parseHash(hash: string): Route {
  const path = hash.replace(/^#/, "").split("?")[0];
  const parts = path.split("/").filter(Boolean);
  switch (parts[0]) {
    case undefined:
    case "dashboard":
      return { name: "dashboard" };
    case "services":
      return parts[1] ? { name: "service", id: decodeURIComponent(parts[1]) } : { name: "services" };
    case "runtimes":
      return { name: "runtimes" };
    case "sites":
      return { name: "sites" };
    case "logs":
      return { name: "logs" };
    case "settings":
      return { name: "settings" };
    default:
      return { name: "dashboard" };
  }
}

export const router = $state<{ route: Route }>({ route: parseHash(location.hash) });

window.addEventListener("hashchange", () => {
  router.route = parseHash(location.hash);
});

export function navigate(path: string): void {
  location.hash = path;
}

/** True when the hash carries `?new` (used by quick links); the flag is removed after reading. */
export function consumeNewFlag(): boolean {
  if (!/\?new\b/.test(location.hash)) return false;
  history.replaceState(null, "", location.pathname + location.search + location.hash.replace(/\?new\b/, ""));
  return true;
}
