# Zero2Dev API contract (v1)

This file is the single source of truth shared by the Go daemon (`cmd/`, `internal/`) and the Svelte UI (`web/`).
If something here is wrong or missing, change this file and say so in your report; do not silently diverge.

## Product in one paragraph

Zero2Dev is a single Windows-first Go program that serves a Svelte dashboard on `http://127.0.0.1:7000`.
From the dashboard a developer registers or installs runtimes (Node, Go, PHP, Java, Caddy), defines services
(a command + args + cwd + port), starts/stops them with live logs, and maps local domains such as
`myapp.localhost` to a folder or a service through a Caddy reverse proxy. No Electron, no Docker.

## Process facts

- Binary: `zero2dev` (`zero2dev.exe` on Windows). Go module name: `zero2dev`. **Go standard library only** (no third-party modules: the module proxy is unreachable in the build environment).
- Home dir: `%USERPROFILE%\.zero2dev` (`$HOME/.zero2dev` elsewhere), override with env `ZERO2DEV_HOME`.
- Port: default `7000`, override with env `ZERO2DEV_PORT`. Bind `127.0.0.1` only.
- `ZERO2DEV_NO_BROWSER=1` stops the daemon opening the browser at startup.
- `ZERO2DEV_DEV_ORIGIN` (optional, e.g. `http://localhost:5173`): one extra allowed `Origin` for the Vite dev server.
- Storage: JSON files under the home dir (`state.json`, written atomically via temp file + rename). Logs under `logs/<serviceId>.log`.
- Home layout:

```
<home>/
  token             random 32-byte hex access token (file mode 0600)
  state.json        services, runtimes, sites, settings
  runtimes/<id>/    installed runtimes (extracted archives)
  config/Caddyfile  generated, never hand-edited
  logs/<serviceId>.log
  downloads/        partial downloads
```

## Auth and request hygiene (every `/api/*` route except where noted)

1. `Host` header must be exactly `127.0.0.1:<port>` or `localhost:<port>` (blocks DNS rebinding) else `403 forbidden_host`.
2. If an `Origin` header is present it must equal `http://127.0.0.1:<port>`, `http://localhost:<port>`, or `ZERO2DEV_DEV_ORIGIN` else `403 forbidden_origin`. No CORS headers are ever sent except for `ZERO2DEV_DEV_ORIGIN` (reflect that one origin, allow `Authorization, Content-Type`).
3. `Authorization: Bearer <token>` required else `401 unauthorized`. Compare in constant time.
4. The static UI files (`/`, `/assets/*`) need no token but still obey the Host check.
5. At startup the daemon prints and (unless disabled) opens `http://127.0.0.1:<port>/#token=<token>`. The UI reads the token from the URL fragment, stores it in `localStorage` key `zero2dev.token`, and removes the fragment with `history.replaceState`.
6. The event stream cannot send headers, so it uses a ticket: `POST /api/events/ticket` -> `{ "ticket": "..." }`, single use, expires after 30 s, then `GET /api/events?ticket=...`.
7. There is no endpoint that runs an arbitrary command. Services are structured records (command + args array), spawned without a shell (`exec.Command`, never `cmd /c <string>`).
8. Path inputs (`cwd`, site `root`, runtime `path`) must be absolute, must exist as directories (files for runtime binary), and are cleaned with `filepath.Clean`.

## Errors

Non-2xx responses always use this body (JSON):

```json
{ "error": { "code": "not_found", "message": "service svc_x not found" } }
```

Codes: `bad_request` 400, `unauthorized` 401, `forbidden_host` 403, `forbidden_origin` 403, `not_found` 404,
`conflict` 409 (e.g. port already in use, wrong state), `internal` 500. Unknown JSON fields are rejected with `bad_request`.
All success bodies are JSON, `Content-Type: application/json`. `204` for deletes and actions with no body.

## Data types

IDs are opaque strings with a prefix: `svc_`, `rt_`, `site_`, `job_` plus 8 random hex chars.
Timestamps are RFC 3339 UTC strings.

### Service

```json
{
  "id": "svc_a1b2c3d4",
  "name": "My API",
  "kind": "node",
  "command": "",
  "args": ["server.js", "--port", "{port}"],
  "cwd": "C:\\work\\my-api",
  "env": { "NODE_ENV": "development" },
  "port": 3000,
  "healthPath": "",
  "runtimeId": "rt_11112222",
  "autostart": false,
  "state": "stopped",
  "pid": 0,
  "startedAt": null,
  "lastError": ""
}
```

- `kind`: `custom | node | php | go | java | caddy`.
- `state`: `stopped | starting | running | stopping | failed`. `state`, `pid`, `startedAt`, `lastError` are runtime fields set by the daemon; clients never send them.
- `port` is `0` when the service has no port (then there is no port health check).
- `healthPath`: optional. If non-empty and `port > 0`, health is an HTTP GET on `http://127.0.0.1:<port><healthPath>` returning any status < 500; otherwise a TCP connect.
- Command resolution (daemon):
  - `custom`: `command` is required and used as-is (absolute path or a name on PATH).
  - `node|php|go|java|caddy`: if `command` is empty, use the binary of the runtime named by `runtimeId` (or, if `runtimeId` is empty, the first registered runtime of that kind, or the program on PATH). Missing binary -> `409 conflict` on start with a clear message.
  - Default args when `args` is empty: `php` -> `["-S","127.0.0.1:{port}","-t","<cwd>"]`; `go` -> `["run","."]`; `caddy` -> `["run","--config","<home>/config/Caddyfile","--adapter","caddyfile"]`; `node` and `java` have no default (start fails with `bad_request`-style message `args required`).
  - Every `{port}` in `args` and every `env` value is replaced with the service port. The daemon also sets `PORT=<port>` in the child environment when `port > 0`.
- Starting: check the port is free first (`409 conflict`, message names the port). Spawn with stdout and stderr captured line by line. State goes `starting` -> `running` when health passes (poll every 250 ms, give up after 30 s -> `failed`, process killed). With `port == 0`, `running` after 500 ms if the process is still alive.
- Unexpected exit: `failed` with `lastError` (`exit status N`); a normal stop sets `stopped`. No auto-restart in v1.
- Stopping: terminate the process tree. On Windows use a Job Object created with kill-on-close for every child plus `taskkill /T /F /PID` fallback; elsewhere SIGTERM the process group then SIGKILL after 5 s. Children must never outlive the daemon.
- On daemon shutdown (Ctrl+C / SIGTERM) all running services are stopped.
- Autostart: services with `autostart: true` are started when the daemon starts.

### ServiceInput (body of create / update)

`name` (required, 1-64 chars), `kind` (required), `command`, `args`, `cwd` (required, absolute existing dir), `env`, `port` (0-65535), `healthPath`, `runtimeId`, `autostart`. Update is a full replacement of those fields (`PUT`); a running service cannot be updated (`409`).

### Runtime

```json
{
  "id": "rt_11112222",
  "kind": "node",
  "version": "22.11.0",
  "name": "Node.js 22.11.0",
  "binary": "C:\\Users\\me\\.zero2dev\\runtimes\\node-22.11.0\\node.exe",
  "source": "installed",
  "installedAt": "2026-11-04T09:00:00Z"
}
```

`kind`: `node | php | go | java | caddy | other`. `source`: `installed` (downloaded by Zero2Dev, deleting it removes the folder) or `registered` (user-provided path, deleting only forgets it).

### CatalogEntry (downloadable runtimes, from the embedded `catalog/manifest.json`)

```json
{
  "id": "node-22.11.0",
  "kind": "node",
  "version": "22.11.0",
  "name": "Node.js 22.11.0",
  "url": "https://nodejs.org/dist/v22.11.0/node-v22.11.0-win-x64.zip",
  "checksum": { "type": "shasums", "url": "https://nodejs.org/dist/v22.11.0/SHASUMS256.txt" },
  "binaryRelPath": "node.exe",
  "stripComponents": 1,
  "os": "windows",
  "arch": "amd64",
  "installed": false,
  "supported": true
}
```

- `checksum` is either `{ "type": "sha256", "value": "<hex>" }`, or `{ "type": "shasums", "url": "..." }` (a text file of `<hex>  <filename>` lines or a single bare hex hash; the daemon matches the archive file name from `url`).
- **Never invent a hash.** Every catalog entry must verify against a published checksum or it must not be in the manifest. The installer refuses to extract if the digest does not match.
- `installed` and `supported` are computed per request (`supported` = entry `os`/`arch` equals `runtime.GOOS`/`GOARCH`).
- Archive formats: `.zip` (archive/zip) and `.tar.gz`. Guard against zip-slip (every extracted path must stay inside the destination).
- The manifest is read from `<home>/catalog.json` if present, otherwise the embedded default, so the user can update versions without rebuilding.

### Site

```json
{
  "id": "site_deadbeef",
  "name": "My shop",
  "domain": "shop.localhost",
  "type": "proxy",
  "root": "",
  "serviceId": "svc_a1b2c3d4",
  "upstreamPort": 0,
  "https": false,
  "enabled": true
}
```

- `type`: `static` (needs `root`), `proxy` (needs `serviceId` or `upstreamPort`; `serviceId` wins and uses that service's port), `php` (needs `serviceId` of a `php` service; behaves like `proxy` in v1).
- `domain`: lowercase hostname, must end with the configured suffix list (`localhost` by default; `.test` allowed). Unique across sites.
- The daemon regenerates `<home>/config/Caddyfile` after every change. Per enabled site (HTTP unless `https`):

```
http://shop.localhost:8080 {        # https:false -> http://<domain>:<proxyHttpPort>
  reverse_proxy 127.0.0.1:3000
}
shop.localhost {                    # https:true  -> Caddy internal CA
  tls internal
  file_server
  root * C:\work\shop
}
```

  Caddy runs unprivileged on port `8080` (HTTP) and `8443` (HTTPS) in v1 (settings `proxyHttpPort`, `proxyHttpsPort`, set in the Caddyfile global options block, `auto_https disable_redirects`). `*.localhost` resolves to loopback in browsers with no hosts-file edit. Ports 80/443 and the hosts file are out of scope for v1.
- After regenerating, if a `caddy` service is `running`, restart it (v1 simplicity). If a `caddy` runtime is registered/installed and no `caddy` service exists, the daemon auto-creates one named `Caddy` (port 0, kind `caddy`).
- Response includes computed `url`: `http(s)://<domain>:<port>` string (read-only).

### Job (long operations)

```json
{ "id": "job_9f8e7d6c", "type": "install", "state": "running", "percent": 40, "step": "Downloading", "error": "", "runtimeId": "" }
```

`state`: `running | done | failed`. Steps for install: `Downloading` (percent follows bytes), `Verifying`, `Extracting`, `Registering`. Jobs are kept in memory only.

### Settings

```json
{ "apiPort": 7000, "homeDir": "C:\\Users\\me\\.zero2dev", "domainSuffix": "localhost", "proxyHttpPort": 8080, "proxyHttpsPort": 8443, "openBrowserOnStart": true }
```

`apiPort` and `homeDir` are read-only (ignored on PUT). Changing proxy ports regenerates the Caddyfile.

## REST endpoints

| Method | Path | Body | Success |
| --- | --- | --- | --- |
| GET | `/api/status` | none | `{ version, uptimeSeconds, os, arch, homeDir, counts: { services, running, runtimes, sites } }` |
| GET | `/api/services` | none | `Service[]` (sorted by name) |
| POST | `/api/services` | ServiceInput | `201 Service` |
| GET | `/api/services/{id}` | none | `Service` |
| PUT | `/api/services/{id}` | ServiceInput | `Service` |
| DELETE | `/api/services/{id}` | none | `204` (stops it first; also removes its log file) |
| POST | `/api/services/{id}/start` | none | `202 Service` (state `starting`) |
| POST | `/api/services/{id}/stop` | none | `202 Service` (state `stopping` or `stopped`) |
| POST | `/api/services/{id}/restart` | none | `202 Service` |
| GET | `/api/services/{id}/logs?tail=500` | none | `{ lines: [{ ts, stream, text }] }` (`stream` is `stdout`, `stderr` or `system`; default tail 500, max 5000) |
| GET | `/api/runtimes` | none | `Runtime[]` |
| GET | `/api/runtimes/detect` | none | `[{ kind, version, binary }]` found on PATH by running `<tool> --version` (node, php, go, java, caddy, nginx) |
| POST | `/api/runtimes/register` | `{ kind, binary, version? }` | `201 Runtime` (verifies the file exists; reads version via `--version` if omitted) |
| POST | `/api/runtimes/install` | `{ catalogId }` | `202 Job` |
| DELETE | `/api/runtimes/{id}` | none | `204` (`409` if a service references it) |
| GET | `/api/catalog` | none | `CatalogEntry[]` |
| GET | `/api/sites` | none | `Site[]` |
| POST | `/api/sites` | SiteInput (Site minus id, url) | `201 Site` |
| PUT | `/api/sites/{id}` | SiteInput | `Site` |
| DELETE | `/api/sites/{id}` | none | `204` |
| GET | `/api/jobs/{id}` | none | `Job` |
| GET | `/api/settings` | none | `Settings` |
| PUT | `/api/settings` | Settings | `Settings` |
| POST | `/api/events/ticket` | none | `{ ticket }` |
| GET | `/api/events?ticket=...` | none | `text/event-stream` (no Authorization needed; the ticket is the credential) |

## Event stream (Server-Sent Events)

Frames are `event: <type>\ndata: <json>\n\n`. A comment line `: ping` is sent every 15 s. The client reconnects with a fresh ticket
(exponential backoff 1 s to 15 s) and refetches lists after reconnecting.

| Event | Data |
| --- | --- |
| `service.status` | `{ id, state, pid, startedAt, lastError }` |
| `log.line` | `{ serviceId, ts, stream, text }` |
| `job.progress` | `{ id, percent, step }` |
| `job.done` | `{ id, state, error, runtimeId }` |
| `services.changed` | `{}` (create, update, delete) |
| `runtimes.changed` | `{}` |
| `sites.changed` | `{}` |

Log lines are broadcast to every connected client (the UI filters by `serviceId`); the daemon keeps the last 2000 lines per service in memory and appends every line to the service's log file (rotate at 5 MB to `.1`).

## Ownership and layout

- Backend agent owns everything except `web/`: `cmd/zero2dev`, `internal/*` (except `internal/webui/dist`, written by the frontend build), `catalog/manifest.json`, tests, `Makefile`/`build.ps1`.
- Frontend agent owns only `web/`. Vite `build.outDir` must be `../internal/webui/dist` with `emptyOutDir: true`; `base` must be `./` (relative) so the embedded server can serve it at `/`.
- The UI is served by the daemon from `internal/webui` (`webui.FS()`), falling back to `index.html` for unknown non-`/api` paths.
- Dev loop: `vp dev` (Vite on port 5173) proxies `/api` to `http://127.0.0.1:7000`; run the daemon with `ZERO2DEV_DEV_ORIGIN=http://localhost:5173`. Because the Vite proxy forwards the request, the `Host` seen by the daemon must be rewritten (`changeOrigin: true`) to `127.0.0.1:7000`, and `Origin` must be rewritten too (`headers: { Origin: 'http://127.0.0.1:7000' }`) or the dev origin env var used.

## Backend notes (implementation choices and clarifications)

- **Manifest location**: the embedded default lives at `internal/catalog/manifest.json` (go:embed cannot reach outside its package), not `catalog/manifest.json`. Format: a JSON array of CatalogEntry without `installed`/`supported`. Ids carry the platform, e.g. `node-22.11.0-windows-amd64`, because one version has one entry per OS. The default manifest has Node 22.11.0, Go 1.23.3 and Caddy 2.8.4 for windows/amd64 and linux/amd64 only (PHP and Java are detect/register only in v1). Entries are validated on load (https only, except loopback http for tests; `binaryRelPath` relative without `..`; sha256 value is 64 hex chars).
- **Install progress**: Downloading maps bytes to 0-80 %, Verifying 82, Extracting 85, Registering 95, done 100. The expected hash is fetched before downloading. A runtime is installed to `<home>/runtimes/<catalogId>/`. A second install of an already installed id or of an id with a running job is `409`; an entry for another OS/arch is `400`.
- **Detect**: version commands differ per tool (`go version`, `caddy version`, `java -version`, `nginx -v`, `--version` otherwise). nginx is reported with `kind: "other"` plus an extra `name: "nginx"` field. Unparseable versions are reported as `"unknown"`.
- **Settings PUT**: every field is optional (missing = unchanged); `apiPort`/`homeDir` are accepted and ignored. Proxy ports must be 1-65535, differ from each other and from the API port.
- **Sites**: `enabled` defaults to `true` when omitted. Allowed domain suffixes are the configured `domainSuffix`, `localhost` and `test`. A proxy site whose service was deleted or has port 0 is left out of the Caddyfile (the site record stays). Service create/update/delete regenerates the Caddyfile because upstream ports may change. A static root containing spaces is written quoted; roots containing quotes, braces or control characters are rejected.
- **Services**: `startedAt` is non-null only while the process is active. `POST .../restart` stops (waiting up to grace + 3 s) and starts before answering. `PUT` is refused with `409` in `starting|running|stopping` (allowed in `failed`/`stopped`). Unknown `/api/...` paths return `404 not_found`; non-GET to static paths return 405 with code `bad_request`.
- **Logs**: the file format is `<ts>\t<stream>\t<text>` per line; on daemon start the last 2000 lines are reloaded from the file so the UI still shows history. Lines over 16 KB are truncated.
- **CORS**: `OPTIONS` preflight from `ZERO2DEV_DEV_ORIGIN` is answered with `204` without a token (preflights carry no credentials); every other `OPTIONS` goes through the normal checks.
- **Process trees**: on Windows `terminate` and `kill` both end the whole Job Object immediately (no graceful signal exists), then `taskkill /T /F /PID` runs as a fallback. On Linux/macOS, if the daemon itself is SIGKILLed, children are not reaped (no Pdeathsig); normal exits and Ctrl+C / SIGTERM stop everything.
