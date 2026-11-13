// The in-app tutorials (Help). A tour is a list of steps; each step points at something on
// a page (a CSS selector, usually [data-tour=...]) and, when it can tell, finishes by itself
// once the thing really happened (the runtime is installed, the service runs).

export interface TourState {
  route: string; // location.hash
  runtimes: { kind: string }[];
  services: { id: string; kind: string; state: string }[];
  sites: { id: string }[];
  /** Whether an element matching the selector is on the page. */
  has: (selector: string) => boolean;
}

/** What existed when the step began, to tell "you just did it" from "it was there before". */
export interface TourBase {
  runtimes: number;
  services: number;
  sites: number;
  running: number;
  serviceIds: string[];
}

export interface TourStep {
  title: string;
  text: string;
  /** The page the step happens on (a hash prefix); the guide offers "Go there" elsewhere. */
  route?: string;
  /** What to highlight. The first visible match is used. */
  target?: string;
  /** Finished by itself when true; steps without it continue with Next. */
  done?: (s: TourState, base: TourBase) => boolean;
  /** Shown while waiting for `done`, e.g. "Waiting for the install to finish". */
  waiting?: string;
}

export interface Tour {
  id: string;
  title: string;
  summary: string;
  minutes: number;
  icon: string;
  steps: TourStep[];
}

const on = (prefix: string) => (s: TourState) => s.route.startsWith(prefix);
const running = (s: TourState) => s.services.filter((x) => x.state === "running").length;
const newServiceRunning = (s: TourState, b: TourBase) =>
  s.services.some((x) => x.state === "running" && !b.serviceIds.includes(x.id)) || running(s) > b.running;

const openServices: TourStep = {
  title: "Open Services",
  text: "Services are the apps Zero2Dev runs for you. Click Services in the sidebar.",
  target: '[data-tour="nav-services"]',
  done: on("#/services"),
};
const openRuntimes: TourStep = {
  title: "Open Runtimes",
  text: "Runtimes are the programs your apps run on, such as Node.js or PHP. Click Runtimes in the sidebar.",
  target: '[data-tour="nav-runtimes"]',
  done: on("#/runtimes"),
};
const newService: TourStep = {
  title: "Start a new service",
  text: "Press New service.",
  route: "#/services",
  target: '[data-tour="new-service"]',
  done: on("#/services/new"),
};
const createIt: TourStep = {
  title: "Create it",
  text: "Press Create service. Anything missing is pointed out next to its field.",
  route: "#/services/",
  target: '[data-tour="create-service"]',
  done: (s, b) => s.services.length > b.services,
  waiting: "Waiting for you to create the service",
};
const startIt: TourStep = {
  title: "Start it",
  text: "Press Start. The state turns to starting, then running once the app answers on its port.",
  route: "#/services",
  target: '[data-tour="start"]',
  done: newServiceRunning,
  waiting: "Waiting for a service to run",
};

function installStep(kind: string, label: string): TourStep[] {
  return [
    {
      title: `Open ${label}`,
      text: `Press Install… on ${label}. It opens with the newest version already picked.`,
      route: "#/runtimes",
      target: `[data-tour="rt-toggle-${kind}"]`,
      done: (s) =>
        s.has(`article[data-kind="${kind}"].open, [data-kind-dialog="${kind}"]`) ||
        s.runtimes.some((r) => r.kind === kind),
    },
    {
      title: "Install it",
      text: "Press Install. Zero2Dev downloads it from the publisher, checks it against the published checksum and puts it in its bin folder.",
      route: "#/runtimes",
      target: `article[data-kind="${kind}"] [data-tour="rt-install"], [data-kind-dialog="${kind}"] [data-tour="rt-install"]`,
      done: (s) => s.runtimes.some((r) => r.kind === kind),
      waiting: `Waiting for ${label} to finish installing`,
    },
  ];
}

export const TOURS: Tour[] = [
  {
    id: "runtime",
    title: "Install a runtime",
    summary: "Download Node.js (or any runtime) into Zero2Dev's own folder, checked and ready to use.",
    minutes: 2,
    icon: "download",
    steps: [
      openRuntimes,
      ...installStep("node", "Node.js"),
      {
        title: "Done",
        text: "Node.js is installed. Services of type Node.js use it from now on. Install more versions side by side the same way; Manage makes one the default.",
        route: "#/runtimes",
        target: 'article[data-kind="node"]',
      },
    ],
  },
  {
    id: "service",
    title: "Create a service",
    summary: "Run your app: choose what it is, point at its folder, check the command line and start it.",
    minutes: 3,
    icon: "services",
    steps: [
      openServices,
      newService,
      {
        title: "Choose what it is",
        text: "Pick the card for your app: Node.js, PHP, a database, or Custom command for anything else.",
        route: "#/services/new",
        target: '[data-tour="type-picker"]',
        done: (s) => s.has('[data-testid="chosen-type"]'),
      },
      {
        title: "Name and folder",
        text: "Give it a name and choose its project folder: the folder with your app's code. Browse... opens the Windows folder dialog. Then press Next.",
        route: "#/services/new",
        target: '[data-tour="basics"]',
      },
      {
        title: "Check what will run",
        text: "This panel shows the exact command Zero2Dev will run, updated as you change the settings. Settings start at values that work; change only what you need.",
        route: "#/services/new",
        target: '[data-tour="cmd-panel"]',
      },
      createIt,
      startIt,
      {
        title: "Done",
        text: "Your service runs. Open it to see its live output, its address and the exact command. Stop or restart it from the list or the dashboard.",
      },
    ],
  },
  {
    id: "site",
    title: "Give it a local domain",
    summary: "Open your app at shop.localhost instead of a port number, with HTTPS if you want it.",
    minutes: 2,
    icon: "sites",
    steps: [
      {
        title: "Open Sites",
        text: "Sites are addresses like shop.localhost. Click Sites in the sidebar.",
        target: '[data-tour="nav-sites"]',
        done: on("#/sites"),
      },
      {
        title: "Start a new site",
        text: "Press New site.",
        route: "#/sites",
        target: '[data-tour="new-site"]',
        done: (s) => s.has("#site-form"),
      },
      {
        title: "Say what it points to",
        text: "A service (its port is used), a port, or a folder of files. Choose the service you created, give the site a name, and the domain is suggested.",
        route: "#/sites",
        target: '[data-tour="site-target"]',
      },
      {
        title: "Create it",
        text: "Press Create site. Zero2Dev writes the web server's configuration and reloads it.",
        route: "#/sites",
        target: '[data-tour="save-site"]',
        done: (s, b) => s.sites.length > b.sites,
        waiting: "Waiting for you to create the site",
      },
      {
        title: "Open it",
        text: "The address is in the list. It works while the site server (Caddy by default) and your service run.",
        route: "#/sites",
        target: "tr[data-site] a",
      },
    ],
  },
  {
    id: "database",
    title: "Add a database",
    summary: "Install MariaDB, run it as a service with its data kept by Zero2Dev, and connect to it.",
    minutes: 4,
    icon: "database",
    steps: [
      openRuntimes,
      ...installStep("mariadb", "MariaDB"),
      { ...openServices, done: (s) => s.route.startsWith("#/services") },
      newService,
      {
        title: "Choose MariaDB",
        text: "Pick the MariaDB card. A database needs no project folder: Zero2Dev keeps its files in its own data folder, and the port is filled in.",
        route: "#/services/new",
        target: '[data-tour="type-picker"]',
        done: (s) => s.has('[data-testid="chosen-type"]'),
      },
      createIt,
      startIt,
      {
        title: "Connect to it",
        text: "Open the service: its page shows the host, port, user and a connection URL to copy. With HeidiSQL or DBeaver installed, Open in fills the connection in for you.",
        route: "#/services",
      },
    ],
  },
  {
    id: "editor",
    title: "Open a project in your editor",
    summary: "Install VS Code into Zero2Dev and open a service's folder in it with one click.",
    minutes: 2,
    icon: "external",
    steps: [
      openRuntimes,
      ...installStep("vscode", "VS Code"),
      {
        title: "Open a service",
        text: "Go to Services and click a service's name.",
        target: '[data-tour="nav-services"]',
        done: (s) => /^#\/services\/[^/?]+$/.test(s.route.split("?")[0]) && !s.route.startsWith("#/services/new"),
      },
      {
        title: "Open in VS Code",
        text: "Press Open in VS Code. Its settings and extensions are kept in Zero2Dev's data folder, apart from any VS Code you installed yourself.",
        target: '[data-testid="open-in"]',
      },
    ],
  },
  {
    id: "tour",
    title: "A quick look around",
    summary: "One minute through every page, to see where things are.",
    minutes: 1,
    icon: "compass",
    steps: [
      {
        title: "Dashboard",
        text: "What runs right now, at a glance, with quick actions. Click any tile to go there.",
        route: "#/dashboard",
        target: '[data-tour="overview"]',
      },
      {
        title: "Services",
        text: "Your apps and databases. Start, stop and open each one; its page has the live output.",
        route: "#/services",
        target: '[data-tour="nav-services"]',
      },
      {
        title: "Runtimes",
        text: "The programs your services run on. Install versions side by side; one per type is the default.",
        route: "#/runtimes",
        target: '[data-tour="nav-runtimes"]',
      },
      {
        title: "Sites",
        text: "Local addresses like shop.localhost, served by Caddy or nginx.",
        route: "#/sites",
        target: '[data-tour="nav-sites"]',
      },
      {
        title: "Logs",
        text: "The output of every service in one place, filterable.",
        route: "#/logs",
        target: '[data-tour="nav-logs"]',
      },
      {
        title: "Settings",
        text: "Site server, ports, HTTPS, the download proxy and where your data lives.",
        route: "#/settings",
        target: '[data-tour="nav-settings"]',
      },
      {
        title: "Help",
        text: "These tutorials, and fixes for the usual problems. You can come back any time.",
        route: "#/help",
        target: '[data-tour="nav-help"]',
      },
    ],
  },
];

export function tourById(id: string): Tour | undefined {
  return TOURS.find((t) => t.id === id);
}

export function baseOf(s: Pick<TourState, "runtimes" | "services" | "sites">): TourBase {
  return {
    runtimes: s.runtimes.length,
    services: s.services.length,
    sites: s.sites.length,
    running: s.services.filter((x) => x.state === "running").length,
    serviceIds: s.services.map((x) => x.id),
  };
}

/** Whether the guide is on the step's page (routes are hash prefixes). */
export function onStepPage(step: TourStep, route: string): boolean {
  return !step.route || route.startsWith(step.route);
}

/** The setup checklist on the dashboard maps to these tours. */
export const SETUP_TOUR: Record<string, string> = { runtime: "runtime", service: "service", site: "site" };

// ---- progress, kept in localStorage so a reload continues the tour ----

export interface TourProgress {
  id: string;
  step: number;
  base: TourBase;
}

const PROGRESS = "zero2dev.tour";
const DONE = "zero2dev.tours.done";
type Store = Pick<Storage, "getItem" | "setItem" | "removeItem">;

function read<T>(st: Store | undefined, key: string, fallback: T): T {
  try {
    const v = st?.getItem(key);
    return v ? (JSON.parse(v) as T) : fallback;
  } catch {
    return fallback;
  }
}
function write(st: Store | undefined, key: string, v: unknown): void {
  try {
    if (v === null) st?.removeItem(key);
    else st?.setItem(key, JSON.stringify(v));
  } catch {
    /* storage unavailable: progress lasts for this page */
  }
}

export function loadProgress(st: Store | undefined = globalThis.localStorage): TourProgress | null {
  const p = read<TourProgress | null>(st, PROGRESS, null);
  const t = p && tourById(p.id);
  if (!p || !t || typeof p.step !== "number" || p.step < 0 || p.step >= t.steps.length || !p.base) return null;
  return p;
}
export function saveProgress(p: TourProgress | null, st: Store | undefined = globalThis.localStorage): void {
  write(st, PROGRESS, p);
}
export function completedTours(st: Store | undefined = globalThis.localStorage): string[] {
  const d = read<unknown>(st, DONE, []);
  return Array.isArray(d) ? d.filter((x): x is string => typeof x === "string") : [];
}
export function markCompleted(id: string, st: Store | undefined = globalThis.localStorage): string[] {
  const d = [...new Set([...completedTours(st), id])];
  write(st, DONE, d);
  return d;
}
