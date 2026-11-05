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
- **Install progress**: Downloading maps bytes to 0-80 %, Verifying 82, Extracting 85, Registering 95, done 100. The expected hash is fetched before downloading. A runtime is installed to `<binDir>/<catalogId>/` (`binDir` is `<exe dir>/bin` in portable mode, `ZERO2DEV_BIN` if set, else `<home>/bin`; since 0.0.2, before that `<home>/runtimes`). A second install of an already installed id or of an id with a running job is `409`; an entry for another OS/arch is `400`.
- **Detect**: version commands differ per tool (`go version`, `caddy version`, `java -version`, `nginx -v`, `--version` otherwise). nginx is reported with `kind: "other"` plus an extra `name: "nginx"` field. Unparseable versions are reported as `"unknown"`.
- **Settings PUT**: every field is optional (missing = unchanged); `apiPort`/`homeDir` are accepted and ignored. Proxy ports must be 1-65535, differ from each other and from the API port.
- **Site on/off** (0.0.2): `POST /api/sites/{id}/enable` and `POST /api/sites/{id}/disable` (no body) -> `200 SiteView`, `404` for an unknown id. Only `enabled` changes; the site is not validated again, so a site whose folder or service went away can still be switched off. Both configs are regenerated and the running site server restarts, as for any site change.
- **Sites**: `enabled` defaults to `true` when omitted. Allowed domain suffixes are the configured `domainSuffix`, `localhost` and `test`. A proxy site whose service was deleted or has port 0 is left out of the Caddyfile (the site record stays). Service create/update/delete regenerates the Caddyfile because upstream ports may change. A static root containing spaces is written quoted; roots containing quotes, braces or control characters are rejected.
- **Services**: `startedAt` is non-null only while the process is active. `POST .../restart` stops (waiting up to grace + 3 s) and starts before answering. `PUT` is refused with `409` in `starting|running|stopping` (allowed in `failed`/`stopped`). Unknown `/api/...` paths return `404 not_found`; non-GET to static paths return 405 with code `bad_request`.
- **Logs**: the file format is `<ts>\t<stream>\t<text>` per line; on daemon start the last 2000 lines are reloaded from the file so the UI still shows history. Lines over 16 KB are truncated.
- **CORS**: `OPTIONS` preflight from `ZERO2DEV_DEV_ORIGIN` is answered with `204` without a token (preflights carry no credentials); every other `OPTIONS` goes through the normal checks.
- **Process trees**: on Windows `terminate` and `kill` both end the whole Job Object immediately (no graceful signal exists), then `taskkill /T /F /PID` runs as a fallback. On Linux/macOS, if the daemon itself is SIGKILLed, children are not reaped (no Pdeathsig); normal exits and Ctrl+C / SIGTERM stop everything.

---

# v0.0.1 amendments (BINDING, these override anything above that conflicts)

Release 0.0.1 is about **isolation and portability**: Zero2Dev must be a portable program. Everything it stores lives in one folder next to the executable, nothing is written to `%USERPROFILE%`, `%APPDATA%`, the registry or the system trust store, and the folder can be moved or copied to another drive and keep working. The version string everywhere is `0.0.1` (single source: the `VERSION` file at the repo root, passed to Go with `-ldflags "-X main.version=<VERSION>"`, default in `main.go` is `0.0.1`).

## Portable home folder

- Home dir resolution, first match wins: (1) env `ZERO2DEV_HOME`; (2) if the running executable is inside a Go build/temp directory (path contains `go-build`, as with `go run`) then `<current working dir>/data`; (3) **`<directory of the executable>/data`** (default, via `os.Executable()` with symlinks resolved). The old `%USERPROFILE%\.zero2dev` default is removed.
- The daemon prints the data folder at startup: `Data folder: <abs path> (portable, next to the program)` or `(from ZERO2DEV_HOME)` / `(current directory, dev mode)`.
- Layout additions: `<home>/tmp/` (all of the daemon's own temp files; set `TMP`/`TEMP`/`TMPDIR` for the daemon process itself at startup to this folder and use it for downloads and extraction staging) and `<home>/caddy/data`, `<home>/caddy/config` (see Caddy isolation).
- **Relocatable**: `state.json` must keep working after the whole folder is moved. On load, for every runtime with `source: "installed"` whose stored `binary` does not exist, rebase it: take the path segments after the first `runtimes` segment, and join them to `<current home>/runtimes/`. (Handle both `\` and `/` in the stored value, whichever OS wrote it.) Persist the rebased value. Runtimes with `source: "registered"` keep their absolute path (they point outside the folder on purpose). Service `cwd` and site `root` are user project folders and are never rewritten.
- `GET /api/status` gains `homeSource` (`"env" | "cwd" | "exe"`) and `portable` (`true` only when `homeSource` is `"exe"`), plus `exeDir`.
- No other location is written to by the daemon. Add a test proving this: run the daemon with `HOME`, `USERPROFILE`, `APPDATA`, `LOCALAPPDATA`, `XDG_*` and `TMPDIR` pointed at an empty temp dir and assert it stays empty (apart from nothing at all) after start/stop of a service and a runtime import.

## Caddy isolation and safety

- When the supervisor starts a service of `kind: "caddy"` (or whose resolved binary is named `caddy`/`caddy.exe`) it adds these environment variables, overriding any user value: `XDG_DATA_HOME=<home>/caddy/data`, `XDG_CONFIG_HOME=<home>/caddy/config`, `TMP`, `TEMP`, `TMPDIR` = `<home>/tmp`. (Caddy honours XDG_* on every OS, otherwise it writes to `%AppData%\Caddy`.)
- The generated Caddyfile global block is, in this order: `admin off` (no local admin API on :2019, we restart Caddy to apply changes), `http_port`, `https_port`, `auto_https disable_redirects`, and `skip_install_trust` **unless** setting `trustLocalCA` is true. The Caddy root certificate then lives at `<home>/caddy/data/caddy/pki/authorities/local/root.crt`.
- `skip_install_trust` exists because by default Caddy installs its local root CA into the operating system trust store (a system change and an OS prompt), which breaks isolation.

## Settings additions

`Settings` gains two fields (PUT accepts partial bodies as before):

```json
{ "trustLocalCA": false, "downloadProxy": "" }
```

- `trustLocalCA`: default `false`. Regenerates the Caddyfile on change.
- `siteServer` (since 0.0.2): `"caddy"` (default; empty in older state files reads as caddy) or `"nginx"`; anything else is `400`. Both `<home>/config/Caddyfile` and `<home>/config/nginx.conf` are always regenerated; only the running service of the active server is restarted. On change the daemon stops running services of the old server and, if one was running, starts the new server's service (in the background, after the response).
  - With `nginx`, a managed service `Nginx (sites)` (kind `nginx`, args `-p {home}/nginx/ -c {home}/config/nginx.conf`, recognised by the `{home}/config/nginx.conf` arg) is created when an nginx runtime exists, like the Caddy service.
  - nginx listens on `127.0.0.1` only; a default server answers `404` for unknown hosts. Proxy sites forward `Host`, `X-Real-IP`, `X-Forwarded-For`, `X-Forwarded-Proto` and WebSocket upgrades.
  - HTTPS sites use one certificate for all HTTPS domains, signed by a local CA that Zero2Dev creates in `<home>/nginx/certs/` (`ca.crt` is kept; `sites.crt` is reissued when the names change or within 30 days of expiry). Nothing is installed into the OS trust store; `trustLocalCA` does not apply.
- Service args: `{home}` is replaced with the data folder in slash form, like `{port}`.
- Static site roots containing `$` (or, on Linux/macOS, a backslash) are rejected because nginx would interpret them.
- `downloadProxy`: empty or an `http://host:port` / `https://host:port` URL (with optional `user:pass@`). Used by the installer's HTTP client. When empty, use the standard `HTTP_PROXY`/`HTTPS_PROXY`/`NO_PROXY` environment (Go's `ProxyFromEnvironment`). Go does NOT read the Windows system proxy, which is a likely reason downloads fail on company networks. Invalid values return `400 bad_request`.

## Catalog entry additions

`CatalogEntry` gains `homepage` (string, a human download page, e.g. `https://github.com/caddyserver/caddy/releases/tag/v2.8.4`, `https://nodejs.org/dist/v22.11.0/`, `https://go.dev/dl/`) and `fileName` (computed: the archive file name from `url`). Add `homepage` to every manifest entry.

## Installer hardening (why: Caddy could not be installed)

The previous installer had one failure mode that is invisible to the user. Fix all of these:

1. **Windows sharing violations**: antivirus (Defender) scans a freshly extracted `caddy.exe` and the final `os.Rename(tmp, final)` can fail with access denied. Retry the rename up to 20 times with growing delays (250 ms, capped at 2 s), then fall back to copying the tree and removing the staging folder. The rename function must be injectable so a test can simulate failures.
2. HTTP client: User-Agent `Zero2Dev/<version>`; transport with dial timeout 15 s, TLS handshake 15 s, response-header timeout 30 s, proxy from `downloadProxy` setting or environment; **no overall client timeout** (the existing 30 minute context stays) but abort a download that receives no bytes for 60 s. Retry network errors (not HTTP 4xx, not checksum mismatch) up to 3 attempts with 1 s / 3 s backoff, resetting progress.
3. **Errors name the step and the cause**: `job.error` looks like `Downloading: Get "https://...": dial tcp ...: i/o timeout` or `Registering: rename ...: Access is denied (antivirus may be scanning the file; try again)`. Include the HTTP status and final URL after redirects when a download fails.
4. Every install and import attempt appends timestamped lines to `<home>/logs/installer.log` (start, each step, URL, final URL, bytes, hashes expected/got, errors). Rotate at 1 MB to `.1`. `GET /api/installer/log?tail=200` returns `{ "lines": ["2026-11-04T10:00:00Z ...", ...] }` (max 1000).

## Import from a file the user downloaded themselves

When a network blocks the download, the user can fetch the archive in a browser and import it.

`POST /api/runtimes/import`, body `{ "catalogId": "caddy-2.8.4-windows-amd64", "filePath": "C:\\Users\\me\\Downloads\\caddy_2.8.4_windows_amd64.zip", "sha256": "" }` -> `202 Job`.

- `filePath` must be absolute and an existing regular file (`400` otherwise); its base name should equal the entry's `fileName`, otherwise `400` naming the expected file.
- Expected hash: `sha256` if given (64 hex chars, `400` if malformed); otherwise the published checksum, fetched with a 10 s timeout. If neither is available: `400 bad_request` with message `Cannot verify the file because the published checksum could not be fetched. Paste the SHA-256 for <fileName> from <checksum url> into the SHA-256 field.`
- Same pipeline as install after the download step: verify (steps `Importing` 0-80%, `Verifying`, `Extracting`, `Registering`), same hardening, same `installer.log`. The user's file is copied into `<home>/tmp` for processing and never modified or deleted.
- Same conflict rules as install (`409` if already installed or already running a job for that entry).

## Behaviour of `GET /api/status`

```json
{ "version": "0.0.2", "uptimeSeconds": 12, "os": "windows", "arch": "amd64", "homeDir": "D:\\zero2dev\\data", "binDir": "D:\\zero2dev\\bin", "homeSource": "exe", "portable": true, "exeDir": "D:\\zero2dev", "folderPicker": true, "counts": { "services": 0, "running": 0, "runtimes": 0, "sites": 0 } }
```

### Backend notes for v0.0.1

- **Home**: `ZERO2DEV_HOME` -> `./data` when the executable path contains `go-build` (or `os.Executable` fails) -> `<exe dir>/data` (symlinks resolved). `<home>/downloads` is no longer created; downloads and extraction staging use `<home>/tmp`, which is emptied at daemon start. The daemon sets `TMP`/`TEMP`/`TMPDIR` to it for itself (children inherit them). `exeDir` is also reported when the home comes from the environment.
- **Rebase** (`RebaseRuntimes`, run at startup): only `source: "installed"` runtimes whose stored binary does not exist are changed. The first `runtimes` path segment is used; if that result does not exist and the path contains a later `runtimes` segment that does resolve, that one wins. Paths without a `runtimes` segment are left alone.
- **Caddy isolation** applies to `kind: "caddy"` services and to any service whose resolved binary is named `caddy`/`caddy.exe`. The values are appended after user env, so they win. `trustLocalCA` and `downloadProxy` are partial in PUT; `downloadProxy` is trimmed and validated (http/https URL with host, optional port 1-65535, no path/query).
- **Installer**: retries cover network errors, stalls and HTTP 5xx (3 attempts, 1 s / 3 s); HTTP 4xx, checksum mismatch and unparseable checksum files are not retried. The checksum file is fetched with the same hardened client. Rename retry: 20 attempts, delay 250 ms * attempt capped at 2 s, then copy fallback. `job.error` starts with the step name (`Downloading`, `Importing`, `Verifying`, `Extracting`, `Registering`). Install jobs have `type: "install"`, import jobs `type: "import"` (initial step `Importing`).
- **Import**: validation order is catalog entry (404), platform (400), `filePath` absolute/exists/regular (400), file name equals `fileName` (400), `sha256` format (400), conflicts (409), then the hash (given, else published checksum fetched with a 10 s timeout, else 400 with the documented message; when the entry has no checksum URL the message names the homepage instead). The comparison of the file name is exact (case-sensitive), so a browser-renamed `file (1).zip` is rejected with the expected name.
- **`GET /api/installer/log`**: `tail` default 200, max 1000; lines are `<RFC3339> [<jobId>] <message>`; reads through into `installer.log.1` when needed.

## Checksums: SHA-256 and SHA-512 (fix after first field test)

The published checksum for Caddy releases is **SHA-512** (128 hex characters per line); Node and Go publish SHA-256 (64). `checksum.type: "shasums"` files and bare-hash files may now contain either, the digest algorithm is chosen from the length of the expected hash, and the archive is verified with that algorithm. The `sha256` field of `POST /api/runtimes/import` (name kept for compatibility) accepts a 64 or 128 character hex digest. Signed query strings on redirect URLs are removed from `installer.log` and from error messages.

---

## Amendment: more runtimes, many versions side by side, version discovery (branch `feature/standalone-runtimes`)

Binding for daemon and UI. Everything not listed here is unchanged. This **replaces** the earlier "one version at a time" rule:
any number of versions of a runtime can be installed at once (PHP 7.1 for one project, PHP 8 for another).

### Kinds
`RuntimeKinds` gains `"nginx"` (kind `"other"` stays valid for old state). `ServiceKinds` gains `"nginx"`.
Version lists can be fetched for six kinds: `node`, `go`, `caddy`, `php`, `java`, `nginx`.

### Which version a service uses
- `Service.runtimeId` set: exactly that runtime.
- `runtimeId` empty ("Default"): the **default runtime of the service's kind**, else the first one of that kind, else PATH (as before).
- Every kind has at most one default. The first runtime of a kind (installed or registered) becomes the default automatically; removing the default promotes the first remaining one.

### Types
`CatalogEntry` gains: `line` (version line: node `"22"`, go `"1.23"`, caddy `"2.8"`, php `"8.4"`, java `"21"`, nginx `"1.26"`), `channel` (`"lts"|"stable"|"mainline"|""`),
`source` (`"builtin"|"remote"`), `latest` (bool: newest version of this kind/os/arch/line), `unverified` (bool: no published checksum; nginx and old PHP builds).
`Runtime` gains `catalogId` (omitted when empty) and `default` (bool).
`Checksum.type` gains `"none"` (only valid for entries with `unverified:true`).
Errors may carry `"details"` (object of strings): `{"error":{"code","message","details":{...}}}`.

### `GET /api/catalog`
Unchanged shape (array): embedded manifest merged with the cached remote versions (never touches the network). Sorted by kind, then os (windows first), then newest version first.

### `GET /api/catalog/status` -> 200
`{"checkedAt": "<RFC3339>"|null, "sources":[{"kind":"php","name":"windows.php.net","ok":true,"count":4,"error":"","checkedAt":"<RFC3339>"}]}`
`sources` is empty until the first refresh, then has one row per kind: `node`, `go`, `caddy`, `php`, `java`, `nginx`.

### `POST /api/catalog/refresh` (no body) -> 200
Asks the publishers for their version lists (node: nodejs.org, go: go.dev, caddy: GitHub releases, php: windows.php.net incl. its archive for older lines, java: Adoptium, nginx: nginx.org), using the Download proxy setting, 40 s total.
A failing source does not fail the call and keeps its previously cached entries. Response: the status object plus `"added":["<catalog id>", ...]`. Cached in `data/cache/remote-catalog.json`.

### `POST /api/runtimes/install` body `{catalogId, acceptUnverified?: bool}` -> 202 Job
Installs that version next to any others. `409 conflict` if that exact version is already installed.
`400 unverified` (code `unverified`) if the entry has `unverified:true` and `acceptUnverified` is not true.

### `POST /api/runtimes/import` body `{catalogId, filePath, sha256?, acceptUnverified?}`
Same rules. For `unverified` entries a pasted `sha256` counts as verification.

### `POST /api/runtimes/{id}/default` (no body) -> 200 Runtime
Makes that runtime the default of its kind (the previous default loses the flag). Emits `runtimes.changed`.

### `DELETE /api/runtimes/{id}`
Unchanged, but when the default is deleted another runtime of that kind becomes the default. Still `409` while a service uses it (message names the service).

### Removed from the earlier draft
`replace`, `single`, `replacedRuntimeId`, error codes `single_install` and `in_use`: none of them exist.

## Amendment: native folder dialog (0.0.2)

The browser's own directory picker never reveals an absolute path, which services (`cwd`) and sites (`root`) need. The daemon runs on the same PC, so it shows the operating system's dialog itself.

### `POST /api/dialog/folder` body `{ "title"?: string, "start"?: string }` (or no body) -> 200

```json
{ "path": "C:\\work\\my-app", "cancelled": false }
{ "path": "", "cancelled": true }
```

- Opens the modern Windows "Select Folder" dialog (IFileOpenDialog with `FOS_PICKFOLDERS | FOS_FORCEFILESYSTEM | FOS_PATHMUSTEXIST`, through raw COM calls in `internal/winpick`, no cgo or third-party module) and answers when the user closes it. The request stays open meanwhile (the HTTP server has no write timeout).
- `title`: dialog title, default `Select a folder`, at most 100 characters on one line (`400` otherwise). `start`: where the dialog opens: that folder if it exists, else its nearest existing parent; ignored when empty or relative.
- The dialog is owned by an invisible always-on-top window centred on the primary screen, so it opens in front of the browser instead of behind it.
- One dialog at a time: a second request while one is open is `409 conflict`.
- Other operating systems: `501` with code `not_supported`. `GET /api/status` reports `folderPicker: true` only when the dialog is available; the UI shows its **Browse...** buttons only then, and typing a path always works.
- `internal/winpick` has a test that opens real dialogs and presses their buttons; it needs a desktop session and only runs with `Z2D_DIALOG_TEST=1`.
