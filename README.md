# Zero2Dev

Version 0.0.3. A portable local development control panel: one program that opens in your browser at `http://127.0.0.1:7000`.
Register or install runtimes (Node, Go, Caddy, nginx, PHP and Java), define services (a command, arguments, a folder and a port),
start and stop them with live logs, and map local domains such as `myapp.localhost` to a folder or a service.

No Electron. No Docker. One Go binary with the Svelte UI embedded.

**New here? Read the [step-by-step guide with screenshots](docs/USAGE.md).**

## Portable by design

Everything Zero2Dev stores lives in the `data` and `bin` folders **next to the program**. Nothing is written to your user profile,
`AppData`, the registry or the Windows certificate store.

```
zero2dev.exe
bin\                 Node, Go, Caddy and anything else installed from the catalog
data\
  token              your access token
  state.json         services, runtimes, sites, settings
  config\            Caddyfile and nginx.conf, generated, do not edit
  caddy\             Caddy's own data and config (kept out of %AppData%)
  nginx\             nginx's logs, temp files and local certificates
  mariadb\ mysql\ postgres\ redis\   database files, one folder per service
  code\ codium\ notepadpp\ heidisql\ dbeaver\   settings and workspaces of installed tools
  cache\             the list of versions found by "Check for new versions"
  logs\              one log per service, plus installer.log
  tmp\               work area for downloads, safe to delete when Zero2Dev is stopped
```

- Copy or move the whole folder (to another drive, a USB stick) and it keeps working. Stop Zero2Dev first.
- To reset everything, stop Zero2Dev and delete `data\` (settings, services, sites, database data) and `bin\` (installed runtimes).
- Set `ZERO2DEV_HOME` to use a different data folder; installed runtimes then go to its `bin` subfolder.
- Set `ZERO2DEV_BIN` to keep installed runtimes in a folder of your choice instead.

What is _not_ isolated: the services you run are your own programs. A Node app still writes to the normal Windows temp folder, and
runtimes you registered by path stay where they are.

## Quick start (Windows)

Build prerequisites: [Go 1.24+](https://go.dev/dl/), [Node.js 22.18+](https://nodejs.org/) and [pnpm 10](https://pnpm.io/installation) (`corepack enable` sets up the pinned version).

```powershell
cd D:\zero2dev
.\build.ps1                 # builds the web UI, then dist\zero2dev.exe
.\dist\zero2dev.exe         # data goes to dist\data, runtimes to dist\bin
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

## What 0.0.3 does

| Area              | Included                                                                                                                                                                                                                                                               |
| ----------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Services          | Create, edit, delete; start, stop, restart; port-conflict checks; health probes; live and stored logs                                                                                                                                                                  |
| Process safety    | Children die with the daemon (Windows Job Object, process groups elsewhere); no shell strings are ever run                                                                                                                                                             |
| Runtimes          | Detect what is on your PATH, register a program by path, install Node, Go, Caddy, nginx, PHP and Java (many versions side by side, a default per runtime, a version per service, and a button to check for new versions), or import an archive you downloaded yourself |
| Databases         | MariaDB, MySQL, PostgreSQL and Redis as services: data kept in the Zero2Dev folder, created on first start, local only, stopped cleanly; connection details on the service page                                                                                        |
| Editors and tools | VS Code, VSCodium and Notepad++ (portable), HeidiSQL and DBeaver: **Open in** a project folder or a database with one click                                                                                                                                            |
| Sites             | `*.localhost` domains for a folder or a service, served by a managed Caddy or nginx (your choice in Settings); optional HTTPS                                                                                                                                          |
| Security          | Loopback only, bearer token, Host and Origin checks, single-use event tickets, Caddy admin API turned off                                                                                                                                                              |

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

## License

Apache License 2.0, see `LICENSE`.
