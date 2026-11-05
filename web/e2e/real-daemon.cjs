// Real-daemon end-to-end test: drives the built UI in Chromium against the real binary.
// Run from web/: `npm i -D playwright` once, build the daemon (make build), then `node e2e/real-daemon.cjs`.
// Regression guard: a fast stop must leave the UI on 'stopped' (stale 202 bodies must not overwrite SSE state).
const { chromium } = require("playwright");
const { spawn } = require("child_process");
const fs = require("fs"),
  path = require("path"),
  os = require("os");
const ROOT = path.resolve(__dirname, "..", "..");
const SH = fs.mkdtempSync(path.join(os.tmpdir(), "z2d-shots-")) + path.sep;
(async () => {
  const home = fs.mkdtempSync(path.join(os.tmpdir(), "z2d-")),
    site = fs.mkdtempSync(path.join(os.tmpdir(), "site-"));
  fs.writeFileSync(path.join(site, "index.html"), "hi");
  const port = 7311,
    svcPort = 7312;
  const d = spawn(path.join(ROOT, "bin", process.platform === "win32" ? "zero2dev.exe" : "zero2dev"), [], {
    env: { ...process.env, ZERO2DEV_HOME: home, ZERO2DEV_PORT: String(port), ZERO2DEV_NO_BROWSER: "1" },
  });
  await new Promise((r) => setTimeout(r, 1500));
  const token = fs.readFileSync(path.join(home, "token"), "utf8").trim();
  const b = await chromium.launch();
  const p = await b.newPage({ viewport: { width: 1280, height: 800 } });
  p.setDefaultTimeout(6000);
  const errs = [];
  p.on("pageerror", (e) => errs.push("pageerror " + e.message));
  p.on("console", (m) => {
    if (m.type() === "error") errs.push("console " + m.text());
  });
  p.on("response", (r) => {
    if (r.status() >= 400 && r.url().includes("/api/")) errs.push(r.status() + " " + r.url());
  });
  const ok = (c, m) => {
    console.log((c ? "ok   " : "FAIL ") + m);
    if (!c) process.exitCode = 1;
  };
  const step = async (name, fn) => {
    try {
      await fn();
    } catch (e) {
      ok(false, name + " :: " + e.message.split("\n")[0]);
      await p.screenshot({ path: SH + "fail-" + name.replace(/\W+/g, "_") + ".png" });
    }
  };
  await p.goto(`http://127.0.0.1:${port}/#token=${token}`);
  await p.waitForSelector("text=Dashboard");
  await p.goto(`http://127.0.0.1:${port}/#/services`);
  await step("create service", async () => {
    await p.getByRole("button", { name: "New service" }).first().click();
    await p.getByLabel("Name", { exact: true }).fill("Static py");
    await p.locator("select").first().selectOption("custom");
    await p.getByLabel("Command").fill(process.platform === "win32" ? "python" : "python3");
    let n = 0;
    for (const a of ["-m", "http.server", "{port}"]) {
      await p.getByRole("button", { name: "Add argument" }).click();
      n++;
      await p.getByLabel("Argument " + n, { exact: true }).fill(a);
    }
    await p.getByLabel("Working directory").fill(site);
    await p.getByLabel("Port").fill(String(svcPort));
    await p.screenshot({ path: SH + "03-form-filled.png" });
    await p.getByRole("button", { name: "Create service" }).click();
    await p.waitForSelector("text=Static py", { timeout: 5000 });
    ok(true, "service created via UI");
  });
  await step("start service", async () => {
    await p
      .getByRole("button", { name: /^start/i })
      .first()
      .click();
    await p.waitForSelector("text=/running/i", { timeout: 15000 });
    ok(true, "service reached running via UI");
    const r = await fetch(`http://127.0.0.1:${svcPort}/`);
    ok(r.ok, "service actually serves HTTP");
  });
  await step("logs", async () => {
    await p.getByText("Static py").first().click();
    await p.waitForSelector("text=/Serving HTTP|system|started/i", { timeout: 8000 });
    await fetch(`http://127.0.0.1:${svcPort}/index.html`);
    await p.waitForSelector("text=/GET \\/index.html/", { timeout: 8000 });
    ok(true, "live log line arrived over SSE");
    await p.screenshot({ path: SH + "04-detail-logs.png" });
  });
  await step("site", async () => {
    await p.goto(`http://127.0.0.1:${port}/#/sites`);
    await p
      .getByRole("button", { name: /new site/i })
      .first()
      .click();
    await p.getByLabel("Name", { exact: true }).fill("Demo");
    await p.getByLabel("Domain").fill("demo.localhost");
    await p.getByLabel("Type").selectOption("static");
    await p.getByLabel("Folder to serve").fill(site);
    await p.screenshot({ path: SH + "05-site-form.png" });
    await p.getByRole("button", { name: /create site/i }).click();
    await p.waitForSelector("text=demo.localhost", { timeout: 5000 });
    const cf = fs.readFileSync(path.join(home, "config", "Caddyfile"), "utf8");
    ok(/demo\.localhost/.test(cf) && cf.includes(site), "Caddyfile generated from UI-created site");
    await p.screenshot({ path: SH + "06-sites.png" });
  });
  await step("stop", async () => {
    await p.goto(`http://127.0.0.1:${port}/#/services`);
    await p.getByRole("button", { name: /^stop/i }).first().click();
    await p.waitForSelector("text=/stopped/i", { timeout: 10000 });
    ok(true, "service stopped via UI");
  });
  await step("settings+runtimes", async () => {
    await p.goto(`http://127.0.0.1:${port}/#/runtimes`);
    await p.waitForSelector("text=/Catalog/i");
    await p.screenshot({ path: SH + "07-runtimes.png" });
    await p.goto(`http://127.0.0.1:${port}/#/settings`);
    await p.waitForSelector("text=/Settings/i");
    ok(true, "runtimes + settings render");
  });
  console.log("API/console errors:", JSON.stringify(errs));
  await b.close();
  d.kill();
})().catch((e) => {
  console.error(e);
  process.exit(1);
});
