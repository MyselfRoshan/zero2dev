# Roadmap

What is planned for Zero2Dev, in the order it is being built. Each stage is tested before the next starts.

**The goal:** a developer opens Zero2Dev and has their whole local stack running in minutes, without reading a program's
documentation, without admin rights and without changing the system. Every item below is judged by one question:
_does it save a developer setup time, a context switch or a trip to the documentation?_

Status: **done** (shipped and tested), **in progress** (code exists but is not finished or not wired in), **planned** (not started).

## At a glance

| Release | Theme | Status |
| --- | --- | --- |
| 0.0.1 | Services, runtimes, sites, portable data folder | done |
| 0.0.2 | Many versions side by side, version discovery, nginx as site server | done |
| 0.0.3 | Databases, code editors, database tools, settings for each service type, no console window (tray icon), a new dashboard, in-app tutorials, and an Ant Design look with your own accent colour | done |
| 0.0.4 | **Projects**: one click starts the whole stack, in the right order, and tells you when something crashes; a `.env` file and database variables for every service; runtimes usable from your own terminals; Node.js 26 LTS | done (a few checks on a real Windows desktop are still open, see below) |
| 0.0.5 | **Data**: databases you can create, fill and reset; snapshots of a whole environment; Mailpit; **Import from this PC** your installed editors, runtimes and databases | done (checks on a real Windows desktop are still open, see below) |
| Engineering | **Taskfile**, one place for the version, release scripts in Go, real-program tests in CI, copy-safe folders (alongside 0.0.6, no visible change) | planned |
| 0.0.6 | **More languages** (Python, Rust), **Vite+**, an **API client**, the **project file**, CPU and memory per service, and **one-click new projects** for each language and framework | planned |
| 0.0.7 | **Real domains**: ports 80/443, `.test` domains, sharing | planned |
| Before 0.1.0 | **Code signing** | planned |
| Later | Updates, command line, installable dashboard | planned |

What Zero2Dev deliberately does not do (WSL, Docker, other platforms, and more) is listed under [Not planned](#not-planned).

## What is done

Checked against the code in this repository.

| Area | What a developer gets | Status |
| --- | --- | --- |
| Portable | Everything in `data\` and `bin\` next to the program; no AppData, registry or certificate store changes, except one opt-in, reversible switch for the user PATH (off by default) | done |
| Runtimes | Install Node (26 LTS and older), Go, Caddy, nginx, PHP and Java; detect what is on PATH; register a program by path | done |
| Versions | Many versions side by side, a default per runtime, a version per service, **Check for new versions** | done |
| Downloads | Verified against the publisher's SHA-256, SHA-512 or MD5; download proxy; **Import file** when downloads are blocked | done |
| Services | Create, edit, start, stop, restart; port-conflict checks; health checks; start with Zero2Dev; **Start all** / **Stop all** on the Services page and the dashboard, done by the daemon | done |
| Projects | A named group of services and sites; **Start** (sites on, site server running, services with their dependencies) and **Stop** per project, on the Projects page and the dashboard | done |
| Start order | **Starts after** and **Database** per service; Start waits for what a service needs; Start all, a project's Start and **Stop all** (apps, databases, site server, reversed dependencies); loops refused | done |
| Crash handling | **Restart if it crashes** (1 s, 2 s, 4 s ... up to 60 s, limit 1-20); tray balloon and a dashboard notice on a crash | done |
| Environment | A `.env` file for any service type; `DATABASE_URL`, `DB_HOST`, `DB_PORT` ... from a linked database; `JAVA_HOME` / `GOROOT`; **Open terminal** with the service's environment | done |
| Command line | `data\shims\` (`node`, `npm`, `php`, `java`, `go` ... and `activate` scripts for the default runtimes); opt-in per-item Environment page for the Windows user PATH and variables | done |
| Process safety | Children stop with the daemon (Job Object on Windows, process groups elsewhere); no shell strings run | done |
| Logs | Live and stored logs per service, filter, follow, copy, clear; installer log | done |
| Settings per type | The form asks only what the type uses, with defaults, explanations and a live **What will run** command line | done |
| Node | Script or npm script (read from `package.json`), `--watch`, debugger, npm cache in `data\` (the `.env` file is a service field for every type now) | done |
| PHP | Per-service `php.ini`, extensions as tick boxes, limits, one-click **Xdebug** matching the PHP version | done |
| Go, Java | Go caches in `data\go`; JAR or main class, heap, Spring Boot port, debugger | done |
| Databases | MariaDB, MySQL, PostgreSQL, Redis as services: created on first start, local only, clean stop, connection URL | done |
| Editors and tools | VS Code, VSCodium, Notepad++, HeidiSQL, DBeaver; **Open in** a project folder or a database | done |
| Sites | `*.localhost` for a folder, a port or a service, served by Caddy or nginx; optional HTTPS with a local CA; **Enable all** / **Disable all** with one site-server restart | done |
| Security | Loopback only, bearer token, Host and Origin checks, single-use event tickets, Caddy admin API off | done |
| Running it | No console window: a double-click opens the dashboard; **tray icon** (open, data folder, stop all services, quit); a second start opens the running one; **Quit** in the UI stops services cleanly; messages in `data\logs\zero2dev.log` | done |
| Dashboard | Status line, clickable tiles, live service cards (uptime, latest output, Start/Stop/Restart, Start all/Stop all), sites with on/off switches, recent activity | done |
| Help | Six **guided tutorials** that take you to the right page, highlight what to press with the rest dimmed, and move on when the step really happened; concepts and common fixes | done |
| Appearance | The **Ant Design** look (its tokens, sizes and palettes, in Svelte); **System / Light / Dark**; an **accent colour** from 8 presets or any colour, with its palette generated and text kept readable (WCAG AA); `DESIGN.md` describes it all | done |

## 0.0.4: projects, one click for the whole stack

Before, a developer started the database, then the API, then the front end, then the site server, one at a time, and found out
from a crash that the order was wrong. A project groups all of that, and the daemon knows the order.

The tray icon that the crash notices need shipped in 0.0.3.

| Stage | What | Why it helps | Status |
| --- | --- | --- | --- |
| 1 | **Projects**: a named group of services and sites; **Start** and **Stop** per project (`#/projects`, `/api/projects`, a box on the dashboard) | One click instead of five | done |
| 2 | **Stop all, done by the daemon**: one request (`POST /api/services/stop-all`, optionally `?project=<id>`) that stops apps first, then databases, then the site server, services in the same group at the same time, and reports what did not stop | Free the machine in one click, from anywhere, without damaging data | done |
| 3 | **Stop all services** in the **tray menu** (Zero2Dev keeps running), next to Quit | Stop everything before a meeting or a game without opening the dashboard | done |
| 4 | **Start order**: **Starts after** and **Database** per service; Start waits for what a service needs, Start all and a project's Start go in waves, Stop all uses the reverse order; a dependency loop is refused | No more "connection refused" on the first start, and no app errors while its database stops | done |
| 5 | **Restart on crash** with a growing delay (1 s, 2 s, 4 s ... up to 60 s) and a limit of 1 to 20, chosen per service | A watcher or dev server that dies comes back by itself | done |
| 6 | **Notify on crash**: a tray balloon (first crash, giving up, or a crash with no restart planned) and a notice on the dashboard and the service page | The developer learns about it before the browser shows an error | done; the balloon has not been seen on a real desktop yet |
| 7 | **`.env` file for any service type** (replaces the Node-only `--env-file` setting, migrated by itself) | Use the project's existing `.env` instead of retyping variables | done |
| 8 | **Open terminal**: a terminal in the service's folder with its runtime versions on PATH, `JAVA_HOME` / `GOROOT`, the database variables and the `.env` file | `php`, `node`, `java` in the terminal match what the service uses | done; not yet checked in a real Windows Terminal or Command Prompt window |
| 9 | **Database connection variables**: link a service to a database service and get `DATABASE_URL`, `DB_HOST`, `DB_PORT`, `DB_USERNAME`, `DB_PASSWORD`... (and `REDIS_URL`) set for it | No copying ports and user names between pages | done |
| 10 | **Start all / Stop all** on the Services page and **Enable all / Disable all** on the Sites page; the dashboard buttons use the daemon too. Sites change with one config write and one site-server restart | Bulk actions that are fast and in the right order, from every page | done |
| 11 | **Runtime environment registration**: `data\shims\` (`node`, `npm`, `php`, `java`, `go`, database clients, `activate.cmd/.ps1/.sh`) regenerated on every runtime change; an opt-in, reversible switch for the Windows user PATH | The runtimes Zero2Dev installed work in any terminal, with the version you made default | done; the registry writes are only tested against a fake, a real Windows check is open |
| 12 | **Node.js 26 LTS** (26.13.1) in the built-in catalog; CI, `@types/node` and the documented prerequisite move to Node 26 | The newest LTS is the one Zero2Dev offers and is built with | done |

Release 0.0.4: **done**. These checks need a real Windows desktop and have only been covered by tests so far:
- **Open terminal**: that Windows Terminal and Command Prompt really receive the service's environment (`node -v`, `JAVA_HOME`).
- **Registry**: turning *Add Zero2Dev's tools to my Windows user PATH* on and off against the real user environment (`reg query HKCU\Environment`, a new shell finding `node`, the old `Path` restored).
- **Tray balloon**: the crash balloons on a real desktop.

## 0.0.5: data, databases and environment snapshots

Databases run today, but creating a schema, loading a dump or starting over still needs a separate tool. And trying a risky
change (a migration, a PHP upgrade, a new extension) has no undo for the environment as a whole.

| Stage | What | Why it helps | Status |
| --- | --- | --- | --- |
| 1 | **Create a database and a user** from the service page | The first step of every new project, without SQL | done |
| 2 | **Import a dump** (`.sql`, `.sql.gz`) and **export** one (`mysqldump`, `pg_dump`) | Load a copy of production or a colleague's data in one step | done |
| 3 | **Database snapshots**: copy one database's data folder before a risky change, restore it later | An easy undo for migrations | done |
| 4 | **Environment snapshots**, one set per project: a named copy of everything the project's environment is made of (its services and their settings, the runtime version each one uses, generated `php.ini` files, sites, and the data of its databases), taken with the services stopped | "Before the PHP 8.4 upgrade": try it, and go back in one click if it breaks | done |
| 5 | **Restore** a snapshot, or **clone** it into a new environment (for example a second copy of the project to test a branch) | Several states of the same project without setting them up again | done |
| 6 | **Snapshot before risky actions**, offered automatically before switching a runtime version, resetting a database or importing a dump | The undo exists even when nobody thought of taking one | done |
| 7 | **Reset a database** (delete its files and create them again) | Start clean without hunting for the data folder | done |
| 8 | **Optional password** for the administrator account | For apps and frameworks that refuse an empty password | done |
| 9 | **Mail catcher** (Mailpit) as a service type, with its SMTP settings shown | Test e-mails without sending real ones | done |
| 10 | **Import from this PC** (`#/migrate`): copy or move the settings and data of an installed VS Code, VSCodium, Notepad++, HeidiSQL or DBeaver into Zero2Dev's portable folders; adopt a runtime found on PATH; move npm, Composer and Go caches; copy databases from another server. Every migration can be reverted | Switch to Zero2Dev without setting your editors and tools up again, and go back if you change your mind | done |

Snapshots live in `data\snapshots\<project id>\<snapshot>` and move with the folder like everything else. Runtimes are not
copied: a snapshot records which installed version each service used, and restoring it offers to install a missing one.

Release 0.0.5: **done**. The tests use a fake for the database programs, the registry and the user folders, so these checks need a real Windows desktop:
- **Databases**: create, import (`.sql`, `.sql.gz`), export, reset and the password against real MariaDB, MySQL, PostgreSQL and Redis servers. With a password set, the clean stop must still work, and HeidiSQL must connect. `redis-cli -x` and `REDISCLI_AUTH` on the Windows Redis builds.
- **Snapshots**: take and restore a project after switching its PHP default; restore after uninstalling that version (the install is offered); clone a project and open both sites.
- **Mailpit**: mail sent to port 1025 shows in the inbox on 8025.
- **Import from this PC**: copy VS Code, open Zero2Dev's VS Code with the same settings and extensions, then revert. Move VSCodium and move it back. The same round trip for Notepad++, DBeaver and HeidiSQL (the registry key comes back after move-back). Adopt a Node.js found on PATH and revert. `npm ls -g` in **Open terminal** after copying the npm globals. Copy a database from a system MySQL and revert.
- **Import, clean-up and space**: copy VS Code, then **Remove the old files** in History: `%APPDATA%\Code\User` and `.vscode\extensions` are gone, Zero2Dev's VS Code still has everything, and **Move back** puts them back. The free space shown on the plan matches the real drive. **Delete backups** really frees the disk space.
- **VS Code and VSCodium `data` link**: double-click `Code.exe` and `VSCodium.exe` in `bin` and see the same settings and extensions as from **Open**; remove a version and check that `data\code` is untouched.

## Engineering: alongside 0.0.6

No visible change for users. Each stage makes the releases after it cheaper and safer to ship. Windows contributors should
not need bash, and the checks should run the real program, not only the mock.

| Stage | What | Why it helps | Status |
| --- | --- | --- | --- |
| 1 | **Taskfile** (go-task) replaces the Makefile and `build.ps1`: the same commands on Windows and Linux (`task build`, `task test`, `task smoke`, `task bump`) | One entry point instead of PowerShell, make and bash; Task is a single program, installed once (for example `winget install Task.Task`) | planned |
| 2 | **The version in one place**: `VERSION` is embedded in the program (a root Go package with `go:embed`), so `main.go` and `installer.go` hold no copy. `task bump` also rewrites `web/package.json` and runs `scripts/winres.mjs` for the exe's version info | Today the version is written in `VERSION`, `web/package.json`, `main.go`, `installer.go` and the committed `.syso` | planned |
| 3 | **Release scripts in Go** (version bump, release check, smoke test) instead of bash, `sed`, `awk`, `python3` and `curl`. `scripts/winres.mjs` stays: Node is needed for the web build anyway | They run on Windows and can be unit-tested; Task's own shell has no `sed` or `awk` on Windows | planned |
| 4 | **Real-program test in CI**, on Windows and Linux: the built program with a temporary home, a tiny Node service, live logs, health check, clean stop, and a short Playwright pass against it. The mock suite stays for fast UI checks. `scripts/smoke.sh` and `web/e2e/real-daemon.cjs` exist today but CI runs neither, and `real-daemon.cjs` looks out of date | A change to a Go handler that breaks the dashboard now fails CI | planned |
| 5 | **Nightly desktop checks** (not blocking a release): real MariaDB, PostgreSQL, Redis and Mailpit on a Windows runner (create, import, export, reset, snapshot, restore) | Automates much of the open "real Windows desktop" list under 0.0.5; it needs the network, so it must not block a release | planned |
| 6 | **Copy or move the folder**: an installed runtime is re-pointed also when its old folder still exists (a copy to a USB stick still runs the original's programs today), and the revert records of **Import from this PC** are checked after a move. Both with tests | The README promises that a copied or moved folder keeps working | planned |

## 0.0.6: more languages and one-click new projects

Starting a new project today means installing the runtime, running the framework's own generator in a terminal, creating a
database, then filling in a service and a site by hand. **New project** does all of it from one form. The same release adds
Rust, Vite+ and an API client, and the project file that describes what **New project** builds.

| Stage | What | Why it helps | Status |
| --- | --- | --- | --- |
| 1 | **Python** runtime with a virtual environment per service (in `data\`) | The most requested missing language; no global `pip install` | planned |
| 2 | **Composer** for PHP, run with the service's PHP version | `composer install` with the right PHP, from the service page | planned |
| 3 | **pnpm and Yarn** through Node's Corepack, with their caches in `data\` | Projects that do not use npm work without a global install | planned |
| 4 | **Run a one-off task** from the service page (`npm install`, `composer install`, `go mod download`, migrations) with its output in the log | Setup commands with the right runtime, without a terminal | planned |
| 5 | **Detect the project type** from the folder (`package.json`, `composer.json`, `go.mod`, `pom.xml`, `manage.py`) and fill in the form | A new service is mostly filled in after choosing the folder | planned |
| 6 | **New project**: choose a language, then a framework, a folder and a name | One form instead of a terminal, a generator's documentation and three Zero2Dev forms | planned |
| 7 | Zero2Dev **installs what is missing** (the runtime version the framework needs, Composer, a database) and **runs the framework's own generator** with it | The project is made the way its framework intends, with the right versions | planned |
| 8 | It then **creates the project**: its services, its database with a user, a `name.localhost` site, a `.env` with the connection filled in, and opens the folder in the installed editor | From nothing to a running app in the browser in one click | planned |
| 9 | Frameworks are a **list shipped with Zero2Dev** (like the runtime catalog), so new ones are added without code | The list can grow quickly and be updated between releases | planned |
| 10 | **Vite+** (`vp`): a project that uses it (a `vite-plus` dependency or a `vp` script) gets its commands (`vp dev`, `vp build`, `vp run`, `vp test`) offered as the service's script, and `vp create` joins the generators below. Vite+ manages Node.js and the package manager itself (`vp env install`, `vp env pin`), so Zero2Dev **tells the user that a Vite+ project can get its Node from Vite+ instead of from Zero2Dev's Node runtime**, and shows which of the two the service will use. The choice is made once per service, so a project never has two Node versions fighting over `PATH` | Vite+ projects work out of the box, and nobody installs Node twice by accident | planned |
| 11 | **Rust** (Cargo) as a language: the toolchain is installed with rustup into `data\rust` (`RUSTUP_HOME` and `CARGO_HOME`), and a service type runs `cargo run`. The toolchain choice is settled when the stage starts: the MSVC toolchain needs the Visual Studio Build Tools (an administrator install), the GNU toolchain should not (to be tested) | A Rust project runs without a global rustup and without changing the system | planned |
| 12 | **API client** as a new tool category, like Database tools: installed portable, its data in `data\<tool>`, **Open** on the project folder. **Both of these are offered**, so the user picks by what matters to them (checked on 2026-11-20 from the release pages): **Bruno** (MIT; Windows zip, 177 MB; its collections are plain files in the project folder, so they go into Git, snapshots and the project file) and **Hoppscotch Desktop** (portable zip, about 45 MB, the small one; its collections are kept in the app or an account, not in the project). Each must pass three checks before it ships: a portable zip, a published checksum (not verified yet), and settings that can move into `data\`. **Yaak** (MIT, about 45 MB) was looked at and left out: it only comes as an installer, so it cannot stay in the portable folder | Test the API a service exposes without a separate install, with a light choice and a project-friendly choice | planned |
| 13 | **Project file** (`zero2dev.json`): export a project, and **Import project** from a file or a cloned repository. It describes what **New project** creates, so the two share one format. Paths are relative to the file, runtimes are a kind plus a version, and secrets are never written | `git clone`, Import, Start: a new team member has the same stack | planned |
| 14 | **CPU and memory per service** on the dashboard and the service page, summed over the service's whole process tree (every service already runs in its own Windows job object) | Find the service that slows the machine down | planned |

Frameworks in the first list, each with the generator Zero2Dev runs:

| Language | Frameworks | Generator |
| --- | --- | --- |
| PHP | Laravel, Symfony, WordPress, plain PHP | `composer create-project`, WordPress download (SHA-1 listed by wordpress.org) |
| JavaScript / TypeScript | Express, NestJS, Next.js, Vite (React, Vue, Svelte), Astro | `npm create ...`, `npx create-next-app`, `vp create` for a Vite+ project |
| Python | Django, FastAPI, Flask | `django-admin startproject`, a starter folder for FastAPI and Flask, in a virtual environment |
| Go | net/http, Gin, Fiber | `go mod init` and a starter `main.go` |
| Java | Spring Boot | a project from start.spring.io (Maven or Gradle) |
| Rust | Axum (a starter) | `cargo new` and a starter `main.rs` |
| Static | HTML and CSS | a starter folder served by the site server |

## 0.0.7: real domains and sharing

| Stage | What | Why it helps | Status |
| --- | --- | --- | --- |
| 1 | **Ports 80 and 443** for sites (optional). Windows does not keep them for administrators, so no permission is needed: Zero2Dev checks that they are free, names what holds them (IIS, the World Wide Web Publishing Service, another web server, a reserved port range) and says how to free them | `http://shop.localhost` without `:8080` | planned |
| 2 | **Hosts-file domains** such as `shop.test` (optional; this is the one place that needs administrator permission, asked for once, with a clear undo) | For apps that need a real-looking domain or cookies across subdomains | planned |
| 3 | **Share a site** for a few hours through a tunnel (for example cloudflared) | Show work to a client or test a webhook without deploying | planned |

## Before 0.1.0

| What | Why it helps | Status |
| --- | --- | --- |
| **Code signing** of the release `.exe` through the free open-source program of the SignPath Foundation, from the release workflow (the program's terms and the repository's eligibility need checking first) | Fewer SmartScreen and antivirus warnings on a downloaded program that starts other programs | planned |

## Later

| What | Why it helps | Status |
| --- | --- | --- |
| **Command line**: `zero2dev start <project>`, `zero2dev stop --all`, `zero2dev logs <service>` | Scripts and editors can drive Zero2Dev | planned |
| **Auto-update** of Zero2Dev itself, verified | No manual download for each release | planned |
| **Installable dashboard**: a web app manifest and icons, so the browser offers **Install** and the dashboard opens in its own window with a taskbar icon. No service worker, so nothing is cached and there are no stale pages after an update | An app-like window at almost no cost | planned |

## Not planned

Zero2Dev is the **portable, no-administrator, no-Docker, Windows-native stack for several languages**. Anything that pulls
away from that is left out on purpose:

| Not planned | Why |
| --- | --- |
| **Docker or container features** | Containers are a different tool with its own ecosystem; Zero2Dev exists for people who do not want one |
| **WSL support** (running services in a distribution, Linux runtimes, remote editors) | WSL needs a Windows feature and a distribution installed, and people who keep their code there already have Linux tooling. It was planned as 0.0.7 and is removed |
| **Dev Containers and Nix-style reproducibility** | The project file and snapshots cover what a team needs from Zero2Dev, with the runtimes Zero2Dev installs |
| **A database administration tool** (a table browser, a query editor, more database engines for their own sake) | HeidiSQL and DBeaver do this, and Zero2Dev already opens them on a database. Snapshots, import and export stay |
| **Linux and macOS as supported platforms** | Those systems have good native tooling (Homebrew, system packages, version managers), and the tray, the job objects, the no-administrator story and the runtime catalog are Windows-specific. The Linux build keeps compiling and runs the race-detector tests in CI, with no promise of the same features; macOS is not built or tested at all |
| **Replacing `state.json` with SQLite** | The file is written atomically (a temporary file, then a rename), the data is small and readable, and Zero2Dev has no third-party Go modules. If corruption is ever reported, a copy of the last good file is the first thing to add |
| **A service helper or `netsh http add urlacl`** to open ports 80 and 443 | Windows has no administrator-only ports, and `urlacl` only applies to programs built on the Windows HTTP Server API, which Caddy and nginx are not. Installing a service would itself need administrator permission |
| **Mock Service Worker** for UI tests | `web/mock/server.mjs` is already a complete fake daemon that the end-to-end test drives through the real API client; the real-program checks are added next to it (see Engineering) |
| **A full PWA with a service worker** (`vite-plugin-pwa`) | The dashboard needs the running daemon to show anything, so offline support has no use, and a cache in front of an interface that is embedded in each release can show an old version after an update |
| **Package managers as the only source of runtimes** (winget, Scoop, Nix) | Version lists are already read from each publisher, so the catalog is not a hand-kept list; its upkeep is the readers that follow each publisher's pages. A package manager's manifests may help as a second source for one runtime whose page is hard to read, judged for that runtime |

---

## 0.0.4 in detail

- **Start order** is computed in waves: services whose dependencies are running are ready; the site server starts first, then databases, then apps; a wave is awaited only when something waits for it, and no wave waits longer than the health timeout (30 s). Stop is the same plan reversed. With no dependencies set it is the old tier order.
- **Environment, later wins:** runtime `PATH`, `JAVA_HOME` / `GOROOT`, the type's own variables, the database variables, the `.env` file, the service's own variables, `PORT`.
- **Restart on crash** gives up after the limit, resets after a minute of stable running, and is cancelled by Stop, Stop all, delete and quit.
- **The Environment page** (per-item switches for PATH folders and variables: shims, one runtime version, your own folder, `JAVA_HOME`, `GOROOT`, custom variables) touches only `HKCU\Environment`, keeps a backup in `data\shims\.registry-backup.json`, never overwrites a variable you have, and undoes only its own changes. Known limits: the `.cmd` shims read the OEM code page (non-ASCII data folder paths can break them) and PowerShell's default execution policy can block `activate.ps1`.
- **Not in 0.0.4:** services that start with Zero2Dev (**Start automatically**) are not ordered.

## 0.0.3 in detail

Goal: install and run the rest of a typical local stack from the same place, without touching the system.

| Stage | What | Status |
| --- | --- | --- |
| 0 | Version 0.0.3, this roadmap | done |
| 1 | MD5 checksums (MySQL publishes only MD5) | done |
| 2 | New kinds and categories: Databases, Code editors, Database tools | done |
| 3 | Download sources for the new kinds | done |
| 4 | **Open** editors and database tools (on a project folder or a database) | done |
| 5 | Databases as services: data folder, first-start setup, safe stop, connection details | done |
| 6 | Mock daemon, docs and end-to-end tests | done |
| 7 | **Settings for each service type**: the form shows only what the type uses, every setting with its default and an explanation, a live command line; PHP extensions, php.ini and Xdebug | done |
| 8 | **Per-tool data folders**: editor and database tool settings in `data\code`, `data\codium`, `data\notepadpp`, `data\heidisql`, `data\dbeaver`; `bin` holds programs only | done |
| 9 | **Service editor** as its own page: type cards, the common settings first and the rest under More settings, Edited/Reset per setting, a sticky **What will run** panel | done |
| 10 | **Settings page** in sections (Sites, HTTPS, Downloads, Startup, This PC, Access) with an unsaved-changes bar | done |
| 11 | **Runtimes page**: one row per runtime with its versions, filters, **List / Grid** views (a grid tile opens its details in a dialog), clearer version-list notices | done |
| 12 | **No console window**: GUI build, tray icon (open, data folder, quit), single instance, **Quit** in the UI, log file, message box on startup errors | done |
| 13 | **Dashboard**: status line, tiles, live service cards, sites, recent activity, welcome card and Get started with **Show me** | done |
| 14 | **Help center**: six guided tutorials (the guide sits next to what to press, the rest of the page dimmed; steps finish by themselves; progress survives a reload), concepts, common fixes | done |
| 15 | Fixes found on the way: **Detect** no longer opens VS Code (versions read from files); current PHP versions verified again (`8.4.27-1` builds); menu links named in the narrow sidebar; the info icon renders | done |
| 16 | **Ant Design look and themes**: antd's tokens and palettes implemented in Svelte (no React); every component restyled (32 px controls, 6/8/4 px radii, 400/600 weights, flat cards); Settings → **Appearance** with System/Light/Dark and an accent colour (8 presets or custom) that recolours the whole UI, kept at AA contrast; a theme button in the top bar; `DESIGN.md` and `docs/design/` for people and AI tools | done |

### What ships

| Category | Tools | Download and check |
| --- | --- | --- |
| Databases | MariaDB | mariadb.org API, SHA-256 |
| | MySQL | dev.mysql.com, **MD5** (the only checksum MySQL publishes) |
| | PostgreSQL | EnterpriseDB binaries ZIP, no checksum published (you confirm, like nginx) |
| | Redis | community Windows build (github.com/redis-windows), SHA-256 |
| Code editors | VS Code | Microsoft update API, SHA-256, data in `data\code` |
| | VSCodium | GitHub releases, SHA-256, data in `data\codium` |
| | Notepad++ | GitHub releases (portable ZIP), SHA-256 |
| Database tools | HeidiSQL | GitHub releases (portable ZIP), SHA-256 |
| | DBeaver CE | GitHub releases, SHA-256, workspace kept in the data folder |

### How databases behave

- Data lives in `data\<kind>\<service id>` (for example `data\postgres\svc_1a2b3c4d`) and moves with the folder. Deleting a service keeps its data.
- The first start creates the database files (`mariadb-install-db`, `mysqld --initialize-insecure`, `initdb`).
- They only listen on `127.0.0.1`. The administrator account has no password (`root` for MariaDB/MySQL, `postgres` for PostgreSQL), which is fine for a local development machine and must never be exposed.
- **Stop** runs the database's own shutdown command (`mysqladmin shutdown`, `pg_ctl stop`, `redis-cli shutdown`) before anything is killed, so data files are not damaged.
- The service page shows the connection details and a copyable URL, plus **Open in HeidiSQL / DBeaver** when those are installed.

### How editors and tools behave

- They are not services: they are opened, not supervised, and keep running when Zero2Dev stops.
- Their settings, extensions and workspaces are kept in the data folder, one folder per tool (`data\code`, `data\codium`, `data\notepadpp`, `data\heidisql`, `data\dbeaver`). `bin` only holds the programs.
- A service page has **Open in** to open its project folder in an installed editor.

### Where everything lives

```
zero2dev.exe
bin\        programs only (one folder per installed version), never settings or data
data\
  code\       VS Code settings and extensions (user-data, extensions)
  codium\     VSCodium settings and extensions
  notepadpp\  Notepad++ settings
  heidisql\   HeidiSQL settings (portable_settings.txt)
  dbeaver\    DBeaver workspace
  php\<service id>\      php.ini, sessions, uploads, Xdebug output of a PHP service
  node\npm-cache\        npm's download cache
  go\                    GOPATH, GOCACHE and GOENV for Go services
  mariadb\<service id>\   mysql\...   postgres\...   redis\...   database files
  nginx\  caddy\          web server files
```

This applies to tools Zero2Dev installed. A program you registered by path is your own install and keeps its usual settings.
