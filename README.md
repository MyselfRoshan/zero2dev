# Zero2Dev

Version 0.0.1. A portable local development control panel: one program that opens in your browser at `http://127.0.0.1:7000`.
Register or install runtimes (Node, Go, Caddy, plus PHP and Java by path), define services (a command, arguments, a folder and a port),
start and stop them with live logs, and map local domains such as `myapp.localhost` to a folder or a service.

No Electron. No Docker. One Go binary with the Svelte UI embedded.

**New here? Read the [step-by-step guide with screenshots](docs/USAGE.md).**

## Portable by design

Everything Zero2Dev stores lives in a `data` folder **next to the program**. Nothing is written to your user profile, `AppData`,
the registry or the Windows certificate store.

```
zero2dev.exe
data\
  token              your access token
  state.json         services, runtimes, sites, settings
  runtimes\          Node, Go, Caddy and anything else installed from the catalog
  caddy\             Caddy's own data and config (kept out of %AppData%)
  config\Caddyfile   generated, do not edit
  logs\              one log per service, plus installer.log
  downloads\  tmp\   work areas, safe to delete when Zero2Dev is stopped
```

- Copy or move the whole folder (to another drive, a USB stick) and it keeps working. Stop Zero2Dev first.
- To reset everything, stop Zero2Dev and delete `data\`.
- Set `ZERO2DEV_HOME` to use a different data folder.

What is *not* isolated: the services you run are your own programs. A Node app still writes to the normal Windows temp folder, and
runtimes you registered by path stay where they are.

## Quick start (Windows)

Build prerequisites: [Go 1.24+](https://go.dev/dl/) and [Node.js 22+](https://nodejs.org/).

```powershell
cd D:\zero2dev
.\build.ps1                 # builds the web UI, then bin\zero2dev.exe
.\bin\zero2dev.exe          # data goes to bin\data
```

The first run prints a URL like `http://127.0.0.1:7000/#token=...` and opens it. The page stores the token and removes it from the address bar.
Linux or macOS: `make build`, then `./bin/zero2dev`.

## What 0.0.1 does

| Area | Included |
| --- | --- |
| Services | Create, edit, delete; start, stop, restart; port-conflict checks; health probes; live and stored logs |
| Process safety | Children die with the daemon (Windows Job Object, process groups elsewhere); no shell strings are ever run |
| Runtimes | Detect what is on your PATH, register a program by path, install Node, Go and Caddy with checksum verification, or import an archive you downloaded yourself |
| Sites | `*.localhost` domains for a folder or a service, through a managed Caddy; optional HTTPS |
| Security | Loopback only, bearer token, Host and Origin checks, single-use event tickets, Caddy admin API turned off |

Not yet: PHP and Java one-click install, ports 80/443 and hosts-file domains, nginx and database templates, auto-update, code signing.

## If a runtime will not install (Caddy, for example)

1. Open **Runtimes**, expand the failed job and **Show installer log** (it is also the file `data\logs\installer.log`). The error names the step that failed.
2. On a company network, set a **Download proxy** in Settings (Go ignores the Windows system proxy).
3. If downloads are blocked, open the entry's **Download page**, save the archive, then use **Import file**. Zero2Dev verifies it against the published checksum, or against the SHA-256 you paste in.
4. Antivirus can briefly lock a freshly extracted `caddy.exe`; Zero2Dev now retries for several seconds before giving up.

## Sites and HTTPS

1. Install Caddy (Runtimes page). A **Caddy** service is created for you.
2. Create a site, for example `shop.localhost`, pointing at a folder or a service, then start the **Caddy** service.
3. Open `http://shop.localhost:8080` (HTTPS sites use port 8443).

Browsers resolve `*.localhost` to your own machine, so no hosts-file edit or admin rights are needed.
For HTTPS, Caddy makes its own local certificate authority. By default Zero2Dev does **not** add it to Windows (that is a system change).
Either trust `data\caddy\data\caddy\pki\authorities\local\root.crt` yourself, or turn on *Trust Caddy's local CA* in Settings.

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

```powershell
# terminal 1: the daemon, with the dev origin allowed and data kept in .\data
$env:ZERO2DEV_DEV_ORIGIN = "http://localhost:5173"; go run .\cmd\zero2dev

# terminal 2: the UI with hot reload
cd web; vp install; vp dev
```

`vp check` lints and type-checks, `vp test` runs the UI tests, `go test ./cmd/... ./internal/...` runs the daemon tests,
`scripts/smoke.sh` runs a real end-to-end check against the built binary, and `web/e2e/real-daemon.cjs` drives the real UI in Chromium.

Environment variables: `ZERO2DEV_HOME`, `ZERO2DEV_PORT`, `ZERO2DEV_NO_BROWSER`, `ZERO2DEV_DEV_ORIGIN`.

## Security notes

The panel can start programs on your computer, so it is reachable only by you: loopback only, a secret token on every API call,
and strict Host and Origin checks so a website you visit cannot drive it. Keep `data\token` private.
The installer only downloads from URLs in the catalog and verifies every archive against a published checksum.
Unsigned programs that download and run executables can trigger antivirus heuristics.

## License

Apache License 2.0, see `LICENSE`.
