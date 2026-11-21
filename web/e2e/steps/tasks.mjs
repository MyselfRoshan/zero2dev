// e2e steps for tasks (0.0.6), run by smoke.mjs against the mock (before the final checks).
//   export default async function steps(t)
// t: { page, BASE, step, apiCall, shot, assert, customService, serviceRow, pickType }
//
// "Run a task" on a service's page: a Node service runs a preset (output in Live output, the
// buttons off while it runs, the result line), runs a package.json script and a command of its
// own (with a bad one refused inline) and shows a failed one with its last lines; a Python service
// asks before a task that has a confirm text; a PHP service says Composer is not installed and
// links to the Runtimes page; a task started elsewhere is followed after a reload.
export default async function steps(t) {
  const { page, BASE, step, apiCall, shot, assert } = t;
  const panel = () => page.getByTestId("task-panel");
  const log = () => page.getByRole("log", { name: "Log output" });
  const result = (state) => page.locator(`[data-testid="task-result"][data-state="${state}"]`);
  const base = (name, kind, extra = {}) => ({
    name,
    kind,
    command: "",
    args: [],
    settings: {},
    cwd: `C:\\work\\${name.toLowerCase().replaceAll(" ", "-")}`,
    env: {},
    port: 3000,
    healthPath: "",
    runtimeId: "",
    autostart: false,
    ...extra,
  });
  let node;
  let py;
  let php;

  async function open(svc) {
    await page.goto(`${BASE}/#/services/${svc.id}`);
    await panel().waitFor();
  }

  await step("tasks: a Node service lists its presets and package.json scripts", async () => {
    node = await apiCall(
      "POST",
      "/api/services",
      base("Task shop", "node", { settings: { run: "npm", npmScript: "dev" } }),
    );
    assert.ok(node?.id, JSON.stringify(node));
    await open(node);
    for (const id of ["npm-install", "npm-build", "npm-test"]) await panel().locator(`li[data-task="${id}"]`).waitFor();
    await panel().locator('li[data-task="npm-install"]').getByText("npm install", { exact: true }).waitFor();
    // build is already a preset; the others are the project's own scripts, run with npm
    assert.equal(await panel().locator('li[data-script="build"]').count(), 0);
    await panel().locator('li[data-script="lint"]').getByText("npm run lint", { exact: true }).waitFor();
    assert.equal(await panel().locator(".btn.primary").count(), 0, "the panel has no primary button");
  });

  await step("tasks: a preset runs, the buttons are off meanwhile, the output goes to Live output", async () => {
    await panel().getByRole("button", { name: "Run Install packages" }).click();
    await page.locator('[data-testid="task-panel"][data-busy="true"]').waitFor();
    await panel().getByTestId("task-running").waitFor();
    await panel().getByTestId("job-progress").waitFor();
    // every Run button is off, and so is "Run your own command"
    for (const b of await panel().locator("ul.tasks .btn").all()) assert.equal(await b.isDisabled(), true);
    assert.equal(await panel().getByTestId("task-run-own").isDisabled(), true);
    // there is no Cancel
    assert.equal(
      await panel()
        .getByRole("button", { name: /cancel/i })
        .count(),
      0,
    );
    await log().getByText("$ npm install", { exact: true }).waitFor({ timeout: 8000 });
    await result("done").waitFor({ timeout: 10000 });
    assert.match(await page.getByTestId("task-result").textContent(), /finished/);
    await log()
      .getByText(/added 214 packages/)
      .waitFor();
    for (const b of await panel().locator("ul.tasks .btn").all()) assert.equal(await b.isDisabled(), false);
    await shot(page, "tasks-done");
  });

  await step("tasks: a script runs with the service's package manager", async () => {
    await panel().getByRole("button", { name: "Run the script lint" }).click();
    await log().getByText("$ npm run lint", { exact: true }).waitFor({ timeout: 8000 });
    await result("done").waitFor({ timeout: 10000 });
    assert.match(await page.getByTestId("task-result").textContent(), /npm run lint/);
  });

  await step("tasks: your own command is explained, parsed with quotes and run without a shell", async () => {
    await panel().getByText("Run your own command", { exact: true }).click();
    await panel()
      .getByTestId("task-own")
      .getByText(/no shell/)
      .waitFor();
    // a bad program is refused before anything starts
    await page.getByTestId("task-program").fill("C:\\tools\\npm");
    await panel().getByTestId("task-run-own").click();
    await page
      .getByTestId("task-own-error")
      .getByText(/without a folder/)
      .waitFor();
    await page.getByTestId("task-program").fill("node");
    await page.getByTestId("task-args").fill('--version "a b"');
    await panel().getByTestId("task-run-own").click();
    await log().getByText("$ node --version a b", { exact: true }).waitFor({ timeout: 8000 });
    await result("done").waitFor({ timeout: 10000 });
  });

  await step("tasks: a failed task keeps its error and last output lines until the next run", async () => {
    await page.getByTestId("task-program").fill("npm");
    await page.getByTestId("task-args").fill("run fail");
    await panel().getByTestId("task-run-own").click();
    await result("failed").waitFor({ timeout: 10000 });
    await page
      .getByTestId("task-result")
      .getByText(/npm exited with code 1/)
      .waitFor();
    await page
      .getByTestId("task-result-lines")
      .getByText(/ELIFECYCLE/)
      .waitFor();
    // still there after leaving the page and coming back
    await page.goto(BASE + "/#/services");
    await open(node);
    await result("failed").waitFor();
    await shot(page, "tasks-failed");
  });

  await step("tasks: a second task is refused (409) and a task started elsewhere is followed", async () => {
    const first = await apiCall("POST", `/api/services/${node.id}/tasks`, { preset: "npm-test" });
    assert.equal(first.type, "task");
    const second = await apiCall("POST", `/api/services/${node.id}/tasks`, { preset: "npm-build" });
    assert.equal(second.error?.code, "conflict");
    assert.equal(second.error?.details?.jobId, first.id);
    // 400 for both and neither, 404 for an unknown preset
    assert.equal((await apiCall("POST", `/api/services/${node.id}/tasks`, {})).error?.code, "bad_request");
    assert.equal(
      (await apiCall("POST", `/api/services/${node.id}/tasks`, { preset: "npm-test", program: "npm" })).error?.code,
      "bad_request",
    );
    assert.equal(
      (await apiCall("POST", `/api/services/${node.id}/tasks`, { preset: "nope" })).error?.code,
      "not_found",
    );
    await page.reload();
    await panel().getByTestId("task-running").waitFor({ timeout: 5000 });
    await result("done").waitFor({ timeout: 10000 });
  });

  await step("tasks: a task with a confirm text asks first; Cancel starts nothing", async () => {
    py = await apiCall("POST", "/api/services", base("Task django", "python", { port: 8000 }));
    assert.ok(py?.id, JSON.stringify(py));
    await open(py);
    await panel().locator('li[data-task="django-migrate"]').waitFor();
    await panel().getByRole("button", { name: "Run Django: migrate" }).click();
    const dlg = page.getByRole("dialog");
    await dlg.waitFor();
    await dlg.getByRole("button", { name: "Cancel" }).click();
    await dlg.waitFor({ state: "hidden" });
    assert.equal(await page.getByTestId("task-result").count(), 0);
    assert.equal((await apiCall("GET", `/api/services/${py.id}/tasks`)).running, "");
    await panel().getByRole("button", { name: "Run Django: migrate" }).click();
    await dlg.getByRole("button", { name: "Run", exact: true }).click();
    await log().getByText("$ python manage.py migrate", { exact: true }).waitFor({ timeout: 8000 });
    await result("done").waitFor({ timeout: 10000 });
  });

  await step("tasks: Composer missing says so with a link to the Runtimes page", async () => {
    const rts = await apiCall("GET", "/api/runtimes");
    for (const r of rts.filter((x) => x.kind === "composer")) await apiCall("DELETE", `/api/runtimes/${r.id}`);
    php = await apiCall("POST", "/api/services", base("Task laravel", "php", { port: 8000 }));
    assert.ok(php?.id, JSON.stringify(php));
    await open(php);
    await panel().getByRole("button", { name: "Run Composer install" }).click();
    const err = page.getByTestId("task-error");
    await err.getByText(/Composer is not installed/).waitFor();
    assert.equal(await page.getByTestId("task-result").count(), 0);
    await err.getByTestId("task-open-runtimes").click();
    await page.waitForURL(/#\/runtimes/);
    // put Composer back for the steps after this one
    await apiCall("POST", "/api/runtimes/install", { catalogId: "composer-2.11.1" });
    for (let i = 0; i < 80 && !(await apiCall("GET", "/api/runtimes")).some((r) => r.kind === "composer"); i++)
      await page.waitForTimeout(250);
    await open(php);
    await panel().getByRole("button", { name: "Run Composer install" }).click();
    await result("done").waitFor({ timeout: 10000 });
  });

  await step("tasks: a kind without tasks has no panel", async () => {
    const db = await apiCall(
      "POST",
      "/api/services",
      base("Task cache", "custom", { command: "C:\\tools\\x.exe", port: 0 }),
    );
    await page.goto(`${BASE}/#/services/${db.id}`);
    await page.getByRole("heading", { name: "Live output" }).waitFor();
    assert.equal(await panel().count(), 0);
  });
}
