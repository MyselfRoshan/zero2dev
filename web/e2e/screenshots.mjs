// Screenshots of every page of the dashboard, for design reviews and design tools.
//
//   pnpm run build                     # the mock serves the built UI from ../internal/webui/dist
//   NODE_PATH=<playwright>/node_modules node e2e/screenshots.mjs [options]
//
// Options (all optional):
//   --out=<dir>            where the images go (default: ../docs/screenshots/pages)
//   --themes=light,dark    colour schemes to shoot
//   --sizes=desktop,mobile viewports: desktop (1440x900), laptop (1280x800), mobile (420x860)
//   --pages=dashboard,...  only these pages (names as in PAGES below)
//   --viewport             only the visible part, not the whole scrolling page
//   --url=<base>           shoot a running daemon instead of the mock, e.g. http://127.0.0.1:8765
//   --token=<token>        its token (the one in the address Zero2Dev opens)
//
// Against the mock, a small demo (a project, a site, a running service) is added first, so the
// pages show real content. Every run writes <out>/<size>-<theme>/<nn>-<page>.png, plus
// manifest.json (what each image shows) and README.md (all images on one page) in <out>.
import { spawn } from "node:child_process";
import { createRequire } from "node:module";
import fs from "node:fs";
import path from "node:path";

const require = createRequire(import.meta.url);
const { chromium } = require("playwright");

const args = Object.fromEntries(
  process.argv.slice(2).map((a) => {
    const [k, ...v] = a.replace(/^--/, "").split("=");
    return [k, v.length ? v.join("=") : true];
  }),
);
const list = (v, all) =>
  typeof v === "string"
    ? v
        .split(",")
        .map((s) => s.trim())
        .filter(Boolean)
    : all;

const SIZES = {
  desktop: { width: 1440, height: 900 },
  laptop: { width: 1280, height: 800 },
  mobile: { width: 420, height: 860 },
};
const themes = list(args.themes, ["light", "dark"]);
const sizes = list(args.sizes, ["desktop", "mobile"]);
for (const t of themes) if (t !== "light" && t !== "dark") throw new Error(`unknown theme ${t} (light or dark)`);
for (const s of sizes) if (!SIZES[s]) throw new Error(`unknown size ${s} (${Object.keys(SIZES).join(", ")})`);
const OUT = path.resolve(
  typeof args.out === "string" ? args.out : path.join(import.meta.dirname, "../../docs/screenshots/pages"),
);
const fullPage = !args.viewport;

// ---- the daemon: the mock, or a running one ----
let BASE;
let TOKEN;
let mock;
let browser;
try {
  if (typeof args.url === "string") {
    BASE = args.url.replace(/\/+$/, "");
    TOKEN = typeof args.token === "string" ? args.token : "";
    if (!TOKEN) throw new Error("--url needs --token=<token> (the token in the address Zero2Dev opens)");
  } else {
    const dist = path.resolve(import.meta.dirname, "../../internal/webui/dist/index.html");
    if (!fs.existsSync(dist)) throw new Error("build the dashboard first: pnpm run build");
    const port = Number(process.env.E2E_PORT) || 7102;
    BASE = `http://127.0.0.1:${port}`;
    TOKEN = "devtoken";
    mock = spawn(process.execPath, [path.resolve(import.meta.dirname, "../mock/server.mjs")], {
      env: { ...process.env, MOCK_PORT: String(port) },
      stdio: ["ignore", "pipe", "inherit"],
    });
    await new Promise((resolve) => mock.stdout.once("data", resolve));
  }

  const api = async (method, route, body) => {
    const r = await fetch(BASE + route, {
      method,
      headers: { Authorization: `Bearer ${TOKEN}`, "Content-Type": "application/json" },
      body: body === undefined ? undefined : JSON.stringify(body),
    });
    if (!r.ok) throw new Error(`${method} ${route}: ${r.status} ${await r.text()}`);
    return r.status === 204 ? null : r.json();
  };

  // A small, believable setup on the mock: a project with three services in start order, a site,
  // one service running.
  async function demo() {
    let services = await api("GET", "/api/services");
    const custom = (name, extra = {}) => ({
      name,
      kind: "custom",
      command: "C:\\tools\\" + name.toLowerCase().replaceAll(" ", "-") + ".exe",
      args: [],
      cwd: "C:\\work\\shop",
      env: {},
      port: 0,
      healthPath: "",
      runtimeId: "",
      autostart: false,
      ...extra,
    });
    const worker = services.find((s) => s.name === "Worker");
    const shopApi = await api(
      "POST",
      "/api/services",
      custom("Shop API", { port: 8080, dependsOn: worker ? [worker.id] : [] }),
    );
    services = await api("GET", "/api/services");
    const site = await api("POST", "/api/sites", {
      name: "Shop",
      domain: "shop.localhost",
      type: "proxy",
      serviceId: shopApi.id,
      https: true,
      enabled: true,
    }).catch((e) => console.warn("demo site:", e.message));
    await api("POST", "/api/projects", {
      name: "Shop",
      serviceIds: services.filter((s) => ["Worker", "Shop API", "Docs site"].includes(s.name)).map((s) => s.id),
      siteIds: site ? [site.id] : [],
    });
    if (worker) await api("POST", `/api/services/${worker.id}/start`).catch(() => {});
    return { service: shopApi };
  }

  let ids = {};
  if (mock) ids = await demo();
  const services = await api("GET", "/api/services");
  const projects = await api("GET", "/api/projects");
  const serviceId = ids.service?.id ?? services[0]?.id;
  const projectId = projects[0]?.id;

  // ---- the pages ----
  // ready: what to wait for before the shot (a heading, or a selector).
  const PAGES = [
    { name: "first-run", hash: "#/", title: "First run (no token)", noToken: true, ready: "h1" },
    { name: "dashboard", hash: "#/dashboard", title: "Dashboard" },
    { name: "services", hash: "#/services", title: "Services list" },
    { name: "service-new", hash: "#/services/new", title: "New service: type picker and form" },
    serviceId && { name: "service-detail", hash: `#/services/${serviceId}`, title: "A service: status, command, logs" },
    serviceId && { name: "service-edit", hash: `#/services/${serviceId}/edit`, title: "Edit a service" },
    { name: "projects", hash: "#/projects", title: "Projects list" },
    { name: "project-new", hash: "#/projects/new", title: "New project" },
    projectId && {
      name: "project-edit",
      hash: `#/projects/${projectId}/edit`,
      title: "Edit a project: services, start order, sites",
    },
    { name: "runtimes", hash: "#/runtimes", title: "Runtimes: installed versions and the catalog" },
    { name: "sites", hash: "#/sites", title: "Sites: local domains" },
    { name: "site-new", hash: "#/sites?new", title: "New site form" },
    { name: "environment", hash: "#/environment", title: "Environment: PATH folders and variables" },
    { name: "logs", hash: "#/logs", title: "Logs of every service" },
    { name: "settings", hash: "#/settings", title: "Settings" },
    { name: "help", hash: "#/help", title: "Help and guided tours" },
  ].filter(Boolean);
  const only = list(args.pages, null);
  const pages = only ? PAGES.filter((p) => only.includes(p.name)) : PAGES;
  if (only) for (const n of only) if (!PAGES.some((p) => p.name === n)) throw new Error(`unknown page ${n}`);

  // The page itself does not scroll: the content panel next to the menu does. For a whole-page
  // shot the window is made as tall as that panel's content (App.svelte's .content), then put back.
  // Lists that scroll on their own (logs, pickers) keep their size.
  async function shoot(page, file, viewport) {
    if (!fullPage) return page.screenshot({ path: file });
    const extra = await page.evaluate(() => {
      const el = document.querySelector(".content");
      return el ? Math.max(0, el.scrollHeight - el.clientHeight) : 0;
    });
    if (extra > 0) {
      await page.setViewportSize({ width: viewport.width, height: Math.min(viewport.height + extra, 12000) });
      await page.waitForTimeout(200);
    }
    await page.screenshot({ path: file, fullPage: true });
    if (extra > 0) await page.setViewportSize(viewport);
  }

  browser = await chromium.launch();
  const manifest = { generated: new Date().toISOString(), source: mock ? "mock" : BASE, fullPage, shots: [] };
  let failures = 0;
  for (const size of sizes) {
    for (const theme of themes) {
      const dir = path.join(OUT, `${size}-${theme}`);
      fs.mkdirSync(dir, { recursive: true });
      // A fresh browser profile each time: no tour in progress, the theme follows colorScheme.
      const ctx = await browser.newContext({
        viewport: SIZES[size],
        colorScheme: theme,
        reducedMotion: "reduce",
        deviceScaleFactor: 1,
      });
      const page = await ctx.newPage();
      for (const [i, p] of pages.entries()) {
        const file = path.join(dir, `${String(i + 1).padStart(2, "0")}-${p.name}.png`);
        try {
          if (p.noToken) {
            await page.goto(BASE + "/");
            await page.evaluate(() => localStorage.removeItem("zero2dev.token"));
            await page.goto(BASE + "/" + p.hash);
          } else {
            await page.goto(`${BASE}/#token=${TOKEN}`);
            await page.getByRole("heading", { level: 1 }).first().waitFor();
            await page.goto(`${BASE}/${p.hash}`);
          }
          await page
            .locator(p.ready ?? "h1")
            .first()
            .waitFor({ timeout: 10000 });
          // (the event stream stays open, so "network idle" may never come: it is only a short wait)
          await page.waitForLoadState("networkidle", { timeout: 1500 }).catch(() => {});
          await page.waitForTimeout(400); // let lists and icons settle
          await shoot(page, file, SIZES[size]);
          manifest.shots.push({
            page: p.name,
            title: p.title,
            route: p.hash,
            size,
            viewport: SIZES[size],
            theme,
            file: path.relative(OUT, file).replaceAll("\\", "/"),
          });
          console.log("shot", path.relative(process.cwd(), file));
        } catch (e) {
          failures++;
          console.log("FAIL", p.name, size, theme, "\n", e.message);
        }
      }
      await ctx.close();
    }
  }

  fs.writeFileSync(path.join(OUT, "manifest.json"), JSON.stringify(manifest, null, 2) + "\n");
  const md = [
    "# Zero2Dev dashboard: every page",
    "",
    "Generated by `web/e2e/screenshots.mjs` (see the comment at its top for the options). Do not edit by hand; run it again.",
    "",
  ];
  for (const p of pages) {
    md.push(`## ${p.title}`, "", `Route \`${p.hash}\``, "");
    for (const s of manifest.shots.filter((x) => x.page === p.name))
      md.push(`![${p.title}, ${s.size}, ${s.theme}](${s.file})`, "");
  }
  fs.writeFileSync(path.join(OUT, "README.md"), md.join("\n"));
  console.log(`\n${manifest.shots.length} screenshots in ${OUT}${failures ? `, ${failures} failed` : ""}`);
  if (failures) process.exitCode = 1;
} finally {
  try {
    if (browser) await browser.close();
  } finally {
    mock?.kill();
  }
}
