# Zero2Dev

Version 0.0.4. A portable local development control panel: one program that opens in your browser at `http://127.0.0.1:7000`.
Register or install runtimes (Node, Go, Caddy, nginx, PHP and Java), define services (a command, arguments, a folder and a port),
start and stop them with live logs, and map local domains such as `myapp.localhost` to a folder or a service.

No Electron. No Docker. One Go binary with the Svelte UI embedded.

The dashboard follows Ant Design's look, in light or dark, with an accent colour you choose (Settings → Appearance). [DESIGN.md](DESIGN.md) describes it.

**New here? Read the [step-by-step guide with screenshots](docs/USAGE.md).**

## Portable by design

Everything Zero2Dev stores lives in the `data` and `bin` folders **next to the program**. Nothing is written to your user profile,
`AppData`, the registry or the Windows certificate store, with one opt-in exception (below the layout).

```
zero2dev.exe
bin\                 Node, Go, Caddy and anything else installed from the catalog
data\
  token              your access token
  state.json         services, runtimes, sites, projects, settings
  config\            Caddyfile and nginx.conf, generated, do not edit
  caddy\             Caddy's own data and config (kept out of %AppData%)
  nginx\             nginx's logs, temp files and local certificates
  mariadb\ mysql\ postgres\ redis\   database files, one folder per service
  code\ codium\ notepadpp\ heidisql\ dbeaver\   settings and workspaces of installed tools
  cache\             the list of versions found by "Check for new versions"
  shims\             node.cmd, npm.cmd, php.cmd ... and activate scripts for your own terminals, generated
  logs\              one log per service, plus installer.log
  tmp\               work area for downloads, safe to delete when Zero2Dev is stopped
```

- Copy or move the whole folder (to another drive, a USB stick) and it keeps working. Stop Zero2Dev first.
- To reset everything, stop Zero2Dev and delete `data\` (settings, services, sites, database data) and `bin\` (installed runtimes).
- Set `ZERO2DEV_HOME` to use a different data folder; installed runtimes then go to its `bin` subfolder.
- Set `ZERO2DEV_BIN` to keep installed runtimes in a folder of your choice instead.

**The one exception, off by default.** The Environment page lists the folders and variables Zero2Dev can put into your Windows environment, and each one has its own switch:
the `shims\` folder, the folder of one particular runtime version, a folder of your own, `JAVA_HOME`, `GOROOT` and any variable you add. When you apply the ones you switched on,
Zero2Dev edits your own user environment in the registry (`HKCU\Environment`, no administrator rights). It never overwrites a variable you already have, and switching an item off
removes exactly what it added. Without it, `shims\` is only a folder of files, and you can use it in one terminal with `activate.cmd` or `activate.ps1`.

What is _not_ isolated: the services you run are your own programs. A Node app still writes to the normal Windows temp folder, and
runtimes you registered by path stay where they are.

## Quick start (Windows)

Build prerequisites: [Go 1.24+](https://go.dev/dl/), [Node.js 26](https://nodejs.org/) (the version the web UI is built and tested with) and [pnpm 10](https://pnpm.io/installation) (`corepack enable` sets up the pinned version).

```powershell
cd D:\zero2dev
.\build.ps1                 # builds the web UI, then dist\zero2dev.exe
.\dist\zero2dev.exe         # data goes to dist\data, runtimes to dist\bin; no console window: it lives in the tray
```

The first run prints a URL like `http://127.0.0.1:7000/#token=...` and opens it. The page stores the token and removes it from the address bar.
Linux or macOS: `make build`, then `./dist/zero2dev`.

## Building from source

The dashboard is compiled into the executable: `internal/webui` embeds the built UI from `internal/webui/dist`.
That folder is generated and **not in git**, so a fresh clone has to build the UI **before any Go command**
(`go build`, `go run`, `go vet`, `go test`). Otherwise Go stops with:

```
internal\webui\webui.go: pattern all:dist: no matching files found
```

Build the UI once after cloning, and again whenever you change files in `web/` (or pull UI changes):

```powershell
cd web
pnpm install
pnpm build                  # writes ..\internal\webui\dist
cd ..
go build ./...              # now works
```

`build.ps1` and every `make` target (`make build`, `make build-windows`, `make vet`, `make test`, `make smoke`) run this step for you.
`make` rebuilds the UI only when `internal/webui/dist` is missing; run `make web` to force a rebuild after UI changes.
The release workflow builds the UI in CI too, so nothing generated has to be committed.

## Screenshots of every page

`web/e2e/screenshots.mjs` takes a full-height screenshot of every page of the dashboard against the mock daemon, with a small demo project loaded. It shoots light and dark, desktop and phone width, and writes them to `docs/screenshots/pages/`. That folder is in `.gitignore`: the images are made locally and never committed. The script also writes `manifest.json`, which says what each image shows, and a `README.md` that lists every image. Feed the images to a design tool, or compare them before and after a UI change.

```sh
cd web
pnpm run build
NODE_PATH=<folder with playwright>/node_modules pnpm run screenshots
# options: --themes=dark  --sizes=desktop,laptop,mobile  --pages=dashboard,projects
#          --viewport (visible part only)  --out=<dir>
#          --url=http://127.0.0.1:8765 --token=<token> (a running Zero2Dev instead of the mock)
```

Playwright is not a project dependency, the same as for the smoke test. Install it separately, for example `npm install --prefix <folder> playwright@1.64.0`, then run `npx playwright install chromium`.

## Tests in CI and releasing

**Every pull request** and every push to `main` and `develop` runs [`.github/workflows/ci.yml`](.github/workflows/ci.yml):

- the dashboard (format, lint, types, unit tests, build);
- Go on Linux (`gofmt`, `go vet` for Linux and Windows, `go test -race`);
- Go on a real Windows runner (`go vet`, `go test`, the GUI build);
- the Playwright smoke test against the mock daemon.

**Releases come from the `release` branch.** Merging into it publishes the version in `VERSION`, but only after the whole CI run passes again on the merged commit:

1. `scripts/bump-version.sh 0.0.5` sets the version in `VERSION`, `web/package.json`, `cmd/zero2dev/main.go` and the installer, and opens a `## 0.0.5` section in `CHANGELOG.md`. Write that section: it becomes the release notes.
2. Open a pull request into `release`. Besides the tests, CI checks that the version is new and written the same everywhere (`scripts/release-check.sh --require-new`).
3. Merge it. [`.github/workflows/release.yml`](.github/workflows/release.yml) runs the tests, builds `zero2dev_<version>_windows_amd64.zip` / `.exe` with a checksum file and a build attestation, then creates the tag `v<version>` on the merged commit and the GitHub release (marked pre-release while the version is `0.x` or has a `-suffix`).

Merging into `release` without a version bump publishes nothing: the run says the version is already released and stops. To try the build without releasing, start **release** by hand in the Actions tab (a dry run that keeps the files as a workflow artifact).

## What 0.0.4 does

| Area              | Included                                                                                                                                                                                                                                                                                           |
| ----------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Services          | Create, edit, delete; start, stop, restart; port-conflict checks; health probes; live and stored logs; **Start all** and **Stop all**; an **Open terminal** button that uses the service's runtime versions                                                                                        |
| Projects          | Group services and sites that belong together (an API, its database, its address). **Start** switches the project's sites on, makes sure the site server runs and starts the services; **Stop** stops them                                                                                         |
| Start order       | A service can start after others, and an app can name its database. Start waits for them; Stop all and a project's Stop go the other way round. A dependency loop is refused with a message                                                                                                        |
| Crash handling    | Optionally restart a service that exits by itself, after 1 s, 2 s, 4 s ... (at most 60 s), up to a limit you set. A tray balloon and a dashboard notice tell you when it crashed or gave up                                                                                                        |
| Environment       | A `.env` file for any service type (relative to its folder); database variables (`DATABASE_URL`, `DB_HOST`, `DB_PORT` ...) for an app that names its database; the runtime's `JAVA_HOME` or `GOROOT`                                                                                               |
| Service settings  | Each type shows only the fields it uses and its settings, each with its default and what it does (no program docs needed), plus the exact command line; PHP extensions, a php.ini per service and one-click Xdebug                                                                                 |
| Process safety    | Children die with the daemon (Windows Job Object, process groups elsewhere); no shell strings are ever run                                                                                                                                                                                         |
| Runtimes          | Detect what is on your PATH, register a program by path, install Node 26 LTS (and other versions), Go, Caddy, nginx, PHP and Java (many versions side by side, a default per runtime, a version per service, and a button to check for new versions), or import an archive you downloaded yourself |
| Command line      | `data\shims\` has `node`, `npm`, `php`, `java`, `go` ... shims for the default runtimes and `activate.cmd` / `activate.ps1` / `activate.sh` for one terminal. The Environment page puts chosen folders and variables on your Windows user PATH, item by item (opt-in, reversible)                                                            |
| Databases         | MariaDB, MySQL, PostgreSQL and Redis as services: data kept in the Zero2Dev folder, created on first start, local only, stopped cleanly; connection details on the service page                                                                                                                    |
| Editors and tools | VS Code, VSCodium and Notepad++, HeidiSQL and DBeaver: **Open in** a project folder or a database with one click                                                                                                                                                                                   |
| Sites             | `*.localhost` domains for a folder or a service, served by a managed Caddy or nginx (your choice in Settings); optional HTTPS; **Enable all** and **Disable all** with one restart of the site server                                                                                              |
| Security          | Loopback only, bearer token, Host and Origin checks, single-use event tickets, Caddy admin API turned off                                                                                                                                                                                          |

Not yet: ports 80/443 and hosts-file domains, database templates, auto-update, code signing.

## If a runtime will not install (Caddy, for example)

1. Open **Runtimes**, expand the failed job and **Show installer log** (it is also the file `data\logs\installer.log`). The error names the step that failed.
2. On a company network, set a **Download proxy** in Settings (Go ignores the Windows system proxy).
3. If downloads are blocked, open the entry's **Download page**, save the archive, then use **Import file**. Zero2Dev verifies it against a published checksum, or against a SHA-256 you paste in. If neither is available, you can confirm an unverified import.
4. Antivirus can briefly lock a freshly extracted `caddy.exe`; Zero2Dev now retries for several seconds before giving up.

## Sites and HTTPS

1. Install Caddy (Runtimes page). A **Caddy** service is created for you.
2. Create a site, for example `shop.localhost`, pointing at a folder or a service, then start the **Caddy** service.
3. Open `http://shop.localhost:8080` (HTTPS sites use port 8443).

Browsers resolve `*.localhost` to your own machine, so no hosts-file edit or admin rights are needed.
For HTTPS, Caddy makes its own local certificate authority. By default Zero2Dev does **not** add it to Windows (that is a system change).
Either trust `data\caddy\data\caddy\pki\authorities\local\root.crt` yourself, or turn on _Trust Caddy's local CA_ in Settings.

## How it fits together

```
Browser (Svelte UI)  --REST + SSE-->  zero2dev daemon  --spawns-->  your services
                                      api, supervisor, runtimes,
                                      installer, sites -> Caddyfile
```

- `cmd/zero2dev`: entry point. `internal/`: API, supervisor, event hub, runtimes, installer, sites, config, store.
- `internal/webui`: embeds the built UI. `web/`: the Svelte 5 app, created and built with Vite+.
- `docs/API.md`: the API contract shared by the daemon and the UI.

## Development

What is being built next, and in which order: [docs/ROADMAP.md](docs/ROADMAP.md).

```powershell
# once after cloning: build the UI so the Go code compiles (see Building from source)
cd web; pnpm install; pnpm build; cd ..

# terminal 1: the daemon, with the dev origin allowed and data kept in .\data
$env:ZERO2DEV_DEV_ORIGIN = "http://localhost:5173"; go run .\cmd\zero2dev

# terminal 2: the UI with hot reload (open http://localhost:5173)
cd web; pnpm dev
```

While `pnpm dev` runs, the browser gets the UI from Vite with hot reload, so you do not need to rebuild for UI changes.
The embedded copy only matters for the executable and for `http://127.0.0.1:7000`.

The UI uses pnpm (version pinned in `web/package.json`, lockfile `web/pnpm-lock.yaml`); do not use npm or commit a `package-lock.json`.

`pnpm check` lints and type-checks, `pnpm test` runs the UI tests, `make test` (or `go test ./cmd/... ./internal/...` after building the UI) runs the daemon tests,
`scripts/smoke.sh` runs a real end-to-end check against the built binary, and `web/e2e/real-daemon.cjs` drives the real UI in Chromium.

Environment variables: `ZERO2DEV_HOME`, `ZERO2DEV_BIN`, `ZERO2DEV_PORT`, `ZERO2DEV_NO_BROWSER`, `ZERO2DEV_DEV_ORIGIN`.

## Security notes

The panel can start programs on your computer, so it is reachable only by you: loopback only, a secret token on every API call,
and strict Host and Origin checks so a website you visit cannot drive it. Keep `data\token` private.
The installer only downloads from URLs in the catalog and verifies each archive against the publisher's checksum when one is
published. Some publishers (nginx.org) publish none: those downloads are marked unverified, Zero2Dev asks before installing them,
and installing them means trusting the download source and the HTTPS connection.
Unsigned programs that download and run executables can trigger antivirus heuristics.

Zero2Dev changes nothing in the registry unless you switch items on in the Environment page and apply them. Every item is off by default.
It writes only to your own user environment (`HKCU\Environment`, never the machine-wide one), keeps a copy of the previous PATH in `data\shims\.registry-backup.json`
and undoes only its own changes when you switch an item off. The registry code is tested against a fake registry; a real Windows check is still pending, so try it on a
machine where you can recover your PATH. Known limits: the `.cmd` shims read the OEM code page, so a data folder with non-ASCII characters in its path can break them,
and PowerShell's default execution policy can block `activate.ps1` (use `Set-ExecutionPolicy -Scope Process Bypass` in that window).

## License

Apache License 2.0, see `LICENSE`.
