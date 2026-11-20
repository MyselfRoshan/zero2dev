// e2e steps for python (0.0.6), run by smoke.mjs against the mock (before the final checks).
//   export default async function steps(t)
// t: { page, BASE, step, apiCall, shot, assert, customService, serviceRow, pickType }
// Python: installed from its own card on the Runtimes page, then a service with run modes whose
// command preview shows the python of the service's virtual environment.
export default async function steps({ page, BASE, step, apiCall, shot, assert, pickType }) {
  const card = page.locator('article[data-kind="python"]');
  const preview = page.getByTestId("preview-command");
  const envs = page.locator(".envs"); // the variables Zero2Dev adds

  await step("python: the Runtimes page installs Python from its card", async () => {
    await page.goto(BASE + "/#/runtimes");
    await card.waitFor();
    const toggle = card.locator("button.toggle");
    if ((await toggle.getAttribute("aria-expanded")) !== "true") await toggle.click();
    await card.getByLabel(/^Install (a|another) version$/).selectOption("python-3.13.18");
    await card.getByRole("button", { name: "Install", exact: true }).click();
    await page.getByText("Python 3.13.18 installed.").waitFor({ timeout: 15000 });
    const rts = await apiCall("GET", "/api/runtimes");
    assert.ok(
      rts.some((r) => r.kind === "python" && r.version === "3.13.18"),
      "the Python runtime is registered",
    );
  });

  await step("python: the form offers the run modes and shows the venv python in the command", async () => {
    await page.goto(BASE + "/#/services/new");
    await page.getByRole("heading", { name: "What do you want to run?" }).waitFor();
    await pickType("Python");
    await page.getByTestId("chosen-type").getByRole("heading", { name: "Python" }).waitFor();
    await page.getByLabel("Name", { exact: true }).fill("Py API");
    await page.getByLabel("Project folder", { exact: true }).fill("C:\\work\\pyapi");

    const run = page.getByLabel("Run", { exact: true });
    const modes = await run.locator("option").allTextContents();
    assert.deepEqual(
      modes,
      [
        "A script (python main.py)",
        "A module (python -m name)",
        "Uvicorn (FastAPI)",
        "Flask (flask run)",
        "Django (runserver)",
      ],
      "the run modes",
    );
    // script mode: the script field; the program is the python of the service's own environment
    await page.getByLabel("Script", { exact: true }).waitFor();
    await preview.getByText(/python\\venvs\\svc_new\\Scripts\\python\.exe/).waitFor();
    await preview.getByText(/main\.py/).waitFor();
    await envs.getByText(/VIRTUAL_ENV=/).waitFor();
    await envs.getByText(/PYTHONUNBUFFERED=1/).waitFor();

    await run.selectOption("module");
    await page.getByLabel("Module", { exact: true }).fill("myapp.server");
    assert.equal(
      await page.getByLabel("Script", { exact: true }).count(),
      0,
      "the script field belongs to the script mode",
    );
    await preview.getByText(/-m myapp\.server/).waitFor();

    await run.selectOption("uvicorn");
    assert.equal(await page.getByLabel("Application", { exact: true }).inputValue(), "main:app");
    await page.locator("#main").getByLabel("Port").fill("8100");
    await preview.getByText(/-m uvicorn --host 127\.0\.0\.1 --port 8100 main:app/).waitFor();
    await page.getByLabel("Host", { exact: true }).fill("0.0.0.0");
    await preview.getByText(/--host 0\.0\.0\.0 --port 8100/).waitFor();

    await run.selectOption("flask");
    await page.getByLabel(/^Flask application/).fill("myapp:create_app()");
    await preview.getByText(/-m flask run --host 0\.0\.0\.0 --port 8100/).waitFor();
    await envs.getByText(/FLASK_APP=myapp:create_app\(\)/).waitFor();

    await run.selectOption("django");
    await preview.getByText(/manage\.py runserver 0\.0\.0\.0:8100/).waitFor();
    await shot(page, "31-python-service-form");

    await run.selectOption("uvicorn");
    await page.getByLabel("Host", { exact: true }).fill("127.0.0.1");
    await page.getByRole("button", { name: "Create service" }).click();
    await page.waitForURL(/#\/services\/[^/]+$/);
    const svc = (await apiCall("GET", "/api/services")).find((s) => s.name === "Py API");
    assert.equal(svc.kind, "python");
    assert.equal(svc.settings.run, "uvicorn");
    assert.equal(svc.port, 8100);
  });

  await step("python: a saved service keeps showing its virtual environment", async () => {
    const svc = (await apiCall("GET", "/api/services")).find((s) => s.name === "Py API");
    const cmd = await apiCall("GET", `/api/services/${svc.id}/command`);
    assert.match(cmd.program, new RegExp(`python\\\\venvs\\\\${svc.id}\\\\Scripts\\\\python\\.exe$`));
    assert.ok(
      cmd.env.some((e) => e.startsWith("VIRTUAL_ENV=")),
      "VIRTUAL_ENV is set",
    );
    await apiCall("DELETE", `/api/services/${svc.id}`);
  });
}
