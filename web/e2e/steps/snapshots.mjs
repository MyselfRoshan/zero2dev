// e2e steps for the 0.0.5 snapshots work, run by smoke.mjs against the mock (before the final checks).
//   export default async function steps(t)
// t: { page, BASE, step, apiCall, shot, assert, customService, serviceRow, pickType }
export default async function steps(t) {
  const { page, BASE, step, apiCall, shot, assert } = t;
  const dlg = () => page.getByRole("dialog");
  const row = (name) => page.locator(`[data-testid="snapshot-row"][data-snapshot="${name}"]`);
  const waitJob = async (job) => {
    for (let i = 0; i < 100; i++) {
      const j = await apiCall("GET", `/api/jobs/${job.id}`);
      if (j.state !== "running") return j;
      await page.waitForTimeout(150);
    }
    throw new Error(`job ${job.id} did not finish`);
  };
  const waitState = async (id, st) => {
    for (let i = 0; i < 60 && (await apiCall("GET", `/api/services/${id}`)).state !== st; i++)
      await page.waitForTimeout(150);
    assert.equal((await apiCall("GET", `/api/services/${id}`)).state, st);
  };
  const nodeOf = async (version) =>
    (await apiCall("GET", "/api/runtimes")).find((r) => r.kind === "node" && r.version === version);
  const ensureNode = async (version) => {
    if (await nodeOf(version)) return;
    const j = await apiCall("POST", "/api/runtimes/install", { catalogId: `node-${version}` });
    assert.equal((await waitJob(j)).state, "done");
  };
  let project;
  let api;

  await step("snapshots: take one of a project from its Snapshots page", async () => {
    await ensureNode("26.13.1");
    await ensureNode("26.14.0");
    await apiCall("POST", `/api/runtimes/${(await nodeOf("26.13.1")).id}/default`);
    api = await apiCall("POST", "/api/services", {
      name: "Snap API",
      kind: "node",
      command: "",
      args: ["server.js"],
      cwd: "C:\\work\\snapshop",
      env: {},
      port: 3310,
      healthPath: "",
      runtimeId: "",
      autostart: false,
    });
    const site = await apiCall("POST", "/api/sites", {
      name: "Snap shop",
      domain: "snapshop.localhost",
      type: "proxy",
      serviceId: api.id,
      https: false,
      enabled: true,
    });
    project = await apiCall("POST", "/api/projects", { name: "Snap shop", serviceIds: [api.id], siteIds: [site.id] });
    await page.goto(BASE + "/#/projects");
    await page.locator('tr[data-project="Snap shop"]').getByRole("link", { name: "Snapshots of Snap shop" }).click();
    await page.getByTestId("project-snapshots").waitFor();
    await page.locator("h1", { hasText: "Snapshots of Snap shop" }).waitFor();
    await page.getByText("No snapshots yet.").waitFor();
    await page.getByTestId("take-snapshot").click();
    await dlg().getByLabel("Name").fill("First");
    await dlg().getByRole("button", { name: "Take snapshot" }).click();
    await row("First").waitFor({ timeout: 10000 });
    await row("First").getByTestId("snapshot-runtime").getByText("Node.js 26.13.1").waitFor();
    await row("First").getByText("1 service, 1 site").waitFor();
    await page.getByText('Snapshot "First" taken.').waitFor();
    const list = await apiCall("GET", `/api/projects/${project.id}/snapshots`);
    assert.equal(list.length, 1);
    assert.equal(list[0].services[0].runtime.followedDefault, true);
  });

  await step("snapshots: running services are stopped, snapshotted and started again", async () => {
    await apiCall("POST", `/api/services/${api.id}/start`);
    await waitState(api.id, "running");
    await page.getByTestId("take-snapshot").click();
    await dlg()
      .getByTestId("take-running")
      .getByText(/Snap API is running/)
      .waitFor();
    const box = dlg().getByLabel(/Stop it, take the snapshot, and start it again/);
    await box.uncheck();
    assert.ok(await dlg().getByRole("button", { name: "Take snapshot" }).isDisabled(), "needs the restart choice");
    await box.check();
    await dlg().getByLabel("Name").fill("While running");
    await dlg().getByRole("button", { name: "Take snapshot" }).click();
    await row("While running").waitFor({ timeout: 10000 });
    await waitState(api.id, "running");
    await apiCall("POST", `/api/services/${api.id}/stop`);
    await waitState(api.id, "stopped");
  });

  await step("snapshots: Make default lists the projects that follow it and offers a snapshot", async () => {
    await page.goto(BASE + "/#/runtimes");
    const b = page.locator('article[data-kind="node"] button.toggle');
    await b.waitFor();
    if ((await b.getAttribute("aria-expanded")) !== "true") await b.click();
    await page
      .locator('article[data-kind="node"] li[data-version="26.14.0"]')
      .getByRole("button", { name: "Make Node.js 26.14.0 the default" })
      .click();
    const md = page.getByTestId("make-default-dialog");
    await md.waitFor();
    await md.getByText(/now 26\.13\.1/).waitFor();
    await md
      .locator('[data-project="Snap shop"]')
      .getByText(/Snap API/)
      .waitFor();
    // only Snap shop gets a snapshot in this test
    for (const box of await md.locator(".proj").all())
      if ((await box.getAttribute("data-project")) !== "Snap shop") await box.getByRole("checkbox").uncheck();
    await shot(page, "40-make-default-offer");
    await page.getByTestId("make-default-confirm").click();
    await md.waitFor({ state: "detached", timeout: 10000 });
    await page.locator('article[data-kind="node"] li[data-version="26.14.0"]').getByTestId("default-badge").waitFor();
    const list = await apiCall("GET", `/api/projects/${project.id}/snapshots`);
    assert.equal(list[0].name, "Before Node.js 26.14.0 became the default");
  });

  await step("snapshots: restore offers the missing runtime, then brings the project back", async () => {
    // change things: delete the service, then the runtime it used
    await apiCall("DELETE", `/api/services/${api.id}`);
    const old = await nodeOf("26.13.1");
    assert.equal(await apiCall("DELETE", `/api/runtimes/${old.id}`), null);
    await page.goto(BASE + `/#/projects/${project.id}/snapshots`);
    await row("First").getByRole("button", { name: "Restore First" }).click();
    const r = page.getByTestId("restore-dialog");
    await r.getByText(/Snap API comes back \(it was deleted\)/).waitFor();
    const miss = r.getByTestId("restore-missing");
    await miss.getByText("Node.js 26.13.1").waitFor();
    assert.ok(await page.getByTestId("restore-confirm").isDisabled(), "restore waits for the runtime");
    await page
      .getByTestId("restore-why")
      .getByText(/Install Node.js 26.13.1 first/)
      .waitFor();
    await shot(page, "41-restore-missing");
    await miss.getByRole("button", { name: "Install Node.js 26.13.1" }).click();
    await miss.waitFor({ state: "detached", timeout: 20000 });
    await page.getByTestId("restore-confirm").click();
    await r.waitFor({ state: "detached", timeout: 10000 });
    await page.getByText('"First" restored.').waitFor();
    const back = (await apiCall("GET", "/api/services")).find((s) => s.id === api.id);
    assert.ok(back, "the deleted service is back with its id");
    assert.equal(back.port, 3310);
    assert.equal(back.runtimeId, (await nodeOf("26.13.1")).id, "pinned to the version it used");
    const prj = (await apiCall("GET", "/api/projects")).find((p) => p.id === project.id);
    assert.deepEqual(prj.serviceIds, [api.id]);
  });

  await step("snapshots: clone into a new project with -copy domains", async () => {
    await row("First").getByRole("button", { name: "Clone First into a new project" }).click();
    const c = page.getByTestId("clone-dialog");
    await c.getByTestId("clone-domain").getByText("snapshop-copy.localhost").waitFor();
    await c
      .getByTestId("clone-shared")
      .getByText(/C:\\work\\snapshop/)
      .waitFor();
    await c.getByLabel("New project name").fill("Snap shop copy");
    await shot(page, "42-clone");
    await dlg().getByRole("button", { name: "Clone", exact: true }).click();
    await c.waitFor({ state: "detached", timeout: 10000 });
    await page.getByText(/Snap shop copy created/).waitFor();
    const copy = (await apiCall("GET", "/api/projects")).find((p) => p.name === "Snap shop copy");
    assert.ok(copy, "the new project exists");
    const site = (await apiCall("GET", "/api/sites")).find((s) => s.id === copy.siteIds[0]);
    assert.equal(site.domain, "snapshop-copy.localhost");
    const svc = (await apiCall("GET", "/api/services")).find((s) => s.id === copy.serviceIds[0]);
    assert.notEqual(svc.id, api.id);
    assert.notEqual(svc.port, 3310, "a free port of its own");
    assert.equal(site.serviceId, svc.id, "the site points at the copy");
    await page.goto(BASE + "/#/projects");
    await page.locator('tr[data-project="Snap shop copy"]').waitFor();
  });

  await step("snapshots: delete one, and a database service has its own", async () => {
    await page.goto(BASE + `/#/projects/${project.id}/snapshots`);
    await row("While running").getByRole("button", { name: "Delete While running" }).click();
    await dlg().getByRole("button", { name: "Delete snapshot" }).click();
    await row("While running").waitFor({ state: "detached" });
    await page.getByText('Snapshot "While running" deleted.').waitFor();
    assert.equal((await apiCall("GET", `/api/projects/${project.id}/snapshots`)).length, 2);
    // a database service: snapshots on its page, without Clone
    const db = await apiCall("POST", "/api/services", {
      name: "Snap DB",
      kind: "mariadb",
      command: "",
      args: [],
      settings: {},
      cwd: "",
      env: {},
      port: 3316,
      healthPath: "",
      runtimeId: "",
      autostart: false,
    });
    assert.ok(db.id, JSON.stringify(db));
    await page.goto(BASE + `/#/services/${db.id}`);
    const sec = page.getByTestId("service-snapshots");
    await sec.getByTestId("take-snapshot").click();
    await dlg().getByLabel("Name").fill("Empty DB");
    await dlg().getByRole("button", { name: "Take snapshot" }).click();
    await sec.locator('[data-snapshot="Empty DB"]').waitFor({ timeout: 10000 });
    assert.equal(await sec.getByRole("button", { name: /^Clone/ }).count(), 0);
    // a service that is not a database has no snapshots section
    await page.goto(BASE + `/#/services/${api.id}`);
    await page.getByTestId("command-line").waitFor();
    assert.equal(await page.getByTestId("service-snapshots").count(), 0);
  });
}
