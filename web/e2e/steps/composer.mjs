// e2e steps for Composer and the package managers (0.0.6), run by smoke.mjs against the mock.
//   export default async function steps(t)
// t: { page, BASE, step, apiCall, shot, assert, customService, serviceRow, pickType }
// Composer: a tool card on the Runtimes page that says it runs with a service's PHP, installed from
// the card. Node: the service form offers the package manager and the preview runs it with Corepack.
export default async function steps({ page, BASE, step, apiCall, assert }) {
  const card = page.locator('article[data-kind="composer"]');

  await step("composer: the Tools card says it runs with the PHP of a service and installs from the card", async () => {
    await page.goto(BASE + "/#/runtimes");
    await page.getByRole("heading", { name: "Tools", exact: true }).waitFor();
    assert.equal(await page.locator('[data-group="tools"] article[data-kind="composer"]').count(), 1);
    await card.getByText(/runs with the PHP of the service/).waitFor();
    const toggle = card.locator("button.toggle");
    if ((await toggle.getAttribute("aria-expanded")) !== "true") await toggle.click();
    await card.getByLabel(/^Install (a|another) version$/).selectOption("composer-2.11.1");
    await card.getByRole("button", { name: "Install", exact: true }).click();
    await page.getByText("Composer 2.11.1 installed.").waitFor({ timeout: 15000 });
    const rts = await apiCall("GET", "/api/runtimes");
    assert.ok(
      rts.some((r) => r.kind === "composer" && r.version === "2.11.1" && r.binary.endsWith("composer.bat")),
      "the Composer runtime is registered with composer.bat as its binary",
    );
  });

  await step("composer: the Node service offers a package manager (npm, pnpm, Yarn)", async () => {
    const kinds = await apiCall("GET", "/api/kinds");
    const opt = kinds.find((k) => k.kind === "node").options.find((o) => o.key === "packageManager");
    assert.deepEqual(
      opt.choices.map((c) => c.value),
      ["npm", "pnpm", "yarn"],
    );
    assert.equal(opt.default, "npm");
    const pv = (packageManager) =>
      apiCall("POST", "/api/services/preview", {
        name: "pm",
        kind: "node",
        cwd: "C:workapp",
        port: 3000,
        settings: { run: "npm", npmScript: "dev", packageManager },
      });
    assert.match((await pv("npm")).program, /npm\.cmd$/);
    const pnpm = await pv("pnpm");
    assert.ok(pnpm.program.endsWith("\\node\\corepack\\bin\\pnpm.cmd"), pnpm.program);
    assert.deepEqual(pnpm.args, ["run", "dev"]);
  });
}
