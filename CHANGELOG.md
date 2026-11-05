# Changelog

## 0.0.2

- **Runtimes now live in a `bin` folder next to `zero2dev.exe`, separate from `data`.** Runtimes installed by 0.0.1 are moved there on first start. `ZERO2DEV_BIN` overrides the location. `GET /api/status` has a new `binDir`.
- **Icons come from Iconify** (Lucide set), bundled into the program: nothing is loaded from the internet.
- Fixed: **Check for new versions** showed "unexpected GitHub answer: unexpected end of JSON input" for Caddy. GitHub's release list is large and was cut off; the page is smaller now, and an oversized answer is reported as such instead of being parsed half-read.
- The build output moved from `bin\` to `dist\` so it does not collide with the runtimes folder.
- Development: the web UI uses **pnpm** instead of npm (`web/pnpm-lock.yaml`, version pinned in `packageManager`). The built UI in `internal/webui/dist` is not tracked in git: build it (`cd web && pnpm install && pnpm build`, `make web` or `build.ps1`) before any Go command. See "Building from source" in the README. `make build` now writes to `dist/` (where `scripts/smoke.sh` looks), and every Go `make` target builds the UI first when it is missing.

- nginx, PHP and Java can be installed from the Runtimes page (Windows x64; Java also on Linux x64). Java is Eclipse Temurin from Adoptium.
- **Many versions side by side** for every runtime (Node, Go, Caddy, PHP, Java, nginx): for example PHP 7.1 and PHP 8.4 installed together. Each kind has one **default** (the first installed; change it with **Make default**; removing it promotes another). A service picks a specific version with its runtime setting, or follows the default.
- **Check for new versions** asks nodejs.org, go.dev, GitHub (Caddy), windows.php.net (current and archived PHP lines such as 7.1 and 7.4), the Adoptium API and nginx.org. Results are cached in `data\cache\remote-catalog.json`; nothing is fetched until you press the button (the page does it once automatically the first time).
- Downloads are verified with the checksum the publisher lists (SHA-256 or SHA-512); old PHP lines without one are treated like nginx. nginx.org publishes no checksums: Zero2Dev asks you to confirm, downloads over HTTPS from nginx.org only, and records the SHA-256 it computed in `installer.log`. You can paste a SHA-256 you trust in **Import file** instead.
- New service kind `nginx` (defaults to `-p <nginx folder>/`).
- **nginx can serve your sites instead of Caddy**: **Settings > Sites and proxy > Site server**. Zero2Dev writes `data\config\nginx.conf`, creates an **Nginx (sites)** service when an nginx runtime is installed, and on switching stops the running server and starts the other one (they share the proxy ports). For HTTPS sites served by nginx, Zero2Dev creates its own local CA in `data\nginx\certs` (nothing is added to Windows). Caddy stays the default.
- **Simpler dashboard and forms.**
  - The dashboard shows a **Get started** checklist (install a runtime, add a service, optionally give it a domain) until setup is done, then your services and sites. The number tiles are gone.
  - The service form asks for Name, Type, Project folder, Arguments, Port and Version. Program override, health check and environment variables are under **Advanced**.
  - The site form asks what the domain **points to**: a service, a port or a folder. The domain is suggested from the name.
  - The service page shows the exact command it runs. **Restart** only appears while a service runs.
  - The Sites page offers **Start** right in its "not running" notice.
- **Runtimes are grouped**: **Languages** (Node.js, PHP, Go, Java) and **Web servers** (Caddy, nginx). Web servers serve your sites rather than run your code; the one currently serving them is marked **Serves your sites**. The service form's Type list and the Register by path dialog use the same groups.
- **Enable / Disable sites in one click** from the Sites list, like Start and Stop for services. API: `POST /api/sites/{id}/enable` and `/disable`.
- **Browse for folders**: the project folder of a service and the folder of a static site have a **Browse...** button that opens the Windows "Select Folder" dialog (shown by the daemon, since browsers never reveal a folder's full path). Typing the path still works. API: `POST /api/dialog/folder`, `folderPicker` in `GET /api/status`.
- **Runtime icons**: every runtime card, service and detected tool shows the brand mark of its kind (Node.js, PHP, Go, Java, Caddy, nginx), bundled like the other icons.
- Service args accept `{home}` (the data folder, with forward slashes) next to `{port}`.
- Static site folders whose path contains `$` are refused (nginx would read it as a variable).
- API: `GET /api/catalog/status`, `POST /api/catalog/refresh`, `POST /api/runtimes/{id}/default`, `acceptUnverified` on install and import, error `details` (see `docs/API.md`).

## 0.0.1

First numbered release. Focus: isolation and portability.

- Portable: all data lives in `data/` next to the executable; the folder can be moved or copied.
- Nothing is written to the user profile, `AppData`, the registry or the OS trust store.
- Caddy runs isolated: its data and config are kept in `data/caddy`, its admin API is off, and its local CA is not added to Windows unless you ask.
- Caddy install fix (root cause found from the installer log): Caddy publishes SHA-512 checksums and the installer only understood SHA-256, so every Caddy install failed with "no checksum for ... in the published checksum file". SHA-256 and SHA-512 are now both verified.
- Other Caddy install hardening: retry when antivirus locks the extracted file, download timeouts and retries, a download proxy setting, full error messages that name the failing step, `data/logs/installer.log`.
- Import a runtime archive you downloaded yourself (verified against the published or a pasted SHA-256).
- Services, runtimes, sites, live logs and the embedded Svelte dashboard from the earlier prototype.
- Added `docs/USAGE.md`, a step-by-step guide with screenshots. The first-run page now says the token is in `data\token` next to the exe.
