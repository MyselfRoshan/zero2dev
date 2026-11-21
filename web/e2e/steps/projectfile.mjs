// e2e steps for the project file (0.0.6, part 2), run by smoke.mjs against the mock (before the final checks).
//   export default async function steps(t)
// t: { page, BASE, step, apiCall, shot, assert, customService, serviceRow, pickType }
// The mock (web/mock/features/projectfile.mjs): a path with "bad" is a problem, "missing-runtime" an install,
// "port" a change, "fail-me" makes the job fail; an export to a file that was exported before is "exists".
export default async function steps({ page, BASE, step, apiCall, assert }) {
  const dlg = () => page.getByTestId("export-dialog");
  const row = (name) => page.locator(`tr[data-project="${name}"]`);
  const path = () => page.getByTestId("import-path");
  const check = async (p) => {
    await path().fill(p);
    await page.getByTestId("import-check").click();
    await page.locator('[data-testid="import-plan"][data-phase="ready"]').waitFor({ timeout: 8000 });
  };

  await step("project file: export from a project row shows what was left out", async () => {
    const svc = await apiCall("POST", "/api/services", {
      name: "Export API",
      kind: "node",
      command: "",
      args: ["server.js"],
      cwd: "C:\\work\\exportshop",
      env: { APP_ENV: "local", APP_KEY: "base64:abc" },
      port: 3411,
      healthPath: "",
      runtimeId: "",
      autostart: false,
    });
    await apiCall("POST", "/api/projects", { name: "Export shop", serviceIds: [svc.id], siteIds: [] });
    await page.goto(BASE + "/#/projects");
    await row("Export shop").getByTestId("project-export").click();
    await dlg().waitFor();
    // the suggestion is the project's folder plus zero2dev.json
    assert.equal(await page.getByTestId("export-path").inputValue(), "C:\\work\\exportshop\\zero2dev.json");
    await page.getByTestId("export-go").click();
    await page.getByTestId("export-saved").getByText("Saved to C:\\work\\exportshop\\zero2dev.json.").waitFor();
    const warns = await page.getByTestId("export-warning").allTextContents();
    assert.equal(warns.length, 1);
    assert.match(warns[0], /APP_KEY/);
    assert.match(warns[0], /secret/);
    await page.getByTestId("export-close").click();
    await dlg().waitFor({ state: "detached" });
  });

  await step("project file: an existing file is only replaced after Overwrite", async () => {
    await row("Export shop").getByTestId("project-export").click();
    await page.getByTestId("export-go").click();
    await page.getByTestId("export-exists").waitFor();
    assert.match(await page.getByTestId("export-exists").innerText(), /exists/);
    // no automatic overwrite: Export is gone, Overwrite is offered
    assert.equal(await page.getByTestId("export-go").count(), 0);
    await page.getByTestId("export-overwrite").click();
    await page.getByTestId("export-saved").waitFor();
    await page.getByTestId("export-close").click();
    // a folder in another place: the project's folder is outside it, so that is said too
    await row("Export shop").getByTestId("project-export").click();
    await page.getByTestId("export-path").fill("D:\\work\\elsewhere");
    await page.getByTestId("export-go").click();
    await page.getByTestId("export-saved").getByText("D:\\work\\elsewhere\\zero2dev.json").waitFor();
    const warns = (await page.getByTestId("export-warning").allTextContents()).join("\n");
    assert.match(warns, /APP_KEY/);
    assert.match(warns, /outside/);
    await page.getByTestId("export-close").click();
  });

  await step("project file: the Projects page leads to Import project", async () => {
    await page.goto(BASE + "/#/projects");
    await page.getByTestId("projects-import").click();
    await page.getByTestId("import-project-page").waitFor();
    assert.equal(await page.getByTestId("import-go").isDisabled(), true);
    await page.getByTestId("import-outline-note").waitFor();
  });

  await step("project file: a problem blocks Import and is listed in words", async () => {
    await check("D:\\work\\bad-shop");
    await page.getByTestId("import-problems").waitFor();
    assert.match(await page.getByTestId("import-problem").first().innerText(), /format 2/);
    assert.equal(await page.getByTestId("import-go").isDisabled(), true);
    assert.match(await page.getByTestId("import-why").innerText(), /problems/);
    // a change of the path drops the stale plan
    await path().fill("D:\\work\\bad-shop2");
    await page.locator('[data-testid="import-plan"][data-phase="idle"]').waitFor();
  });

  await step("project file: a clean plan shows installs and changes, Import runs and links the result", async () => {
    await check("D:\\work\\missing-runtime-port-shop");
    assert.equal(await page.getByTestId("import-problems").count(), 0);
    assert.equal(await page.getByTestId("import-install").count(), 1);
    await page.getByTestId("import-install").getByText("PHP 8.4.1").waitFor();
    await page
      .getByTestId("import-change")
      .getByText(/Port 8000 is taken/)
      .waitFor();
    await page
      .getByTestId("import-services")
      .getByText(/Missing Runtime Port Shop \(PHP, port 80\d\d\)/)
      .waitFor();
    assert.equal(await page.getByTestId("import-go").isEnabled(), true);
    await page.getByTestId("import-go").click();
    await page.getByTestId("import-progress").waitFor();
    await page.getByTestId("import-result").waitFor({ timeout: 15000 });
    await page.getByTestId("import-link-service").waitFor();
    await page.getByTestId("import-link-site").waitFor();
    const link = page.getByTestId("import-link-project");
    await link.getByText("Missing Runtime Port Shop").waitFor();
    // the project, service and site exist in the daemon, and PHP was installed
    const projects = await apiCall("GET", "/api/projects");
    const made = projects.find((p) => p.name === "Missing Runtime Port Shop");
    assert.ok(made, "the imported project exists");
    assert.equal(made.serviceIds.length, 1);
    assert.equal(made.siteIds.length, 1);
    assert.ok((await apiCall("GET", "/api/runtimes")).some((r) => r.catalogId === "php-8.4.1-windows-amd64"));
    await page.getByTestId("import-start").click();
    await page.getByTestId("import-start").getByText("Started").waitFor({ timeout: 15000 });
    // later steps (the quit confirmation) expect only the demo service to run
    await apiCall("POST", `/api/services/${made.serviceIds[0]}/stop`);
    for (let i = 0; i < 60 && (await apiCall("GET", `/api/services/${made.serviceIds[0]}`)).state !== "stopped"; i++)
      await page.waitForTimeout(150);
    await link.click();
    await page.waitForURL(new RegExp(`#/projects/${made.id}/edit`));
  });

  await step("project file: a failed import says why and nothing is left", async () => {
    const before = (await apiCall("GET", "/api/projects")).length;
    await page.goto(BASE + "/#/projects/import");
    await check("D:\\work\\fail-me-shop");
    await page.getByTestId("import-go").click();
    await page.getByTestId("import-failed").waitFor({ timeout: 15000 });
    assert.match(await page.getByTestId("import-error").innerText(), /port could not be reserved/);
    assert.equal((await apiCall("GET", "/api/projects")).length, before);
    await page.getByTestId("import-back").click();
    await page.getByTestId("import-check").waitFor();
  });
}
