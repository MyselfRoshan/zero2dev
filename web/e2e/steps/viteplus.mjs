// e2e steps for Vite+ (0.0.6, part 2), run by smoke.mjs against the mock (before the final checks).
//   export default async function steps(t)
// t: { page, BASE, step, apiCall, shot, assert, customService, serviceRow, pickType }
// A Node service in the Vite+ mode: the detected project, the vp.cmd command in the preview, the
// "Node from" setting (only Zero2Dev's Node can be chosen), the pin warning and the service page.
// The mock reads no folders: the folder NAME steers it (vite-plus: detected; no-vp: vp is not
// installed; pinned: the folder pins Node 22.18.0).
export default async function steps({ page, BASE, step, apiCall, assert, pickType }) {
  const folder = () => page.getByLabel("Project folder", { exact: true });
  const preview = () => page.getByTestId("preview-command");
  const notes = () => page.getByRole("note");

  async function openNode(dir) {
    await page.goto(BASE + "/#/services");
    await page.goto(BASE + "/#/services/new");
    await page.getByRole("heading", { name: "What do you want to run?" }).waitFor();
    await pickType("Node.js");
    await page.getByTestId("chosen-type").getByRole("heading", { name: "Node.js" }).waitFor();
    await page.getByLabel("Name", { exact: true }).fill("Vite plus app");
    await folder().fill(dir);
  }

  await step("vite+: a vite-plus project is detected and runs through vp", async () => {
    await page.goto(BASE + "/#/services");
    await page.goto(BASE + "/#/services/new");
    await page.getByRole("heading", { name: "What do you want to run?" }).waitFor();
    await folder().fill("C:\\work\\vite-plus-app");
    const hint = page.getByTestId("detect-hint");
    await hint.waitFor({ timeout: 5000 });
    assert.match(await hint.textContent(), /Looks like a Vite\+ project/);
    assert.equal(await page.getByLabel("Run", { exact: true }).inputValue(), "vp", "run mode Vite+");
    assert.equal(await page.getByLabel("Vite+ command", { exact: true }).inputValue(), "dev");
    assert.equal(await page.locator('input[inputmode="numeric"]').first().inputValue(), "5173");
    await preview()
      .getByText(/C:\\work\\vite-plus-app\\node_modules\\\.bin\\vp\.cmd\s+dev\s+--port 5173/)
      .waitFor();
  });

  await step(
    "vite+: picking the run mode by hand shows vp.cmd, the port follows, and the commands change",
    async () => {
      await openNode("C:\\work\\app");
      const port = page.locator('input[inputmode="numeric"]').first();
      assert.equal(await port.inputValue(), "3000");
      await page.getByLabel("Run", { exact: true }).selectOption("vp");
      await preview()
        .getByText(/C:\\work\\app\\node_modules\\\.bin\\vp\.cmd\s+dev/)
        .waitFor();
      assert.equal(await port.inputValue(), "5173", "the port follows the mode");
      await page.getByLabel("Vite+ command", { exact: true }).selectOption("build");
      await preview()
        .getByText(/vp\.cmd\s+build$/)
        .waitFor();
      await page.getByLabel("Vite+ command", { exact: true }).selectOption("run");
      await page.getByLabel("Script or task").fill("lint");
      await preview()
        .getByText(/vp\.cmd\s+run lint$/)
        .waitFor();
      // back to npm: the port goes back too
      await page.getByLabel("Run", { exact: true }).selectOption("npm");
      assert.equal(await port.inputValue(), "3000");
    },
  );

  await step(
    "vite+: Node from says which Node runs the service; Vite+'s Node runs through the Vite+ program",
    async () => {
      await openNode("C:\\work\\app");
      await page.getByLabel("Run", { exact: true }).selectOption("vp");
      const from = page.getByLabel("Node from");
      assert.equal(await from.inputValue(), "zero2dev");
      assert.equal(await from.locator('option[value="viteplus"]').isDisabled(), false, "Vite+'s Node can be chosen");
      await page.getByText(/With Vite\+'s Node the service runs through the Vite\+ program/).waitFor();
      await from.selectOption("viteplus");
      await preview()
        .getByText(/vite-plus\\bin\\vp\.exe\s+dev\s+--port 5173/)
        .waitFor();
      await notes()
        .getByText(/Vite\+ is not installed: install it on the Runtimes page/)
        .waitFor();
    },
  );

  await step("vite+: vp not installed in the project is said in the preview", async () => {
    await openNode("C:\\work\\no-vp-app");
    await page.getByLabel("Run", { exact: true }).selectOption("vp");
    await notes()
      .getByText(/vp is not installed in this project: run npm install \(or a task\) first/)
      .waitFor();
  });

  await step("vite+: a pinned Node that the runtime does not satisfy is one warning in the preview", async () => {
    // the pin is checked against the service's Node: make sure one is registered
    if (!(await apiCall("GET", "/api/runtimes")).some((r) => r.kind === "node"))
      await apiCall("POST", "/api/runtimes/register", {
        kind: "node",
        binary: "C:\\tools\\node20\\node.exe",
        version: "20.11.1",
      });
    await openNode("C:\\work\\pinned-app");
    await notes()
      .getByText(/This folder pins Node 22\.18\.0 \(\.node-version\) but the Node of this service is \d+\.\d+\.\d+/)
      .waitFor();
    assert.equal(
      await notes()
        .getByText(/This folder pins Node/)
        .count(),
      1,
      "one warning line",
    );
    // a folder that pins nothing says nothing
    await folder().fill("C:\\work\\plain");
    await page.waitForFunction(() => !document.body.innerText.includes("This folder pins Node"), null, {
      timeout: 5000,
    });
  });

  await step(
    "vite+: the choice is stored and the service page says it in words; the API accepts Vite+'s Node",
    async () => {
      const svc = await apiCall("POST", "/api/services", {
        name: "Vp stored",
        kind: "node",
        cwd: "C:\\work\\vite-plus-stored",
        port: 5173,
        settings: { run: "vp", vpCommand: "dev", nodeFrom: "zero2dev" },
        env: {},
        args: [],
        dependsOn: [],
      });
      assert.equal(svc.settings.nodeFrom, "zero2dev");
      const viaVp = await apiCall("POST", "/api/services", {
        name: "Vp via Vite+",
        kind: "node",
        cwd: "C:\\work\\vite-plus-via",
        port: 5174,
        settings: { run: "vp", nodeFrom: "viteplus" },
        env: {},
        args: [],
        dependsOn: [],
      });
      assert.equal(viaVp.settings.nodeFrom, "viteplus");
      await apiCall("DELETE", `/api/services/${viaVp.id}`);
      await page.goto(BASE + `/#/services/${encodeURIComponent(svc.id)}`);
      await page
        .getByTestId("node-from")
        .filter({ hasText: /^Zero2Dev's Node: .* downloaded and kept by Zero2Dev\.$/ })
        .waitFor({ timeout: 5000 });
      await page
        .getByTestId("command-line")
        .filter({ hasText: /vite-plus-stored\\node_modules\\\.bin\\vp\.cmd dev/ })
        .waitFor();
      await apiCall("DELETE", `/api/services/${svc.id}`);
    },
  );
}
