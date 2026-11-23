// e2e steps for CPU and memory per service (0.0.6, part 2), run by smoke.mjs against the mock (before the final checks).
//   export default async function steps(t)
// t: { page, BASE, step, apiCall, shot, assert, customService, serviceRow, pickType }
// The mock samples a running service every 1.5 s (service.usage); the table shows "-" while it is stopped.
export default async function steps({ page, BASE, step, apiCall, assert, customService, serviceRow }) {
  await step("usage: CPU and Memory columns show numbers while running and '-' when stopped", async () => {
    const svc = await apiCall("POST", "/api/services", customService("Usage app"));
    await page.goto(BASE + "/#/services");
    const row = serviceRow("Usage app");
    await row.waitFor();
    const heads = await page.locator("table.t thead th").allTextContents();
    assert.ok(heads.indexOf("CPU") === heads.indexOf("Status") + 1, "the CPU column follows Status");
    assert.ok(heads.indexOf("Memory") === heads.indexOf("CPU") + 1, "the Memory column follows CPU");
    assert.equal((await row.getByTestId("svc-cpu").textContent()).trim(), "-", "stopped: no number");
    assert.equal((await row.getByTestId("svc-mem").textContent()).trim(), "-");

    await row.getByRole("button", { name: "Start Usage app" }).click();
    await row.getByText("running", { exact: true }).waitFor({ timeout: 10000 });
    // the first sample arrives as an event (at most 1.5 s)
    await row
      .getByTestId("svc-cpu")
      .filter({ hasText: /^\d+(\.\d)? %$/ })
      .waitFor({ timeout: 8000 });
    await row
      .getByTestId("svc-mem")
      .filter({ hasText: /^\d+ (KB|MB)$|^\d\.\d GB$/ })
      .waitFor({ timeout: 8000 });

    // the service page lists them too
    await page.goto(BASE + `/#/services/${encodeURIComponent(svc.id)}`);
    await page.getByTestId("detail-cpu").filter({ hasText: / %$/ }).waitFor({ timeout: 8000 });
    await page
      .getByTestId("detail-mem")
      .filter({ hasText: / (KB|MB|GB)$/ })
      .waitFor();
    // and the API answers with the numbers
    const live = await apiCall("GET", `/api/services/${svc.id}`);
    assert.ok(typeof live.cpu === "number" && live.memBytes > 0, "GET /api/services/{id} carries cpu and memBytes");

    await page.goto(BASE + "/#/services");
    await row.getByRole("button", { name: "Stop Usage app" }).click();
    await row.getByText("stopped", { exact: true }).waitFor({ timeout: 10000 });
    await row.getByTestId("svc-cpu").filter({ hasText: /^-$/ }).waitFor();
    await row.getByTestId("svc-mem").filter({ hasText: /^-$/ }).waitFor();
    const gone = await apiCall("GET", `/api/services/${svc.id}`);
    assert.equal(gone.cpu, 0, "stopped: cpu is 0");
    assert.equal(gone.memBytes, 0, "stopped: memBytes is 0");
    await apiCall("DELETE", `/api/services/${svc.id}`);
  });
}
