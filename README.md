# Zero2Dev

A local development control panel that runs as one program and opens in your browser at `http://127.0.0.1:7000`.
Register or install runtimes (Node, Go, PHP, Java, Caddy), define services (a command, arguments, a folder and a port),
start and stop them with live logs, and map friendly local domains such as `myapp.localhost` to a folder or a service.

No Electron. No Docker. One Go binary with the Svelte UI embedded.

## Quick start (Windows)

Prerequisites to build from source: [Go 1.24+](https://go.dev/dl/) and [Node.js 22+](https://nodejs.org/).

```powershell
cd D:\zeo2dev
.\build.ps1          # builds the web UI, then bin\zero2dev.exe
.\bin\zero2dev.exe   # starts the daemon and opens your browser
```

The first run prints a URL like `http://127.0.0.1:7000/#token=...`. That token is your access key; the page stores it
and removes it from the address bar. It lives in `%USERPROFILE%\.zero2dev\token`.

Linux or macOS: `make build` then `./bin/zero2dev`.

## What v1 does

| Area | Included in v1 |
| --- | --- |
| Services | Create, edit, delete; start, stop, restart; port-conflict checks; health probes; live and stored logs |
| Process safety | Children are killed with the daemon (Windows Job Object, process groups elsewhere); no shell strings are ever run |
| Runtimes | Detect what is already on your PATH, register any binary by path, install Node, Go and Caddy from official archives with checksum verification |
| Sites | `*.localhost` domains for a folder or a service, optional local HTTPS, through a managed Caddy |
| Security | Binds to 127.0.0.1 only, bearer token, Host and Origin checks, single-use event tickets |

Not in v1 (planned): PHP and Java one-click install (use Detect or Register for now), ports 80/443 and hosts-file
domains (needs an elevated helper), nginx and database templates, per-project version pinning, auto-update, code signing.

## How it fits together

```
Browser (Svelte UI)  --REST + SSE-->  zero2dev daemon  --spawns-->  your services
                                      |  api, supervisor, runtimes,
                                      |  installer, sites -> Caddyfile
                                      v
                                 %USERPROFILE%\.zero2dev
```

- `cmd/zero2dev`: program entry point.
- `internal/`: API, supervisor, event hub, runtimes, installer, sites, config, store.
- `internal/webui`: embeds the built UI.
- `web/`: the Svelte 5 app, created and built with Vite+.
- `docs/API.md`: the full API contract shared by the daemon and the UI.

## Using sites with Caddy

1. Runtimes page: install Caddy from the catalog (or register your own `caddy.exe`). A service named **Caddy** is created for you.
2. Sites page: create a site, for example `shop.localhost` pointing at a folder (static) or a service (proxy).
3. Start the **Caddy** service and open `http://shop.localhost:8080`.

Browsers resolve `*.localhost` to your own machine, so no hosts-file edit or admin rights are needed.
For HTTPS, enable it on the site; Caddy issues a certificate from its own local CA, and you trust that CA once.

## Development

```powershell
# terminal 1: the daemon with the dev origin allowed
$env:ZERO2DEV_DEV_ORIGIN = "http://localhost:5173"; go run .\cmd\zero2dev

# terminal 2: the UI with hot reload
cd web; vp install; vp dev
```

`vp check` lints, formats and type-checks; `vp test` runs the UI tests; `go test ./...` runs the daemon tests.

Environment variables: `ZERO2DEV_HOME`, `ZERO2DEV_PORT`, `ZERO2DEV_NO_BROWSER`, `ZERO2DEV_DEV_ORIGIN`.

## Security notes

The panel can start programs on your computer, so it is built to be reachable only by you: loopback only, a secret token on every API call,
and strict Host and Origin checks so a website you visit cannot drive it. Keep the token private. Unsigned programs that download and run
executables can trigger antivirus heuristics; the installer only downloads from the URLs in the catalog and verifies each archive against its published checksum.

## License

Choose one before sharing this code (for example MIT).
