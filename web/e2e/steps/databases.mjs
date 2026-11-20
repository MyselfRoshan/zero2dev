// e2e steps for the 0.0.5 databases work, run by smoke.mjs against the mock (before the final checks).
//   export default async function steps(t)
// t: { page, BASE, step, apiCall, shot, assert, customService, serviceRow, pickType }
//
// A MariaDB service: the tools explain what needs the server running; then create a database and
// a user, import a dump (progress to done, and a failing one), export (and replace the file), set
// and remove the administrator password (the Connection section follows), and reset it, stopped.

export default async function steps(t) {
  const { page, BASE, step, apiCall, shot, assert } = t;
  let svc;
  const tools = () => page.getByTestId("db-tools");
  const dialog = (name) => page.getByRole("dialog", name ? { name } : undefined);

  // The real "Take a snapshot first?" offer (snapshot-ui) may open before an import or a reset;
  // this test goes ahead without one. With the Wave 0 stub nothing opens.
  async function passSnapshotOffer(until) {
    const offer = page.getByRole("dialog", { name: /snapshot/i });
    const which = await Promise.race([
      until.waitFor({ timeout: 8000 }).then(() => "done"),
      offer.waitFor({ timeout: 8000 }).then(() => "offer"),
    ]);
    if (which === "offer") {
      await offer
        .getByRole("button", { name: /without|skip|go ahead|continue/i })
        .first()
        .click();
      await until.waitFor();
    }
  }
  const jobDone = async (state = "done") => {
    await page.locator(`[data-testid="db-job"][data-state="${state}"]`).waitFor({ timeout: 10000 });
    return (await page.getByTestId("db-job-text").textContent()).trim();
  };

  await step("databases: a MariaDB service explains what needs the server running", async () => {
    if (!(await apiCall("GET", "/api/runtimes")).some((r) => r.kind === "mariadb")) {
      await apiCall("POST", "/api/runtimes/install", { catalogId: "mariadb-11.4.13" });
      for (let i = 0; i < 80 && !(await apiCall("GET", "/api/runtimes")).some((r) => r.kind === "mariadb"); i++)
        await page.waitForTimeout(250);
    }
    svc = await apiCall("POST", "/api/services", {
      name: "Orders DB",
      kind: "mariadb",
      command: "",
      args: [],
      settings: {},
      cwd: "",
      env: {},
      port: 3310,
      healthPath: "",
      runtimeId: "",
      autostart: false,
    });
    assert.ok(svc?.id, JSON.stringify(svc));
    await page.goto(`${BASE}/#/services/${svc.id}`);
    await tools().waitFor();
    await page
      .getByTestId("db-state-note")
      .getByText(/Orders DB is not running/)
      .waitFor();
    // stopped: the tools that need the server are off, and say why
    for (const id of ["db-create", "db-user", "db-import", "db-export", "db-password-set"])
      assert.equal(await page.getByTestId(id).isDisabled(), true, id + " is disabled while stopped");
    await page
      .getByTestId("db-why")
      .getByText("Start Orders DB first: a database is created inside the running server.")
      .waitFor();
    await page
      .getByTestId("db-password-why")
      .getByText(/^Start Orders DB first/)
      .waitFor();
    assert.equal(await page.getByTestId("db-reset").isDisabled(), false, "reset works while stopped");
    assert.equal(await page.getByTestId("connection-url").textContent(), "mysql://root@127.0.0.1:3310");
    await page.getByTestId("connection-password").getByText("none").waitFor();
    // running: the tools turn on, reset turns off
    await page.getByRole("button", { name: "Start Orders DB", exact: true }).click();
    await page.getByTestId("db-empty").waitFor({ timeout: 8000 });
    assert.equal(await page.getByTestId("db-create").isDisabled(), false);
    assert.equal(await page.getByTestId("db-state-note").count(), 0);
    assert.equal(await page.getByTestId("db-reset").isDisabled(), true);
    await page
      .getByTestId("db-reset-why")
      .getByText(/^Stop Orders DB first/)
      .waitFor();
  });

  await step("databases: create a database and a user", async () => {
    await page.getByTestId("db-create").click();
    let dlg = dialog(/Create a database/);
    await dlg.getByLabel("Database name").fill("shop-db");
    await dlg.getByRole("button", { name: "Create database" }).click();
    await dlg.getByText("Database name must be 1-64 letters, digits or _.").waitFor();
    await dlg.getByLabel("Database name").fill("mysql");
    await dlg.getByRole("button", { name: "Create database" }).click();
    await dlg.getByText(/system database/).waitFor();
    await dlg.getByLabel("Database name").fill("shop");
    await dlg.getByRole("button", { name: "Create database" }).click();
    await page.locator('[data-testid="db-row"][data-db="shop"]').waitFor();
    assert.equal(await dlg.count(), 0, "the dialog closed");
    // the same name again: the daemon's message, inline
    await page.getByTestId("db-create").click();
    dlg = dialog(/Create a database/);
    await dlg.getByLabel("Database name").fill("shop");
    await dlg.getByRole("button", { name: "Create database" }).click();
    await dlg.getByRole("alert").getByText("database shop exists").waitFor();
    await dlg.getByRole("button", { name: "Cancel" }).click();

    await page.getByTestId("db-user").click();
    dlg = dialog(/Create a user/);
    await dlg.getByLabel("User name").fill("shop_app");
    await dlg.getByRole("button", { name: "Create user" }).click();
    await dlg.getByText("Enter a password.").waitFor();
    await dlg.getByRole("button", { name: "Generate" }).click();
    assert.match(await dlg.getByLabel("Password", { exact: true }).inputValue(), /^[A-Za-z0-9]{20}$/);
    await dlg.getByLabel("Access to").selectOption("shop");
    await dlg.getByRole("button", { name: "Create user" }).click();
    await page.getByText("Created the user shop_app with all privileges on shop.").waitFor();
    await page
      .getByTestId("db-users")
      .getByText(/shop_app/)
      .waitFor();
  });

  await step("databases: import a dump with progress, and a failing one", async () => {
    await page.getByRole("button", { name: "Import a dump into shop" }).click();
    const dlg = dialog(/Import a dump/);
    assert.equal(await dlg.getByLabel("Into the database").inputValue(), "shop");
    await dlg.getByRole("button", { name: "Import" }).click();
    await dlg.getByText(/Enter the path of the file/).waitFor();
    await dlg.getByRole("button", { name: "Browse for the dump file" }).click();
    await page.waitForFunction(() => document.querySelector("#db-imp-file")?.value.endsWith("shop.sql"));
    assert.equal(await dlg.getByLabel("Dump file", { exact: true }).inputValue(), "C:\\Users\\me\\Downloads\\shop.sql");
    await dlg.getByRole("button", { name: "Import" }).click();
    await passSnapshotOffer(page.getByTestId("db-job"));
    // progress while it runs, then done
    await page
      .getByTestId("db-job-text")
      .getByText(/^Importing shop\.sql into shop\.\.\./)
      .waitFor();
    await page.waitForFunction(
      () =>
        Number(document.querySelector('[data-testid="db-job"] [role="progressbar"]')?.getAttribute("aria-valuenow")) >
        0,
    );
    await shot(page, "db-01-import-progress");
    // while it runs, nothing else starts on this server
    assert.equal(await page.getByTestId("db-export").isDisabled(), true);
    await page.getByTestId("db-why").getByText("Wait until an import on Orders DB has finished.").waitFor();
    assert.equal(await jobDone(), "Imported shop.sql into shop.");
    await page.locator('[data-testid="db-row"][data-db="shop"] td.num').getByText(/MB$/).waitFor();
    await page.getByTestId("db-job-dismiss").click();
    assert.equal(await page.getByTestId("db-job").count(), 0);

    // into a new database, from a dump with an error
    await page.getByTestId("db-import").click();
    await dlg.getByLabel("Into the database").selectOption({ label: "A new database..." });
    await dlg.getByLabel("New database name").fill("broken");
    await dlg.getByLabel("Dump file", { exact: true }).fill("C:\\dumps\\bad.sql");
    await dlg.getByRole("button", { name: "Import" }).click();
    await passSnapshotOffer(page.getByTestId("db-job"));
    assert.match(await jobDone("failed"), /^The import of bad\.sql into broken failed: ERROR 1064/);
    await page.locator('[data-testid="db-row"][data-db="broken"]').waitFor();
    // a file that is not there: the daemon's 400, inline
    await page.getByTestId("db-import").click();
    await dlg.getByLabel("Dump file", { exact: true }).fill("C:\\dumps\\missing.sql");
    await dlg.getByRole("button", { name: "Import" }).click();
    await passSnapshotOffer(dlg.getByRole("alert"));
    await dlg.getByRole("alert").getByText("C:\\dumps\\missing.sql does not exist").waitFor();
    await dlg.getByRole("button", { name: "Cancel" }).click();
  });

  await step("databases: export, and replace the file after a confirm", async () => {
    await page.getByRole("button", { name: "Export shop" }).click();
    const dlg = dialog(/Export a database/);
    await dlg.getByRole("button", { name: "Browse for where to save the dump" }).click();
    await page.waitForFunction(() =>
      /\\shop-\d{4}-\d\d-\d\d\.sql$/.test(document.querySelector("#db-exp-file")?.value ?? ""),
    );
    const file = await dlg.getByLabel("Save as", { exact: true }).inputValue();
    assert.match(file, /^C:\\Users\\me\\Documents\\shop-\d{4}-\d\d-\d\d\.sql$/);
    await dlg.getByRole("button", { name: "Export" }).click();
    assert.equal(await jobDone(), `Exported shop to ${file}.`);
    // the same file again: Zero2Dev asks before replacing it
    await page.getByRole("button", { name: "Export shop" }).click();
    await dlg.getByLabel("Save as", { exact: true }).fill(file);
    await dlg.getByRole("button", { name: "Export" }).click();
    const ask = dialog(/^Replace /);
    await ask.getByText(`${file} already exists. Exporting replaces it with a new dump of shop.`).waitFor();
    await ask.getByRole("button", { name: "Replace file" }).click();
    await page.locator('[data-testid="db-job"][data-state="running"]').waitFor();
    assert.equal(await jobDone(), `Exported shop to ${file}.`);
    assert.equal(await page.getByRole("dialog").count(), 0, "both dialogs closed");
    await shot(page, "db-02-tools");
  });

  await step("databases: set and remove the administrator password (Connection follows)", async () => {
    await page.getByTestId("db-password-set").click();
    const dlg = dialog(/Set the password of root/);
    await dlg.getByLabel("New password").fill("p@ss word");
    await dlg.getByRole("button", { name: "Set password" }).click();
    await page
      .getByTestId("db-password-state")
      .getByText(/^root has a password/)
      .waitFor();
    assert.equal(
      await page
        .getByTestId("db-password-set")
        .textContent()
        .then((s) => s.trim()),
      "Change password",
    );
    // masked until shown; the URL carries the password, URL-escaped
    assert.equal(await page.getByTestId("connection-url").textContent(), "mysql://root:********@127.0.0.1:3310");
    assert.ok(!(await page.getByTestId("connection-password-value").textContent()).includes("p@ss"));
    await page
      .getByTestId("connection-hint")
      .getByText(/^Password set: apps get it in DB_PASSWORD/)
      .waitFor();
    await page.getByTestId("connection-password-toggle").click();
    assert.equal(await page.getByTestId("connection-password-value").textContent(), "p@ss word");
    assert.equal(await page.getByTestId("connection-url").textContent(), "mysql://root:p%40ss%20word@127.0.0.1:3310");
    await page.getByTestId("connection-url-copy").click();
    assert.equal(
      await page.evaluate(() => navigator.clipboard.readText()),
      "mysql://root:p%40ss%20word@127.0.0.1:3310",
    );
    const stored = (await apiCall("GET", `/api/services/${svc.id}`)).adminPassword;
    assert.equal(stored, "p@ss word");
    await shot(page, "db-03-password");
    // remove it
    await page.getByTestId("db-password-remove").click();
    const ask = dialog(/Remove the password of root/);
    await ask.getByRole("button", { name: "Remove password" }).click();
    await page
      .getByTestId("db-password-state")
      .getByText(/^root has no password/)
      .waitFor();
    assert.equal(await page.getByTestId("connection-url").textContent(), "mysql://root@127.0.0.1:3310");
    await page
      .getByTestId("connection-hint")
      .getByText(/^No password/)
      .waitFor();
    assert.equal(await page.getByTestId("db-password-remove").count(), 0);
  });

  await step("databases: reset, stopped, behind a confirm that says what is deleted", async () => {
    // a password to see it cleared
    await apiCall("PUT", `/api/services/${svc.id}/database/password`, { password: "x" });
    await page
      .getByTestId("db-password-state")
      .getByText(/^root has a password/)
      .waitFor();
    await page.getByRole("button", { name: "Stop Orders DB", exact: true }).click();
    await page
      .getByTestId("db-state-note")
      .getByText(/Orders DB is not running/)
      .waitFor({ timeout: 8000 });
    await page.getByTestId("db-reset").click();
    const ask = dialog(/Reset Orders DB/);
    await ask.getByText(/every database \(broken, shop\), every user and the administrator password/).waitFor();
    await ask.getByText(/\\mariadb\\svc_/).waitFor();
    await ask.getByText(/cannot be undone/).waitFor();
    await shot(page, "db-04-reset-confirm");
    await ask.getByRole("button", { name: "Reset database" }).click();
    await passSnapshotOffer(page.getByText("Orders DB was reset. The next start sets it up again, empty."));
    await page
      .getByTestId("db-password-state")
      .getByText(/^root has no password/)
      .waitFor();
    assert.equal((await apiCall("GET", `/api/services/${svc.id}`)).adminPassword, "");
    // empty after the next start
    await page.getByRole("button", { name: "Start Orders DB", exact: true }).click();
    await page.getByTestId("db-empty").waitFor({ timeout: 8000 });
    await page.getByRole("button", { name: "Stop Orders DB", exact: true }).click();
    await page.getByTestId("db-state-note").waitFor({ timeout: 8000 });
    await apiCall("DELETE", `/api/services/${svc.id}`);
  });
}
