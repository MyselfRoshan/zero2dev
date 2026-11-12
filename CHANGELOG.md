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
