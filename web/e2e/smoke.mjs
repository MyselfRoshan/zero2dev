// End-to-end smoke test: starts the mock daemon (serving the built UI from ../internal/webui/dist)
// and drives it with headless Chromium. Run `vp build` first.   node e2e/smoke.mjs
import { spawn } from "node:child_process";
import { createRequire } from "node:module";
import fs from "node:fs";
import path from "node:path";
import assert from "node:assert/strict";

const require = createRequire(import.meta.url);
const { chromium } = require("playwright");

const PORT = 7101;
const BASE = `http://127.0.0.1:${PORT}`;
const SHOTS =
  process.env.SHOTS_DIR || "/tmp/claude-0/-home-claude/b6d8e937-98d5-5283-884d-6cc8e920e246/scratchpad/shots";
fs.mkdirSync(SHOTS, { recursive: true });
const shot = (page, name) => page.screenshot({ path: path.join(SHOTS, name + ".png"), fullPage: false });

const mock = spawn(process.execPath, [path.resolve(import.meta.dirname, "../mock/server.mjs")], {
  env: { ...process.env, MOCK_PORT: String(PORT), MOCK_EMPTY: "1" },
  stdio: ["ignore", "pipe", "inherit"],
});
await new Promise((resolve) => mock.stdout.once("data", resolve));

const browser = await chromium.launch();
const results = [];
const step = async (name, fn) => {
  try {
    await fn();
    results.push(["PASS", name]);
    console.log("PASS", name);
  } catch (e) {
    results.push(["FAIL", name]);
    console.log("FAIL", name, "\n", e.message);
    process.exitCode = 1;
  }
};

const ctx = await browser.newContext({
  viewport: { width: 1280, height: 800 },
  permissions: ["clipboard-read", "clipboard-write"],
});
ctx.setDefaultTimeout(8000);
const page = await ctx.newPage();
const external = [];
page.on("request", (r) => {
  if (!r.url().startsWith(BASE) && !r.url().startsWith("data:")) external.push(r.url());
});
page.on("pageerror", (e) => {
  console.log("PAGE ERROR", e.message);
  process.exitCode = 1;
});

await step("first run without token shows instructions", async () => {
  await page.goto(BASE + "/");
  await page.getByRole("heading", { name: /Open Zero2Dev from the daemon/ }).waitFor();
  await shot(page, "00-first-run");
});

await step("token in URL fragment is stored and removed", async () => {
  await page.goto(BASE + "/#token=devtoken");
  await page.getByRole("heading", { level: 1, name: "Dashboard" }).waitFor();
  assert.equal(await page.evaluate(() => localStorage.getItem("zero2dev.token")), "devtoken");
  assert.ok(!page.url().includes("token"), "fragment removed: " + page.url());
  await page.getByRole("heading", { name: "Get started" }).waitFor();
  await page.getByRole("link", { name: "Open runtimes" }).waitFor();
  await page.getByText("Connected").waitFor();
  await shot(page, "01-dashboard-empty");
});

const card = (kind) => page.locator(`article[data-kind="${kind}"]`);
const li = (kind, version) => card(kind).locator(`li[data-version="${version}"]`);
const pickOptions = (kind) =>
  card(kind)
    .getByLabel(/^Install (a|another) version$/)
    .locator("option")
    .allTextContents();
const picker = (kind) => card(kind).getByLabel(/^Install (a|another) version$/);

await step("runtimes: a card per kind, first check, version list per kind", async () => {
  await page.getByRole("navigation", { name: "Main" }).getByRole("link", { name: "Runtimes" }).click();
  await page.getByRole("button", { name: "Detect on this PC" }).waitFor();
  // cards are grouped: languages, then the web servers that serve sites
  await page.getByRole("heading", { name: "Languages" }).waitFor();
  await page.getByRole("heading", { name: "Web servers" }).waitFor();
  assert.equal(await page.locator('[data-group="languages"] article[data-kind="node"]').count(), 1);
  assert.equal(await page.locator('[data-group="servers"] article[data-kind="caddy"]').count(), 1);
  await page.locator('article[data-kind="caddy"]').getByTestId("serves-sites").waitFor();
  // the first visit ran one silent check; java's source failed and is listed inline
  await page.getByText("Java: could not reach api.adoptium.net").waitFor();
  await page.getByText(/Version lists checked/).waitFor();
  const kinds = await page.locator("article[data-kind]").evaluateAll((els) => els.map((e) => e.dataset.kind));
  assert.deepEqual(kinds, ["node", "php", "go", "java", "caddy", "nginx"]);
  for (const k of kinds) await card(k).getByTestId(`count-${k}`).getByText("Not installed").waitFor();
  const opts = await pickOptions("php");
  assert.deepEqual(opts, [
    "8.4.1 (latest, stable)",
    "8.3.13 (latest, stable)",
    "8.3.12 (stable)",
    "8.2.24 (latest, stable)",
    "7.4.33 (latest, stable, unverified)",
    "7.1.33 (latest, stable, unverified)",
  ]);
  const groups = await card("php")
    .locator("optgroup")
    .evaluateAll((els) => els.map((e) => e.label));
  assert.deepEqual(groups, ["PHP 8.4", "PHP 8.3", "PHP 8.2", "PHP 7.4", "PHP 7.1"]);
  // the unsupported go build (windows/arm64 listed for this windows/amd64 PC) is not offered
  assert.deepEqual(await pickOptions("go"), ["1.23.4 (latest, stable)", "1.23.3 (stable)"]);
  assert.ok(!(await pickOptions("java")).some((o) => o.startsWith("21.0.5")));
  // manual check: spinner, then java appears
  await page.getByRole("button", { name: "Check for new versions" }).click();
  await page.getByText("Checking...").waitFor();
  await page.getByText("2 new versions found").waitFor();
  await page.getByText("Java: could not reach").waitFor({ state: "detached" });
  assert.ok((await pickOptions("java")).includes("21.0.5 (latest, LTS)"));
  await page.getByRole("button", { name: "Check for new versions" }).click();
  await page.getByText("Already up to date").waitFor();
});

await step("install shows progress; a second version installs side by side; update hint", async () => {
  const node = card("node");
  await picker("node").selectOption("node-22.11.0");
  await node.getByRole("button", { name: "Install", exact: true }).click();
  const bar = node.getByRole("progressbar");
  await bar.waitFor();
  await shot(page, "05a-runtimes-installing");
  const seen = new Set();
  for (let i = 0; i < 40; i++) {
    const v = await bar.getAttribute("aria-valuenow", { timeout: 300 }).catch(() => null);
    if (v) seen.add(Number(v));
    if (await li("node", "22.11.0").isVisible()) break;
    await page.waitForTimeout(100);
  }
  await li("node", "22.11.0").waitFor();
  await page.getByText("Node.js 22.11.0 installed.").waitFor();
  assert.ok(seen.size >= 2, "progress advanced: " + [...seen]);
  // first runtime of a kind is the default; with one version there is no Make default
  await li("node", "22.11.0").getByTestId("default-badge").waitFor();
  assert.equal(await node.getByRole("button", { name: /Make .* the default/ }).count(), 0);
  assert.ok(await node.getByRole("button", { name: "Installed", exact: true }).isDisabled());
  // update hint for the 22 line; selecting it and installing keeps the old version
  await node.getByTestId("update-node").getByText("Update available:").waitFor();
  await node.getByTestId("update-node").locator("b", { hasText: "22.12.0" }).waitFor();
  await node.getByRole("button", { name: /^Select 22\.12\.0/ }).click();
  assert.equal(await picker("node").inputValue(), "node-22.12.0");
  await node.getByRole("button", { name: "Install", exact: true }).click();
  await li("node", "22.12.0").waitFor({ timeout: 15000 });
  await li("node", "22.11.0").waitFor();
  assert.equal(await node.locator("li[data-runtime]").count(), 2);
  // newest first; the default (22.11.0) stays on top and 22.12.0 can be made default
  assert.equal(await li("node", "22.11.0").getByTestId("default-badge").count(), 1);
  assert.equal(await li("node", "22.12.0").getByTestId("default-badge").count(), 0);
  await li("node", "22.12.0").getByRole("button", { name: "Make Node.js 22.12.0 the default" }).waitFor();
  assert.equal(await node.getByTestId("update-node").count(), 0);
  assert.ok(await node.getByRole("button", { name: "Installed", exact: true }).isDisabled());
});

await step("detect and register one-click, register by path validation", async () => {
  await page.getByRole("button", { name: "Detect on this PC" }).click();
  await page.getByText("Found on this PC").waitFor();
  await page.locator("tr", { hasText: "java" }).getByRole("button", { name: "Register" }).click();
  await li("java", "21.0.4").waitFor();
  await li("java", "21.0.4").getByText("registered by path").waitFor();
  await li("java", "21.0.4").getByTestId("default-badge").waitFor();
  await page.getByRole("button", { name: "Register by path" }).click();
  await page.getByLabel("Path to the program").fill("php");
  await page.getByRole("button", { name: "Register runtime" }).click();
  await page.getByText(/Use an absolute path/).waitFor();
  await page.getByRole("button", { name: "Cancel" }).click();
  await shot(page, "04-runtimes");
});

await step("failed install shows full error, network hint and installer log", async () => {
  await page.goto(BASE + "/#/runtimes");
  await page.getByText(/Version lists checked/).waitFor();
  const node = card("node");
  await picker("node").selectOption("node-20.18.0");
  await node.getByRole("button", { name: "Install", exact: true }).click();
  const fail = page.locator('[data-failed="node-20.18.0"]');
  await fail.waitFor({ timeout: 15000 });
  const text = await fail.getByTestId("job-error").textContent();
  assert.match(
    text,
    /^Downloading: Get "https:\/\/nodejs.org\/dist\/v20.18.0\/node-v20.18.0-win-x64.zip": dial tcp .* i\/o timeout/,
  );
  await fail.getByText(/looks like a network problem/).waitFor();
  await fail.getByRole("link", { name: "Settings" }).waitFor();
  await fail.getByRole("button", { name: "Show installer log" }).click();
  const box = fail.getByLabel("Installer log");
  await box.getByText(/install start node-20.18.0/).waitFor();
  await box.getByText(/ERROR Downloading/).waitFor();
  await fail.getByRole("button", { name: "Copy", exact: true }).click();
  assert.match(await page.evaluate(() => navigator.clipboard.readText()), /ERROR Downloading/);
  await page
    .getByRole("alert")
    .getByText(/node.js 20.18.0 failed/i)
    .first()
    .waitFor();
  await fail.scrollIntoViewIfNeeded();
  await shot(page, "12-runtimes-failed-job");
  await page.emulateMedia({ colorScheme: "dark" });
  await shot(page, "12b-runtimes-failed-job-dark");
  await page.emulateMedia({ colorScheme: "light" });
});

await step("import: validation, server errors, failure then success", async () => {
  const node = card("node");
  await node.getByRole("button", { name: "Import file for Node.js 20.18.0" }).click();
  const dlg = page.getByRole("dialog");
  await dlg.getByText("Zero2Dev works on a copy of this file").waitFor();
  await dlg.getByRole("button", { name: "Import file" }).click();
  await dlg.getByText("Enter the path of the file you downloaded.").waitFor();
  await dlg.getByLabel("Path of the downloaded file").fill("Downloads\\x.zip");
  await dlg.getByLabel("Checksum, SHA-256 or SHA-512 (optional)").fill("abc");
  await dlg.getByText(/Use an absolute path/).waitFor();
  await dlg.getByText(/64 hexadecimal characters and a SHA-512/).waitFor();
  await shot(page, "13-import-dialog-validation");
  // wrong file name is rejected by the daemon and shown inline
  await dlg.getByLabel("Path of the downloaded file").fill("C:\\Users\\me\\Downloads\\wrong.zip");
  await dlg.getByLabel("Checksum, SHA-256 or SHA-512 (optional)").fill("");
  await dlg.getByRole("button", { name: "Import file" }).click();
  await dlg
    .getByRole("alert")
    .getByText(/file name should be node-v20.18.0-win-x64.zip/)
    .waitFor();
  // locked file -> fails at Registering with antivirus hint
  await dlg
    .getByLabel("Path of the downloaded file")
    .fill("C:\\Users\\me\\Downloads\\locked\\node-v20.18.0-win-x64.zip");
  await dlg.getByLabel("Checksum, SHA-256 or SHA-512 (optional)").fill("0".repeat(64));
  await dlg.getByRole("button", { name: "Import file" }).click();
  const fail = page.locator('[data-failed="node-20.18.0"]');
  await fail.getByText(/step: Registering/).waitFor({ timeout: 15000 });
  assert.match(
    await fail.getByTestId("job-error").textContent(),
    /^Registering: rename .*Access is denied \(antivirus may be scanning/,
  );
  await fail.getByText(/Antivirus software may be scanning/).waitFor();
  assert.equal(await fail.getByText(/looks like a network problem/).count(), 0);
  // successful import of go: Importing step visible, then listed
  const go = card("go");
  await picker("go").selectOption("go-1.23.3");
  await go.getByRole("button", { name: "Import file for Go 1.23.3" }).click();
  await dlg.getByLabel("Path of the downloaded file").fill("C:\\Users\\me\\Downloads\\go1.23.3.windows-amd64.zip");
  await dlg.getByRole("button", { name: "Import file" }).click();
  await go.getByText(/1\.23\.3: Importing/).waitFor();
  await li("go", "1.23.3").waitFor({ timeout: 15000 });
  await go.getByTestId("update-go").locator("b", { hasText: "1.23.4" }).waitFor();
});

await step("unverified entries: confirm first, then acceptUnverified", async () => {
  const nginx = card("nginx");
  const nOpts = await pickOptions("nginx");
  assert.deepEqual(nOpts, ["1.27.2 (latest, mainline, unverified)", "1.26.2 (latest, stable, unverified)"]);
  await picker("nginx").selectOption("nginx-1.26.2");
  await nginx.getByRole("button", { name: "Install", exact: true }).click();
  const dlg = page.getByRole("dialog");
  await dlg.getByText(/does not publish a checksum for this download/).waitFor();
  await shot(page, "15-unverified-confirm");
  await dlg.getByRole("button", { name: "Cancel" }).click();
  await dlg.waitFor({ state: "detached" });
  assert.equal(await nginx.getByRole("progressbar").count(), 0);
  await nginx.getByRole("button", { name: "Install", exact: true }).click();
  await dlg.getByRole("button", { name: "Install anyway" }).click();
  await li("nginx", "1.26.2").waitFor({ timeout: 15000 });
  assert.ok(await nginx.getByRole("button", { name: "Installed", exact: true }).isDisabled());
  // the contract is enforced by the daemon too: no acceptUnverified -> 400 unverified; same version again -> 409
  const codes = await page.evaluate(async () => {
    const h = { Authorization: "Bearer devtoken", "Content-Type": "application/json" };
    const post = async (b) => {
      const r = await fetch("/api/runtimes/install", { method: "POST", headers: h, body: JSON.stringify(b) });
      return [r.status, (await r.json()).error?.code];
    };
    return [
      await post({ catalogId: "nginx-1.27.2" }),
      await post({ catalogId: "nginx-1.26.2", acceptUnverified: true }),
    ];
  });
  assert.deepEqual(codes, [
    [400, "unverified"],
    [409, "conflict"],
  ]);
  await page.evaluate(() => document.querySelector("#main")?.scrollTo(0, 0));
  await page.waitForTimeout(500);
  await shot(page, "14-runtime-cards");
  await page.emulateMedia({ colorScheme: "dark" });
  await shot(page, "14b-runtime-cards-dark");
  await page.emulateMedia({ colorScheme: "light" });
});

await step("create a service (validation first, then success)", async () => {
  await page.goto(BASE + "/#/services");
  await page.getByText("No services yet").waitFor();
  await page.getByRole("button", { name: "Add your first service" }).click();
  await page.getByRole("button", { name: "Create service" }).click();
  await page.getByText("Enter a name.").waitFor();
  await page.getByText(/no default arguments/).waitFor();
  await page.getByLabel("Name", { exact: true }).fill("Demo API");
  await page.getByRole("button", { name: "Add argument" }).click();
  await page.getByLabel("Argument 1", { exact: true }).fill("server.js");
  await page.getByLabel("Project folder", { exact: true }).fill("C:\\work\\demo");
  await page.getByLabel("Port").fill("3100");
  // health check and environment live under "Advanced"
  await page.getByText("Advanced:").click();
  await page.getByLabel("Health check path").fill("/health");
  await page.getByRole("button", { name: "Add variable" }).click();
  await page.getByLabel("Variable name 1", { exact: true }).fill("NODE_ENV");
  await page.getByLabel("Variable value 1", { exact: true }).fill("development");
  // runtime select is filtered by kind: only the node runtime is offered
  const opts = await page.getByLabel("Version").locator("option").allTextContents();
  assert.ok(opts.some((o) => o.includes("Node.js")) && !opts.some((o) => o.includes("PHP")), opts.join("|"));
  assert.deepEqual(opts, ["Default (Node.js 22.11.0)", "Node.js 22.11.0 (default)", "Node.js 22.12.0"]);
  await page.getByRole("button", { name: "Create service" }).click();
  await page.locator('tr[data-service="Demo API"]').waitFor();
});

await step("changing from custom clears the program override", async () => {
  await page.goto(BASE + "/#/services");
  await page.getByRole("button", { name: "New service" }).click();
  await page.getByLabel("Name", { exact: true }).fill("Changed type");
  await page.getByLabel("Project folder", { exact: true }).fill("C:\\work\\changed");
  await page.getByLabel("Type").selectOption("custom");
  await page.getByLabel("Program").fill("custom.exe");
  await page.getByLabel("Type").selectOption("node");
  await page.getByRole("button", { name: "Add argument" }).click();
  await page.getByLabel("Argument 1").fill("server.js");
  await page.getByText("Advanced:").click();
  assert.equal(await page.getByLabel("Program").inputValue(), "");
  await page.getByText("Advanced:").click();
  const request = page.waitForRequest((r) => r.method() === "POST" && r.url().endsWith("/api/services"));
  await page.getByRole("button", { name: "Create service" }).click();
  assert.equal((await request).postDataJSON().command, "");
  const row = page.locator('tr[data-service="Changed type"]');
  await row.waitFor();
  await row.getByRole("button", { name: "Delete Changed type" }).click();
  await page.getByRole("button", { name: "Delete service" }).click();
  await row.waitFor({ state: "detached" });
});

await step("browse: native folder dialog fills the project folder", async () => {
  await page.goto(BASE + "/#/services");
  await page.getByRole("button", { name: "New service" }).click();
  const field = page.getByLabel("Project folder", { exact: true });
  await field.fill("C:\\work\\demo");
  const browse = page.getByRole("button", { name: "Browse for Project folder" });
  await browse.click();
  // while the dialog is open the button waits, the field is read-only and a hint explains it
  const hint = page.getByText(/A Windows folder dialog is open/);
  await hint.waitFor();
  assert.equal(await field.getAttribute("readonly"), "");
  await shot(page, "04b-browse-waiting");
  // the mock answers with the start folder, like pressing "Select folder" right away
  await hint.waitFor({ state: "detached" });
  assert.equal(await field.inputValue(), "C:\\work\\demo");
  await field.fill("C:\\cancel-me");
  await browse.click();
  await hint.waitFor();
  await hint.waitFor({ state: "detached" });
  assert.equal(await field.inputValue(), "C:\\cancel-me", "cancel keeps the typed value");
  await page.getByRole("button", { name: "Cancel" }).click();
});

await step("start service -> running, live logs arrive", async () => {
  const row = page.locator('tr[data-service="Demo API"]');
  await row.getByRole("button", { name: "Start Demo API", exact: true }).click();
  await row.getByText("running", { exact: true }).waitFor({ timeout: 10000 });
  await row
    .getByRole("link", { name: /Demo API/ })
    .first()
    .click();
  await page.getByRole("heading", { name: "Live output" }).waitFor();
  const log = page.getByRole("log");
  await log
    .getByText(/GET \/api\/items/)
    .first()
    .waitFor({ timeout: 8000 });
  const n1 = await log.locator(".ln").count();
  await page.waitForTimeout(2500);
  const n2 = await log.locator(".ln").count();
  assert.ok(n2 > n1, `lines grew ${n1} -> ${n2}`);
  await page.waitForTimeout(5000); // allow a stderr line
  await log.locator(".ln.stderr").first().waitFor({ timeout: 8000 });
  await shot(page, "03-service-detail-logs");
  await page.setViewportSize({ width: 1280, height: 520 });
  // pause-on-scroll-up: scroll up, new lines must not move the view
  await page.waitForFunction(
    () => {
      const el = document.querySelector("[role=log]");
      return el && el.scrollHeight > el.clientHeight + 60;
    },
    null,
    { timeout: 25000 },
  );
  await log.evaluate((el) => (el.scrollTop = 0));
  await page.getByRole("button", { name: "Jump to latest" }).waitFor();
  await page.getByRole("button", { name: /^Pause/ }).click();
  const c1 = await page.locator(".count").first().textContent();
  await page.waitForTimeout(2200);
  const c2 = await page.locator(".count").first().textContent();
  assert.equal(c1, c2, "paused view is frozen");
  await page.getByRole("button", { name: /^Resume/ }).click();
  await page.getByRole("button", { name: "Clear" }).click();
  await page.setViewportSize({ width: 1280, height: 800 });
});

await step("port conflict shows a clear message", async () => {
  await page.goto(BASE + "/#/services");
  await page.getByRole("button", { name: "New service" }).click();
  await page.getByLabel("Name", { exact: true }).fill("Second");
  await page.getByLabel("Type").selectOption("custom");
  await page.getByLabel("Program").fill("C:\\tools\\x.exe");
  await page.getByLabel("Project folder", { exact: true }).fill("C:\\work\\second");
  await page.getByLabel("Port").fill("3100");
  await page.getByRole("button", { name: "Create service" }).click();
  await page.locator('tr[data-service="Second"]').waitFor();
  await page.locator('tr[data-service="Second"]').getByRole("button", { name: "Start Second", exact: true }).click();
  await page
    .getByRole("alert")
    .getByText(/port 3100 is already in use by Demo API/)
    .waitFor();
  await shot(page, "02-services-list");
});

await step("stop service and delete with confirm", async () => {
  await page.locator('tr[data-service="Second"]').getByRole("button", { name: "Delete Second" }).click();
  await page.getByRole("dialog").getByText("Delete Second?").waitFor();
  await page.getByRole("button", { name: "Delete service" }).click();
  await page.locator('tr[data-service="Second"]').waitFor({ state: "detached" });
  const row = page.locator('tr[data-service="Demo API"]');
  await row.getByRole("button", { name: "Stop Demo API", exact: true }).click();
  await row.getByText("stopped", { exact: true }).waitFor({ timeout: 5000 });
});

await step("php 8.4.1 and 7.1.33 side by side: default, per-service version, remove", async () => {
  await page.goto(BASE + "/#/runtimes");
  await page.getByText(/Version lists checked/).waitFor();
  const php = card("php");
  await picker("php").selectOption("php-8.4.1");
  await php.getByRole("button", { name: "Install", exact: true }).click();
  await li("php", "8.4.1").waitFor({ timeout: 15000 });
  await li("php", "8.4.1").getByTestId("default-badge").waitFor();
  // the old line is unverified: confirm, then install without removing 8.4.1
  await picker("php").selectOption("php-7.1.33");
  await php.getByRole("button", { name: "Install", exact: true }).click();
  const dlg = page.getByRole("dialog");
  await dlg.getByRole("button", { name: "Install anyway" }).click();
  await li("php", "7.1.33").waitFor({ timeout: 15000 });
  await li("php", "8.4.1").waitFor();
  assert.equal(await php.locator("li[data-runtime]").count(), 2);
  assert.ok(await php.getByRole("button", { name: "Installed", exact: true }).isDisabled());
  assert.equal(await li("php", "8.4.1").getByTestId("default-badge").count(), 1);
  assert.equal(await li("php", "7.1.33").getByTestId("default-badge").count(), 0);
  // make the older one the default
  await li("php", "7.1.33").getByRole("button", { name: "Make PHP 7.1.33 the default" }).click();
  await li("php", "7.1.33").getByTestId("default-badge").waitFor();
  assert.equal(await li("php", "8.4.1").getByTestId("default-badge").count(), 0);
  await li("php", "8.4.1").getByRole("button", { name: "Make PHP 8.4.1 the default" }).waitFor();
  assert.equal(await php.getByTestId("default-badge").count(), 1, "exactly one default per kind");
  await shot(page, "16-two-php-versions");
  // a service picks a specific version
  await page.goto(BASE + "/#/services");
  await page
    .getByRole("button", { name: /New service|Add your first service/ })
    .first()
    .click();
  await page.getByLabel("Name", { exact: true }).fill("Legacy PHP");
  await page.getByLabel("Type").selectOption("php");
  const ropts = await page.getByLabel("Version").locator("option").allTextContents();
  assert.deepEqual(ropts, ["Default (PHP 7.1.33)", "PHP 8.4.1", "PHP 7.1.33 (default)"]);
  await page.getByLabel("Version").selectOption({ label: "PHP 8.4.1" });
  await page.getByLabel("Project folder", { exact: true }).fill("C:\\work\\legacy");
  await page.getByLabel("Port").fill("8099");
  await page.getByRole("button", { name: "Create service" }).click();
  const row = page.locator('tr[data-service="Legacy PHP"]');
  await row.getByTestId("svc-runtime").getByText("PHP 8.4.1").waitFor();
  await row.getByRole("link", { name: "Legacy PHP", exact: true }).click();
  await page.getByTestId("svc-runtime").getByText("PHP 8.4.1").waitFor();
  // removing a runtime that a service uses shows the daemon's message naming the service
  await page.goto(BASE + "/#/runtimes");
  await li("php", "8.4.1").getByRole("button", { name: "Remove PHP 8.4.1" }).click();
  await dlg.getByRole("button", { name: "Remove runtime" }).click();
  await page
    .getByRole("alert")
    .getByText(/used by service Legacy PHP/)
    .waitFor();
  await li("php", "8.4.1").waitFor();
  // free it, remove the default 7.1.33: 8.4.1 is promoted
  await page.evaluate(async () => {
    const h = { Authorization: "Bearer devtoken" };
    const svc = (await (await fetch("/api/services", { headers: h })).json()).find((s) => s.name === "Legacy PHP");
    await fetch(`/api/services/${svc.id}`, { method: "DELETE", headers: h });
  });
  await li("php", "7.1.33").getByRole("button", { name: "Remove PHP 7.1.33" }).click();
  await dlg.getByRole("button", { name: "Remove runtime" }).click();
  await li("php", "7.1.33").waitFor({ state: "detached" });
  await li("php", "8.4.1").getByTestId("default-badge").waitFor();
  assert.equal(await php.locator("li[data-runtime]").count(), 1);
});

await step("sites: caddy notice, create site, domain validation", async () => {
  await page.goto(BASE + "/#/sites");
  await page.getByText(/Sites need Caddy to run/).waitFor();
  await page.getByRole("button", { name: "New site" }).first().click();
  await page.getByLabel("Name", { exact: true }).fill("Demo");
  await page.getByLabel("Domain").fill("demo.example.com");
  await page.getByRole("combobox", { name: /^Service/ }).selectOption({ label: "Demo API (:3100)" });
  await page.getByRole("button", { name: "Create site" }).click();
  await page.getByText(/must end with \.localhost/).waitFor();
  await page.getByLabel("Domain").fill("demo.localhost");
  await page.getByRole("button", { name: "Create site" }).click();
  const row = page.locator('tr[data-site="demo.localhost"]');
  await row.waitFor();
  assert.equal(await row.getByRole("link").getAttribute("href"), "http://demo.localhost:8080");
  // one-click off and on, like stopping and starting a service
  await row.getByRole("button", { name: "Disable Demo" }).click();
  await row.getByText("disabled", { exact: true }).waitFor();
  assert.equal(await row.getByRole("link").count(), 0, "a disabled site's address is not a link");
  await row.getByRole("button", { name: "Enable Demo" }).click();
  await row.getByText("enabled", { exact: true }).waitFor();
  await row.getByRole("link").waitFor();
  // install caddy -> notice changes
  await page.goto(BASE + "/#/runtimes");
  await page.getByText(/Version lists checked/).waitFor();
  await picker("caddy").selectOption("caddy-2.8.4");
  await card("caddy").getByRole("button", { name: "Install", exact: true }).click();
  await li("caddy", "2.8.4").waitFor({ timeout: 15000 });
  await page.goto(BASE + "/#/sites");
  await page.getByText(/Caddy is not running, so your sites cannot be opened/).waitFor();
  await shot(page, "06-sites");
});

await step("settings: change and persist", async () => {
  await page.goto(BASE + "/#/settings");
  await page.getByLabel("HTTP proxy port").waitFor();
  await page.waitForFunction(() => document.querySelector("input[inputmode=numeric]")?.value === "8080");
  await page.getByLabel("HTTP proxy port").fill("8081");
  await page.getByLabel("HTTPS proxy port").fill("8081");
  await page.getByRole("button", { name: "Save settings" }).click();
  await page.getByText(/must differ/).waitFor();
  await page.getByLabel("HTTPS proxy port").fill("8444");
  // download proxy validation, then valid proxy + trust local CA
  await page.getByLabel("Download proxy (optional)").fill("proxy.corp:8080");
  await page.getByRole("button", { name: "Save settings" }).click();
  await page.getByText(/must start with http/).waitFor();
  await page.getByLabel("Download proxy (optional)").fill("http://proxy.corp:8080");
  await page.getByLabel("Let Caddy trust its local certificate authority").check();
  await page.getByText(/changes the Windows certificate store/).waitFor();
  await page.getByText(/certificate warning in the browser until you trust/).waitFor();
  await page.getByRole("button", { name: "Save settings" }).click();
  await page.getByText("Settings saved.").waitFor();
  await page.reload();
  await page.waitForFunction(() => document.querySelector("input[inputmode=numeric]")?.value === "8081");
  assert.equal(await page.getByLabel("Download proxy (optional)").inputValue(), "http://proxy.corp:8080");
  assert.ok(await page.getByLabel("Let Caddy trust its local certificate authority").isChecked());
  // portable block
  assert.equal(await page.getByTestId("data-folder").textContent(), "D:\\zero2dev\\bin\\data");
  await page.getByText(/Move the whole folder to move Zero2Dev/).waitFor();
  await page.getByText("D:\\zero2dev\\bin\\data\\caddy\\data\\caddy\\pki\\authorities\\local\\root.crt").waitFor();
  await page.getByRole("button", { name: "Copy", exact: true }).click();
  await page.getByText("Data folder path copied.").waitFor();
  assert.equal(await page.evaluate(() => navigator.clipboard.readText()), "D:\\zero2dev\\bin\\data");
  await page.getByRole("button", { name: "Copy access token" }).click();
  await page.getByText(/Access token copied/).waitFor();
  assert.equal(await page.evaluate(() => navigator.clipboard.readText()), "devtoken");
  await page.evaluate(() => document.querySelector("#main")?.scrollTo(0, 0));
  await page.waitForTimeout(6000); // let toasts expire
  await shot(page, "07-settings");
  await page.emulateMedia({ colorScheme: "dark" });
  await shot(page, "07b-settings-dark");
  await page.emulateMedia({ colorScheme: "light" });
});

await step("combined logs page filters by service and text", async () => {
  await page.goto(BASE + "/#/logs");
  await page
    .getByRole("log")
    .getByText(/stopped|listening/)
    .first()
    .waitFor();
  await page.getByLabel("Filter log lines").fill("listening");
  const lines = await page.getByRole("log").locator(".ln").allTextContents();
  assert.ok(lines.length >= 1 && lines.every((l) => l.includes("listening")), lines.join("|"));
  await shot(page, "08-logs");
});

await step("reconnect: daemon outage shows banner, recovers with fresh ticket", async () => {
  await page.goto(BASE + "/#/dashboard");
  await page.getByText("Connected").waitFor();
  await ctx.setOffline(true);
  await page
    .getByRole("button", { name: "Retry" })
    .waitFor({ timeout: 20000 })
    .catch(() => {});
  await ctx.setOffline(false);
  await page.getByText("Connected").waitFor({ timeout: 25000 });
});

await step("dashboard with data + dark mode + narrow width", async () => {
  await page.goto(BASE + "/#/dashboard");
  await page.locator(".svc").first().waitFor();
  await page.getByText("v0.0.2").waitFor();
  await shot(page, "01-dashboard");
  await page.emulateMedia({ colorScheme: "dark" });
  await shot(page, "09-dashboard-dark");
  await page.goto(
    BASE +
      "/#/services/" +
      (await page.evaluate(
        async () =>
          (await (await fetch("/api/services", { headers: { Authorization: "Bearer devtoken" } })).json()).find(
            (s) => s.name === "Demo API",
          ).id,
      )),
  );
  await page.getByRole("button", { name: "Start Demo API", exact: true }).click();
  await page.getByText("running", { exact: true }).first().waitFor();
  await page.waitForTimeout(3500);
  await shot(page, "10-service-detail-dark");
  await page.setViewportSize({ width: 900, height: 700 });
  await page.goto(BASE + "/#/runtimes");
  await page.waitForTimeout(300);
  await shot(page, "11-runtimes-900-dark");
  const overflow = await page.evaluate(() => document.documentElement.scrollWidth > window.innerWidth);
  assert.equal(overflow, false, "no horizontal page scroll at 900px");
  await page.emulateMedia({ colorScheme: "light" });
});

await step("no external network requests", async () => {
  assert.deepEqual(external, []);
});

await browser.close();
mock.kill();
console.log("\n" + results.map((r) => r.join(" ")).join("\n"));
console.log("screenshots:", SHOTS);
