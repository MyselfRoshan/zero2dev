// e2e steps for Rust (0.0.6, part 2), run by smoke.mjs against the mock (before the final checks).
//   export default async function steps(t)
// t: { page, BASE, step, apiCall, shot, assert, customService, serviceRow, pickType }
// Rust: installed from its own card on the Runtimes page (the installer's .exe type: the mock runs
// the same steps), then a service whose command preview shows cargo run and CARGO_TARGET_DIR.
export default async function steps({ page, BASE, step, apiCall, shot, assert, pickType }) {
  const card = page.locator('article[data-kind="rust"]');
  const preview = page.getByTestId("preview-command");
  const envs = page.locator(".envs"); // the variables Zero2Dev adds

  await step("rust: the Runtimes page installs Rust from its card", async () => {
    await page.goto(BASE + "/#/runtimes");
    await card.waitFor();
    await card
      .getByText(/needs no administrator install/)
      .first()
      .waitFor();
    await card
      .getByText(/needs MinGW/)
      .first()
      .waitFor();
    const toggle = card.locator("button.toggle");
    if ((await toggle.getAttribute("aria-expanded")) !== "true") await toggle.click();
    await card.getByLabel(/^Install (a|another) version$/).selectOption("rust-1.100.0");
    await card.getByRole("button", { name: "Install", exact: true }).click();
    await page
      .getByText("Rust 1.100.0 (stable, GNU) installed.")
      .or(page.getByText(/Rust .*installed\./))
      .first()
      .waitFor({ timeout: 15000 });
    const rts = await apiCall("GET", "/api/runtimes");
    const rt = rts.find((r) => r.kind === "rust" && r.version === "1.100.0");
    assert.ok(rt, "the Rust runtime is registered");
    assert.match(rt.binary, /\\cargo\\bin\\cargo\.exe$/, "the binary is cargo.exe in cargo\\bin");
  });

  await step("rust: the form offers the run modes and shows cargo run with its target folder", async () => {
    await page.goto(BASE + "/#/services/new");
    await page.getByRole("heading", { name: "What do you want to run?" }).waitFor();
    await pickType("Rust");
    await page.getByTestId("chosen-type").getByRole("heading", { name: "Rust" }).waitFor();
    await page.getByLabel("Name", { exact: true }).fill("Rust API");
    await page.getByLabel("Project folder", { exact: true }).fill("C:\\work\\rustapi");

    const run = page.getByLabel("Run", { exact: true });
    const modes = await run.locator("option").allTextContents();
    assert.deepEqual(
      modes,
      ["cargo run (debug build)", "cargo run --release (optimised, slower to build)"],
      "run modes",
    );
    await preview.getByText(/cargo\.exe/).waitFor();
    await envs.getByText(/CARGO_TARGET_DIR=.*\\rust\\target\\svc_new/).waitFor();
    await envs.getByText(/CARGO_HOME=.*\\cargo$/).waitFor();
    await envs.getByText(/RUSTUP_HOME=.*\\rustup$/).waitFor();

    await run.selectOption("release");
    await preview.getByText(/run --release/).waitFor();
    await page.getByLabel(/^Package/).fill("api");
    await page.getByLabel(/^Binary/).fill("server");
    await preview.getByText(/run --release -p api --bin server/).waitFor();
    await shot(page, "32-rust-service-form");
    await page.getByLabel(/^Binary/).fill("");
    await page.getByLabel(/^Package/).fill("");
    await run.selectOption("debug");
    await page.getByRole("button", { name: "Create service" }).click();
    await page.waitForURL(/#\/services\/[^/]+$/);
    const svc = (await apiCall("GET", "/api/services")).find((s) => s.name === "Rust API");
    assert.equal(svc.kind, "rust");
  });

  await step("rust: a saved service keeps its target folder; the tasks are Cargo's", async () => {
    const svc = (await apiCall("GET", "/api/services")).find((s) => s.name === "Rust API");
    const cmd = await apiCall("GET", `/api/services/${svc.id}/command`);
    assert.match(cmd.program, /cargo\.exe$/);
    assert.ok(
      cmd.env.some((e) => new RegExp(`^CARGO_TARGET_DIR=.*\\\\rust\\\\target\\\\${svc.id}$`).test(e)),
      "CARGO_TARGET_DIR is set",
    );
    assert.ok(cmd.args.includes("run"), "cargo run");
    const tasks = await apiCall("GET", `/api/services/${svc.id}/tasks`);
    const ids = (tasks.presets ?? tasks).map((t) => t.id);
    for (const id of ["cargo-build", "cargo-test", "cargo-fetch", "cargo-clippy"])
      assert.ok(ids.includes(id), `task ${id}`);
    await apiCall("DELETE", `/api/services/${svc.id}`);
  });
}
