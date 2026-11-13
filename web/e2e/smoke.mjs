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
// The service editor's type cards are radio buttons named after the type.
const pickType = (label) => page.getByRole("radio", { name: label, exact: true }).click();
const advanced = () => page.locator("summary", { hasText: "Advanced" }).click();
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

// Runtime rows are one line until opened; expand() opens one (Manage / Install...).
const expand = async (kind) => {
  const b = page.locator(`article[data-kind="${kind}"] button.toggle`);
  if ((await b.getAttribute("aria-expanded")) !== "true") await b.click();
};
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
  // prettier-ignore
  assert.deepEqual(kinds, [
    "node", "php", "go", "java", // languages
    "caddy", "nginx", // web servers
    "mariadb", "mysql", "postgres", "redis", // databases
    "vscode", "vscodium", "notepadpp", // code editors
    "heidisql", "dbeaver", // database tools
  ]);
  for (const k of kinds) await card(k).getByTestId(`count-${k}`).getByText("Not installed").waitFor();
  await expand("php");
  const opts = await pickOptions("php");
  assert.deepEqual(opts, [
    "8.4.1 (latest, stable)",
    "8.3.13 (latest, stable)",
    "8.3.12 (stable)",
    "8.2.24 (latest, stable)",
    "7.4.33 (latest, stable, unverified)",
    "7.1.33 (latest, stable, unverified)",
  ]);
  await expand("php");
  const groups = await card("php")
    .locator("optgroup")
    .evaluateAll((els) => els.map((e) => e.label));
  assert.deepEqual(groups, ["PHP 8.4", "PHP 8.3", "PHP 8.2", "PHP 7.4", "PHP 7.1"]);
  // the unsupported go build (windows/arm64 listed for this windows/amd64 PC) is not offered
  await expand("go");
  assert.deepEqual(await pickOptions("go"), ["1.23.4 (latest, stable)", "1.23.3 (stable)"]);
  await expand("java");
  assert.ok(!(await pickOptions("java")).some((o) => o.startsWith("21.0.5")));
  // manual check: spinner, then java appears
  await page.getByRole("button", { name: "Check for new versions" }).click();
  await page.getByText("Checking...").waitFor();
  await page.getByText("2 new versions found").waitFor();
  await page.getByText("Java: could not reach").waitFor({ state: "detached" });
  await expand("java");
  assert.ok((await pickOptions("java")).includes("21.0.5 (latest, LTS)"));
  await page.getByRole("button", { name: "Check for new versions" }).click();
  await page.getByText("Already up to date").waitFor();
});

await step("install shows progress; a second version installs side by side; update hint", async () => {
  await expand("node");
  const node = card("node");
  await expand("node");
  await picker("node").selectOption("node-22.11.0");
  await node.getByRole("button", { name: "Install", exact: true }).click();
  const bar = node.getByRole("progressbar");
  await bar.waitFor();
  await shot(page, "05a-runtimes-installing");
  const seen = new Set();
  for (let i = 0; i < 40; i++) {
    const v = await bar.getAttribute("aria-valuenow", { timeout: 300 }).catch(() => null);
    if (v) seen.add(Number(v));
    await expand("node");
    if (await li("node", "22.11.0").isVisible()) break;
    await page.waitForTimeout(100);
  }
  await expand("node");
  await li("node", "22.11.0").waitFor();
  await page.getByText("Node.js 22.11.0 installed.").waitFor();
  assert.ok(seen.size >= 2, "progress advanced: " + [...seen]);
  // first runtime of a kind is the default; with one version there is no Make default
  await expand("node");
  await li("node", "22.11.0").getByTestId("default-badge").waitFor();
  assert.equal(await node.getByRole("button", { name: /Make .* the default/ }).count(), 0);
  assert.ok(await node.getByRole("button", { name: "Installed", exact: true }).isDisabled());
  // update hint for the 22 line; selecting it and installing keeps the old version
  await node.getByTestId("update-node").getByText("Update available:").waitFor();
  await node.getByTestId("update-node").locator("b", { hasText: "22.12.0" }).waitFor();
  await node.getByRole("button", { name: /^Select 22\.12\.0/ }).click();
  await expand("node");
  assert.equal(await picker("node").inputValue(), "node-22.12.0");
  await node.getByRole("button", { name: "Install", exact: true }).click();
  await expand("node");
  await li("node", "22.12.0").waitFor({ timeout: 15000 });
  await expand("node");
  await li("node", "22.11.0").waitFor();
  assert.equal(await node.locator("li[data-runtime]").count(), 2);
  // newest first; the default (22.11.0) stays on top and 22.12.0 can be made default
  await expand("node");
  assert.equal(await li("node", "22.11.0").getByTestId("default-badge").count(), 1);
  await expand("node");
  assert.equal(await li("node", "22.12.0").getByTestId("default-badge").count(), 0);
  await expand("node");
  await li("node", "22.12.0").getByRole("button", { name: "Make Node.js 22.12.0 the default" }).waitFor();
  assert.equal(await node.getByTestId("update-node").count(), 0);
  assert.ok(await node.getByRole("button", { name: "Installed", exact: true }).isDisabled());
});

await step("detect and register one-click, register by path validation", async () => {
  await page.getByRole("button", { name: "Detect on this PC" }).click();
  await page.getByText("Found on this PC").waitFor();
  await page.locator("tr", { hasText: "java" }).getByRole("button", { name: "Register" }).click();
  await expand("java");
  await li("java", "21.0.4").waitFor();
  await expand("java");
  await li("java", "21.0.4").getByText("registered by path").waitFor();
  await expand("java");
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
  await expand("node");
  const node = card("node");
  await expand("node");
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
  await expand("node");
  const node = card("node");
  await node.getByRole("button", { name: "Import file for Node.js 20.18.0" }).click();
  const dlg = page.getByRole("dialog");
  await dlg.getByText("Zero2Dev works on a copy of this file").waitFor();
  await dlg.getByRole("button", { name: "Import file" }).click();
  await dlg.getByText("Enter the path of the file you downloaded.").waitFor();
  await dlg.getByLabel("Path of the downloaded file").fill("Downloads\\x.zip");
  await dlg.getByLabel("Checksum, SHA-256, SHA-512 or MD5 (optional)").fill("abc");
  await dlg.getByText(/Use an absolute path/).waitFor();
  await dlg
    .getByText("Paste an MD5 (32), SHA-256 (64) or SHA-512 (128) hexadecimal checksum", { exact: false })
    .waitFor();
  await shot(page, "13-import-dialog-validation");
  // wrong file name is rejected by the daemon and shown inline
  await dlg.getByLabel("Path of the downloaded file").fill("C:\\Users\\me\\Downloads\\wrong.zip");
  await dlg.getByLabel("Checksum, SHA-256, SHA-512 or MD5 (optional)").fill("");
  await dlg.getByRole("button", { name: "Import file" }).click();
  await dlg
    .getByRole("alert")
    .getByText(/file name should be node-v20.18.0-win-x64.zip/)
    .waitFor();
  // locked file -> fails at Registering with antivirus hint
  await dlg
    .getByLabel("Path of the downloaded file")
    .fill("C:\\Users\\me\\Downloads\\locked\\node-v20.18.0-win-x64.zip");
  await dlg.getByLabel("Checksum, SHA-256, SHA-512 or MD5 (optional)").fill("0".repeat(64));
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
  await expand("go");
  const go = card("go");
  await expand("go");
  await picker("go").selectOption("go-1.23.3");
  await go.getByRole("button", { name: "Import file for Go 1.23.3" }).click();
  await dlg.getByLabel("Path of the downloaded file").fill("C:\\Users\\me\\Downloads\\go1.23.3.windows-amd64.zip");
  await dlg.getByRole("button", { name: "Import file" }).click();
  await go.getByText(/1\.23\.3: Importing/).waitFor();
  await expand("go");
  await li("go", "1.23.3").waitFor({ timeout: 15000 });
  await go.getByTestId("update-go").locator("b", { hasText: "1.23.4" }).waitFor();
});

await step("unverified entries: confirm first, then acceptUnverified", async () => {
  await expand("nginx");
  const nginx = card("nginx");
  await expand("nginx");
  const nOpts = await pickOptions("nginx");
  assert.deepEqual(nOpts, ["1.27.2 (latest, mainline, unverified)", "1.26.2 (latest, stable, unverified)"]);
  await expand("nginx");
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
  await expand("nginx");
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
  // the editor is a page; its first step is the type
  await page.getByRole("heading", { name: "What do you want to run?" }).waitFor();
  assert.equal(
    await page.getByRole("button", { name: "Create service" }).count(),
    0,
    "nothing to create before a type is chosen",
  );
  await shot(page, "04a-service-type");
  await pickType("Node.js");
  await page.getByTestId("chosen-type").getByText("Node.js").waitFor();
  await page.getByRole("button", { name: "Create service" }).click();
  await page.getByText("Enter a name.").waitFor();
  await page.getByText("Script is required").waitFor();
  await page.getByLabel("Name", { exact: true }).fill("Demo API");
  // every setting explains itself and shows its default
  await page.locator('[data-option="watch"]').getByText("--watch").waitFor();
  await page.getByLabel("Script", { exact: true }).fill("server.js");
  await page.getByLabel("Project folder", { exact: true }).fill("C:\\work\\demo");
  await page.getByText(/Your app's code, with package.json/).waitFor();
  await page.getByLabel("Port").fill("3100");
  await page
    .getByTestId("preview-command")
    .getByText(/node\.exe server\.js/)
    .waitFor();
  // an npm script runs npm from the same Node folder
  await page.getByLabel("Run", { exact: true }).selectOption("npm");
  await page
    .getByTestId("preview-command")
    .getByText(/npm\.cmd run dev/)
    .waitFor();
  await shot(page, "04c-service-form-settings");
  await page.getByLabel("Run", { exact: true }).selectOption("script");
  // the rest of the settings are one click away; a changed one stays up front
  assert.equal(await page.getByRole("textbox", { name: /^.env file/ }).count(), 0, "uncommon settings start folded");
  await page.getByRole("button", { name: /More settings/ }).click();
  await page.getByRole("textbox", { name: /^.env file/ }).fill(".env.local");
  await page.locator('[data-option="envFile"]').getByText("Edited").waitFor();
  await page.getByRole("button", { name: "Fewer settings" }).click();
  await page.getByRole("textbox", { name: /^.env file/ }).waitFor(); // still shown: it was changed
  await page
    .getByTestId("changed-summary")
    .getByText(/2 settings changed/) // the script and the .env file
    .waitFor();
  await page
    .getByTestId("preview-command")
    .getByText(/--env-file=\.env\.local/)
    .waitFor();
  await page.getByRole("button", { name: "Reset .env file to its default" }).click();
  await page
    .getByTestId("changed-summary")
    .getByText(/1 setting changed/)
    .waitFor();
  assert.equal(await page.getByRole("textbox", { name: /^.env file/ }).count(), 0, "a reset setting folds away again");
  // the command panel stays in view while the form scrolls
  await page.evaluate(() => document.querySelector("#main")?.scrollTo(0, 2000));
  assert.ok(await page.getByTestId("preview-command").isVisible(), "the command line stays visible");
  // health check and environment live under "Advanced"
  await advanced();
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
  await pickType("Custom command");
  await page.getByLabel("Name", { exact: true }).fill("Changed type");
  await page.getByLabel("Working folder", { exact: true }).fill("C:\\work\\changed");
  await page.getByLabel("Program").fill("custom.exe");
  await page.getByRole("button", { name: "Change type" }).click();
  await pickType("Node.js");
  await page.getByLabel("Project folder", { exact: true }).fill("C:\\work\\changed");
  await page.getByLabel("Script", { exact: true }).fill("server.js");
  await advanced();
  assert.equal(await page.getByLabel("Program").count(), 0, "no program override without your own command line");
  await advanced();
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
  await pickType("Node.js");
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
  await pickType("Custom command");
  await page.getByLabel("Name", { exact: true }).fill("Second");
  await page.getByLabel("Program").fill("C:\\tools\\x.exe");
  await page.getByLabel("Working folder", { exact: true }).fill("C:\\work\\second");
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
  await expand("php");
  const php = card("php");
  await expand("php");
  await picker("php").selectOption("php-8.4.1");
  await php.getByRole("button", { name: "Install", exact: true }).click();
  await expand("php");
  await li("php", "8.4.1").waitFor({ timeout: 15000 });
  await expand("php");
  await li("php", "8.4.1").getByTestId("default-badge").waitFor();
  // the old line is unverified: confirm, then install without removing 8.4.1
  await expand("php");
  await picker("php").selectOption("php-7.1.33");
  await php.getByRole("button", { name: "Install", exact: true }).click();
  const dlg = page.getByRole("dialog");
  await dlg.getByRole("button", { name: "Install anyway" }).click();
  await expand("php");
  await li("php", "7.1.33").waitFor({ timeout: 15000 });
  await expand("php");
  await li("php", "8.4.1").waitFor();
  assert.equal(await php.locator("li[data-runtime]").count(), 2);
  assert.ok(await php.getByRole("button", { name: "Installed", exact: true }).isDisabled());
  await expand("php");
  assert.equal(await li("php", "8.4.1").getByTestId("default-badge").count(), 1);
  await expand("php");
  assert.equal(await li("php", "7.1.33").getByTestId("default-badge").count(), 0);
  // make the older one the default
  await expand("php");
  await li("php", "7.1.33").getByRole("button", { name: "Make PHP 7.1.33 the default" }).click();
  await expand("php");
  await li("php", "7.1.33").getByTestId("default-badge").waitFor();
  await expand("php");
  assert.equal(await li("php", "8.4.1").getByTestId("default-badge").count(), 0);
  await expand("php");
  await li("php", "8.4.1").getByRole("button", { name: "Make PHP 8.4.1 the default" }).waitFor();
  assert.equal(await php.getByTestId("default-badge").count(), 1, "exactly one default per kind");
  await shot(page, "16-two-php-versions");
  // a service picks a specific version
  await page.goto(BASE + "/#/services");
  await page
    .getByRole("button", { name: /New service|Add your first service/ })
    .first()
    .click();
  await pickType("PHP");
  await page.getByLabel("Name", { exact: true }).fill("Legacy PHP");
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
  await expand("php");
  await li("php", "8.4.1").getByRole("button", { name: "Remove PHP 8.4.1" }).click();
  await dlg.getByRole("button", { name: "Remove runtime" }).click();
  await page
    .getByRole("alert")
    .getByText(/used by service Legacy PHP/)
    .waitFor();
  await expand("php");
  await li("php", "8.4.1").waitFor();
  // free it, remove the default 7.1.33: 8.4.1 is promoted
  await page.evaluate(async () => {
    const h = { Authorization: "Bearer devtoken" };
    const svc = (await (await fetch("/api/services", { headers: h })).json()).find((s) => s.name === "Legacy PHP");
    await fetch(`/api/services/${svc.id}`, { method: "DELETE", headers: h });
  });
  await expand("php");
  await li("php", "7.1.33").getByRole("button", { name: "Remove PHP 7.1.33" }).click();
  await dlg.getByRole("button", { name: "Remove runtime" }).click();
  await expand("php");
  await li("php", "7.1.33").waitFor({ state: "detached" });
  await expand("php");
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
  await expand("caddy");
  await picker("caddy").selectOption("caddy-2.8.4");
  await expand("caddy");
  await card("caddy").getByRole("button", { name: "Install", exact: true }).click();
  await expand("caddy");
  await li("caddy", "2.8.4").waitFor({ timeout: 15000 });
  await page.goto(BASE + "/#/sites");
  await page.getByText(/Caddy is not running, so your sites cannot be opened/).waitFor();
  await shot(page, "06-sites");
});

await step("settings: change and persist", async () => {
  await page.goto(BASE + "/#/settings");
  await page.getByLabel("HTTP proxy port").waitFor();
  await page.waitForFunction(() => document.querySelector("input[inputmode=numeric]")?.value === "8080");
  // Save only shows up with unsaved changes; Discard puts the saved values back
  assert.equal(await page.getByRole("button", { name: "Save settings" }).count(), 0);
  await page.getByLabel("HTTP proxy port").fill("9999");
  await page.getByRole("region", { name: "Unsaved changes" }).waitFor();
  await page.getByRole("button", { name: "Discard" }).click();
  assert.equal(await page.getByLabel("HTTP proxy port").inputValue(), "8080");
  await page.getByRole("region", { name: "Unsaved changes" }).waitFor({ state: "detached" });
  // without trust, the certificate path is explained on request
  await page.getByText("How to trust it yourself").click();
  await page.getByText("D:\\zero2dev\\bin\\data\\caddy\\data\\caddy\\pki\\authorities\\local\\root.crt").waitFor();
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
  await page.getByRole("button", { name: "Save settings" }).click();
  await page.getByText("Settings saved.").waitFor();
  await page.getByRole("region", { name: "Unsaved changes" }).waitFor({ state: "detached" });
  await page.reload();
  await page.waitForFunction(() => document.querySelector("input[inputmode=numeric]")?.value === "8081");
  assert.equal(await page.getByLabel("Download proxy (optional)").inputValue(), "http://proxy.corp:8080");
  assert.ok(await page.getByLabel("Let Caddy trust its local certificate authority").isChecked());
  // portable block
  assert.equal(await page.getByTestId("data-folder").textContent(), "D:\\zero2dev\\bin\\data");
  await page.getByText(/Move the whole folder to move Zero2Dev/).waitFor();
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
  await page.getByText("v0.0.3").waitFor();
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

await step("databases, code editors and database tools", async () => {
  await page.setViewportSize({ width: 1280, height: 800 });
  await page.emulateMedia({ colorScheme: "light" });
  await page.goto(BASE + "/#/runtimes");
  for (const h of ["Databases", "Code editors", "Database tools"])
    await page.getByRole("heading", { name: h }).waitFor();
  // MySQL is checked with the MD5 it publishes
  await expand("mysql");
  const my = await pickOptions("mysql");
  assert.ok(
    my.some((o) => o.includes("MD5")),
    my.join("|"),
  );
  await expand("mysql");
  await card("mysql").getByTestId("md5-hint").waitFor();
  // PostgreSQL has no checksum: unverified
  await expand("postgres");
  assert.ok((await pickOptions("postgres")).some((o) => o.includes("unverified")));

  // install VS Code and HeidiSQL, open VS Code from its card
  for (const [kind, id, ver] of [
    ["vscode", "vscode-1.145.0", "1.145.0"],
    ["heidisql", "heidisql-12.22", "12.22"],
  ]) {
    await expand(kind);
    await picker(kind).selectOption(id);
    await expand(kind);
    await card(kind).getByRole("button", { name: "Install", exact: true }).click();
    await expand(kind);
    await li(kind, ver).waitFor({ timeout: 15000 });
  }
  const opened = page.waitForRequest((r) => /\/api\/runtimes\/[^/]+\/launch$/.test(r.url()) && r.method() === "POST");
  await expand("vscode");
  await li("vscode", "1.145.0")
    .getByRole("button", { name: /^Open / })
    .click();
  await opened;
  await shot(page, "17-runtimes-databases-editors");

  // a PostgreSQL service: default port, connection details, Open in editor and database tool
  await page.goto(BASE + "/#/services");
  await page.getByRole("button", { name: "New service" }).click();
  await pickType("PostgreSQL");
  await page.getByLabel("Name", { exact: true }).fill("Shop DB");
  assert.equal(await page.getByLabel(/^Port/).inputValue(), "5432", "databases get their usual port");
  assert.equal(await page.getByLabel("Project folder", { exact: true }).count(), 0, "a database has no project folder");
  await page
    .getByTestId("data-folder")
    .getByText(/postgres\\svc_new/)
    .waitFor();
  await page.getByLabel("Max connections").fill("50");
  await page
    .getByTestId("preview-command")
    .getByText(/-c max_connections=50/)
    .waitFor();
  await page.getByTestId("always-added").getByText("Always added").click();
  await page.getByTestId("always-added").getByText("listen_addresses=127.0.0.1").waitFor();
  await shot(page, "19-database-settings");
  await page.getByRole("button", { name: "Create service" }).click();
  const row = page.locator('tr[data-service="Shop DB"]');
  await row.waitFor();
  await row.getByRole("link", { name: "Shop DB", exact: true }).click();
  assert.equal(await page.getByTestId("connection-url").textContent(), "postgresql://postgres@127.0.0.1:5432/postgres");
  await page
    .getByTestId("command-line")
    .getByText(/max_connections=50/)
    .waitFor();
  await page
    .getByTestId("data-dir")
    .getByText(/\\postgres\\svc_/)
    .waitFor();
  const toTool = page.waitForRequest((r) => r.url().includes("/launch") && r.method() === "POST");
  await page.getByRole("button", { name: "Open Shop DB in HeidiSQL" }).click();
  const body = JSON.parse((await toTool).postData());
  assert.ok(body.serviceId && !body.path, JSON.stringify(body));
  await shot(page, "18-database-service");
  await page.emulateMedia({ colorScheme: "dark" });
  await shot(page, "18b-database-service-dark");
  await page.emulateMedia({ colorScheme: "light" });
});

await step("php settings: extensions, Xdebug download, php.ini, open in editor", async () => {
  await page.goto(BASE + "/#/services");
  await page.getByRole("button", { name: "New service" }).click();
  await pickType("PHP");
  await page.getByLabel("Name", { exact: true }).fill("Shop site");
  assert.equal(await page.getByLabel(/^Port/).inputValue(), "8000");
  await page.getByLabel("Project folder", { exact: true }).fill("C:\\work\\shop");
  // the loaded extensions are chips; another one is added by name
  const chips = page.getByRole("list", { name: "Loaded PHP extensions" });
  await chips.getByText("pdo_mysql").waitFor();
  await page.getByLabel("Add a PHP extension").fill("gd");
  await page.getByLabel("Add a PHP extension").press("Enter");
  await chips.getByText("gd", { exact: true }).waitFor();
  await chips.getByRole("button", { name: "Remove gd" }).click();
  // the full list is one click away
  await page.getByRole("button", { name: /^Show all/ }).click();
  const exts = page.getByRole("group", { name: "PHP extensions" });
  await exts.getByLabel("pdo_mysql").waitFor();
  assert.equal(await exts.getByLabel("pdo_mysql").isChecked(), true, "common extensions start ticked");
  assert.equal(await exts.getByLabel("pdo_pgsql").isChecked(), false);
  await exts.getByLabel("pdo_pgsql").check();
  await page.getByLabel("Memory limit").fill("512M");
  await page
    .getByTestId("preview-command")
    .getByText(/-c .*php\\svc_new\\php\.ini -S 127\.0\.0\.1:8000/)
    .waitFor();
  await page.getByText(/Writes .*php\.ini before each start/).waitFor();
  // Xdebug is downloaded on demand, then ticked, and its settings appear
  await page.getByRole("button", { name: "Download Xdebug" }).click();
  await exts.getByLabel("xdebug").waitFor({ timeout: 10000 });
  assert.equal(await exts.getByLabel("xdebug").isChecked(), true);
  await page.locator('[data-option="xdebugMode"]').waitFor();
  await shot(page, "20-php-settings");
  const created = page.waitForRequest((r) => r.method() === "POST" && r.url().endsWith("/api/services"));
  await page.getByRole("button", { name: "Create service" }).click();
  const sent = (await created).postDataJSON();
  assert.ok(
    sent.settings.extensions.split(",").includes("pdo_pgsql") && sent.settings.extensions.includes("xdebug"),
    sent.settings.extensions,
  );
  assert.equal(sent.settings.memoryLimit, "512M");
  assert.equal(sent.customArgs, false);
  const row = page.locator('tr[data-service="Shop site"]');
  await row.waitFor();
  await row.getByRole("link", { name: "Shop site", exact: true }).click();
  const toEditor = page.waitForRequest((r) => r.url().includes("/launch") && r.method() === "POST");
  await page.getByRole("button", { name: "Open Shop site folder in VS Code" }).click();
  assert.deepEqual(JSON.parse((await toEditor).postData()), { path: "C:\\work\\shop" });
});

await step("a service from before settings keeps its own command line", async () => {
  await page.evaluate(async () => {
    const h = { Authorization: "Bearer devtoken", "Content-Type": "application/json" };
    const body = {
      name: "Old app",
      kind: "node",
      command: "",
      args: ["old.js", "--port", "{port}"],
      cwd: "C:\\work\\old",
      env: {},
      port: 3200,
      healthPath: "",
      runtimeId: "",
      autostart: false,
    };
    await fetch("/api/services", { method: "POST", headers: h, body: JSON.stringify(body) });
  });
  await page.goto(BASE + "/#/services");
  const row = page.locator('tr[data-service="Old app"]');
  await row.getByRole("link", { name: "Old app", exact: true }).click();
  await page
    .getByTestId("command-line")
    .getByText(/old\.js --port 3200/)
    .waitFor();
  await page.getByRole("button", { name: "use the settings instead" }).waitFor();
  await page.getByRole("button", { name: "Edit" }).click();
  assert.equal(await page.getByTestId("custom-args").isChecked(), true);
  await page.getByText(/The settings are off/).waitFor();
  await page.getByRole("button", { name: "Cancel" }).click();
});

await step("dashboard: tiles, live service cards, activity", async () => {
  await page.setViewportSize({ width: 1280, height: 800 });
  await page.emulateMedia({ colorScheme: "light" });
  await page.goto(BASE + "/#/dashboard");
  await page
    .getByTestId("status-sentence")
    .getByText(/services? running/)
    .waitFor();
  // a tile opens its page, already filtered
  await page.getByTestId("tile-runtimes").click();
  await page.waitForURL(/#\/runtimes\?filter=installed/);
  assert.equal(await page.getByRole("button", { name: /^Installed/ }).getAttribute("aria-pressed"), "true");
  await page.goto(BASE + "/#/dashboard");
  // start and stop from a card; its latest output shows up there and in the activity list
  const card = page.locator('article.svc[data-service="Demo API"]');
  await card.waitFor();
  if (await card.getByRole("button", { name: "Stop Demo API", exact: true }).count()) {
    await card.getByRole("button", { name: "Stop Demo API", exact: true }).click();
    await card.getByRole("button", { name: "Start Demo API", exact: true }).waitFor({ timeout: 8000 });
  }
  await card.getByRole("button", { name: "Start Demo API", exact: true }).click();
  await card.locator('.badge[data-state="running"]').waitFor({ timeout: 8000 });
  await card
    .locator(".last")
    .getByText(/listening|GET|ready|compiled/)
    .waitFor({ timeout: 8000 });
  await page.getByTestId("activity").getByText("Demo API").first().waitFor();
  await shot(page, "21-dashboard-live");
  await card.getByRole("button", { name: "Stop Demo API", exact: true }).click();
  await card.locator('.badge[data-state="stopped"]').waitFor({ timeout: 8000 });
});

await step("runtimes: grid view, remembered after a reload", async () => {
  await page.goto(BASE + "/#/runtimes");
  await page.getByRole("button", { name: "Grid" }).click();
  await page.locator("article.rt.tile").first().waitFor();
  await page.reload();
  assert.equal(await page.getByRole("button", { name: "Grid" }).getAttribute("aria-pressed"), "true");
  assert.ok((await page.locator("article.rt.tile").count()) > 5);
  // a tile opens its details in a dialog; the grid itself stays as it is
  await page.getByRole("button", { name: "Manage Node.js" }).click();
  const det = page.locator('[data-kind-dialog="node"]');
  await det.getByText("Installed", { exact: true }).waitFor();
  await det.getByRole("heading", { name: "Install another version" }).waitFor();
  await shot(page, "22-runtimes-grid");
  await page.getByRole("dialog").locator(".dlg-foot").getByRole("button", { name: "Close", exact: true }).click();
  await det.waitFor({ state: "detached" });
  assert.equal(await page.locator("article.rt.open").count(), 0, "no tile spans the grid");
  await page.getByRole("button", { name: "List" }).click();
  assert.equal(await page.locator("article.rt.tile").count(), 0);
});

await step("help: the Create a service tutorial guides and completes", async () => {
  await page.goto(BASE + "/#/help");
  await page.getByRole("heading", { name: "Tutorials" }).waitFor();
  await page.getByRole("button", { name: "Start: Create a service" }).click();
  const guide = page.getByTestId("tour-guide");
  await guide.getByText("Step 1 of 8").waitFor();
  // the spotlight is on the Services link; clicking it moves the tutorial on by itself
  await page.locator(".ring").waitFor();
  await page.getByRole("navigation", { name: "Main" }).getByRole("link", { name: "Services" }).click();
  await guide.getByText("Step 2 of 8").waitFor();
  await page.getByRole("button", { name: "New service" }).click();
  await guide.getByText("Step 3 of 8").waitFor();
  await pickType("Custom command");
  await guide.getByText("Step 4 of 8").waitFor();
  await page.getByLabel("Name", { exact: true }).fill("Tour app");
  await page.getByLabel("Working folder", { exact: true }).fill("C:\\work\\tour");
  await page.getByLabel("Program").fill("C:\\tools\\tour.exe");
  await guide.getByRole("button", { name: "Next" }).click();
  await guide.getByText("Check what will run").waitFor();
  await shot(page, "23-tour-step");
  await guide.getByRole("button", { name: "Next" }).click();
  await guide.getByText(/Waiting for you to create the service/).waitFor();
  await page.getByRole("button", { name: "Create service" }).click();
  await guide.getByText("Step 7 of 8").waitFor();
  // a reload keeps the tutorial where it was
  await page.reload();
  await page.getByTestId("tour-guide").getByText("Step 7 of 8").waitFor();
  await page
    .locator('tr[data-service="Tour app"]')
    .getByRole("button", { name: "Start Tour app", exact: true })
    .click();
  await page.getByTestId("tour-guide").getByText("Step 8 of 8").waitFor({ timeout: 10000 });
  await page.getByTestId("tour-guide").getByRole("button", { name: "Finish" }).click();
  await page.getByText("Tutorial finished: Create a service.").waitFor();
  assert.equal(await page.getByTestId("tour-guide").count(), 0);
  await page.goto(BASE + "/#/help");
  await page
    .locator('[data-tour-card="service"]')
    .getByRole("button", { name: "Do again: Create a service" })
    .waitFor();
  await shot(page, "24-help");
  const row = page.locator('tr[data-service="Tour app"]');
  await page.goto(BASE + "/#/services");
  await row.getByRole("button", { name: "Stop Tour app", exact: true }).click();
});

await step("appearance: the theme button's popover sets the theme and accent, saved and kept after a reload", async () => {
  const settings = () =>
    page.evaluate(async () => (await fetch("/api/settings", { headers: { Authorization: "Bearer devtoken" } })).json());
  await page.goto(BASE + "/#/services");
  const toggle = page.getByTestId("theme-toggle");
  const pop = page.getByTestId("theme-popover");
  const openPop = async () => {
    if (!(await pop.isVisible())) await toggle.click();
    await pop.waitFor();
  };
  // the page's own primary button shows the accent
  const primary = page.locator("main .btn.primary").first();
  // Buttons fade between colours (0.2 s), so wait for the value to settle.
  const css = async (prop, want) => {
    let v = "";
    for (let i = 0; i < 40; i++) {
      v = await primary.evaluate((el, p) => getComputedStyle(el)[p], prop);
      if (v === want) break;
      await page.waitForTimeout(50);
    }
    return v;
  };

  // Settings no longer has an Appearance section
  await page.goto(BASE + "/#/settings");
  await page.locator("#set-sites").waitFor();
  assert.equal(await page.locator("#set-appearance").count(), 0);
  await page.goto(BASE + "/#/services");

  // the popover opens from the top bar, with focus on the selected theme; Escape closes it
  await toggle.click();
  await pop.waitFor();
  assert.equal(await toggle.getAttribute("aria-expanded"), "true");
  assert.equal(await page.evaluate(() => document.activeElement?.getAttribute("name")), "theme-mode");
  await page.keyboard.press("Escape");
  await pop.waitFor({ state: "hidden" });
  assert.equal(await page.evaluate(() => document.activeElement?.dataset.testid), "theme-toggle");

  // a preset: buildTheme("#722ed1", "light")["accent-solid"] is #722ed1 (white text reads on it)
  await openPop();
  await pop.getByRole("radio", { name: "Purple" }).click();
  await pop.getByText("Accent Purple saved.").waitFor();
  assert.equal(await css("backgroundColor", "rgb(114, 46, 209)"), "rgb(114, 46, 209)");
  assert.equal((await settings()).accent, "#722ed1");
  // a click outside closes it
  await page.locator("main h1, main").first().click({ position: { x: 5, y: 5 } });
  await pop.waitFor({ state: "hidden" });

  await page.reload();
  await primary.waitFor();
  // the inline script in index.html applied the cached theme before the app ran
  assert.equal(await page.evaluate(() => JSON.parse(localStorage.getItem("zero2dev.theme") ?? "{}").accent), "#722ed1");
  assert.equal(await page.evaluate(() => document.documentElement.dataset.theme), "light");
  assert.equal(await css("backgroundColor", "rgb(114, 46, 209)"), "rgb(114, 46, 209)");
  await openPop();
  assert.equal(await pop.getByRole("radio", { name: "Purple" }).getAttribute("aria-checked"), "true");

  // theme: Dark sets data-theme, then System and Light
  await pop.locator(".seg label", { hasText: "Dark" }).click();
  await page.waitForFunction(() => document.documentElement.dataset.theme === "dark");
  assert.equal((await settings()).theme, "dark");
  await pop.locator(".seg label", { hasText: "System" }).click();
  await page.waitForFunction(() => document.documentElement.dataset.themeMode === "system");
  await pop.locator(".seg label", { hasText: "Light" }).click();
  await page.waitForFunction(() => document.documentElement.dataset.theme === "light");

  // a custom colour: a bad value is explained; a light one gets dark text on buttons
  const hex = pop.getByLabel("Custom accent colour (hex)");
  await hex.fill("#zz0000");
  await hex.press("Enter");
  await pop.getByText(/Use a colour like #1677ff/).waitFor();
  assert.equal(await hex.getAttribute("aria-invalid"), "true");
  await hex.fill("#fadb14");
  await hex.press("Enter");
  await pop.getByText("Accent #fadb14 saved.").waitFor();
  assert.equal(await css("color", "rgb(31, 31, 31)"), "rgb(31, 31, 31)");
  // Undo puts the purple back
  await pop.getByRole("button", { name: "Undo" }).click();
  await pop.getByText("Change undone.").waitFor();
  assert.equal((await settings()).accent, "#722ed1");

  // back to the defaults for the rest of the run
  await pop.locator(".seg label", { hasText: "System" }).click();
  await pop.getByRole("radio", { name: "Blue", exact: true }).click();
  await page.waitForFunction(() => document.documentElement.style.getPropertyValue("--accent") === "#1677ff");
  await page.keyboard.press("Escape");
  const s = await settings();
  assert.equal(s.theme, "system");
  assert.equal(s.accent, "#1677ff");
});

await step("no external network requests", async () => {
  assert.deepEqual(external, []);
});

await step("quit: confirm names what runs, then the stopped screen", async () => {
  await page.goto(BASE + "/#/dashboard");
  const card = page.locator('article.svc[data-service="Demo API"]');
  await card.waitFor();
  if (await card.getByRole("button", { name: "Stop Demo API", exact: true }).count()) {
    await card.getByRole("button", { name: "Stop Demo API", exact: true }).click();
    await card.getByRole("button", { name: "Start Demo API", exact: true }).waitFor({ timeout: 8000 });
  }
  await card.getByRole("button", { name: "Start Demo API", exact: true }).click();
  await card.locator('.badge[data-state="running"]').waitFor({ timeout: 8000 });
  await page.getByRole("button", { name: "Quit Zero2Dev" }).click();
  const dlg = page.getByRole("dialog");
  await dlg.getByText(/stops the running service \(Demo API\)/).waitFor();
  await dlg.getByRole("button", { name: "Quit Zero2Dev" }).click();
  await page.getByRole("heading", { name: "Zero2Dev has stopped" }).waitFor();
  await shot(page, "25-stopped");
});

await browser.close();
mock.kill();
console.log("\n" + results.map((r) => r.join(" ")).join("\n"));
console.log("screenshots:", SHOTS);
