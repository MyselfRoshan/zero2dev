// e2e steps for detect (0.0.6), run by smoke.mjs against the mock (before the final checks).
//   export default async function steps(t)
// t: { page, BASE, step, apiCall, shot, assert, customService, serviceRow, pickType }
//
// Project detection in the new service form: a folder named like a Laravel project chooses PHP and
// fills the name, port and settings; the hint says so and Undo puts everything back. What the
// person typed is never replaced, a folder that says nothing changes nothing, and the editor of an
// existing service does not detect. (The mock judges the folder's name: web/mock/features/detect.mjs.)
export default async function steps({ page, BASE, step, apiCall, shot, assert, pickType }) {
  const folder = () => page.getByLabel("Project folder", { exact: true });
  const hint = () => page.getByTestId("detect-hint");
  const name = () => page.getByLabel("Name", { exact: true });
  const port = () => page.locator('input[inputmode="numeric"]').first();

  async function openNew() {
    await page.goto(BASE + "/#/services");
    await page.goto(BASE + "/#/services/new");
    await page.getByRole("heading", { name: "What do you want to run?" }).waitFor();
  }

  await step("detect: a Laravel folder chooses PHP and fills the empty fields", async () => {
    await openNew();
    await folder().fill("C:\\work\\laravel-shop");
    await hint().waitFor({ timeout: 5000 });
    await page.getByTestId("chosen-type").getByRole("heading", { name: "PHP" }).waitFor();
    assert.equal(await name().inputValue(), "laravel-shop");
    assert.equal(await port().inputValue(), "8000");
    assert.equal(await folder().inputValue(), "C:\\work\\laravel-shop");
    const text = await hint().textContent();
    assert.match(text, /Looks like a Laravel project \(composer\.json, artisan\)\./);
    assert.match(text, /Filled in: type PHP, name, .*port 8000.*\.env/);
    await shot(page, "detect-hint");
  });

  await step("detect: Undo puts the old values back and the folder stays", async () => {
    await hint().getByTestId("detect-undo").click();
    await page.getByRole("heading", { name: "What do you want to run?" }).waitFor();
    assert.equal(await hint().count(), 0);
    assert.equal(await folder().inputValue(), "C:\\work\\laravel-shop");
    // choosing PHP by hand afterwards does not detect the same folder again
    await pickType("PHP");
    await page.getByTestId("chosen-type").getByRole("heading", { name: "PHP" }).waitFor();
    assert.equal(await name().inputValue(), "");
    assert.equal(await hint().count(), 0);
  });

  await step("detect: a typed name is not overwritten and the hint can be dismissed", async () => {
    await openNew();
    await pickType("PHP");
    await name().fill("My shop");
    await folder().fill("C:\\work\\laravel-two");
    await hint().waitFor({ timeout: 5000 });
    assert.equal(await name().inputValue(), "My shop");
    const text = await hint().textContent();
    assert.doesNotMatch(text, /Filled in: .*\bname\b/);
    assert.match(text, /Filled in: /);
    await hint().getByTestId("detect-dismiss").click();
    assert.equal(await hint().count(), 0);
    assert.equal(await name().inputValue(), "My shop");
  });

  await step("detect: a folder of another type than the chosen one changes nothing", async () => {
    await openNew();
    await pickType("Node.js");
    await folder().fill("C:\\work\\laravel-three");
    await page.waitForTimeout(900);
    assert.equal(await hint().count(), 0);
    assert.equal(await name().inputValue(), "");
    await page.getByTestId("chosen-type").getByRole("heading", { name: "Node.js" }).waitFor();
  });

  await step("detect: a folder that says nothing, or fails, stays silent", async () => {
    await openNew();
    await folder().fill("C:\\work\\plain-folder");
    await page.waitForTimeout(900);
    assert.equal(await hint().count(), 0);
    await page.getByRole("heading", { name: "What do you want to run?" }).waitFor();
    await folder().fill("C:\\work\\missing-folder");
    await page.waitForTimeout(900);
    assert.equal(await hint().count(), 0);
    assert.equal(await page.locator(".toast, [data-testid=toast]").count(), 0);
    // the route itself: 400 without a folder
    assert.equal((await apiCall("GET", "/api/project/detect?dir=")).error?.code, "bad_request");
    const next = await apiCall("GET", "/api/project/detect?dir=" + encodeURIComponent("C:\\work\\next-blog"));
    assert.equal(next.framework, "nextjs");
  });

  await step("detect: editing an existing service does not detect", async () => {
    const svc = await apiCall("POST", "/api/services", {
      name: "Detect edit",
      kind: "php",
      command: "",
      args: [],
      settings: {},
      cwd: "C:\\work\\somewhere",
      env: {},
      port: 8000,
      healthPath: "",
      runtimeId: "",
      autostart: false,
    });
    assert.ok(svc?.id, JSON.stringify(svc));
    await page.goto(`${BASE}/#/services/${svc.id}/edit`);
    await folder().waitFor();
    await folder().fill("C:\\work\\laravel-edit");
    await page.waitForTimeout(900);
    assert.equal(await hint().count(), 0);
    assert.equal(await name().inputValue(), "Detect edit");
  });
}
