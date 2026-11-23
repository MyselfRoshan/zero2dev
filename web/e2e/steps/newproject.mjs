// e2e steps for Create a project (0.0.6, create-ui), run by smoke.mjs against the mock (before the final checks).
//   export default async function steps(t)
// t: { page, BASE, step, apiCall, shot, assert, customService, serviceRow, pickType }
// The mock (web/mock/features/frameworks.mjs and newproject.mjs): an inline list of frameworks; the
// names "taken" (folder exists) and "fail-me" (the generator fails, the folder is kept) are special.
export default async function steps({ page, BASE, step, apiCall, shot, assert }) {
  const plan = () => page.getByTestId("create-plan");
  const planReady = () => page.locator('[data-testid="create-plan"][data-phase="ready"]').waitFor({ timeout: 8000 });
  const fw = (id) => page.locator(`[data-testid="create-framework"][data-framework="${id}"]`);
  const lang = (label) => page.getByTestId("create-languages").locator("label").filter({ hasText: label });
  const nameInput = () => page.locator("#cp-name");
  const parentInput = () => page.getByLabel("Parent folder", { exact: true });
  const go = () => page.getByTestId("create-go");
  const problems = () => page.getByTestId("create-problems");

  await step("create: languages change the frameworks, Laravel is chosen", async () => {
    await page.goto(BASE + "/#/create");
    await page.getByTestId("create-page").waitFor();
    await page.getByTestId("create-languages").waitFor();
    assert.equal(await page.getByTestId("create-languages").locator("label").count(), 7);
    for (const l of ["PHP", "JavaScript / TypeScript", "Python", "Go", "Java", "Rust", "Static"])
      await lang(l).first().waitFor();
    // PHP is chosen first, with its first framework
    assert.ok(await page.locator('input[name="cp-language"][value="php"]').isChecked());
    await fw("laravel").waitFor();
    await fw("laravel")
      .getByText(/Needs: PHP 8\.2 or newer, Composer/)
      .waitFor();
    await lang("JavaScript / TypeScript").click();
    await fw("nextjs").waitFor();
    assert.equal(await fw("laravel").count(), 0);
    await lang("Static").click();
    await fw("html").getByText("Needs: Nothing to install").waitFor();
    await lang("PHP").click();
    await fw("laravel").click();
    assert.ok(await fw("laravel").locator("input").isChecked());
    // Laravel has a database: None and the three kinds
    await page.getByTestId("create-database").getByText("None").waitFor();
    assert.ok(await page.locator('input[name="cp-db"][value="mariadb"]').isChecked());
  });

  await step("create: name, folder and the plan with installs and what is created", async () => {
    // Make the installs visible whatever the earlier steps left: Composer and an unused PHP go.
    for (const rt of await apiCall("GET", "/api/runtimes"))
      if (rt.kind === "composer") await apiCall("DELETE", `/api/runtimes/${rt.id}`);
    await page.goto(BASE + "/#/help");
    await page.goto(BASE + "/#/create");
    await page.getByTestId("create-page").waitFor();
    await fw("laravel").click();
    await nameInput().fill("Order Shop");
    await page.getByTestId("create-slug").getByText("order-shop").waitFor();
    await parentInput().fill("D:\\www");
    await page.getByTestId("create-folder").getByText("D:\\www\\order-shop").waitFor();
    await planReady();
    // what is installed: Composer always (just removed), PHP and MariaDB when the mock has none yet
    const have = await apiCall("GET", "/api/runtimes");
    const need = new Set(["composer"]);
    if (!have.some((r) => r.kind === "php" && /^8\.(2|3|4|5)|^9/.test(r.version))) need.add("php");
    if (!have.some((r) => r.kind === "mariadb")) need.add("mariadb");
    const shown = await page
      .getByTestId("create-install")
      .evaluateAll((els) => els.map((e) => e.getAttribute("data-kind")));
    assert.deepEqual(
      [...shown].sort((a, b) => a.localeCompare(b)),
      [...need].sort((a, b) => a.localeCompare(b)),
    );
    await page
      .getByTestId("create-installs")
      .getByText(/Composer 2\./)
      .waitFor();
    await page
      .getByTestId("create-installs")
      .getByText(/Laravel is created and updated with Composer/)
      .waitFor();
    const creates = plan().getByTestId("create-creates");
    await creates.getByText(/Service Order Shop \(PHP, port \d+\)/).waitFor();
    await creates.getByText("Site https://order-shop.localhost").waitFor();
    await creates.getByText("Project Order Shop").waitFor();
    await plan().getByTestId("create-steps").getByText("Creating the Laravel project").waitFor();
    await plan().getByTestId("create-warnings").waitFor();
    await plan()
      .getByTestId("create-assurance")
      .getByText(/Nothing outside/)
      .waitFor();
    // the Create button is the only primary one
    assert.equal(await page.locator("main .btn.primary").count(), 1);
    assert.ok(await go().isEnabled());
    await shot(page, "create-plan");
  });

  await step("create: a taken folder and a used domain stop Create, with a way to fix them", async () => {
    await nameInput().fill("Taken");
    await problems()
      .getByText(/exists and is not empty/)
      .waitFor({ timeout: 8000 });
    assert.ok(await go().isDisabled());
    assert.match(await page.getByTestId("create-why").innerText(), /problem/);
    // the action puts the keyboard on the name
    await problems().getByRole("button", { name: "Change the name" }).click();
    assert.equal(await page.evaluate(() => document.activeElement?.id), "cp-name");
    // a site that has the default domain already
    await apiCall("POST", "/api/sites", {
      name: "Dupe",
      domain: "order-shop.localhost",
      type: "static",
      root: "C:\\work\\dupe",
      https: false,
      enabled: false,
    });
    await nameInput().fill("Order Shop");
    await problems()
      .getByText(/order-shop\.localhost is used by the site Dupe/)
      .waitFor({ timeout: 8000 });
    assert.ok(await go().isDisabled());
    // a bad domain is told next to its field and Create waits
    await page.getByLabel("Domain", { exact: true }).fill("Bad.dev");
    await page.getByText("Use lowercase letters only.").waitFor();
    assert.ok(await go().isDisabled());
    await page.getByLabel("Domain", { exact: true }).fill("order-shop2.localhost");
    await planReady();
    assert.equal(await problems().count(), 0);
    assert.ok(await go().isEnabled());
    // the site that was in the way is removed again
    const dupe = (await apiCall("GET", "/api/sites")).find((s) => s.name === "Dupe");
    await apiCall("DELETE", `/api/sites/${dupe.id}`);
  });

  await step("create: the progress view, then the result with its links", async () => {
    await go().click();
    await page.getByTestId("create-progress").waitFor();
    await page.getByTestId("create-live-step").waitFor();
    assert.ok((await page.getByTestId("create-step").count()) >= 5);
    await page.locator('[data-testid="create-step"][data-state="running"]').first().waitFor({ timeout: 5000 });
    await page.getByTestId("create-result").waitFor({ timeout: 20000 });
    await page.getByTestId("create-result").getByText("Order Shop is ready").waitFor();
    await page
      .getByTestId("create-result")
      .getByText(/Created in D:\\www\\order-shop/)
      .waitFor();
    assert.equal(await page.locator("main .btn.primary").count(), 1);
    // what exists now
    const project = (await apiCall("GET", "/api/projects")).find((p) => p.name === "Order Shop");
    assert.ok(project, "the project exists");
    assert.ok(project.serviceIds.length >= 1, "the service");
    assert.equal(project.siteIds.length, 1);
    const sites = await apiCall("GET", "/api/sites");
    assert.ok(sites.some((s) => s.domain === "order-shop2.localhost" && s.https));
    const runtimes = await apiCall("GET", "/api/runtimes");
    assert.ok(runtimes.some((r) => r.kind === "composer"));
    assert.ok(runtimes.some((r) => r.kind === "php"));
    // the links
    const site = page.getByTestId("create-link-site");
    assert.match(await site.getAttribute("href"), /^https:\/\/order-shop2\.localhost/);
    assert.match(await page.getByTestId("create-link-project").getAttribute("href"), /#\/projects\/prj_.+\/edit/);
    await shot(page, "create-result");
    await page.getByTestId("create-start").click();
    await page.getByRole("button", { name: "Started" }).waitFor({ timeout: 15000 });
    await page.getByTestId("create-link-service").first().click();
    await page.waitForURL(/#\/services\/svc_/);
    await page.getByRole("heading", { name: "Order Shop" }).first().waitFor();
    // the remembered folder comes back on a new visit
    await apiCall("POST", "/api/services/stop-all");
    await page.goto(BASE + "/#/create");
    await page.getByTestId("create-page").waitFor();
    await page.getByTestId("create-languages").waitFor();
    assert.equal(await parentInput().inputValue(), "D:\\www");
  });

  await step("create: a failing generator shows the error and Back keeps the choices", async () => {
    await page.goto(BASE + "/#/help");
    await page.goto(BASE + "/#/create");
    await page.getByTestId("create-page").waitFor();
    await fw("laravel").click();
    await nameInput().fill("fail-me");
    await page.getByLabel("Domain", { exact: true }).fill("fail-me.localhost");
    await planReady();
    await go().click();
    await page.getByTestId("create-failed").waitFor({ timeout: 20000 });
    await page
      .getByTestId("create-error")
      .getByText(/The folder D:\\www\\fail-me was kept\./)
      .waitFor();
    await page
      .getByTestId("create-failed")
      .getByText(/Zero2Dev removed what it had made/)
      .waitFor();
    assert.equal(await page.locator("main .btn.primary").count(), 1);
    await shot(page, "create-failed");
    // nothing was created
    assert.ok(!(await apiCall("GET", "/api/projects")).some((p) => p.name === "fail-me"));
    assert.ok(!(await apiCall("GET", "/api/services")).some((s) => s.name === "fail-me"));
    await page.getByTestId("create-back").click();
    assert.equal(await nameInput().inputValue(), "fail-me");
    assert.equal(await parentInput().inputValue(), "D:\\www");
    assert.ok(await fw("laravel").locator("input").isChecked());
    // the kept folder is in the way of a second try, and the plan says so
    await problems()
      .getByText(/fail-me exists and is not empty/)
      .waitFor({ timeout: 8000 });
    assert.ok(await go().isDisabled());
  });

  await step("create: a link prefills the framework and the name; the entry points exist", async () => {
    await page.goto(BASE + "/#/create?framework=django&name=Blog");
    await page.getByTestId("create-page").waitFor();
    await fw("django").waitFor();
    assert.ok(await fw("django").locator("input").isChecked());
    assert.ok(await page.locator('input[name="cp-language"][value="python"]').isChecked());
    assert.equal(await nameInput().inputValue(), "Blog");
    // Django's database is optional: None is there, PostgreSQL is the default
    assert.ok(await page.locator('input[name="cp-db"][value="postgres"]').isChecked());
    await shot(page, "create-prefill");
    // the Projects page has a plain button for it, next to the primary New project
    await page.goto(BASE + "/#/projects");
    const entry = page.getByTestId("projects-from-framework");
    await entry.waitFor();
    assert.ok(!(await entry.getAttribute("class")).includes("primary"));
    await entry.click();
    await page.getByTestId("create-page").waitFor();
    await page.waitForURL(/#\/create/);
    // Help lists the tutorial
    await page.goto(BASE + "/#/help");
    await page.getByText("Create a project", { exact: true }).first().waitFor();
  });
}
