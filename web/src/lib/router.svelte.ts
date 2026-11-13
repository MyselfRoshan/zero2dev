export type Route =
  | { name: "dashboard" }
  | { name: "services" }
  | { name: "service"; id: string }
  | { name: "serviceNew" }
  | { name: "serviceEdit"; id: string }
  | { name: "runtimes" }
  | { name: "sites" }
  | { name: "logs" }
  | { name: "settings" }
  | { name: "help" };

export function parseHash(hash: string): Route {
  const path = hash.replace(/^#/, "").split("?")[0];
  const parts = path.split("/").filter(Boolean);
  switch (parts[0]) {
    case undefined:
    case "dashboard":
      return { name: "dashboard" };
    case "services":
      if (parts[1] === "new") return { name: "serviceNew" };
      if (parts[1] && parts[2] === "edit") return { name: "serviceEdit", id: decodeURIComponent(parts[1]) };
      return parts[1] ? { name: "service", id: decodeURIComponent(parts[1]) } : { name: "services" };
    case "runtimes":
      return { name: "runtimes" };
    case "sites":
      return { name: "sites" };
    case "logs":
      return { name: "logs" };
    case "settings":
      return { name: "settings" };
    case "help":
      return { name: "help" };
    default:
      return { name: "dashboard" };
  }
}

export const router = $state<{ route: Route }>({ route: parseHash(location.hash) });

let acceptedHash = location.hash;
const guards = new Set<(next: Route) => boolean>();

/** Register a guard while a page has unsaved changes. */
export function beforeNavigate(guard: (next: Route) => boolean): () => void {
  guards.add(guard);
  return () => {
    guards.delete(guard);
  };
}

window.addEventListener("hashchange", () => {
  const next = parseHash(location.hash);
  if ([...guards].some((guard) => !guard(next))) {
    history.replaceState(null, "", location.pathname + location.search + acceptedHash);
    return;
  }
  acceptedHash = location.hash;
  router.route = next;
});

/** The value of `?key=` in the hash, e.g. `#/services/svc_1/edit?back=list`. */
export function hashParam(key: string, hash: string = location.hash): string {
  const q = hash.split("?")[1] ?? "";
  return new URLSearchParams(q).get(key) ?? "";
}

export function navigate(path: string): void {
  location.hash = path;
}

/** True when the hash carries `?new` (used by quick links); the flag is removed after reading. */
export function consumeNewFlag(): boolean {
  if (!/\?new\b/.test(location.hash)) return false;
  history.replaceState(null, "", location.pathname + location.search + location.hash.replace(/\?new\b/, ""));
  return true;
}
