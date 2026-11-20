// e2e steps for the 0.0.5 mail work, run by smoke.mjs against the mock (before the final checks).
//   export default async function steps(t)
// t: { page, BASE, step, apiCall, shot, assert, customService, serviceRow, pickType }
// Mailpit: installed from its own "Mail" card, created as a service, started, and its page shows
// where apps send mail (SMTP host and port, no login) with the .env lines to copy.
export default async function steps({ page, BASE, step, apiCall, shot, assert, serviceRow, pickType }) {
  const card = page.locator('article[data-kind="mailpit"]');

  await step("mail: Mailpit has a Mail group on the Runtimes page and installs from its card", async () => {
    await page.goto(BASE + "/#/runtimes");
    await page.getByRole("heading", { name: "Mail", exact: true }).waitFor();
    assert.equal(await page.locator('[data-group="mail"] article[data-kind="mailpit"]').count(), 1);
    const toggle = card.locator("button.toggle");
    if ((await toggle.getAttribute("aria-expanded")) !== "true") await toggle.click();
    await card.getByLabel(/^Install (a|another) version$/).selectOption("mailpit-1.33.0");
    await card.getByRole("button", { name: "Install", exact: true }).click();
    await page.getByText("Mailpit 1.33.0 installed.").waitFor({ timeout: 15000 });
    const rts = await apiCall("GET", "/api/runtimes");
    assert.ok(
      rts.some((r) => r.kind === "mailpit" && r.version === "1.33.0"),
      "the Mailpit runtime is registered",
    );
  });

  await step("mail: create a Mailpit service; web port 8025 and an SMTP port setting", async () => {
    await page.goto(BASE + "/#/services/new");
    await page.getByRole("heading", { name: "What do you want to run?" }).waitFor();
    await pickType("Mailpit");
    await page.getByTestId("chosen-type").getByText("Mailpit").waitFor();
    await page.getByLabel("Name", { exact: true }).fill("Mail catcher");
    const port = page.getByLabel("Port", { exact: true });
    assert.equal(await port.inputValue(), "8025", "the web port defaults to 8025");
    const smtp = page.getByLabel("SMTP port", { exact: true });
    assert.equal(await smtp.inputValue(), "1025", "the SMTP port defaults to 1025");
    await page
      .getByTestId("preview-command")
      .getByText(/--listen 127\.0\.0\.1:8025 --smtp 127\.0\.0\.1:1025 --database /)
      .waitFor();
    await smtp.fill("2525");
    await page
      .getByTestId("preview-command")
      .getByText(/--smtp 127\.0\.0\.1:2525/)
      .waitFor();
    await page.getByRole("button", { name: "Create service" }).click();
    // the new service opens on its own page
    await page.getByTestId("mail-info").waitFor();
    const svc = (await apiCall("GET", "/api/services")).find((s) => s.name === "Mail catcher");
    assert.equal(svc.kind, "mailpit");
    assert.equal(svc.port, 8025);
    assert.equal(svc.settings.smtpPort, "2525");
  });

  await step("mail: the service page shows the SMTP server and the .env lines", async () => {
    await page.goto(BASE + "/#/services");
    await serviceRow("Mail catcher").getByRole("button", { name: "Start Mail catcher", exact: true }).click();
    await serviceRow("Mail catcher").locator('.badge[data-state="running"]').waitFor({ timeout: 8000 });
    await serviceRow("Mail catcher").getByRole("link", { name: "Mail catcher" }).click();
    const box = page.getByTestId("mail-info");
    await box.waitFor();
    assert.equal(await box.getByTestId("mail-host").textContent(), "127.0.0.1");
    assert.equal(await box.getByTestId("mail-port").textContent(), "2525");
    await box.getByText("No authentication").waitFor();
    assert.equal(await box.getByTestId("mail-web").getAttribute("href"), "http://127.0.0.1:8025/");
    assert.equal(
      (await box.getByTestId("mail-env").textContent()).trim(),
      "MAIL_MAILER=smtp\nMAIL_HOST=127.0.0.1\nMAIL_PORT=2525",
    );
    assert.equal(await box.getByRole("button", { name: "Copy" }).count(), 1);
    // a mail catcher has no database connection box
    assert.equal(await page.getByTestId("connection").count(), 0);
    await shot(page, "30-mailpit-service");
    // stop it again so the later steps start from a quiet PC
    await page.getByRole("button", { name: "Stop Mail catcher", exact: true }).click();
    await page.getByRole("button", { name: "Start Mail catcher", exact: true }).waitFor({ timeout: 8000 });
  });
}
