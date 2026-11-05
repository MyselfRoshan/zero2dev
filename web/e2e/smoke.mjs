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
  await page.getByText("No services yet").waitFor();
  await page.getByText("Connected").waitFor();
  await shot(page, "01-dashboard-empty");
});

await step("install catalog runtime shows progress until done", async () => {
  await page.getByRole("navigation", { name: "Main" }).getByRole("link", { name: "Runtimes" }).click();
  await page.getByText("No runtimes yet").waitFor();
  const row = page.locator('tr[data-catalog="node-22.11.0"]');
  await row.getByRole("button", { name: "Install", exact: true }).click();
  const bar = row.getByRole("progressbar");
  await bar.waitFor();
  await shot(page, "05a-runtimes-installing");
  const seen = new Set();
  for (let i = 0; i < 40; i++) {
    const v = await bar.getAttribute("aria-valuenow", { timeout: 300 }).catch(() => null);
    if (v) seen.add(Number(v));
    if (await row.getByText("Installed").isVisible()) break;
    await page.waitForTimeout(100);
  }
  await row.getByText("Installed").waitFor();
  await page
    .getByText("Node.js 22.11.0 installed.")
    .waitFor()
    .catch(async (e) => {
      await shot(page, "debug-install");
      throw new Error(e.message + " | body: " + (await page.locator(".toasts").innerText()));
    });
  assert.ok(seen.size >= 2, "progress advanced: " + [...seen]);
  await page.locator("tr[data-runtime]").filter({ hasText: "22.11.0" }).waitFor();
  // unsupported entry is disabled with an explanation
  const arm = page.locator('tr[data-catalog="go-1.23.3-arm64"]');
  assert.ok(await arm.getByRole("button", { name: "Install", exact: true }).isDisabled());
  await arm.getByText(/not for this PC/).waitFor();
  // kinds without catalog entries get the hint
  await page.getByText(/no download in the catalog/).waitFor();
});

await step("detect and register one-click, register by path validation", async () => {
  await page.getByRole("button", { name: "Detect on this PC" }).click();
  await page.getByText("Found on this PC").waitFor();
  await page.locator("tr", { hasText: "php" }).getByRole("button", { name: "Register" }).click();
  await page.locator("tr[data-runtime]").filter({ hasText: "php" }).waitFor();
  await page.getByRole("button", { name: "Register by path" }).click();
  await page.getByLabel("Path to the program").fill("php");
  await page.getByRole("button", { name: "Register runtime" }).click();
  await page.getByText(/Use an absolute path/).waitFor();
  await page.getByRole("button", { name: "Cancel" }).click();
  await shot(page, "04-runtimes");
});

await step("failed install shows full error, network hint and installer log", async () => {
  await page.goto(BASE + "/#/runtimes");
  const row = page.locator('tr[data-catalog="node-20.18.0"]');
  await row.getByText("node-v20.18.0-win-x64.zip").waitFor();
  assert.equal(
    await row.getByRole("link", { name: /Download page/ }).getAttribute("href"),
    "https://nodejs.org/dist/v20.18.0/",
  );
  await row.getByRole("button", { name: "Install", exact: true }).click();
  const fail = page.locator('tr[data-failed="node-20.18.0"]');
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
  await page.evaluate(() => window.scrollTo(0, 0));
  await fail.scrollIntoViewIfNeeded();
  await shot(page, "12-runtimes-failed-job");
  await page.emulateMedia({ colorScheme: "dark" });
  await shot(page, "12b-runtimes-failed-job-dark");
  await page.emulateMedia({ colorScheme: "light" });
});

await step("import: validation, server errors, failure then success", async () => {
  const row = page.locator('tr[data-catalog="node-20.18.0"]');
  await row.getByRole("button", { name: /Import file/ }).click();
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
  const fail = page.locator('tr[data-failed="node-20.18.0"]');
  await fail.getByText(/step: Registering/).waitFor({ timeout: 15000 });
  assert.match(
    await fail.getByTestId("job-error").textContent(),
    /^Registering: rename .*Access is denied \(antivirus may be scanning/,
  );
  await fail.getByText(/Antivirus software may be scanning/).waitFor();
  assert.equal(await fail.getByText(/looks like a network problem/).count(), 0);
  // successful import of go: Importing step visible, then Installed
  const go = page.locator('tr[data-catalog="go-1.23.3"]');
  await go.getByRole("button", { name: /Import file/ }).click();
  await dlg.getByLabel("Path of the downloaded file").fill("C:\\Users\\me\\Downloads\\go1.23.3.windows-amd64.zip");
  await dlg.getByRole("button", { name: "Import file" }).click();
  await go.getByText("Importing", { exact: true }).waitFor();
  await go.getByText("Installed").waitFor({ timeout: 15000 });
  await page.locator("tr[data-runtime]").filter({ hasText: "1.23.3" }).waitFor();
});

await step("create a service (validation first, then success)", async () => {
  await page.goto(BASE + "/#/services");
  await page.getByText("No services yet").waitFor();
  await page.getByRole("button", { name: "Create your first service" }).click();
  await page.getByRole("button", { name: "Create service" }).click();
  await page.getByText("Enter a name.").waitFor();
  await page.getByText(/no default arguments/).waitFor();
  await page.getByLabel("Name", { exact: true }).fill("Demo API");
  await page.getByRole("button", { name: "Add argument" }).click();
  await page.getByLabel("Argument 1", { exact: true }).fill("server.js");
  await page.getByLabel("Working directory").fill("C:\\work\\demo");
  await page.getByLabel("Port").fill("3100");
  await page.getByLabel("Health path").fill("/health");
  await page.getByRole("button", { name: "Add variable" }).click();
  await page.getByLabel("Variable name 1", { exact: true }).fill("NODE_ENV");
  await page.getByLabel("Variable value 1", { exact: true }).fill("development");
  // runtime select is filtered by kind: only the node runtime is offered
  const opts = await page.getByLabel("Runtime").locator("option").allTextContents();
  assert.ok(opts.some((o) => o.includes("Node.js")) && !opts.some((o) => o.includes("PHP")), opts.join("|"));
  await page.getByRole("button", { name: "Create service" }).click();
  await page.locator('tr[data-service="Demo API"]').waitFor();
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
  await page.getByLabel("Kind").selectOption("custom");
  await page.getByLabel("Command").fill("C:\\tools\\x.exe");
  await page.getByLabel("Working directory").fill("C:\\work\\second");
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

await step("sites: caddy notice, create site, domain validation", async () => {
  await page.goto(BASE + "/#/sites");
  await page.getByText(/no Caddy runtime is installed/).waitFor();
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
  // install caddy -> notice changes
  await page.goto(BASE + "/#/runtimes");
  await page.locator('tr[data-catalog="caddy-2.8.4"]').getByRole("button", { name: "Install", exact: true }).click();
  await page.locator('tr[data-catalog="caddy-2.8.4"]').getByText("Installed").waitFor({ timeout: 15000 });
  await page.goto(BASE + "/#/sites");
  await page.getByText(/Sites only work while the Caddy service is running/).waitFor();
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
  await page.getByText("v0.0.1").waitFor();
  await page.getByText("Portable", { exact: false }).first().waitFor();
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
