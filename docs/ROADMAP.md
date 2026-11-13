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
| 0.0.4 | **Projects**: one click starts the whole stack | planned |
| 0.0.5 | **Data**: databases you can create, fill and reset; snapshots of a whole environment | planned |
| 0.0.6 | **More languages** and **one-click new projects** for each language and framework | planned |
| 0.0.7 | **WSL aware**: run, detect and open projects that live in WSL | planned |
| 0.0.8 | **Real domains**: ports 80/443, `.test` domains, sharing | planned |
| Later | Updates, signing, other systems, command line | planned |

## What is done

Checked against the code in this repository.

| Area | What a developer gets | Status |
| --- | --- | --- |
| Portable | Everything in `data\` and `bin\` next to the program; no AppData, registry or certificate store changes | done |
| Runtimes | Install Node, Go, Caddy, nginx, PHP and Java; detect what is on PATH; register a program by path | done |
| Versions | Many versions side by side, a default per runtime, a version per service, **Check for new versions** | done |
| Downloads | Verified against the publisher's SHA-256, SHA-512 or MD5; download proxy; **Import file** when downloads are blocked | done |
| Services | Create, edit, start, stop, restart; port-conflict checks; health checks; start with Zero2Dev | done |
| Process safety | Children stop with the daemon (Job Object on Windows, process groups elsewhere); no shell strings run | done |
| Logs | Live and stored logs per service, filter, follow, copy, clear; installer log | done |
| Settings per type | The form asks only what the type uses, with defaults, explanations and a live **What will run** command line | done |
| Node | Script or npm script (read from `package.json`), `--watch`, `--env-file`, debugger, npm cache in `data\` | done |
| PHP | Per-service `php.ini`, extensions as tick boxes, limits, one-click **Xdebug** matching the PHP version | done |
| Go, Java | Go caches in `data\go`; JAR or main class, heap, Spring Boot port, debugger | done |
| Databases | MariaDB, MySQL, PostgreSQL, Redis as services: created on first start, local only, clean stop, connection URL | done |
| Editors and tools | VS Code, VSCodium, Notepad++, HeidiSQL, DBeaver; **Open in** a project folder or a database | done |
| Sites | `*.localhost` for a folder, a port or a service, served by Caddy or nginx; optional HTTPS with a local CA | done |
| Security | Loopback only, bearer token, Host and Origin checks, single-use event tickets, Caddy admin API off | done |
| Running it | No console window: a double-click opens the dashboard; **tray icon** (open, data folder, quit); a second start opens the running one; **Quit** in the UI stops services cleanly; messages in `data\logs\zero2dev.log` | done |
| Dashboard | Status line, clickable tiles, live service cards (uptime, latest output, Start/Stop/Restart, Start all/Stop all), sites with on/off switches, recent activity | done |
| Help | Six **guided tutorials** that take you to the right page, highlight what to press with the rest dimmed, and move on when the step really happened; concepts and common fixes | done |
| Appearance | The **Ant Design** look (its tokens, sizes and palettes, in Svelte); **System / Light / Dark**; an **accent colour** from 8 presets or any colour, with its palette generated and text kept readable (WCAG AA); `DESIGN.md` describes it all | done |

## 0.0.4: projects, one click for the whole stack

Today a developer starts the database, then the API, then the front end, then the site server, one at a time, and finds out
from a crash that the order was wrong. A project groups all of that.

The tray icon this needs for crash notices shipped in 0.0.3.

| Stage | What | Why it helps | Status |
| --- | --- | --- | --- |
| 1 | **Projects**: a named group of services and sites with **Start all** / **Stop all** | One click instead of five | planned |
| 2 | **Stop all, done by the daemon**: one request (`POST /api/services/stop-all`, optionally for one project) that stops apps first, then databases, then the site server, services in the same group at the same time, and reports what did not stop | Free the machine in one click, from anywhere, without damaging data | planned |
| 3 | **Stop all services** in the **tray menu** (Zero2Dev keeps running), next to Quit | Stop everything before a meeting or a game without opening the dashboard | planned |
| 4 | **Start order**: a service can wait for another (the API waits until the database is ready); Stop all uses the same order in reverse | No more "connection refused" on the first start, and no app errors while its database stops | planned |
| 5 | **Restart on crash** with a growing delay and a limit, chosen per service | A watcher or dev server that dies comes back by itself | planned |
| 6 | **Notify on crash**: a tray balloon and a dashboard badge when a service stops unexpectedly | The developer learns about it before the browser shows an error | planned |
| 7 | **`.env` file for any service type** (today only Node has `--env-file`) | Use the project's existing `.env` instead of retyping variables | planned |
| 8 | **Open terminal here**: a terminal in the project folder with the service's runtime versions on PATH | `php`, `node`, `composer` in the terminal match what the service uses | planned |
| 9 | **Database connection variables**: link a service to a database service and get `DATABASE_URL` (and `DB_HOST`, `DB_PORT`...) set for it | No copying ports and user names between pages | planned |

**Stop all today** (0.0.3) is a dashboard button: the browser stops the running services one at a time, in list order, so it
only works while the dashboard is open, and a database can stop before the app that uses it. Stages 2 to 4 replace that loop
with the daemon's own Stop all; the dashboard button, the Services page and the tray then all call it.

## 0.0.5: data, databases and environment snapshots

Databases run today, but creating a schema, loading a dump or starting over still needs a separate tool. And trying a risky
change (a migration, a PHP upgrade, a new extension) has no undo for the environment as a whole.

| Stage | What | Why it helps | Status |
| --- | --- | --- | --- |
| 1 | **Create a database and a user** from the service page | The first step of every new project, without SQL | planned |
| 2 | **Import a dump** (`.sql`, `.sql.gz`) and **export** one (`mysqldump`, `pg_dump`) | Load a copy of production or a colleague's data in one step | planned |
| 3 | **Database snapshots**: copy one database's data folder before a risky change, restore it later | An easy undo for migrations | planned |
| 4 | **Environment snapshots**, one set per project: a named copy of everything the project's environment is made of (its services and their settings, the runtime version each one uses, generated `php.ini` files, sites, and the data of its databases), taken with the services stopped | "Before the PHP 8.4 upgrade": try it, and go back in one click if it breaks | planned |
| 5 | **Restore** a snapshot, or **clone** it into a new environment (for example a second copy of the project to test a branch) | Several states of the same project without setting them up again | planned |
| 6 | **Snapshot before risky actions**, offered automatically before switching a runtime version, resetting a database or importing a dump | The undo exists even when nobody thought of taking one | planned |
| 7 | **Reset a database** (delete its files and create them again) | Start clean without hunting for the data folder | planned |
| 8 | **Optional password** for the administrator account | For apps and frameworks that refuse an empty password | planned |
| 9 | **Mail catcher** (Mailpit) as a service type, with its SMTP settings shown | Test e-mails without sending real ones | planned |

Snapshots live in `data\snapshots\<project id>\<snapshot>` and move with the folder like everything else. Runtimes are not
copied: a snapshot records which installed version each service used, and restoring it offers to install a missing one.

## 0.0.6: more languages and one-click new projects

Starting a new project today means installing the runtime, running the framework's own generator in a terminal, creating a
database, then filling in a service and a site by hand. **New project** does all of it from one form.

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

Frameworks in the first list, each with the generator Zero2Dev runs:

| Language | Frameworks | Generator |
| --- | --- | --- |
| PHP | Laravel, Symfony, WordPress, plain PHP | `composer create-project`, WordPress download (SHA-1 listed by wordpress.org) |
| JavaScript / TypeScript | Express, NestJS, Next.js, Vite (React, Vue, Svelte), Astro | `npm create ...`, `npx create-next-app` |
| Python | Django, FastAPI, Flask | `django-admin startproject`, a starter folder for FastAPI and Flask, in a virtual environment |
| Go | net/http, Gin, Fiber | `go mod init` and a starter `main.go` |
| Java | Spring Boot | a project from start.spring.io (Maven or Gradle) |
| Static | HTML and CSS | a starter folder served by the site server |

## 0.0.7: WSL aware

Many Windows developers keep their code in WSL. Today Zero2Dev only sees Windows programs and Windows folders.

| Stage | What | Why it helps | Status |
| --- | --- | --- | --- |
| 1 | **Find WSL**: list the installed distributions and whether each is running | Zero2Dev knows WSL is there without any setup | planned |
| 2 | **WSL folders**: accept `\\wsl$\Ubuntu\home\me\app` and `/home/me/app`, and convert between them | Pick a project folder inside WSL from the same folder picker | planned |
| 3 | **Run a service inside WSL** (`wsl.exe -d <distro> --cd <folder> -- <command>`), with its logs, health check and clean stop like any other service | A Node or PHP app in WSL is started and stopped from Zero2Dev | planned |
| 4 | **Runtimes inside WSL**: detect `node`, `php`, `go`, `python` and databases installed in a distribution, and install Linux builds there from the catalog | Use the Linux versions of the tools without leaving Zero2Dev | planned |
| 5 | **Ports and sites**: reach a WSL service on `127.0.0.1` (WSL forwards localhost) and point a `*.localhost` site at it | `shop.localhost` works the same whether the app runs in Windows or WSL | planned |
| 6 | **Open in** VS Code or VSCodium connected to WSL, and a terminal in the distribution | Edit and run commands where the code lives | planned |
| 7 | **Warnings that save time**: a WSL project kept on `/mnt/c` (slow file access), or a Windows project run inside WSL | Points at the setup mistakes that make WSL feel slow | planned |
| 8 | **New project** and **snapshots** work for WSL projects too | The same one-click creation and undo inside WSL | planned |

Zero2Dev stays a Windows program: it never installs WSL, never changes a distribution's configuration, and only runs what
the user set up as a service.

## 0.0.8: real domains and sharing

| Stage | What | Why it helps | Status |
| --- | --- | --- | --- |
| 1 | **Ports 80 and 443** for sites (optional, asks for permission once) | `http://shop.localhost` without `:8080` | planned |
| 2 | **Hosts-file domains** such as `shop.test` (optional, with a clear undo) | For apps that need a real-looking domain or cookies across subdomains | planned |
| 3 | **Share a site** for a few hours through a tunnel (for example cloudflared) | Show work to a client or test a webhook without deploying | planned |

## Later

| What | Why it helps | Status |
| --- | --- | --- |
| **Project file** (`zero2dev.json` in the repository) that creates the project's services, sites and databases | A new team member clones the repository and clicks **Import project** | planned |
| **Command line**: `zero2dev start <project>`, `zero2dev stop --all`, `zero2dev logs <service>` | Scripts and editors can drive Zero2Dev | planned |
| **CPU and memory per service** on the dashboard | Find the service that slows the machine down | planned |
| **Auto-update** of Zero2Dev itself, verified | No manual download for each release | planned |
| **Code signing** | Fewer antivirus and SmartScreen warnings | planned |
| **Linux and macOS** at the same level as Windows (today the catalog has Linux builds of Node, Go and Caddy only, and Java from Adoptium; no tray) | The same tool for the whole team | planned |

---

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
