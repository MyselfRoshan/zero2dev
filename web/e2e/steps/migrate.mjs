// e2e steps for Bring in (0.0.5, migrate-ui), run by smoke.mjs against the mock (before the final checks).
//   export default async function steps(t)
// t: { page, BASE, step, apiCall, shot, assert, customService, serviceRow, pickType }
// The mock's sources (web/mock/features/migrations.mjs): VS Code and HeidiSQL are installed in
// Zero2Dev by the earlier steps, Notepad++ is not; Java 21.0.4 is registered by path.
export default async function steps({ page, BASE, step, apiCall, shot, assert }) {
  const source = (id) => page.locator(`[data-testid="migrate-source"][data-source="${id}"]`);
  const openSource = async (id) => {
    const s = source(id);
    await s.waitFor();
    const b = s.getByTestId("migrate-open");
    if ((await b.getAttribute("aria-expanded")) !== "true") await b.click();
    await s.getByTestId("migrate-plan").waitFor();
    return s;
  };
  const historyRow = (sourceId, state) =>
    page.locator(`[data-testid="migrate-history-row"][data-source="${sourceId}"][data-state="${state}"]`);
  const result = (text) => page.getByTestId("migrate-result").getByText(text).waitFor({ timeout: 10000 });
  const setRunning = (program, running) => apiCall("POST", "/api/mock/migrations/running", { program, running });
  const revert = async (sourceId, how) => {
    const row = historyRow(sourceId, "done").first();
    const open = row.getByTestId("migrate-revert");
    if ((await open.getAttribute("aria-expanded")) !== "true") await open.click();
    const panel = row.getByTestId("migrate-revert-panel");
    await panel.getByRole("radio", { name: new RegExp(how) }).check();
    await panel.getByTestId("migrate-revert-go").click();
    const dlg = page.getByRole("dialog");
    await dlg.getByRole("button", { name: how, exact: true }).click();
  };

  await step("bring in: sources by group, a blocker with Check again, copy VS Code, in history", async () => {
    await page.goto(BASE + "/#/migrate");
    await page.getByRole("heading", { level: 1, name: "Bring in" }).waitFor();
    for (const h of ["Editors and tools", "Runtimes", "Caches and global packages", "Databases", "History"])
      await page.getByRole("heading", { name: h, exact: false }).first().waitFor();
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
    await historyRow("vscode", "done").getByText("Copied", { exact: true }).waitFor();
    const list = await apiCall("GET", "/api/migrations");
    assert.equal(list[0].source, "vscode");
    assert.equal(list[0].mode, "copy");
    // brought in already: a second copy waits for a revert
    await openSource("vscode");
    await source("vscode").getByTestId("migrate-already").waitFor();
    await shot(page, "40-migrate");
  });

  await step("bring in: revert the VS Code copy with discard, behind a confirm", async () => {
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

  await step("bring in: move Notepad++ into Zero2Dev, then move it back", async () => {
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
    await s.getByTestId("migrate-mode").getByText("Move (originals moved into Zero2Dev)").click();
    await s.getByText(/removed from their old place/).waitFor();
    await s.getByRole("checkbox", { name: /Plugin settings/ }).uncheck();
    await s.getByTestId("migrate-review").click();
    await page
      .getByTestId("migrate-review-text")
      .getByText(/^This moves 1.5 MB \(settings and shortcuts and themes\) of Notepad\+\+/)
      .waitFor();
    await page.getByTestId("migrate-start").click();
    await result("Done.");
    const row = historyRow("notepadpp", "done");
    await row.getByText("Moved", { exact: true }).waitFor();
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

  await step("bring in: adopt a registered runtime from the Runtimes page; Move is copy-only", async () => {
    await page.goto(BASE + "/#/runtimes");
    await page.getByRole("button", { name: "Detect on this PC" }).click();
    await page.getByText("Found on this PC").waitFor();
    await page.getByTestId("bring-in-hint").waitFor();
    // the card notes: installed VS Code settings, and the registered Java
    await page
      .locator('article[data-kind="vscode"]')
      .getByTestId("bring-in-note")
      .getByText(/Your installed VS Code settings can be brought in/)
      .waitFor();
    await page
      .locator('article[data-kind="java"]')
      .getByTestId("bring-in-note")
      .getByRole("link", { name: "Adopt into Zero2Dev" })
      .waitFor();
    await page.locator("tr", { hasText: "java" }).getByTestId("adopt-runtime").click();
    await page.waitForURL(/#\/migrate\?source=runtime%3A/);
    const s = page.locator('[data-testid="migrate-source"][data-source^="runtime:"]', {
      has: page.getByTestId("migrate-plan"),
    });
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

  await step("bring in: the database copy form checks what it needs", async () => {
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
    const s = source("database");
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
}
