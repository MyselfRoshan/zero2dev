// e2e steps for API clients (0.0.6, part 2), run by smoke.mjs against the mock (before the final checks).
//   export default async function steps(t)
// t: { page, BASE, step, apiCall, shot, assert, customService, serviceRow, pickType }
// Bruno and Hoppscotch: two cards in the "API clients" group (after the database tools), a note each,
// install, Open (no body: neither takes a folder), and no "Open in" button on a service page.
export default async function steps(t) {
  const { page, BASE, step, apiCall, shot, assert } = t;
  const card = (kind) => page.locator(`article[data-kind="${kind}"]`);
  const expand = async (kind) => {
    const b = card(kind).locator("button.toggle");
    if ((await b.getAttribute("aria-expanded")) !== "true") await b.click();
  };

  await step("api clients: a group of two cards after the database tools, with their notes", async () => {
    await page.goto(BASE + "/#/runtimes");
    await page.getByRole("heading", { name: "API clients" }).waitFor();
    const groups = await page.locator("section[data-group]").evaluateAll((els) => els.map((e) => e.dataset.group));
    assert.ok(groups.indexOf("apiclients") === groups.indexOf("dbtools") + 1, groups.join(","));
    const kinds = await page
      .locator('[data-group="apiclients"] article[data-kind]')
      .evaluateAll((els) => els.map((e) => e.dataset.kind));
    assert.deepEqual(kinds, ["bruno", "hoppscotch"]);
    await expand("bruno");
    assert.equal(
      (await card("bruno").getByTestId("note-bruno").textContent()).trim(),
      "Collections are plain files in your project folder, so they go into Git.",
    );
    await expand("hoppscotch");
    const hn = (await card("hoppscotch").getByTestId("note-hoppscotch").textContent()).trim();
    assert.ok(hn.startsWith("Collections stay in the app or an account, not in your project."), hn);
    assert.ok(hn.includes("WebView2"), hn);
    await shot(page, "api-clients-cards");
  });

  await step("api clients: install both, Open sends no folder", async () => {
    await page.goto(BASE + "/#/runtimes");
    for (const kind of ["bruno", "hoppscotch"]) {
      await expand(kind);
      await card(kind)
        .getByLabel(/^Install (a|another) version$/)
        .selectOption({ index: 0 });
      await card(kind).getByRole("button", { name: "Install", exact: true }).click();
      await card(kind).locator("li[data-version]").first().waitFor({ timeout: 15000 });
    }
    for (const kind of ["bruno", "hoppscotch"]) {
      const sent = page.waitForRequest((r) => /\/api\/runtimes\/[^/]+\/launch$/.test(r.url()) && r.method() === "POST");
      await card(kind)
        .locator("li[data-version]")
        .first()
        .getByRole("button", { name: /^Open / })
        .click();
      const req = await sent;
      const body = req.postData();
      assert.ok(!body || body === "{}", `${kind}: Open must send no folder or service, got ${body}`);
    }
    await page
      .getByText(/is opening\./)
      .first()
      .waitFor();
    await shot(page, "api-clients-installed");
  });

  await step("api clients: a service page has no Open in button for them", async () => {
    const svc = (await apiCall("GET", "/api/services")).find((s) => s.name === "Demo API");
    assert.ok(svc, "Demo API exists");
    await page.goto(BASE + `/#/services/${encodeURIComponent(svc.id)}`);
    await page.getByRole("button", { name: "Open terminal" }).waitFor();
    assert.equal(await page.getByRole("button", { name: /Open .* in (Bruno|Hoppscotch)/ }).count(), 0);
    assert.equal(await page.getByText(/Open in (Bruno|Hoppscotch)/).count(), 0);
  });
}
