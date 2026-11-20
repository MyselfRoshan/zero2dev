# Using Zero2Dev

A step-by-step guide, up to date for 0.0.4. Zero2Dev is one program: start it, and the control panel opens in your browser at `http://127.0.0.1:7000`.

> The screenshots were taken on a Linux test machine, so paths look like `/tmp/...`. On Windows you will see paths such as `C:\work\my-app`. Everything else is identical.

Contents: [1. Start](#1-start-zero2dev) · [2. Install Caddy](#2-install-a-runtime-caddy-node-go) · [3. Run an app](#3-run-an-app-as-a-service) · [4. Give it a local domain](#4-give-it-a-local-domain-sites) · [Databases and editors](#databases-code-editors-and-database-tools) · [Projects](#projects-start-and-stop-a-whole-stack) · [Start order and crashes](#start-order-and-restart-on-crash) · [Command line](#command-line-use-the-runtimes-in-your-own-terminal) · [5. Logs](#5-watch-the-logs) · [6. Settings](#6-settings) · [7. Portable data](#7-your-data-moving-backing-up-resetting) · [8. Troubleshooting](#8-troubleshooting)

---

## 1. Start Zero2Dev

1. Put `zero2dev.exe` in a folder you own, for example `D:\zero2dev\`. (Download it from the GitHub release, or build it with `build.ps1`.)
2. Double-click it. No window opens: Zero2Dev starts in the background, puts its icon in the notification area (next to the clock) and opens your browser at a link like `http://127.0.0.1:7000/#token=...`. The token is your password; the page saves it and removes it from the address bar.
3. The first run creates a `data` folder next to the exe. Messages go to `data\logs\zero2dev.log` (started from PowerShell, `.\zero2dev.exe` prints them in the terminal instead).

**Getting back to it and stopping it.**

| To | Do |
| --- | --- |
| Open the dashboard again | Click the Zero2Dev icon in the notification area, or double-click `zero2dev.exe` again (a second start just opens the dashboard of the one already running) |
| Open the data folder | Right-click the icon > **Open data folder** |
| Stop every service but keep Zero2Dev open | Right-click the icon > **Stop all services**, or **Stop all** on the dashboard or the Services page. Apps stop first, then databases, then the web server (and a service that others wait for stops after them); a message says how many stopped |
| Quit | Right-click the icon > **Quit Zero2Dev**, or **Quit Zero2Dev** at the bottom of the sidebar. Your running services are stopped cleanly first (the dialog lists them) |

If Zero2Dev cannot start (another program uses its port, or the data folder is damaged), a message box says why.

**If your browser shows this page instead**, it has no token yet. Open the link again from the tray icon, or paste the token from `data\token`:

![First run page asking for the access token](screenshots/01-first-run.png)

You land on the **Dashboard**:
- **On a fresh install:** a welcome card with a one-minute tour, and the **Get started** checklist. Each step has **Show me**, which starts the matching tutorial.
- **Later, the state of everything at a glance:**
  - A status line, such as "2 of 3 services running · sites served by Caddy (running)".
  - Four tiles: Services, Sites, Runtimes and Databases. Each opens its page, already filtered.
  - A live card per service, with its uptime, latest output line, Start, Stop and Restart, and links to its logs and address. **Start all** and **Stop all** are above the cards: the program does the ordering (databases first, and a service after the ones it starts after).
  - A yellow notice for each service that crashed, until you start it again or dismiss it (see [Restart on crash](#start-order-and-restart-on-crash)).
  - **Projects**, when you have made some: one **Start** and **Stop** per project (see [Projects](#projects-start-and-stop-a-whole-stack)).
  - Your sites, each with an on/off switch.
  - **Recent activity**: the latest output across all services.

![Empty dashboard with Create a service, Install a runtime and Create a site](screenshots/02-dashboard-empty.png)

The left menu has **Dashboard**, **Services**, **Projects**, **Runtimes**, **Sites**, **Logs**, **Settings** and **Help**.

**Help** has interactive tutorials:
- The tutorials are **Install a runtime**, **Create a service**, **Give it a local domain**, **Add a database**, **Open a project in your editor** and **A quick look around**.
- Starting one opens a small guide next to what to press, with an arrow pointing at it and the rest of the page dimmed (you can still click anywhere). It takes you to the right page and moves on by itself when it sees the step happen (the runtime finished installing, the service runs).
- **Back**, **Skip** and the × always work. A reload continues where you were.

The Help page also explains how runtimes, services, sites and the data folder fit together, and has fixes for the usual problems. The green *Connected* box at the bottom left means the page is talking to the program.

---

## 2. Install a runtime (Caddy, Node, Go)

A *runtime* is a program Zero2Dev can run for you. **Caddy** is the web server that gives your apps local domains; Node and Go run your code.

1. Open **Runtimes**. **List** and **Grid** (top right) switch between rows and tiles; the choice is remembered. In the grid, a tile's **Manage** or **Install…** opens its details in a dialog. In the list, every runtime is one row: its name, what it is, the versions you have (the default is marked *default*) and one button. Use the filters at the top (**All**, **Installed**, **Languages**, **Web servers**, **Databases**, **Code editors**, **Database tools**) to shorten the list.
2. Press **Install…** on the **Caddy** row. The row opens with the newest version already picked and a line on how the download is checked; press **Install**.
3. Zero2Dev downloads it, checks the download against the published checksum, and unpacks it into the `bin` folder next to `zero2dev.exe`. While it works, the row shows the progress, even when you close it. When it finishes, the version appears in the row and a **Caddy** service is created for you.

![Runtimes page: one row per runtime, grouped, with filters on top](screenshots/03-runtimes.png)

Other ways to get a runtime:

| Button | Use it when |
| --- | --- |
| **Detect on this PC** | A program is already installed. Zero2Dev lists what it finds on your PATH; press **Register** on a line, or **Register all**. Registering only remembers the path. |
| **Register by path** | The program lives somewhere unusual, e.g. `C:\php\php.exe`. |
| **Import it** (in an open row, under the version list) | Downloads are blocked on your network. Save the archive in your browser, then import it. Zero2Dev still verifies it. |

Nothing is installed system-wide: runtimes stay in the `bin` folder next to `zero2dev.exe`, and you can delete one by removing it from the list.

### Install runtimes and keep several versions side by side

Every runtime has its own row on the **Runtimes** page; **Manage** opens it. You can install **as many versions as you like at the same time**, for example PHP 7.1 for an old project and PHP 8.4 for a new one.

1. Open **Runtimes**. Press **Check for new versions** to ask the publishers (nodejs.org, go.dev, GitHub for Caddy, windows.php.net, Adoptium, nginx.org) which versions exist. It tells you how many new ones it found. A site that cannot be reached is named in a notice; that runtime keeps the versions known before, and the others still work. The page asks once automatically the first time.
2. In an open row, pick a version from the list and press **Install**. Tags: *latest* (newest of that version line), *LTS* / *stable* / *mainline*, *installed*, *unverified*.

   ![Runtimes page with one card per runtime, each with a version list](screenshots/12-runtimes-versions.png)

3. **Some downloads cannot be verified.** nginx.org publishes no checksum, and some old PHP releases (7.x) have none either. Zero2Dev tells you and asks you to confirm. It still downloads over HTTPS from the publisher only, and writes the SHA-256 it computed to `data\logs\installer.log`. If you have a SHA-256 you trust, use **Import file** and paste it instead; then it is verified like everything else.

   ![Confirmation: nginx publishes no checksum, install anyway?](screenshots/13-nginx-unverified-confirm.png)

   ![Confirmation for an old PHP line without a published checksum](screenshots/14-php-old-unverified.png)

4. Install a second version the same way. Both appear in the row. A newer version of a line you have shows as **Update** in the row, with **Select** to pick it. The first version you install becomes the **Default**; press **Make default** on another one to change it. **Remove** deletes one version only; if you remove the default, another installed version becomes the default.

   ![PHP card with two versions installed](screenshots/15-php-two-versions.png)

   ![After Make default: PHP 8.4.1 is the default](screenshots/16-php-default-changed.png)

5. **Choose a version per project.** In a service's form, the **Version** list shows every installed version of that type. Leave it on *Default (...)* to follow the default, or pick e.g. *PHP 7.1.33* for that service only. Another service can use PHP 8.4.1 at the same time.

Good to know:
- **Removing a version in use** is refused while a service picked that exact version; the message names the service.
- **Windows only** for nginx and PHP (the publishers provide no Linux builds). Java (Eclipse Temurin) is available on Windows and Linux.
- **PHP needs the Microsoft Visual C++ Redistributable** (x64) installed on Windows. If `php.exe` will not start, install it from Microsoft.
- **nginx settings live inside the nginx folder** (`bin\nginx-<version>\conf`). Every nginx version has its own folder, so keep your own configuration in a folder of your own and pass it to the service with `-p` and `-c` arguments.
- Runtimes you added with **Register by path** are never touched by installs or removals (Forget only removes them from the list).

---

## 3. Run an app as a service

A *service* is a command Zero2Dev starts, stops and watches for you, with its output in one place.

1. Open **Services** and click **New service**. The editor opens as its own page.
2. **What do you want to run?** Pick a card: Node.js, PHP, Go, Java, a database (MariaDB, MySQL, PostgreSQL, Redis), or *Custom command* for any other program. Each card says in one line what the type is. Caddy and nginx are not offered: Zero2Dev creates their services itself when you install them. The type then shows at the top of the form with a **Change type** button.
3. Fill in the form. It only asks for what the chosen type uses:

   | Part | What to enter |
   | --- | --- |
   | **Name** | Anything, e.g. `My API` |
   | **Project folder** | Your app's **code**, e.g. `C:\work\my-api`. Zero2Dev starts the program in this folder; it never stores its own data there. Click **Browse...** to pick it in the Windows folder dialog, or type the path. Databases have no project folder: their form shows the **data folder** Zero2Dev keeps their files in instead. For *Custom command* it is called **Working folder** |
   | **Program** | Only for *Custom command*: the program to run, e.g. `python` |
   | **Port** | The port your app listens on (your app also gets it as `PORT`). Databases and PHP start with their usual port |
   | **Version** | Which installed version to use. Leave on *Default (...)* to follow the default of that type |
   | **Start automatically when Zero2Dev starts** | A switch |
   | **Settings** | The few settings most people change for this type, already set to values that work. **More settings (n)** shows the rest, grouped. Each setting says what it does and ends with what it writes (e.g. `--max-connections`, `php.ini memory_limit`). A changed setting is marked **Edited** with a **Reset** link, and it always stays visible, even when its group is folded. See [Settings by type](#settings-by-type) |
   | **Advanced** | Rarely needed: **arguments for your app** (added after the generated ones), a **Health check path** such as `/health` where the type has one, **Database** and **Starts after** (see [Start order](#start-order-and-restart-on-crash)), **Restart if it crashes**, an **.env file** (see [below](#the-env-file-and-database-variables)), **Environment variables** (yours win over the settings), and **Write my own command line** |

   Beside the form, **What will run** stays in view while you scroll:
   - the exact command line (with **Copy**);
   - the variables Zero2Dev adds;
   - the files it writes before each start (PHP's php.ini);
   - what is still missing ("Fill in Script to complete the command line") or wrong ("No MariaDB is installed yet");
   - how many settings differ from the defaults, with **Reset all**;
   - **Always added**: the arguments Zero2Dev always adds, each with the reason (e.g. `--bind-address=127.0.0.1`: only this PC can connect).

   On a narrow window the panel moves below the form, and **Create service** stays pinned at the bottom.

   ![Service editor for a PHP site: settings with extension chips, and the What will run panel beside them](screenshots/04-service-form.png)

4. Click **Create service**. It appears in the list, stopped:

   ![Services list with one stopped service](screenshots/05-services-list.png)

5. Click **Start**. The state changes to *starting*, then *running* once the port answers. While it runs you get **Stop** and **Restart**. Stopping a service stops the whole process tree. The **Services** page also has **Start all** and **Stop all** at the top: **Start all** starts every service that is not running, databases first and each service after the ones it starts after; **Stop all** stops everything that runs, in the opposite order, and Zero2Dev keeps running.

Typical examples:

- **Node app**: Type *Node.js*, Script `server.js` (or Run: *An npm script*, `dev`), Project folder = your project.
- **PHP**: Type *PHP*, Project folder = your site, Document root `public` for Laravel or Symfony. Tick the extensions you need.
- **Go app**: Type *Go*. Package `.` runs `go run .` in your project; `./cmd/api` runs a subfolder.
- **A database**: Type *MariaDB*, *MySQL*, *PostgreSQL* or *Redis*. Nothing else is needed.
- **Anything else**: Type *Custom command*, Program = the program, Arguments = its options.

Click a service's name to open its page: **Edit** opens the same editor, and Cancel or Save brings you back. The page shows the exact command it runs (e.g. `node.exe server.js --port 3000`), folder, process id, start time, address and live output.

![Service page showing running state, port link and live log lines](screenshots/06-service-running-logs.png)

**Open terminal.** The service page has an **Open terminal** button. It opens a console in the service's folder with the same environment the service starts with: the runtime versions the service uses are first on `PATH` (so `node -v`, `php -v` or `java -version` there match the service), `JAVA_HOME` or `GOROOT` is set, and so are the database variables, the `.env` file and the variables you gave the service. It opens Windows Terminal when it is installed, and a Command Prompt window otherwise. The console is yours: it is not stopped with the service and stays open when Zero2Dev quits. (Windows only; other systems answer that opening a terminal is not available. The hand-over of the environment to the console has not been checked in a real window yet.)

### The .env file and database variables

Under **Advanced**, **.env file** takes a path relative to the project folder, for example `.env` or `config/app.env`. It works for every service type (before 0.0.4 only Node had it, as `--env-file`; a Node service that used it keeps working, its value moved here).

- Its `KEY=VALUE` lines become environment variables of the service. Comments (`#`), `export KEY=VALUE`, quoted values and multi-line quoted values work; `$OTHER` is not expanded.
- **Your own variables** in the form win over the file, and the port wins over both (the service always gets `PORT`).
- A file that does not exist is only a note in the start log and the **What will run** panel; the service still starts. A path that leaves the project folder (`..` or a drive letter) is refused.
- In **What will run**, the value of a variable whose name looks secret (`PASSWORD`, `SECRET`, `TOKEN`, `KEY` ...) is shown as `(hidden)`. The service still gets the real value.

Pick a **Database** (also under Advanced) and the service gets its connection as variables, so you do not copy ports between pages: `DB_CONNECTION`, `DB_HOST` (`127.0.0.1`), `DB_PORT`, `DB_USERNAME` and `DB_USER` (`root`, or `postgres`), an empty `DB_PASSWORD` and `DATABASE_URL`, for example `postgresql://postgres@127.0.0.1:5432/postgres`. For Redis you get `DB_CONNECTION`, `DB_HOST`, `DB_PORT`, `DB_PASSWORD` and `REDIS_URL`. The order, later one wins: the runtime's `PATH`, `JAVA_HOME`/`GOROOT`, the type's own variables, the database variables, the `.env` file, your own variables, `PORT`. Choosing a database also makes the service start after it (see below).

---

### Settings by type

| Type | Settings (defaults in brackets) | Kept in the data folder |
| --- | --- | --- |
| Node.js | Run a **script** or an **npm script** (names from your package.json are suggested); **Restart when a file changes** `--watch` (Node 18.11+); **debugger** `--inspect` [9229]; **NODE_ENV** [development] | npm's download cache: `data\node\npm-cache` |
| PHP | **Document root**; **Router script**; php.ini **start from** development or production; **Extensions** (tick boxes for everything in that PHP's `ext` folder) [curl, fileinfo, mbstring, openssl, mysqli, pdo_mysql, pdo_sqlite, sqlite3, zip, intl]; **memory_limit** [256M], **upload_max_filesize** / **post_max_size** [64M], **max_execution_time** [60], **display_errors** [on], **date.timezone** [UTC]; **More php.ini lines**; **Xdebug** mode, start and editor port [9003] | The service's own `php.ini`, sessions, uploads and Xdebug output: `data\php\<service id>` |
| Go | **Package** [.]; **Build tags**; **Keep Go's downloads and build cache in the data folder** [on] | `data\go` (GOPATH, GOCACHE, GOENV) |
| Java | Run a **JAR** or a **main class** with a class path; **Max memory** `-Xmx`; **Pass the port as server.port** (Spring Boot); **debugger** [5005] | none |
| MariaDB, MySQL | **Character set** [utf8mb4], **Collation**, **SQL mode** (server default, strict, permissive for old apps), **Max connections** [151], **InnoDB buffer pool** [128M], **Max packet size** [64M], **Log every query**, **Log slow queries** [after 2 s] | The database files: `data\mariadb\<service id>`, `data\mysql\...` |
| PostgreSQL | **Max connections** [100], **Shared buffers** [128MB], **Log statements** [none], **Log statements slower than** [off], **Time zone** | `data\postgres\<service id>` |
| Redis | **Keep the data**: snapshots (Redis default), after every write, or not at all; **Memory limit** and what to do **when the limit is reached**; **Databases** [16] | `data\redis\<service id>` |

**PHP extensions and Xdebug.** The loaded extensions show as chips: remove one with its ×, add one by typing its name in **Add an extension**, or open the full tick-box list with **Show all**. The list is read from the selected PHP version's `ext` folder, so it always matches that version. Zero2Dev writes the service's own php.ini on every start (in its data folder) from the version's `php.ini-development` or `php.ini-production` plus your settings; PHP's own folder is never changed. The **Xdebug** row below the extensions shows whether that version has it. When it has none, **Download Xdebug** fetches the matching build from xdebug.org, checks it against the SHA-256 xdebug.org lists, and puts it into that version's `ext` folder (it is program code, so it goes to `bin`). It is loaded right after the download (the row then has a **Load it** switch), and its settings appear. Xdebug downloads are for PHP 8.0 and newer.

**Your own command line.** Under **Advanced**, **Write my own command line** turns the settings off and lets you write every argument yourself, starting from the generated ones. Services created before 0.0.3 that had arguments open this way and keep running exactly as before; their page offers **use the settings instead**, which shows the new command line before switching.

---

## 4. Give it a local domain (Sites)

A *site* maps a name like `demo.localhost` to a folder (a static website) or to a running service. Caddy does the work, so **install Caddy first** (step 2).

1. Open **Sites** and click **New site**.
2. Fill in:
   - **Name**: a label for you. The domain is filled in from it (`My Shop` becomes `my-shop.localhost`) until you edit the domain yourself.
   - **Domain**: lowercase, ending in `.localhost`, e.g. `demo.localhost`.
   - **Points to**: *A service* (one of your services; the site follows its port), *A port* (an app you start yourself), or *A folder* (static files served as they are).
   - **Use HTTPS instead of HTTP** if you want `https://` (see [Settings](#6-settings)).

   ![New site form: name, domain, type static, folder to serve](screenshots/07-site-form.png)

3. Click **Create site**. It is listed as *enabled* with its address:

   ![Sites list showing demo.localhost on port 8080](screenshots/08-sites-list.png)

4. Go to **Services** and **Start** the **Caddy** service (created when you installed Caddy).
5. Open `http://demo.localhost:8080` in your browser (HTTPS sites use port `8443`).

Browsers resolve `*.localhost` to your own computer, so no hosts-file edit and no administrator rights are needed. Edits to sites are applied to Caddy automatically.

To take a site offline without deleting it, click **Disable** in its row; **Enable** brings it back. It works like Stop and Start for services. **Enable all** and **Disable all** at the top of the page switch every site at once; the site server restarts once, not once per site.

**Prefer nginx?** Install nginx on the **Runtimes** page, then pick **nginx** under **Settings > Site server** and save. Zero2Dev creates an **Nginx (sites)** service, stops Caddy if it was running and starts nginx in its place. Sites, domains and ports stay the same. nginx only listens on `127.0.0.1`.

---

## Databases, code editors and database tools

### A database

1. On **Runtimes**, install a database from the **Databases** group: **MariaDB**, **MySQL**, **PostgreSQL** or **Redis**.
   - MariaDB, Redis and the editors are checked against a SHA-256.
   - MySQL is checked against the MD5 it publishes.
   - PostgreSQL publishes no checksum, so you confirm first, as with nginx.
2. On **Services**, click **New service** and pick the database as the **Type**. The usual port is filled in (3306, 5432 or 6379). Pick a free one if another database already uses it.
3. Click **Start**. The first start creates the database files, which takes a few seconds; the log shows each step.
4. The service page shows **Connection**: host `127.0.0.1`, the port, the user (`root` for MariaDB/MySQL, `postgres` for PostgreSQL) and a URL to copy. There is no password, so the server only accepts connections from this PC.

Good to know:
- **Where the data lives:** `data\<database>\<service id>`, for example `data\postgres\svc_1a2b3c4d`. It moves with the Zero2Dev folder, and deleting the service keeps it, so delete that folder yourself when you no longer need it.
- **Stopping:** **Stop** asks the server to shut down cleanly (`mysqladmin shutdown`, `pg_ctl stop`, `redis-cli shutdown`) and only ends it if it does not stop within 20 seconds.
- **Run Zero2Dev without administrator rights.** PostgreSQL refuses to start from an administrator account.
- **Custom arguments:** if you set your own **Arguments**, Zero2Dev does not create or manage the data folder for you.

### Code editors and database tools

- Install **VS Code**, **VSCodium** or **Notepad++** from **Code editors**. Their settings and extensions go to `data\code` and `data\codium` (Notepad++: `data\notepadpp`), not into `bin`, which only holds the programs.
- Each installed editor or tool has an **Open** button on its card.
- A service page has **Open in VS Code** (one button per installed editor), which opens the project folder.
- A MariaDB, MySQL or PostgreSQL service page has **Open in HeidiSQL** and **Open in DBeaver** when those are installed. They connect straight to the database. HeidiSQL keeps its settings in `data\heidisql`, DBeaver its workspace in `data\dbeaver\workspace`.
- Programs opened this way are not services: they keep running when Zero2Dev stops.

### Working with a database

A MariaDB, MySQL or PostgreSQL service page has a **Databases** section. It lists the databases on the server with their sizes, and the users. Most of its tools work inside the running server, so they are off while it is stopped, and the section says why. **Reset** is the other way round: it works only while the server is stopped. A Redis service page has only the password and reset.

- **Create database:** enter a name of 1-64 letters, digits or `_`. The system databases (`mysql`, `information_schema`, `performance_schema`, `sys`, `postgres`, `template0`, `template1`) are not listed and cannot be used.
- **Create user:** a user name, a password (**Generate** makes a random one) and, optionally, the database the user gets all privileges on. Your app then logs in with this user.
- **Import a dump:** pick the database, or **A new database...** to create one first. Then enter the path of a `.sql` file or a compressed `.sql.gz`, or click **Browse...**. The statements run in that database, with a progress bar. If one fails, the import stops and what came before it stays. The file itself is only read.
- **Export:** pick the database and where to save it (**Browse...** suggests `shop-2026-11-18.sql`). A name ending in `.gz` writes a compressed dump. The file appears only when the export has finished. If it already exists, Zero2Dev asks before replacing it.
- While an import or an export runs, the other tools wait until it has finished. Its progress stays on the page when you leave and come back.

**Administrator password.** The server starts without one (`root`, or `postgres`). **Set password** gives the administrator a password while the server runs; **Change password** and **Remove password** do what they say.
- Zero2Dev keeps the password and uses it to stop the server cleanly. Apps that use the database get it in `DB_PASSWORD` and `DATABASE_URL` (Redis: `REDIS_URL`) from their next start, so restart them. **Open in HeidiSQL** and **Open in DBeaver** use it too.
- **Connection** shows it hidden, with **Show** and **Copy**. The URL includes it once one is set.
- PostgreSQL still lets programs on this PC connect without a password. The password is there for apps and tools that ask for one.

**Reset** deletes the server's data folder: every database, every user and the password. The confirmation names the databases and the folder. The next start sets the server up again, empty and without a password. Stop the service first. Zero2Dev offers to take a snapshot before a reset or an import, so you can go back.

<!-- mailpit: "### Catch e-mails with Mailpit" goes here. -->

---

<!-- snapshot-ui: "## Snapshots: undo for a whole project" goes here (take, restore, clone, the offer before risky actions). -->

<!-- migrate-ui: "## Bring in: use your installed VS Code and other programs in Zero2Dev" goes here (copy or move, revert). -->

## Projects: start and stop a whole stack

A *project* is a named group of services and sites that belong together: for example an API, its database and `shop.localhost`. One **Start** brings the whole set up.

1. Open **Projects** and click **New project** (or **Create your first project**).
2. Give it a **Project name**, tick the **Services** that belong to it and the **Sites** that go with them, and click **Create project**.
3. In the list, each project shows its status (*stopped*, *partly running*, *running*), how many of its services run and how many of its sites are on. Use **Start** and **Stop** in its row, or the same buttons in the **Projects** box on the dashboard.

What the buttons do:
- **Start** switches the project's sites on, makes sure the site server (Caddy or nginx) is running if the project has a site, and starts the project's services in the project's **start order** (below). Services that are already running are left alone. A service starts after the ones it needs: its database and the services it **starts after**, even if they are not in the project (see below).
- **Stop** stops the project's services one at a time, from the bottom of the start order up. The sites, the site server and every other service keep running.

**Start order.** Once a project has two or more services, its form shows them under **Start order**, in the order you ticked them. Use the arrows to move a service up or down.
- Start goes down the list. It waits for each service to be running (or to fail) before it starts the next one.
- A service always starts after the services it needs. If you put an API above its database, the row says *Starts after Postgres, which it needs.*, and the line under the list shows the real order, for example *Start runs Postgres, then API, then Web.*
- A service that fails does not stop the ones after it, unless they need it.
- The result is a message such as "Started 3 services." or the names of the ones that did not start, with the reason.

A project only groups things. **Delete** (the bin icon) removes the group, not its services or sites, and deleting a service or a site just takes it out of the projects it was in. A service can be in several projects.

---

## Start order and restart on crash

### Start order: Database and Starts after

Some services need another one running first: an API needs its database. In the service form, under **Advanced**:

- **Database**: pick one of your database services. It is started first and stopped last, and the service gets its connection as `DATABASE_URL`, `DB_HOST`, `DB_PORT` and the like (see [The .env file and database variables](#the-env-file-and-database-variables)).
- **Starts after**: tick any other services that must be running first (a queue worker that needs the API, for example).

What Zero2Dev does with it:
- **Start**, on a service page or in a row, first starts the services it needs that are not running, one wave at a time, and waits until each is running. The button answers after that, so it can take a few seconds when a database has to start first (up to about 30 seconds for each step of a chain). Then it starts the service.
- **Start all** and **Start** of a project use the same rule, so the database is up before the app, and Caddy or nginx first of all.
- **Stop all** and a project's **Stop** go the other way: apps first, then what they depend on, so an app never loses its database while it still runs.
- If a service that others need fails to start, those others are not started either and are listed as *waits for <name>*.
- Two services cannot wait for each other: saving a loop is refused with a message that names it (`Orders -> Billing -> Orders`).
- Services that start with Zero2Dev (**Start automatically**) are started without this ordering.

### Restart if it crashes

Under **Advanced**, **Restart if it crashes** (off by default) starts a service again when it stops by itself: when it exits for any reason or never answers its health check in time. Stopping it yourself, **Stop all** or quitting never restarts it.

- The delays grow: 1 second, then 2, 4, 8 ... and never more than 60 seconds apart.
- **Give up after (attempts)** is how many restarts in a row it may take, from 1 to 20 (5 by default). After that it stays *failed*.
- The count starts again once the service has stayed up for a minute, or when you start it yourself.
- The service page's log shows each try (`restarting in 2s (attempt 2/5)`) and the end (`giving up: restarted 5 times and it keeps failing`).

You are told when something crashes, whether or not it restarts:
- **Tray balloon**: one when the service first crashes ("*Name* crashed. Zero2Dev is starting it again in 1 second."), one when Zero2Dev gives up ("*Name* keeps crashing"), and one for a service without restart on crash ("*Name* stopped unexpectedly. It was not restarted."). The retries in between are silent. Balloons need the tray icon, so they appear only while it is there.
- **Dashboard and service page**: a yellow notice with the reason, **See logs** and **Dismiss**. It stays until you start the service again by hand or dismiss it.

---

## Command line: use the runtimes in your own terminal

The programs Zero2Dev installs are not on your Windows `PATH`, so `node` in a new terminal is not the Node you installed there. Zero2Dev keeps a folder of small shims, `data\shims\`, to fix that when you want to:

- It holds a `.cmd` for every command of the **default** version of each runtime: `node`, `npm`, `npx`, `php`, `java`, `javac`, `jar`, `go` and `gofmt`, and the client tools of the databases that have them (`mysql`, `mysqldump`, `psql`, `pg_dump`, `redis-cli` ...). Java and Go shims set `JAVA_HOME` and `GOROOT` for their own run.
- It also holds `activate.cmd`, `activate.ps1` and `activate.sh`.
- Zero2Dev rewrites the folder whenever a runtime changes: installed, removed, registered, or **Make default**. Anything you put there yourself is left alone.

**In one terminal, once.** Open **Settings > Command line** and copy the line for your shell:
- Command Prompt: `call "D:\zero2dev\data\shims\activate.cmd"`
- PowerShell: `. 'D:\zero2dev\data\shims\activate.ps1'`
- Git Bash: `. /d/zero2dev/data/shims/activate.sh`

The default runtimes are then first on `PATH` in that terminal, and `JAVA_HOME` and `GOROOT` are set there. Nothing else changes. (If PowerShell says running scripts is disabled, run `Set-ExecutionPolicy -Scope Process Bypass` in that window first.) For the version a particular service uses, use **Open terminal** on its page instead.

**In every terminal (optional): the Environment page.** Open **Environment** in the sidebar. The top line says what Zero2Dev adds to your Windows environment right now. Below it, every folder and variable has its own switch. **Nothing is on by default**, and nothing changes until you apply.

- **Quick start:** **Use the default versions everywhere** turns on *Default versions* (the `shims\` folder: `node`, `npm`, `php`, `java`, `go` ... of the version marked Default), plus `JAVA_HOME` / `GOROOT` when Java or Go is installed.
- **Folders on your PATH:**
  - *Default versions*, the folder of **one particular version** (**Add a runtime version**, for example only Node 26.13.1), or **Add your own folder** (with Browse).
  - Each row says which commands it gives you. A terminal searches the folders **from the top down**, so the higher folder wins. Use the up and down arrows to change the order. A row whose commands are already given by a folder above it says so.
- **Variables:** `JAVA_HOME` and `GOROOT` are suggested, and a one-click chip offers `NODE_HOME` / `PHP_HOME` when those runtimes are installed. **Add a variable** lets you pick where the value comes from, and shows the exact line before you add it:
  - the **default** version of a runtime, which moves along when you **Make default**;
  - **one version**;
  - **your own value**.
- **One plain line per row:** *On your PATH*, *Set*, *Will be added when you apply*, *Will be removed when you apply*, *Off* ... A problem comes with a way out, for example *No Java runtime is installed* with **Install Java**.
- **Review, then apply:** the bar at the bottom lists exactly what will change ("Add C:\tools\bin to your PATH", "Set JAVA_HOME = ..."). Press **Apply N changes**, or **Discard**. Afterwards the page says what was done and which command to try in a **new** terminal (open terminals keep what they had).
- **What it touches:** only your own user environment in the registry (`HKCU\Environment`). No administrator rights, nothing machine-wide. This is the one place where Zero2Dev changes something outside its own folder.
- **Nothing of yours is overwritten:** a variable you already have is never overwritten, and the row says *You already have JAVA_HOME = ...*. A folder you already had on `PATH` stays yours.
- **To undo:**
  - Switch a row off and apply, or use **Remove everything** at the bottom.
  - Only what Zero2Dev added is taken back: its folders leave `PATH`, and its variables are deleted if they still hold what it set.
  - The old `PATH` is saved in `data\shims\.registry-backup.json`, which is also Zero2Dev's record of what it may take back.
  - If Windows refuses the change, you get a message and nothing is saved.
- **Moving the folder:** switch everything off **before** you move or delete the Zero2Dev folder. A `PATH` entry is a full path, so an entry left behind points at a folder that is gone. You can then remove it in Windows' Environment Variables dialog.

Things to know:
- The registry changes are tested against a fake registry only; a check on a real Windows machine is still to be done.
- The `.cmd` shims read the Windows OEM code page, so a data folder whose path has characters outside plain ASCII can break them. Keep Zero2Dev in a plain path such as `D:\zero2dev` if you use them.
- Windows' default PowerShell execution policy can block `activate.ps1` (see above).

---

## 5. Watch the logs

- Each service page has **Live output**: type in **Filter lines** to search, **Pause** to stop scrolling, **Copy** to copy the lines, **Clear** to empty the view.
- The **Logs** page shows output from all services in one stream.
- Logs are also saved as files in `data\logs\`, one per service, so they survive a restart.

![Logs page with output from all services](screenshots/09-logs.png)

---

## 6. Settings

The Settings page is split into sections, listed on the left (tabs on a narrow window): **Appearance**, **Sites**, **HTTPS**, **Downloads**, **Startup**, **Command line**, **This PC** and **Access**. As soon as you change something, an **Unsaved changes** bar appears at the bottom with **Discard** and **Save settings**; leaving the page with unsaved changes asks first.

![Settings page with its sections and the unsaved changes bar](screenshots/10-settings.png)

| Section | Setting | What it does |
| --- | --- | --- |
| Appearance | **Theme**, **Accent colour** | How the dashboard looks; saved at once (see below) |
| Sites | **Site server** | **Caddy** (default) or **nginx** serves your sites. Switching stops the running one and starts the other; both use the same ports |
| | **Domain suffix** | The ending your site domains must use. `localhost` works everywhere with no setup |
| | **HTTP / HTTPS proxy port** | Ports the site server listens on (`8080` / `8443`). The line below shows the resulting address. A running Caddy or nginx restarts for you |
| HTTPS | **Let Caddy trust its local certificate authority** | Caddy only. **Off by default**: nothing outside the Zero2Dev folder changes, and browsers warn about `https://` sites; **How to trust it yourself** shows the certificate file to import. Switched on, a notice explains that Caddy will add its certificate to Windows on save (you may get a security prompt). With nginx, Zero2Dev makes its own CA at `data\nginx\certs\ca.crt` |
| Downloads | **Download proxy** | Used when installing runtimes on a company network, e.g. `http://proxy.company.com:8080`. Zero2Dev does not read the Windows proxy setting |
| Startup | **Open this dashboard when Zero2Dev starts** | A switch |
| Command line | **Shims folder**, the **activate** lines, and **Add Zero2Dev's tools to my Windows user PATH** | Use the default runtimes from your own terminals. The switch is off by default and is the one setting that writes to the registry (your user environment only); see [Command line](#command-line-use-the-runtimes-in-your-own-terminal). It is saved with **Save settings**, and if Windows refuses, a message says why and nothing is saved |
| This PC | | The data folder (with **Copy**), whether the install is portable, the version, the API port and the program folder |
| Access | | **Copy access token**, and **Sign out** of this browser in a separate box |

### Appearance

**Appearance** is the first section of Settings. Changes apply at once and are saved straight away; there is nothing to Save, and the toast that confirms it has **Undo**.

- **Theme**: **System** (follows Windows, and switches when Windows does), **Light** or **Dark**. The sun/moon button in the top bar switches between them from any page.
- **Accent colour**: Blue (the default), Geek blue, Purple, Magenta, Volcano, Orange, Green or Cyan, or **Custom…** for any colour (the colour picker or a `#rrggbb` code). Buttons, links, the selected menu item, switches and progress bars all take the new colour, in light and dark. A preview shows them before you leave the page.
- Very light or bright colours stay readable: Zero2Dev uses a deeper shade or dark text where white text would be hard to read.
- The choice is kept by Zero2Dev itself, so another browser shows the same theme.

The look follows Ant Design; [DESIGN.md](../DESIGN.md) describes it for anyone changing the UI.

---

## 7. Your data: moving, backing up, resetting

Zero2Dev keeps two folders next to `zero2dev.exe`: `data` for your own information and `bin` for the downloaded runtimes.

```
zero2dev.exe
bin\             downloaded programs only, one folder per version (php-8.4.1, node-26.13.1, ...)
data\
  token          your access token
  state.json     your services, runtimes, sites, projects and settings
  caddy\         Caddy's own data (nothing goes to AppData)
  config\        generated Caddyfile and nginx.conf
  nginx\         used by the Nginx (sites) service: logs, temp files, certs (local CA)
  logs\          service logs and installer.log
  shims\         node.cmd, npm.cmd, php.cmd ... and activate.cmd/.ps1/.sh, generated (see Command line)
  mariadb\ mysql\ postgres\ redis\   database files, one folder per service (svc_...)
  code\ codium\  VS Code / VSCodium settings and extensions
  notepadpp\     Notepad++ settings
  heidisql\ dbeaver\   database tool settings and workspace
```

Settings of the editors and database tools never go into `bin`. Only tools Zero2Dev installed use these folders; a program you registered by path keeps its usual settings.

- **Move or copy** the whole folder (exe, `bin` and `data`) to another drive or a USB stick. Stop Zero2Dev first.
- **Back up** by copying `data\` while Zero2Dev is stopped. `bin\` only holds downloads that can be installed again.
- **Reset** by stopping Zero2Dev and deleting `data\` (and `bin\` to remove the runtimes too). If you turned on *Add Zero2Dev's tools to my Windows user PATH*, turn it off first so your user `PATH` is cleaned up.
- **Upgrading from 0.0.1**: runtimes you installed before are moved from `data\runtimes` to `bin` the first time 0.0.2 starts.
- **Different location**: set `ZERO2DEV_HOME` to another data folder (runtimes then go to its `bin` subfolder) and, if you like, `ZERO2DEV_BIN` to another runtimes folder.

Keep `data\token` private: anyone who has it can start programs on your computer through Zero2Dev.

---

## 8. Troubleshooting

| Problem | What to do |
| --- | --- |
| A runtime will not install | **Runtimes** → open the failed job → **Show installer log** (also `data\logs\installer.log`). The message names the step that failed |
| Download blocked or very slow | Set a **Download proxy** in Settings, or use **Import file** with an archive you downloaded yourself |
| Windows or antivirus warns about `zero2dev.exe` | The program is not code-signed yet and it downloads and starts programs, which antivirus heuristics dislike. Check the SHA-256 in the release's checksum file, then allow it |
| *Port already in use* | Another program uses that port. Pick a different port in the service or in Settings |
| `demo.localhost:8080` will not open | Check that the **Caddy** service is *running* (Services page) and read its log |
| HTTPS shows a certificate warning | Expected while *Let Caddy trust its local certificate authority* is off. Trust `data\caddy\data\caddy\pki\authorities\local\root.crt` yourself, or turn the setting on |
| The page asks for a token | Open the full `#token=` link from the terminal, or paste `data\token` |
| Service stays *failed* | Open its page: the last error and the live output say why (wrong folder, missing runtime, command not found). To have a service that keeps dying started again, turn on **Restart if it crashes** under Advanced |
| *Dependency loop* when saving a service | Two services wait for each other (through **Starts after** or **Database**). The message names the loop; remove one link |
| A service did not start because *it waits for* another | The service it starts after failed. Fix that one first (its page says why), then start again |
| `node` in a terminal is the wrong version | Use **Open terminal** on the service's page, or `activate.cmd` / `activate.ps1` from Settings > Command line. Only the Environment page changes every new terminal |
| *Could not change your user PATH* | The registry change did not work (for example the user `Path` has an unusual type). Nothing was saved. The message says why; you can keep using `activate.cmd` instead |

Environment variables: `ZERO2DEV_HOME` (data folder), `ZERO2DEV_PORT` (panel port, default 7000), `ZERO2DEV_NO_BROWSER` (do not open the browser).
