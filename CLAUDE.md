# Zero2Dev

A portable local development environment for Windows: one Go daemon that downloads runtimes, runs services and serves local sites, with a web dashboard.

## Layout

- `cmd/zero2dev/`: the program (`main.go`), console and log output, single instance, tray start.
- `internal/`: the daemon packages. The main ones:
  - `api` (HTTP API)
  - `supervisor` (runs services)
  - `installer` and `catalog` (downloads)
  - `kinds` (per-type settings, from `profiles.json`)
  - `sites` (Caddy/nginx)
  - `store` (`state.json`)
  - `tray` (Windows tray icon)
- `web/`: the dashboard. It is Svelte 5, built with Vite+ (`vp`) and pnpm. The build goes to `internal/webui/dist` and is embedded into the exe.
- `web/mock/server.mjs`: a fake daemon for UI work and the e2e test.
- `docs/`: USAGE, API, ROADMAP and screenshots. `CHANGELOG.md` is at the root.

## UI rules

- UI work follows [DESIGN.md](DESIGN.md), which is Ant Design's look implemented in Svelte. There are no React or antd components. The upstream reference is in `docs/design/`.
- Colours come only from the theme tokens (`var(--accent)`, `var(--surface)`, …) that `web/src/lib/theme.ts` generates. Never hard-code hex values in components.
- Keep text at 4.5:1 contrast in light and dark for every accent, and use one primary button per view.
- Keep the `data-tour` and `data-testid` attributes: the guided tours and the e2e test use them.

## Checks

- **Go:** `go vet ./...`, `go test ./internal/... ./cmd/...`, and `GOOS=linux go vet ./...` for the non-Windows build.
- **Web (in `web/`):** `pnpm run check` (format, lint, types), `pnpm test`, `pnpm run build`.
- **e2e:** `node e2e/smoke.mjs` drives the mock with Playwright, which is not a project dependency. Install it separately and point `NODE_PATH` at it.
- **Windows build:** `build.ps1` builds a GUI-subsystem exe (`-H windowsgui`).
