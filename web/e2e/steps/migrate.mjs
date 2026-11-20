// e2e steps for Import (0.0.5, migrate-ui), run by smoke.mjs against the mock (before the final checks).
//   export default async function steps(t)
// t: { page, BASE, step, apiCall, shot, assert, customService, serviceRow, pickType }
// The mock's sources (web/mock/features/migrations.mjs): VS Code and HeidiSQL are installed in
// Zero2Dev by the earlier steps, Notepad++ is not; Java 21.0.4 is registered by path.
export default async function steps({ page, BASE, step, apiCall, shot, assert }) {
  const source = (id) => page.locator(`[data-testid="migrate-source"][data-source="${id}"]`);
  // The plan is the side panel on a wide screen and sits under its row on a narrow one: one
  // [data-testid=migrate-plan] either way.
  const plan = () => page.getByTestId("migrate-plan");
  const openProgramsTab = async () => {
    const t = page.getByTestId("migrate-tab-programs");
    if ((await t.getAttribute("aria-selected")) !== "true") await t.click();
  };
  const openHistory = async () => {
    const t = page.getByTestId("migrate-tab-history");
    if ((await t.getAttribute("aria-selected")) !== "true") await t.click();
    await page.getByTestId("migrate-history").waitFor();
  };
  const openSource = async (id) => {
    await openProgramsTab();
    const s = source(id);
    await s.waitFor();
    const b = s.getByTestId("migrate-open");
    if ((await b.getAttribute("aria-expanded")) !== "true") await b.click();
    await plan().waitFor();
    assert.equal(await plan().getAttribute("data-plan"), id);
    return plan();
  };
  const historyRow = (sourceId, state) =>
    page.locator(`[data-testid="migrate-history-row"][data-source="${sourceId}"][data-state="${state}"]`);
  const result = (text) => page.getByTestId("migrate-result").getByText(text).waitFor({ timeout: 10000 });
  const setRunning = (program, running) => apiCall("POST", "/api/mock/migrations/running", { program, running });
  const revert = async (sourceId, how) => {
    await openHistory();
    const row = historyRow(sourceId, "done").first();
    const open = row.getByTestId("migrate-revert");
    if ((await open.getAttribute("aria-expanded")) !== "true") await open.click();
    const panel = row.getByTestId("migrate-revert-panel");
    await panel.getByRole("radio", { name: new RegExp(how) }).check();
    await panel.getByTestId("migrate-revert-go").click();
    const dlg = page.getByRole("dialog");
    await dlg.getByRole("button", { name: how, exact: true }).click();
  };

  await step("import: sources by group, a blocker with Check again, copy VS Code, in history", async () => {
    await page.goto(BASE + "/#/migrate");
    await page.getByRole("heading", { level: 1, name: "Import from this PC" }).waitFor();
    for (const h of ["Editors and tools", "Runtimes", "Caches and global packages", "Databases"])
      await page.getByRole("heading", { name: h, exact: false }).first().waitFor();
    await page.getByRole("tab", { name: /^History/ }).waitFor();
    await page
      .locator('[data-group="tools"]')
      .getByTestId("migrate-missing")
      .getByText(/VSCodium/)
      .waitFor();
    await source("vscode").getByText("Settings and Extensions, 414 MB").waitFor();
    // VS Code is running: the blocker says so, and Check again clears it once it is closed
    await setRunning("vscode", true);
    await page.getByTestId("migrate-refresh").click();
    await page.getByTestId("migrate-refresh").getByText("Check again").waitFor();
    const s = await openSource("vscode");
    await s.getByTestId("migrate-blocker").getByText("Close VS Code first.").waitFor();
    assert.ok(await s.getByTestId("migrate-review").isDisabled(), "Review waits for the blocker");
    await setRunning("vscode", false);
    await s.getByTestId("migrate-recheck").click();
    await s.getByTestId("migrate-blocker").waitFor({ state: "detached" });
    // copy (the default) keeps the originals; the review says what happens before anything does
    assert.ok(await s.getByTestId("migrate-mode").getByRole("radio", { name: /^Copy/ }).isChecked());
    await s
      .getByTestId("migrate-note")
      .getByText(/Settings Sync/)
      .waitFor();
    await s.getByTestId("migrate-review").click();
    await page
      .getByTestId("migrate-review-text")
      .getByText(
        "This copies 414 MB (settings and extensions) of VS Code into Zero2Dev's folder. The originals stay where they are.",
      )
      .waitFor();
    await page.getByTestId("migrate-review-dialog").getByText("C:\\Users\\me\\.vscode\\extensions").waitFor();
    await page.getByTestId("migrate-start").click();
    await page.getByTestId("migrate-progress").waitFor();
    await result("Done.");
    const list = await apiCall("GET", "/api/migrations");
    assert.equal(list[0].source, "vscode");
    assert.equal(list[0].mode, "copy");
    // the row says it is in; a second copy waits for a revert
    await source("vscode").getByTestId("migrate-status").getByText("Copied", { exact: true }).waitFor();
    await openSource("vscode");
    await plan().getByTestId("migrate-already").waitFor();
    await shot(page, "40-migrate");
    await openHistory();
    await historyRow("vscode", "done").getByText("Copied", { exact: true }).first().waitFor();
  });

  await step("import: revert the VS Code copy with discard, behind a confirm", async () => {
    await openHistory();
    const row = historyRow("vscode", "done").first();
    await row.getByTestId("migrate-revert").click();
    const panel = row.getByTestId("migrate-revert-panel");
    // both choices are explained; the one that loses nothing is preselected
    assert.ok(await panel.getByRole("radio", { name: /Discard the copy/ }).isChecked());
    await panel.getByText(/Removes the copy in Zero2Dev's folder/).waitFor();
    await panel.getByText(/Copies what Zero2Dev has now over the installed VS Code/).waitFor();
    await panel.getByTestId("migrate-revert-go").click();
    const dlg = page.getByRole("dialog");
    await dlg.getByText(/installed VS Code is not touched/).waitFor();
    await dlg.getByRole("button", { name: "Discard the copy", exact: true }).click();
    await result("Reverted.");
    await historyRow("vscode", "reverted").waitFor();
    assert.equal((await apiCall("GET", "/api/migrations"))[0].state, "reverted");
  });

  await step("import: move Notepad++ into Zero2Dev, then move it back", async () => {
    let s = await openSource("notepadpp");
    await s.getByTestId("migrate-blocker").getByText("Install Notepad++ in Zero2Dev first.").waitFor();
    await s.getByTestId("migrate-blocker").getByRole("link", { name: "Open Runtimes" }).waitFor();
    // install it (the Runtimes page is tested elsewhere), then look again
    const entry = (await apiCall("GET", "/api/catalog")).find((c) => c.kind === "notepadpp" && c.supported);
    await apiCall("POST", "/api/runtimes/install", { catalogId: entry.id, acceptUnverified: true });
    for (let i = 0; i < 60; i++) {
      if ((await apiCall("GET", "/api/runtimes")).some((r) => r.kind === "notepadpp")) break;
      await page.waitForTimeout(250);
    }
    await page.getByTestId("migrate-refresh").click();
    s = await openSource("notepadpp");
    await s.getByTestId("migrate-blocker").waitFor({ state: "detached" });
    await s
      .getByTestId("migrate-target-warning")
      .getByText(/What is there now is saved first/)
      .waitFor();
    await s.getByTestId("migrate-mode").getByRole("radio", { name: /^Move/ }).check();
    await s.getByText(/removed from their old place/).waitFor();
    await s.getByRole("checkbox", { name: /Plugin settings/ }).uncheck();
    await s.getByTestId("migrate-review").click();
    await page
      .getByTestId("migrate-review-text")
      .getByText(/^This moves 1.5 MB \(settings and shortcuts and themes\) of Notepad\+\+/)
      .waitFor();
    await page.getByTestId("migrate-start").click();
    await result("Done.");
    await openHistory();
    const row = historyRow("notepadpp", "done");
    await row.getByText("Moved", { exact: true }).first().waitFor();
    const mig = (await apiCall("GET", "/api/migrations")).find((m) => m.source === "notepadpp");
    assert.deepEqual(
      mig.items.map((i) => i.label),
      ["Settings and shortcuts", "Themes"],
    );
    assert.deepEqual(mig.revertOptions, ["move-back"]);
    // moving back is the only way back for a move
    await row.getByTestId("migrate-revert").click();
    assert.equal(await row.getByTestId("migrate-revert-how").getByRole("radio").count(), 1);
    await revert("notepadpp", "Move back");
    await result("Reverted.");
    await historyRow("notepadpp", "reverted").waitFor();
  });

  await step("import: adopt a registered runtime from the Runtimes page; Move is copy-only", async () => {
    await page.goto(BASE + "/#/runtimes");
    await page.getByRole("button", { name: "Detect on this PC" }).click();
    await page.getByText("Found on this PC").waitFor();
    await page.getByTestId("bring-in-hint").waitFor();
    // the card notes: installed VS Code settings, and the registered Java
    await page
      .locator('article[data-kind="vscode"]')
      .getByTestId("bring-in-note")
      .getByText(/Your installed VS Code settings can be imported/)
      .waitFor();
    await page
      .locator('article[data-kind="java"]')
      .getByTestId("bring-in-note")
      .getByRole("link", { name: "Adopt into Zero2Dev" })
      .waitFor();
    await page.locator("tr", { hasText: "java" }).getByTestId("adopt-runtime").click();
    await page.waitForURL(/#\/migrate\?source=runtime%3A/);
    const s = plan();
    await s.waitFor();
    await s
      .getByText(/Java 21.0.4/)
      .first()
      .waitFor();
    const move = s.getByTestId("migrate-mode").getByRole("radio", { name: /^Move/ });
    assert.ok(await move.isDisabled(), "a runtime cannot be moved");
    await s
      .getByTestId("migrate-copy-only")
      .getByText(/belong to the program's own installer/)
      .waitFor();
    await s.getByTestId("migrate-review").click();
    await page
      .getByTestId("migrate-review-text")
      .getByText(/Services that use it keep working/)
      .waitFor();
    await page.getByTestId("migrate-start").click();
    await result("Done.");
    let java = (await apiCall("GET", "/api/runtimes")).find((r) => r.kind === "java" && r.version === "21.0.4");
    assert.equal(java.source, "installed");
    assert.ok(java.binary.includes("\\adopted\\java-21.0.4\\bin\\java.exe"), java.binary);
    // revert: the registered program comes back
    await revert(`runtime:${java.id}`, "Discard the copy");
    await result("Reverted.");
    java = (await apiCall("GET", "/api/runtimes")).find((r) => r.id === java.id);
    assert.equal(java.source, "registered");
    assert.equal(java.binary, "C:\\Program Files\\Java\\jdk-21\\bin\\java.exe");
  });

  await step("import: the database copy form checks what it needs", async () => {
    const services = await apiCall("GET", "/api/services");
    if (!services.some((x) => x.name === "Legacy DB"))
      await apiCall("POST", "/api/services", {
        name: "Legacy DB",
        kind: "mariadb",
        command: "",
        args: [],
        cwd: "",
        env: {},
        port: 3310,
        healthPath: "",
        runtimeId: "",
        autostart: false,
      });
    await page.goto(BASE + "/#/migrate?source=database");
    const s = plan();
    await s.getByTestId("migrate-db-form").waitFor();
    assert.ok((await s.getByTestId("migrate-mode").count()) === 0, "databases are copy-only, no mode choice");
    const form = s.getByTestId("migrate-db-form");
    await form.getByLabel("Host").fill("");
    await form.getByLabel("Port").fill("99999");
    await form.getByLabel("User").fill("");
    await s.getByTestId("migrate-db-list").click();
    await form.getByText("Enter the server's host name or address").waitFor();
    await form.getByText("The port is a number from 1 to 65535.").waitFor();
    await form.getByText("Enter the user name to sign in with.").waitFor();
    assert.equal(await form.getByLabel("Host").getAttribute("aria-invalid"), "true");
    // a Zero2Dev service is not the server to copy from
    await form.getByLabel("Host").fill("127.0.0.1");
    await form.getByLabel("Port").fill("3310");
    await form.getByLabel("User").fill("root");
    await s.getByTestId("migrate-db-list").click();
    await form.getByText(/Port 3310 is Legacy DB, a Zero2Dev service/).waitFor();
    // the server's own answer is shown as it is
    await form.getByLabel("Port").fill("3306");
    await form.getByLabel("Password").fill("wrong");
    await s.getByTestId("migrate-db-list").click();
    await s
      .getByTestId("migrate-db-error")
      .getByText(/Access denied for user 'root'/)
      .waitFor();
    await form.getByLabel("Password").fill("secret");
    await s.getByTestId("migrate-db-list").click();
    const found = s.getByTestId("migrate-db-list-result");
    for (const n of ["shop", "blog", "crm"]) await found.getByText(n, { exact: true }).waitFor();
    // pick nothing: Review says what is missing
    await s.getByTestId("migrate-review").click();
    await found.getByText("Pick at least one database.").waitFor();
    await found.getByRole("checkbox", { name: /shop/ }).check();
    await s.getByTestId("migrate-db-target").selectOption({ label: "Legacy DB (mariadb, port 3310)" });
    await form.getByText(/Legacy DB is stopped/).waitFor();
    await s.getByTestId("migrate-review").click();
    await page
      .getByTestId("migrate-review-text")
      .getByText(
        "This copies the database shop (48 MB) from 127.0.0.1:3306 into Legacy DB. The server you copy from is not changed.",
      )
      .waitFor();
    await page.getByTestId("migrate-start").click();
    // the daemon refuses while the target is stopped, and the plan shows why
    await s.getByTestId("migrate-error").getByText("start Legacy DB first").waitFor();
  });

  // ---- 0.0.5 Import 2.0: guidance, free space, several at once, clean-up, backups ----
  const primaries = () => page.locator('[data-testid="migrate-page"] .btn.primary:visible').count();

  await step("import: what is worth it, the summary, and one primary button", async () => {
    await page.goto(BASE + "/#/migrate");
    await page.getByTestId("migrate-summary").waitFor();
    assert.ok(Number(await page.getByTestId("migrate-sum-found").innerText()) >= 4);
    // the plan of a cache says it is not worth it; the settings say they are
    let s = await openSource("npm-cache");
    await s.getByTestId("migrate-items").getByText("Not worth it").waitFor();
    await s
      .getByTestId("migrate-advice")
      .getByText(/fills up again by itself/)
      .waitFor();
    assert.equal(await primaries(), 1, "the open plan has the one primary button");
    s = await openSource("vscode");
    await s.getByTestId("migrate-items").getByText("Recommended").first().waitFor();
    await page.getByTestId("migrate-empty-plan").waitFor({ state: "detached" });
    await s.getByRole("button", { name: "Cancel" }).click();
    await page.getByTestId("migrate-empty-plan").waitFor();
    // blocked programs show why in plain words, without an alarm badge
    await source("dbeaver").getByTestId("migrate-why").getByText("Install DBeaver in Zero2Dev first.").waitFor();
    assert.equal(await source("dbeaver").locator(".badge.warn").count(), 0);
    // nothing selected: Select recommended is the one primary button
    assert.equal(await primaries(), 1);
    assert.ok(await page.getByTestId("migrate-select").evaluate((b) => b.classList.contains("primary")));
    const rec = await page.getByTestId("migrate-recommended").innerText();
    for (const name of ["VS Code", "Notepad++", "HeidiSQL", "Global npm packages"]) assert.ok(rec.includes(name), name);
    assert.ok(!rec.includes("download cache") && !rec.includes("Go"), "caches are not recommended");
  });

  await step("import: free space warns when tight and stops when short", async () => {
    await apiCall("POST", "/api/mock/migrations/free", { gb: 0.5 });
    await page.getByTestId("migrate-refresh").click();
    const s = await openSource("go-cache");
    await s
      .getByTestId("migrate-free-space")
      .getByText(/only 500 MB is free.*Free some space first/)
      .waitFor();
    assert.ok(await s.getByTestId("migrate-review").isDisabled(), "Review waits for room");
    await apiCall("POST", "/api/mock/migrations/free", { gb: 1.2 });
    await page.getByTestId("migrate-refresh").click();
    await s
      .getByTestId("migrate-free-space")
      .getByText(/That is tight/)
      .waitFor();
    assert.ok(await s.getByTestId("migrate-review").isEnabled(), "tight still lets you go on");
    await apiCall("POST", "/api/mock/migrations/free", {});
    await page.getByTestId("migrate-refresh").click();
    await s
      .getByTestId("migrate-free-space")
      .getByText(/Needs 1.1 GB; 38 GB is free/)
      .waitFor();
    await s.getByRole("button", { name: "Cancel" }).click();
  });

  await step("import: several at once, one after another, each in History", async () => {
    await page.getByTestId("migrate-select").click();
    const bar = page.getByTestId("migrate-batch-bar");
    await bar.getByText(/4.* selected/).waitFor();
    assert.equal(await primaries(), 1, "the batch bar owns the primary button now");
    assert.ok(await page.getByTestId("migrate-batch-review").evaluate((b) => b.classList.contains("primary")));
    // one row ticked off by hand
    await source("heidisql").getByTestId("migrate-pick").uncheck();
    await bar.getByText(/3.* selected/).waitFor();
    await source("heidisql").getByTestId("migrate-pick").check();
    await page.getByTestId("migrate-batch-review").click();
    const dlg = page.getByTestId("migrate-batch-dialog");
    for (const id of ["vscode", "notepadpp", "heidisql", "npm-global"])
      await dlg.locator(`[data-testid="migrate-batch-entry"][data-source="${id}"]`).waitFor();
    await dlg.getByText(/Needs .* is free/).waitFor();
    await page.getByTestId("migrate-batch-start").click();
    await page.getByTestId("migrate-batch-result").getByText("All 4 are in Zero2Dev now.").waitFor({ timeout: 20000 });
    for (const id of ["vscode", "notepadpp", "heidisql", "npm-global"])
      await dlg.locator(`[data-testid="migrate-batch-entry"][data-source="${id}"][data-state="done"]`).waitFor();
    await page.getByTestId("migrate-batch-done").click();
    const done = (await apiCall("GET", "/api/migrations")).filter((m) => m.state === "done").map((m) => m.source);
    for (const id of ["vscode", "notepadpp", "heidisql", "npm-global"]) assert.ok(done.includes(id), id);
    await source("vscode").getByTestId("migrate-status").getByText("Copied", { exact: true }).waitFor();
    // imported: no tick any more, and nothing left selected
    assert.equal(await source("vscode").getByTestId("migrate-pick").count(), 0);
    assert.equal(await page.getByTestId("migrate-batch-bar").count(), 0);
  });

  await step("import: history details, filter, and removing the old files after a copy", async () => {
    await openHistory();
    const row = historyRow("vscode", "done").first();
    await row.getByTestId("migrate-details").click();
    const detail = row.getByTestId("migrate-history-detail");
    await detail.getByRole("cell", { name: "C:\\Users\\me\\.vscode\\extensions" }).waitFor();
    await detail
      .getByText(/Backups/)
      .first()
      .waitFor();
    // the clean-up asks first and says what it does
    await detail.getByTestId("migrate-cleanup").click();
    const dlg = page.getByRole("dialog");
    await dlg.getByText(/checks that its copy is complete first/).waitFor();
    await dlg.getByText(/Revert can put the files back/).waitFor();
    await dlg.getByRole("button", { name: "Remove the old files", exact: true }).click();
    await result("Old files removed.");
    await openHistory();
    const after = historyRow("vscode", "done").first();
    await after.getByText("Moved", { exact: true }).first().waitFor();
    const mig = (await apiCall("GET", "/api/migrations")).find((m) => m.source === "vscode" && m.state === "done");
    assert.equal(mig.mode, "move");
    assert.deepEqual(mig.revertOptions, ["move-back"]);
    assert.equal(mig.canCleanup, false);
    await after.getByTestId("migrate-details").click();
    assert.equal(await after.getByTestId("migrate-cleanup").count(), 0, "the old files are gone already");
    // the filter keeps only what it names
    await page.getByTestId("migrate-history-filter").getByText("Reverted", { exact: true }).click();
    assert.equal(await page.locator('[data-testid="migrate-history-row"][data-state="done"]').count(), 0);
    assert.ok((await page.locator('[data-testid="migrate-history-row"][data-state="reverted"]').count()) >= 1);
    await page.getByTestId("migrate-history-filter").getByText("All", { exact: true }).click();
  });

  await step("import: backups are counted and can be deleted; a reverted record can be removed", async () => {
    await openHistory();
    // Notepad++ had data of its own in Zero2Dev, so its copy saved it first
    const row = historyRow("notepadpp", "done").first();
    await row.getByText(/Backup 3.0 MB/).waitFor();
    await page.getByTestId("migrate-backups-total").getByText("3.0 MB").waitFor();
    await row.getByTestId("migrate-details").click();
    await row.getByTestId("migrate-backups-delete").click();
    const dlg = page.getByRole("dialog");
    await dlg.getByText(/a revert will no longer bring those back/).waitFor();
    await dlg.getByRole("button", { name: "Delete backups", exact: true }).click();
    await row.getByText(/Backup 3.0 MB/).waitFor({ state: "detached" });
    assert.equal(
      (await apiCall("GET", "/api/migrations")).find((m) => m.source === "notepadpp" && m.state === "done").backupBytes,
      0,
    );
    // a reverted record leaves the list for good
    const before = await page.locator('[data-testid="migrate-history-row"][data-state="reverted"]').count();
    const old = historyRow("notepadpp", "reverted").first();
    await old.getByTestId("migrate-details").click();
    await old.getByTestId("migrate-remove").click();
    await page.getByRole("dialog").getByRole("button", { name: "Remove from history", exact: true }).click();
    await page.waitForFunction(
      (n) => document.querySelectorAll('[data-testid="migrate-history-row"][data-state="reverted"]').length === n,
      before - 1,
    );
    // a copy of a runtime has no old files to remove, and the page says why
    assert.equal(await page.getByTestId("migrate-remove").count(), 0, "only one record open at a time shows Remove");
    await shot(page, "41-migrate-history");
  });
}
