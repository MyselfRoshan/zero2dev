# Changelog

## 0.0.3

- **Databases**: MariaDB, MySQL, PostgreSQL and Redis can be installed from the Runtimes page and run as services.
  - Their data lives in `data\<kind>\<service id>`, for example `data\postgres\svc_1a2b3c4d`.
  - The first start creates the database files, with `mariadb-install-db`, `mysqld --initialize-insecure` or `initdb`.
  - They listen on 127.0.0.1 only. The administrator account (`root` / `postgres`) has no password.
  - **Stop** asks the server to shut down with its own tool (`mariadb-admin`, `mysqladmin`, `pg_ctl`, `redis-cli`) before anything is ended, so data files are not damaged.
  - The service page shows the connection details and a copyable URL. Deleting a database service keeps its files.
- **Code editors**: VS Code, VSCodium and Notepad++. Their settings and extensions are kept in the data folder, one folder per tool (`data\code`, `data\codium`, `data\notepadpp`); `bin` only holds the programs. A service page has **Open in VS Code** (or another installed editor) for its project folder.
- **Database tools**: HeidiSQL and DBeaver. A MariaDB, MySQL or PostgreSQL service page has **Open in HeidiSQL / DBeaver** with the connection filled in. Their settings go to `data\heidisql` and `data\dbeaver`.
- **Settings for each service type.** The service form only asks for what the chosen type uses, and shows that type's settings with their defaults filled in and a sentence on what each does, plus the argument or php.ini line it writes. You no longer need a program's documentation to run it.
  - A live **Command line** at the bottom of the form shows exactly what will run, the variables Zero2Dev adds and the files it writes. The service page shows the same, from the daemon.
  - **Project folder** now says what it is: your app's code, where the program starts. Zero2Dev never stores data there. Databases have no project folder (the form shows their data folder instead), and a custom command calls it **Working folder**.
  - Fields a type does not use are gone: no health check, program or project folder for databases; Caddy and nginx are no longer offered as new services (Zero2Dev creates them).
  - **Node.js**: script or npm script (with the scripts from package.json suggested), `--watch`, `--env-file`, debugger, NODE_ENV. npm's cache goes to `data\node\npm-cache`.
  - **PHP**: document root, router script, php.ini from development or production, tick-box **extensions** read from that PHP version's ext folder, memory/upload/time limits, errors, time zone, extra php.ini lines. Each PHP service gets its own php.ini in `data\php\<service id>`, written on every start; PHP's folder is never changed.
  - **Xdebug**: one click downloads the build matching the PHP version from xdebug.org, checked against the SHA-256 it lists, into that version's ext folder. Then tick it and choose its mode.
  - **Go**: package, build tags, and Go's module and build caches in `data\go` instead of your user profile.
  - **Java**: JAR or main class, max memory, Spring Boot port, debugger.
  - **MariaDB/MySQL**: character set, collation, SQL mode, max connections, buffer pool, max packet size, query and slow-query logs. **PostgreSQL**: max connections, shared buffers, statement logging, time zone. **Redis**: persistence (snapshots, every write, none), memory limit and eviction policy, number of databases.
  - **Advanced > Write my own command line** turns the settings off. Services created before this, with their own arguments, keep running exactly as before and can switch to the settings from their page.
  - API: `GET /api/kinds`, `POST /api/services/preview`, `GET /api/services/{id}/command`, `GET /api/runtimes/{id}/php-extensions`, `POST /api/runtimes/{id}/xdebug`, `GET /api/project/scripts`; services have `settings` and `customArgs`; `cwd` is optional for databases; the `{data}` placeholder (see `docs/API.md`).
- **A clearer service editor.** New and Edit open a page instead of a dialog.
  - It starts with **What do you want to run?**: one card per type, each with a one-line description.
  - Then comes a short Basics part, and the few settings most people change for that type. **More settings (n)** holds the rest, grouped. A changed setting is marked **Edited** with **Reset**, and stays visible even when folded.
  - Beside the form, a **What will run** panel stays in view: the command line with Copy, what is still missing in plain words ("Fill in Script…", "No MariaDB is installed yet…"), the number of changed settings with **Reset all**, the arguments that are always added and why, and Save.
  - On/off settings are switches.
  - PHP extensions are chips with an **Add an extension** box and **Show all**, and Xdebug has its own row.
  - On narrow windows the panel moves below the form and Save stays pinned at the bottom.
  - Routes: `#/services/new`, `#/services/{id}/edit`.
  - `profiles.json` gained `summary` per type and `common` per setting (API.md).
- **No terminal window.** `zero2dev.exe` is a Windows GUI program now: double-clicking it opens the dashboard in the browser and nothing else.
  - **Tray icon** (notification area): click it to open the dashboard. Right-click for **Open dashboard**, **Open data folder** and **Quit Zero2Dev**; its tooltip says how many services run.
  - **Quit Zero2Dev** is also at the bottom of the sidebar. The confirmation names the running services; they are stopped cleanly, then the page says Zero2Dev has stopped. API: `POST /api/shutdown`.
  - **Starting it twice** opens the dashboard of the one already running instead of failing on the busy port.
  - **Messages** go to `data\logs\zero2dev.log` (rotated at 1 MB); started from a terminal, they still print there, and `--version` works when piped. Startup errors are shown in a message box.
  - The Windows build uses `-H windowsgui` (`build.ps1`, `make build-windows`, the release workflow).
- **A livelier dashboard.**
  - A one-line status, quick actions, and tiles for services, sites, runtimes and databases that open their page already filtered.
  - Live service cards: uptime, latest output line, Start, Stop and Restart, and links to logs and the app. Start all and Stop all.
  - Sites with a reachable dot and an on/off switch, and recent activity across services.
  - On a fresh install, a welcome card and the Get started checklist with **Show me** tutorials.
- **Help center with guided tutorials.** Help (sidebar and top bar) lists six interactive tutorials: install a runtime, create a service, give it a local domain, add a database, open a project in your editor, and a quick look around.
  - The guide takes you to the right page and sits right next to what to press, with an arrow pointing at it and the rest of the page dimmed (clicks still go through). It moves on by itself when the step really happened. Progress survives a reload, and finished tutorials are ticked.
  - Help also explains the main concepts and the common fixes.
- **Ant Design look.** The dashboard follows Ant Design's design language: its colour tokens and palettes, 14 px type with only two weights, 32 px controls, 6 px corners on controls and 8 px on cards, flat cards with light borders, antd-style buttons, inputs, switches, tables, tags, alerts, dialogs and menu. It is still Svelte; no React components are used.
- **Appearance** (Settings → Appearance, and a theme button in the top bar).
  - **System, Light or Dark.** System follows Windows as it changes.
  - **Accent colour**: Blue, Geek blue, Purple, Magenta, Volcano, Orange, Green or Cyan, or any colour you pick. A full palette is generated from it (as antd does), so buttons, links, selected menu items, switches and progress bars all follow, in light and dark.
  - Text stays readable: when a colour would be too faint (antd's own blue with white text is 4.1:1), a deeper shade or dark text is used, so everything reaches WCAG AA.
  - The choice is saved by Zero2Dev, not the browser, so every browser shows it, and the page opens in it without a flash. API: settings `theme` and `accent` (see `docs/API.md`).
- **Design files.** `DESIGN.md` (in the `design.md` format AI design tools read) describes Zero2Dev's tokens and rules. `docs/design/` has Ant Design's own `design.md` and links to its AI documentation. A test keeps `DESIGN.md` in step with the code.
- **Runtimes: List or Grid view**, remembered between visits. In the grid, Manage or Install opens the runtime's details in a dialog, so the grid itself never changes shape.
- Fixed: in the narrow sidebar (below 1000 px) the menu links had no names for screen readers; and the info icon never rendered.
- **A clearer Runtimes page.**
  - Each runtime is one row instead of a large card: its icon, name and what it is, the versions you have (the default marked), an **Update** badge when a newer version of a line exists, and one button (**Install…** or **Manage**).
  - The install form and the installed versions open in the row, so the page is about a third as long. An install in progress or a failed one opens its row by itself.
  - Filters on top: All, Installed, and each group.
  - A version list that could not be checked is a notice that explains what it means, not a red line.
  - **Detect on this PC** shows proper names and has **Register all**.
  - The page header no longer breaks on narrow windows.
- **A clearer Settings page.**
  - Sections (Sites, HTTPS, Downloads, Startup, This PC, Access), with a side menu.
  - An **Unsaved changes** bar with Discard and Save appears only when something changed, and leaving the page asks first.
  - The HTTPS warning only shows when trusting is switched on; otherwise **How to trust it yourself** explains the certificate file.
  - Signing out is in a separate box.
- Fixed: **Check for new versions** offered the current PHP 8.2 to 8.5 only as unverified downloads. windows.php.net now names rebuilt packages `8.4.27-1`, which the version check refused; those versions are verified with their SHA-256 again.
- Fixed: **Detect** opened VS Code when its install folder, not its `bin` folder, is on PATH (as Laragon sets it up), because it ran `Code.exe --version`. VS Code and VSCodium are no longer run to read their version: it is read from their `resources\app\package.json`, and the editor's own `.exe` is recorded. Detect also finds `VSCodium.exe` on PATH.
- Runtimes are grouped as **Languages, Web servers, Databases, Code editors, Database tools**.
- **MD5 checksums**: MySQL publishes only MD5, so MySQL downloads are checked against it, and the installer log also records a SHA-256. **Import file** accepts a pasted MD5.
- **New download sources**:
  - mariadb.org (SHA-256).
  - EnterpriseDB for PostgreSQL. It publishes no checksum, so you confirm first, as with nginx.
  - The VS Code update API (SHA-256).
  - GitHub releases for Redis (community Windows build), VSCodium, Notepad++, HeidiSQL and DBeaver. GitHub's per-file SHA-256 is used.
  - MySQL versions come from the built-in list, because its download pages are not meant for programs.
- API: `POST /api/runtimes/{id}/launch`; new runtime and service kinds; checksum type `md5` (see `docs/API.md`).
- Tests:
  - The supervisor and API test helpers no longer exit at once: a bare `select {}` was reported as a deadlock. Most of those tests had been failing on every machine; they run again.
  - The obsolete `internal/installer/single_test.go`, which tested a removed design, is gone.
- The roadmap is in `docs/ROADMAP.md`.

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
