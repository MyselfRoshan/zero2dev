# Changelog

## 0.0.6

### Python

- **Python runtime.** Python comes from python-build-standalone (`install_only` `.tar.gz`, checked with the SHA-256 of the release). Python 3.13 and 3.12 are built in for Windows (amd64 and arm64); `Check for new versions` adds the newest patch of each of the last four lines from the project's GitHub releases. Python has its own card in the Languages group.
- **Python services.** A new service type with a **Run** setting: a script, a module, Uvicorn (FastAPI), Flask or Django `runserver`, with fields for the script, module, application and host. The port reaches Uvicorn, Flask and Django as their own option and every mode as `PORT`. `PYTHONUNBUFFERED=1` is set so `print()` shows up in the log at once.
- **A virtual environment for each service.** Before the first start Zero2Dev makes `data\python\venvs\<service id>` (`python -m venv`) and runs the service with its `python.exe`, `VIRTUAL_ENV` set and its `Scripts` folder first on `PATH`. When the service's runtime changed, the environment is made again and the log says so. Deleting the service deletes its environment; snapshots never copy it.
- **Tasks of a Python service.** Install requirements (`pip install -r requirements.txt`), List packages, and for Django projects migrate and collectstatic.
- **`python` and `pip` in the command line shims.** `pip` is found in the runtime's `Scripts` folder, which the activate scripts put on `PATH` too.

### Composer, pnpm and Yarn

- **Composer** is a tool on the Runtimes page: install a version (2.11 and the 2.2 long-term-support line, checked with the publisher's SHA-256, for Windows on x64 and on arm64). It is one `composer.phar` with a `composer.bat` launcher, and the list of versions comes from getcomposer.org.
- Composer runs with the PHP of the service. The `composer` command in your own terminal always uses your default PHP, whatever PHP is on your PATH.
- Composer's home and cache are now always in the data folder (`COMPOSER_HOME`, `COMPOSER_CACHE_DIR`) for PHP services, Open terminal and the `composer` command, not only after an import.
- A Node service has a **Package manager** setting: npm, pnpm or Yarn. pnpm and Yarn come from Corepack, which Zero2Dev installs into the data folder the first time (Node 26 does not include it). Corepack's versions, pnpm's store and Yarn's cache are in the data folder too.
- A service can have steps that run before it starts (the Corepack setup is the first one); their output is in the service's log and a failing step stops the start.
- API: a catalog source for Composer, and the `composer` and pnpm/Yarn environment; see "Composer and package manager details" in `docs/API.md`.

### Tasks and project detection

- **One-off tasks of a service.** `GET /api/services/{id}/tasks` lists what can be run: the ready-made tasks of the service's kind whose file is in the project folder (`npm install`, `composer install`, `go mod tidy`, `php artisan migrate` ...) and the scripts of a Node project's `package.json`. `POST` runs one, or a program of your own, as a job.
- **Same environment as the service, no shell.** A task runs in the project folder with the environment the service starts with (the runtime on `PATH`, the `.env` file, the database variables), after the service's own preparation (the Python virtual environment, Corepack). The program and each argument reach the program as they are. A service that is running may have a task; a second task for the same service is refused.
- **Output in the service's log.** Every line of a task goes to the service's log as it comes, starting with `$ program args`. A failing program fails the job with its exit code and the last lines it wrote; a task that runs too long is stopped with its whole process tree.
- **Presets for Node, PHP and Go.** Node: install, build and test with the service's package manager (npm unless set). PHP: Composer install, update and dump-autoload, and Laravel's migrate (asks first) and cache:clear. Go: mod download, mod tidy, test and build.
- **Project detection.** `GET /api/project/detect?dir=` reads a folder (it never writes) and says which kind and framework it holds (Laravel, Symfony, WordPress, Next.js, NestJS, Astro, Vite, Express, Gin, Fiber, Spring Boot, Django, FastAPI, Flask), a name, the usual port, the `.env` file, and settings for the kind's options, so the New service form can fill itself in.
- **Run a task, on the service's page.** A new **Run a task** section lists the presets of the service's type (with their help and the exact command), the `package.json` scripts of a Node service (run with its package manager) and a **Run your own command** form (program and arguments, split on spaces with quotes; no shell, and the form says so). While a task runs a progress row shows it, every Run button is off and the output streams into **Live output** (lines of the stream `task` get a mark on the left); the result, or the error with the last output lines, stays until the next run. Tasks with a confirm text (Composer update, `artisan migrate`, Django migrate) ask first. A task already running, started elsewhere, is shown after a reload. A missing Composer is explained with a link to Runtimes.
- **The New service form detects the project.** Choosing or typing a folder calls `GET /api/project/detect`: it picks the type when none is chosen, and fills only the empty fields (name, settings, port, `.env` file). A dismissible note says what it recognised and filled in, with **Undo**. Nothing you typed is replaced, it never runs when editing a service, and a failure is silent. The Python rules now set the run mode: Django (`run` Django, `manage.py`), FastAPI (Uvicorn, `main:app`, `app:app` or `app.main:app` from the files found) and Flask (`flaskApp` `app` or `wsgi`).

### Create a project from a framework

- **A list of 19 frameworks** shipped as data (`internal/frameworks/frameworks.json`, or `<data>\frameworks.json`): PHP (Laravel, Symfony, WordPress, plain PHP), JavaScript (Express, NestJS, Next.js, Vite with React, Vue or Svelte, Astro), Python (Django, FastAPI, Flask), Go (net/http, Gin, Fiber), Java (Spring Boot) and HTML and CSS. Every generator runs without questions (`--no-interaction`, `--yes`, `--no-git`, `CI=1`).
- **`POST /api/new-project/plan` and `POST /api/new-project`.** The plan says what would be installed, created and run, and lists problems (folder not empty, domain or port taken, no runtime version fits) and warnings. The job installs the missing runtime, Composer and database server, creates the database with a user of its own (random password, only in the `.env`), runs the framework's generator with the runtime's own tools (no shell, no console window), writes the `.env`, creates the services, site and project in one change, and opens the folder in the installed editor.
- **Safe failure.** A failed step stops the job and removes only what it created (services, site, project, database and user); the generated folder and the installed runtimes are kept, and the error says so. A project name may use letters, digits, spaces and `. _ - ( )`; it becomes the folder and host name (`My Shop` is `my-shop`).
- **Schema additions for the list:** a step may set `env`; a `download` step may be `unverified` with a `reason` (shown as a warning; start.spring.io publishes no checksum); a program written `./name` is a file of the project (`./mvnw.cmd`); the placeholders `{venv}` and `{home}`.
- **Shared code.** The checks of the service and project routes are in `internal/validate`, the free-port search in `internal/netutil`, the `.env` editor in `internal/envfile`; `dbtools` can drop a user and `sites` exports its domain check.
- **Create a project page (`#/create`).** Choose a language (PHP, JavaScript / TypeScript, Python, Go, Java, Static) and a framework (each card says what it needs: runtime, Composer, database), a name, a folder and, where the framework has one, a database. The plan on the right is always visible and follows every change: what is installed first and why, the steps in order, what is created, and the problems (which stop Create, with a link or a button to fix them) and warnings.
- **Progress, result and failure.** Create shows the steps of the plan as a list (waiting, running with its percent, done, failed) with the generator's output. A finished project shows links to its service, site and project, with **Start the project** and **Create another**. A failed one shows the error (it names the kept folder), says what was removed, and **Back to the form** keeps your choices.
- **Entry points and links.** **New project from a framework** on Projects (a plain button next to **New project**) and on the Dashboard's Projects box. `#/create?framework=laravel&name=Shop` prefills the form (also `parent`, `db`, `domain`, `port`). The last folder is remembered in the browser. A **Create a project** tutorial and a Help entry explain it.
- **Mock.** `GET /api/frameworks`, `POST /api/new-project/plan` and `POST /api/new-project` in the dashboard's fake daemon, with the names `taken` (folder in the way) and `fail-me` (the generator fails), and e2e steps for the whole flow.

### Rust

<!-- rust: runtime installed with rustup (GNU), service type, Axum starter -->

### API clients

<!-- apiclients: Bruno and Hoppscotch Desktop, the API clients group -->

### Vite+

<!-- viteplus: vp run modes, detection, Node from Zero2Dev or Vite+ -->

### CPU and memory per service

- Every service shows its **CPU** and **Memory** use: columns on the Services page, a line on each Dashboard card and two entries on the service page. CPU is the share of the whole machine (all processors), Memory is the working set; both cover the service's whole process tree, so a Node dev server with its workers is counted as one. Stopped services show "-".
- The daemon samples every running service every 2 seconds from its job object (CPU time from the job's accounting, memory from the processes in the job) and publishes one `service.usage` event with the numbers of all running services. `GET /api/services` and `GET /api/services/{id}` carry `cpu` and `memBytes`. Nothing is sampled while no service runs, and off Windows the numbers stay 0.

### Project file

<!-- projectfile: export a project, Import project -->

## 0.0.5

### Databases

- **Database tools in the daemon.** `GET /api/services/{id}/database` lists a running MariaDB, MySQL or PostgreSQL server's databases (with sizes) and users. New routes create a database, or a user with every privilege on one database. Zero2Dev runs the server's own client programs (`mariadb`/`mysql`, `psql`, `redis-cli`) from the runtime's folder, with no console window. Every run is logged to the service's log.
- **Import and export dumps as jobs.** An import reads a `.sql` file, or a gzip-compressed one recognised by its first bytes, and can create the database first. Its progress follows the bytes read. An export uses `mariadb-dump --single-transaction --routines --triggers` or `pg_dump --no-owner`, gzips when the name ends in `.gz`, and writes through `<file>.tmp`, so a failed export never leaves half a file. An existing file is kept unless `overwrite` is set.
- **Reset a database.** While the service is stopped, its data folder is deleted, so the next start sets the database up again, empty. A reset is refused while an import, an export, a snapshot or a migration works on the service.
- **Optional administrator password.** `PUT .../database/password` sets it for every `root` account, for `postgres`, or as Redis' `requirepass`. It then reaches the clean stop (`MYSQL_PWD`, `REDISCLI_AUTH`), `DB_PASSWORD`, the URL-escaped `DATABASE_URL` / `REDIS_URL`, and HeidiSQL and DBeaver. Redis is started with `--requirepass`. The command preview and the start log show the password as `********`. Passwords travel only in environment variables or stdin, never on a client's command line.
- **Native Open and Save as dialogs** (`POST /api/dialog/file`), with file type filters, a suggested name and a start folder, for picking dumps.
- **`dbtools.CopyFromServer`** pipes `mysqldump`/`pg_dump` from another server straight into a Zero2Dev database service. If it fails, it drops what it created. Import uses it.
- **Databases on the service page.** A MariaDB, MySQL or PostgreSQL service page has a **Databases** section: the databases with their sizes and the users, **Create database**, **Create user** (with a generated password and all privileges on one database), **Import a dump** (`.sql` or `.sql.gz`, into an existing or a new database, with a progress bar) and **Export** (a name ending in `.gz` compresses it; an existing file is replaced only after a confirm). **Browse...** opens the Windows Open and Save as dialogs. Tools that need the running server are off while it is stopped, and the page says why.
- **Administrator password.** **Set password**, **Change password** and **Remove password** while the server runs, on Redis too. **Connection** shows the password hidden, with **Show** and **Copy**, and the URL includes it (URL-escaped). The hint under it says what the password changes.
- **Reset a database.** A danger button, only while the service is stopped, behind a confirmation that names the databases and the data folder it deletes. A snapshot is offered first, as before an import.

### Snapshots

- **Snapshots** of a project or a database service: its services' settings, the runtime each one used, its database data folders, its `php.ini` files and its sites, kept in `snapshots\<owner>\<snapshot>\` inside the data folder. With "restart", running services are stopped in the project's stop order and started again afterwards.
- **Restore** puts the settings, data folders, `php.ini` files, sites and the project's members back. It pins each service to the runtime it used and recreates deleted services with their old ids. It refuses while a needed runtime is not installed, and a failed restore puts the data folders back as they were.
- **Clone** a project snapshot into a new project, with new ids, links pointing at the copies, free ports, its own data copies and `-copy` domains.
- **Snapshots** in the dashboard: a **Snapshots** button on each project opens its snapshots page. Take one (with a choice to stop running services and start them again), and see each snapshot's date, size, services, runtime versions and warnings.
- **Restore** shows what changes first: settings, data, and the runtime version each service runs again. A missing runtime gets an **Install** button, and restoring is possible once none is missing.
- **Clone** a project snapshot into a new project, with a preview of the new `-copy` domains and a note that both projects share the code folder.
- A database service page has its own **Snapshots** box.
- **Make default** on a runtime now asks first when services follow that default. It lists the affected projects and takes a snapshot of each one you keep ticked.
- **Take a snapshot first?** is offered before a database import or reset.

### Mailpit

- **Mailpit** catches the e-mails your apps send. Install it from the new **Mail** group on the Runtimes page (Mailpit 1.33.0 built in, newer versions from its GitHub releases), then add a Mailpit service: the web inbox on port 8025, SMTP on 1025, the messages in the service's data folder.
- A Mailpit service page shows **Sending mail**: the SMTP host and port, no login, the inbox link and the `MAIL_MAILER` / `MAIL_HOST` / `MAIL_PORT` lines to copy into `.env`.
- The start is refused when the SMTP port is taken or equals the web port. The health check asks Mailpit's `/livez`.

### Editors

- **VS Code and VSCodium keep your settings however you start them.** Before, only **Open** in Zero2Dev used `data\code` / `data\codium`; double-clicking `Code.exe` in `bin` started without your extensions and settings. Each installed version now has a `data` link next to its exe that points there, which is VS Code's own portable mode. Zero2Dev makes it on install and repairs it at every start (for example after the folder moved). Removing a version deletes the link, never the settings.

### Go tools

- **Go tools on PATH.** Programs that `go install` builds (`air`, `dlv`, `gopls`) go to `data\go\path\bin`. Go services (with "Keep Go's downloads and build cache in the data folder" on, the default), custom services and **Open terminal** get that folder on `PATH` and `GOPATH` set to `data\go\path`, next to `GOROOT`. With the setting off Go keeps its own default (`%USERPROFILE%\go`) and its `bin` folder is on `PATH` instead. The `go` and `gofmt` shims set `GOPATH` too, every tool in the folder gets a shim of its own, and the activate scripts put the folder on `PATH`.

### Import from this PC

- **A calmer Import from this PC page.** Two columns: the programs on the left, the plan of the open one in a side panel (under its row on a narrow screen). Programs and History are tabs. A summary shows what was found, what can come in, what is in and what the backups take. Blocked programs say why in plain words instead of an amber badge, and Copy / Move are two cards that explain themselves.
- **What is worth importing.** Each item is labelled Recommended, Optional or Not worth it, with the reason, and only the recommended items are ticked at first. **Select recommended** picks the programs worth it.
- **Several at once.** Tick programs (or Select recommended), review them together, and they are imported one after another. A failure does not stop the others.
- **Free space.** The plan shows what it needs against the free space of Zero2Dev's drive, warns when it is tight and waits when it is not enough (`targetFreeBytes` on each source).
- **Remove the old files after a copy** (`POST /api/migrations/{id}/cleanup`). The copy is checked first; the migration then counts as a move, so Move back still works. Never for runtimes and databases.
- **Richer history.** Filter by state and program, a Details view with every item (from, to, size), the time it took (`finishedAt`) and the backup size (`backupBytes`), **Delete backups** and **Remove from history**.
- **Import from this PC** page (`#/migrate`): lists what was found on this PC (editors and tools, registered runtimes, caches and global packages, databases on another server) with each item's size and where it goes. Choose **Copy** (keep the originals) or **Move**; Move is greyed out with the reason for runtimes and databases. Blockers such as "Install VS Code in Zero2Dev first" or "Close VS Code first" come with a link or a **Check again** button, and a warning says when Zero2Dev's folder already holds data (it is saved first).
- Nothing starts before a review dialog that says what will happen ("This copies 414 MB ..."). Progress shows while the job runs, and a notice says what was done.
- Databases: a form for the server (type, host, port, user, password), **List databases**, then pick the databases and the Zero2Dev service to copy into, with a check for each field.
- **History** with **Revert**: each way back (discard the copy, copy back, move back) is explained in plain words and confirmed first.
- Entry points: **Adopt into Zero2Dev** for registered runtimes in the Runtimes page's detected list, and a note on the runtime cards when an installed VS Code, VSCodium, Notepad++, HeidiSQL or DBeaver (or a registered runtime) can be imported.

- **Import from this PC (server).** The daemon can copy or move the settings and data of programs installed on this PC into the data folder, and revert it. The routes are under `/api/migrations`.
  - It covers VS Code and VSCodium (settings and extensions), Notepad++, DBeaver's workspace and HeidiSQL's sessions.
  - It covers npm's global packages and cache, Go's module and build caches, and Composer's home.
  - It also covers registered runtimes, which are adopted into `bin\adopted\` and keep their id, and databases of another server.
- **Records and backups.** Every migration is recorded in `data\migrations\<id>\`.
  - Whatever the portable place held is saved first.
  - A move across drives is copied, checked, and only then deleted.
  - Reverting offers discard, copy back or move back. Originals are saved before they are touched.
- **HeidiSQL.** The registry key is written as HeidiSQL's own `portable_settings.txt`. A move removes the key after saving a `.reg` backup of it.
- **Portable caches.** After such a migration, Zero2Dev's Node.js, PHP and Go services can get `npm_config_prefix` and the global folder on `PATH`, `COMPOSER_HOME`, or `GOMODCACHE`/`GOCACHE` in the data folder (`Settings.portable`).

## 0.0.4

- **zero2dev.exe has its icon,** in Explorer, on the taskbar, in Alt+Tab and in a pinned shortcut, and its name and version in Properties and Task Manager ("Zero2Dev"). The exe is DPI aware, so the tray icon is sharp on scaled screens. The resources are in `cmd/zero2dev/rsrc_windows_amd64.syso`, written by `node scripts/winres.mjs` (and by `scripts/bump-version.sh`); `scripts/release-check.sh` fails when it is out of date.
- **A taskbar button while Zero2Dev runs,** with the line Windows draws under running programs, so you can see at a glance that it is running. Clicking it opens the dashboard; **Close window** on it quits Zero2Dev like the tray's **Quit**. Its tooltip shows the same status as the tray icon ("2 services running").
- **Stop all stops things in a safe order.** One request to the daemon (`POST /api/services/stop-all`) stops apps first, then databases, then the web server (Caddy or nginx), so an app never loses its database while it is still running. Services in the same group stop at the same time, and the answer lists what stopped and what did not. Before, the dashboard stopped services one by one in list order, and only while the page was open.
  - The dashboard's **Stop all** uses it and says how many services stopped, or names the ones that did not.
  - The tray menu has **Stop all services** (Zero2Dev keeps running); a balloon reports the result.
  - A service that others depend on (see **Start order**) now stops after them. `?project=<id>` stops only the services of one project.
- **Start all, Stop all, Enable all and Disable all are the daemon's job now.** The buttons call one request each instead of looping in the page.
  - **Services** page: **Start all** and **Stop all** at the top. The dashboard's two buttons use the same requests (`POST /api/services/start-all`, `stop-all`).
  - **Sites** page: **Enable all** and **Disable all** (`POST /api/sites/enable-all`, `disable-all`). All sites change with one config write and **one** restart of Caddy or nginx, not one per site. The answer is `{"changed": n}`.
  - Both start and stop accept `?project=<id>`.
- **Projects.** A named group of services and sites: `#/projects`, and a **Projects** box on the dashboard.
  - **Start** switches the project's sites on, makes sure the site server runs, and starts the services with the ones they depend on. **Stop** stops the project's services only; sites, the site server and other services keep running.
  - **Start order per project.** The project form lists the chosen services under **Start order**, with buttons to move them up and down. Start goes down the list and waits for each service to be running before it starts the next. Stop goes back up, one service at a time. A service that needs one listed below it still starts after it, and the form says so. A service that fails does not hold back the ones after it, unless they need it. The order is the order of `serviceIds`.
  - Create, edit and delete from the page. Deleting a project keeps its services and sites. A deleted service or site leaves its projects by itself.
  - API: `GET/POST /api/projects`, `PUT/DELETE /api/projects/{id}`; event `projects.changed`. Projects are kept in `state.json`.
- **Start order.** A service can **start after** other services, and an app can name its **Database** (Advanced in the service form).
  - `POST /api/services/{id}/start` starts what is not running first and waits for it (about 30 s at most per step of the chain), then starts the service. If a dependency fails, the service is not started and the answer is `409` naming it.
  - **Start all** and a project's **Start** start in waves: the site server, then databases, then apps, each after what it needs. The dependents of a service that fails are reported as failed ("waits for X") and are not started.
  - **Stop all** goes the other way round. Without any dependencies set, the order is the same as before.
  - A dependency loop cannot be saved: the `400` names it (`A -> B -> A`).
  - Services that start with Zero2Dev (**Start automatically**) are not ordered.
- **Restart on crash.** Under Advanced, **Restart if it crashes** starts a service again when it exits by itself or never passes its health check.
  - Delays of 1 s, 2 s, 4 s ... up to 60 s; **Give up after** 1 to 20 attempts (5 by default). The count starts again after the service has run for a minute, or on a manual Start. Stop, Stop all, delete and quitting cancel a pending restart.
  - **Notice on a crash:** a tray balloon on the first crash, one when Zero2Dev gives up, and one for a crash with no restart planned (the retries in between are silent), plus a notice with **See logs** and **Dismiss** on the dashboard and the service page.
  - API: `restartOnCrash` and `restartLimit` on services, `restarts` in the service status, and the event `service.crashed` (`id, name, error, attempt, limit, retryInSec, gaveUp`; `limit` 0 means no restart is planned).
- **A `.env` file for any service type.** Under Advanced, a path relative to the project folder; its `KEY=VALUE` lines are added to the service's environment (comments, `export`, quotes and multi-line values work; no `$VAR` expansion).
  - It replaces the Node-only **.env file** setting (`--env-file`); a Node service that had one is moved over by itself, so no old state is lost.
  - A missing file is a warning, not an error. A path outside the project folder is refused. In **What will run**, values whose names look secret (`PASSWORD`, `TOKEN`, `KEY` ...) are shown as `(hidden)`.
  - **Environment order** (later wins): the runtime on `PATH`, `JAVA_HOME` / `GOROOT` when a Zero2Dev runtime is used, the type's variables, the database variables, the `.env` file, the service's own variables, `PORT`.
- **Database variables.** A service with a **Database** gets `DB_CONNECTION`, `DB_HOST`, `DB_PORT`, `DB_USERNAME`, `DB_USER`, an empty `DB_PASSWORD` and `DATABASE_URL` (Redis: `DB_CONNECTION`, `DB_HOST`, `DB_PORT`, `DB_PASSWORD` and `REDIS_URL`). The databases only listen on `127.0.0.1` and have no password.
- **Open terminal.** A button on the service page opens a console in the service's folder with the runtime versions the service uses on `PATH`, plus `JAVA_HOME` / `GOROOT`, the database variables, the `.env` file and the service's variables: Windows Terminal when `wt.exe` is found, otherwise a Command Prompt window. API: `POST /api/services/{id}/terminal` (`204`; `501` off Windows). The environment hand-over has not been checked in a real window yet.
- **Use the runtimes from your own terminals.** `data\shims\` holds `node`, `npm`, `npx`, `php`, `java`, `javac`, `jar`, `go`, `gofmt` and the database client tools (`mysql`, `psql`, `redis-cli` ...) for the default runtime of each kind, plus `activate.cmd`, `activate.ps1` and `activate.sh`. They are rewritten on every runtime change, **Make default** included. **Settings > Command line** shows the folder and the lines to run in one terminal.
  - **A new Environment page, item by item, nothing on by default.** Instead of one all-or-nothing switch, every folder and variable has its own switch: the shims folder, the folder of **one particular runtime version** (only Node 26.13.1, only Java 17 ...), a folder of your own, `JAVA_HOME`, `GOROOT`, and any variable you add (the default version of a runtime, one version you pick, or a value you type). **Quick start** sets up the default versions in one click. The list order is the PATH order (up/down arrows), each folder says which commands it gives and which ones a folder above already wins, and each row has one plain status line with a way out of problems (*No Java runtime is installed*, **Install Java**). A review bar lists exactly what **Apply** will change, the page then says what was done and which command to try in a new terminal, and **Remove everything** undoes it all. It is the one exception to "nothing is written to the registry": it edits only your own user environment (`HKCU\Environment`, no administrator rights), the registry type and the rest of `Path` stay as they were, and the old `Path` is saved in `data\shims\.registry-backup.json`. API: `GET` and `PUT /api/environment`; the `registerPath` setting is gone.
  - **Reversible and careful:** a variable you already have is never overwritten, and a folder you already had on `Path` is never removed. Switching an item off removes only what Zero2Dev added. A `default` variable follows **Make default**; deleting a runtime takes its items away. If Windows refuses a change, `PUT /api/environment` answers `409` with the reason and nothing is saved.
  - Known limits: the registry code is tested against a fake registry, and a real Windows check is still to do. The `.cmd` shims read the OEM code page, so a data folder with non-ASCII characters in its path can break them. PowerShell's default execution policy can block `activate.ps1`.
- **Node.js 26 LTS** (26.13.1, "Lithium") replaces Node 22 in the built-in catalog, so **Install** offers it from the start. Zero2Dev's own build uses it too: CI runs Node 26, `@types/node` is 26, and the README names Node 26 as the version the web UI is built with (vite-plus needs 22.18, 24.11 or 26 and newer).
- Fixed: a test that expected "no PHP installed" failed on machines that have PHP (such as the GitHub runners); it now runs with an empty PATH.
- Version 0.0.4. The plan for this release is in `docs/ROADMAP.md`.

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
