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

IDs are opaque strings with a prefix: `svc_`, `rt_`, `site_`, `job_`, `prj_` plus 8 random hex chars.
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
  "version": "26.13.1",
  "name": "Node.js 26.13.1",
  "binary": "C:\\Users\\me\\.zero2dev\\runtimes\\node-26.13.1\\node.exe",
  "source": "installed",
  "installedAt": "2026-11-04T09:00:00Z"
}
```

`kind`: `node | php | go | java | caddy | other`. `source`: `installed` (downloaded by Zero2Dev, deleting it removes the folder) or `registered` (user-provided path, deleting only forgets it).

### CatalogEntry (downloadable runtimes, from the embedded `catalog/manifest.json`)

```json
{
  "id": "node-26.13.1-windows-amd64",
  "kind": "node",
  "version": "26.13.1",
  "name": "Node.js 26.13.1",
  "url": "https://nodejs.org/dist/v26.13.1/node-v26.13.1-win-x64.zip",
  "checksum": { "type": "shasums", "url": "https://nodejs.org/dist/v26.13.1/SHASUMS256.txt" },
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

- **Manifest location**: the embedded default lives at `internal/catalog/manifest.json` (go:embed cannot reach outside its package), not `catalog/manifest.json`. Format: a JSON array of CatalogEntry without `installed`/`supported`. Ids carry the platform, e.g. `node-26.13.1-windows-amd64`, because one version has one entry per OS. The default manifest has Node 26.13.1, Go 1.23.3 and Caddy 2.8.4 for windows/amd64 and linux/amd64 only (PHP and Java are detect/register only in v1). Entries are validated on load (https only, except loopback http for tests; `binaryRelPath` relative without `..`; sha256 value is 64 hex chars).
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

`CatalogEntry` gains `homepage` (string, a human download page, e.g. `https://github.com/caddyserver/caddy/releases/tag/v2.8.4`, `https://nodejs.org/dist/v26.13.1/`, `https://go.dev/dl/`) and `fileName` (computed: the archive file name from `url`). Add `homepage` to every manifest entry.

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
`CatalogEntry` gains: `line` (version line: node `"26"`, go `"1.23"`, caddy `"2.8"`, php `"8.4"`, java `"21"`, nginx `"1.26"`), `channel` (`"lts"|"stable"|"mainline"|""`),
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

## Amendment: databases, code editors and database tools (0.0.3)

### Kinds and categories

| Category | Runtime kinds | Service kind? |
| --- | --- | --- |
| language | `node`, `php`, `go`, `java` | yes |
| server | `caddy`, `nginx` | yes |
| database | `mariadb`, `mysql`, `postgres`, `redis` | yes |
| mail (0.0.5) | `mailpit` | yes (not a database: no dumps, no clean stop; it stops with the apps) |
| editor | `vscode`, `vscodium`, `notepadpp` | no: opened with `launch` |
| dbtool | `heidisql`, `dbeaver` | no: opened with `launch` |

`model.KindCategory`, `model.Launchable(kind)` and `model.IsDatabase(kind)` hold this table. The UI mirrors it in `RUNTIME_GROUPS` (`web/src/lib/catalog.ts`).

- **Detection:** `mariadbd`, `mysqld`, `postgres` and `redis-server` are probed with `--version`; `code` / `codium` (the `.cmd` wrappers) too. `mailpit` is probed with `version --no-release-check` (without the flag it asks GitHub for updates); the version is read from `<path> v1.33.0 compiled with ...`.
- **Registering:** a program of kind `notepadpp`, `heidisql`, `dbeaver`, or a `vscode`/`vscodium` `.exe`, is never run to read its version (it would open a window). The version is the one given, or `unknown`.

### Checksum type `md5`

`{"type": "md5", "value": "<32 hex>"}`. The installer picks the algorithm by digest length: 32 is MD5, 64 SHA-256, 128 SHA-512.
- With MD5 the installer log also records the file's SHA-256.
- Import accepts a pasted MD5.
- `shasums` files may contain MD5 lines.
- Only MySQL entries use it: MySQL publishes nothing stronger.

### Catalog sources

| Kind | Source | Checksum |
| --- | --- | --- |
| mariadb | `downloads.mariadb.org/rest-api/mariadb/` (stable lines: the newest rolling one plus LTS, at most 4), then `/{line}/latest/` and its `winx64.zip`. The URL is rewritten from http to https. | sha256 |
| postgres | `enterprisedb.com/download-postgresql-binaries`: the Windows x86-64 `getfile.jsp` link of the 5 newest versions, resolved with one HEAD request to its `get.enterprisedb.com/postgresql/postgresql-X-N-windows-x64-binaries.zip` target | none (unverified) |
| vscode | `update.code.visualstudio.com/api/update/win32-x64-archive/stable/latest` | sha256 (`sha256hash`) |
| redis, vscodium, notepadpp, heidisql, dbeaver, mailpit | GitHub releases (`api.github.com/repos/<repo>/releases?per_page=10`), newest stable assets matching the Windows ZIP pattern. Mailpit names its ZIP `mailpit-windows-amd64.zip` in every release, so its version comes from the tag (`v1.33.0`; other tags are skipped); the newest 3 are kept | sha256 from the asset's `digest`; none means unverified |
| mysql | none: entries are pinned in the built-in manifest (dev.mysql.com pages are not meant for programs) | md5 |

**Archive layouts** (checked against the real archives):

| Kind | Strip | Binary |
| --- | --- | --- |
| mariadb | 1 | `bin/mariadbd.exe` |
| mysql | 1 | `bin/mysqld.exe` |
| postgres | 1 | `bin/postgres.exe` |
| redis | 1 | `redis-server.exe` |
| mailpit | 0 | `mailpit.exe` |
| dbeaver | 1 | `dbeaver.exe` |
| vscode | 0 | `Code.exe` |
| vscodium | 0 | `VSCodium.exe` |
| notepadpp | 0 | `notepad++.exe` |
| heidisql | 0 | `heidisql.exe` |

**Bin holds programs only.** Tool data goes to `<home>/<model.DataDirName(kind)>`: `code`, `codium`, `notepadpp`, `heidisql`, `dbeaver`, and for databases `mariadb`, `mysql`, `postgres`, `redis` (and `mailpit` for Mailpit services). Nothing is created inside `bin`.

### Database services

**Data folder:** `<home>/<kind>/<serviceId>` (e.g. `<home>/postgres/svc_1a2b3c4d`), computed at each start so it follows a moved folder.

**Fixed args** (always added unless `customArgs`; the settings follow them, see "Amendment: settings for each service type"):

| Kind | Args |
| --- | --- |
| mariadb | `--no-defaults --datadir=DATA --port={port} --bind-address=127.0.0.1` (+ `--console` on Windows) |
| mysql | the MariaDB args + `--mysqlx=OFF` |
| postgres | `-D DATA -p {port} -c listen_addresses=127.0.0.1` |
| redis | `--port {port} --bind 127.0.0.1 --dir DATA` (forward slashes) |

**First start:** when DATA is missing or empty, the database files are created before the server starts.

| Kind | Setup command |
| --- | --- |
| mariadb | `mariadb-install-db --datadir=DATA` (or `mysql_install_db`) |
| mysql | `mysqld --no-defaults --initialize-insecure --datadir=DATA` |
| postgres | `initdb -D DATA -U postgres -A trust -E UTF8` |
| redis | create the folder only |

- Each command is the sibling of the server binary. It runs with a 2-minute timeout, and its output goes to the service log as `system` lines.
- If it fails, `start` answers `409`, the half-made folder is removed and nothing is started.
- With `customArgs` there is no setup: the user manages the data folder.

**Stop:** a database first gets its own shutdown command.

| Kind | Shutdown command |
| --- | --- |
| mariadb | `mariadb-admin` (or `mysqladmin`) `--no-defaults -u root --host=127.0.0.1 --port=P --protocol=tcp shutdown` |
| mysql | `mysqladmin`, same arguments |
| postgres | `pg_ctl stop -D DATA -m fast -w` (default args only) |
| redis | `redis-cli -h 127.0.0.1 -p P shutdown` |

- The process tree is ended only if the server has not exited `DBGrace` (20 s) after that. Other services keep the immediate stop.
- `StopWait` allows twice the grace.
- Daemon shutdown waits up to 30 s.

**Deleting** a database service keeps its data folder.

### Mailpit services (0.0.5)

Kind `mailpit` (category `mail`) catches the e-mails apps send.

- **Port:** `port` is the web inbox (default 8025). The health check asks for `/livez` when the service sets no `healthPath` (`kinds.Profile.healthPath`, `kinds.HealthPath`).
- **Setting:** `smtpPort` (number, default `1025`), where apps send mail. `maxMessages` (default `500`, `0` keeps all) becomes `--max`.
- **Data folder:** `<home>/mailpit/<serviceId>` (`dataDir: service`), created before the start.
- **Fixed args:** `--listen 127.0.0.1:{port} --smtp 127.0.0.1:{s.smtpPort} --database {dataSlash}/mailpit.db --smtp-auth-accept-any --smtp-auth-allow-insecure --disable-version-check`. No login is needed, and a client that sends one anyway is let in.
- **Start:** `409` "SMTP port N is already in use" when the SMTP port is taken, and `409` when it equals the web port. With `customArgs` the SMTP port is not checked.
- **Stop:** no shutdown command; it stops in the apps tier of Stop all.

### `POST /api/runtimes/{id}/launch` body `{ "path"?: string, "serviceId"?: string }` -> 204

Opens an `editor` or `dbtool` runtime. The program is detached: not in the supervisor's job object, not waited for, and it keeps running when the daemon stops.

**Errors:**

| Status | When |
| --- | --- |
| `400` | The runtime is not launchable; `path` is relative or missing; an editor got a `serviceId`; the service is not a database the tool can open (Redis is not supported) |
| `404` | Unknown runtime or service |
| `409` | The binary is gone |
| `501` | Launching is not available |

**Editors:** `args = [path]`, working dir = `path`, or its folder when `path` is a file.

**Tool data** (only for runtimes with `source: "installed"`; registered programs are the user's own installs and get nothing):

| Kind | Redirect | Folder |
| --- | --- | --- |
| vscode, vscodium | env `VSCODE_PORTABLE=<dir>` (portable mode with the data outside the program folder). The install also gets `<install>/data`, a directory junction to `<dir>`, so starts outside Zero2Dev use the same data; it is made on install, checked on every launch and at daemon start, and removed (link only) before the version's folder is deleted. A real `data` folder there is left alone. | `<home>/code`, `<home>/codium` (`user-data`, `extensions`) |
| notepadpp | `-settingsDir=<dir>` | `<home>/notepadpp` |
| heidisql | `--psettings=<dir>/portable_settings.txt`, created empty if missing | `<home>/heidisql` |
| dbeaver | `-data <dir>/workspace`. This moves only DBeaver's workspace; DBeaver still writes some files to its default location. | `<home>/dbeaver` |

These options come before the path or connection arguments.

**Database tools with `serviceId`:**
- HeidiSQL: `--nettype=0` (MariaDB/MySQL) or `8` (PostgreSQL, plus `--databases=postgres`), `--description=<name> --host=127.0.0.1 --port=P --user=<root|postgres>`. The long options take `=` in every version.
- DBeaver: `-con "driver=<MariaDB|MySQL|PostgreSQL>|host=127.0.0.1|port=P|user=<user>|name=<service name>|connect=true"` (+ `|database=postgres`).

## Amendment: settings for each service type (0.0.3)

Each service type now says which fields it uses and which settings it has. The descriptions are in `internal/kinds/profiles.json`. Three parts read that one file:
- The daemon embeds it and builds command lines from it.
- The UI draws the service form from it.
- The mock daemon reads it too.

### `GET /api/kinds` -> 200 `KindProfile[]`

One profile per service kind:

```jsonc
{
  "kind": "mariadb", "label": "MariaDB",
  "summary": "A MySQL-compatible database server.",   // one line for the type picker
  "managed": false,                       // true for caddy/nginx: created by Zero2Dev, not offered for new services
  "note": "",                             // shown at the top of the form
  "folder": { "use": "hidden" },          // or { "use": "required", "label": "Project folder", "hint": "..." }
  "port": { "use": "required", "default": 3306, "hint": "..." },   // use: required | optional | hidden
  "health": false,                        // whether a health check path applies
  "dataDir": "service",                   // service = <home>/<kind>/<id>; shared = <home>/<kind>; none
  "extraArgs": "Extra arguments", "extraArgsHint": "...",
  "fixed": [ { "args": ["--datadir={data}"], "help": "...", "windows": false } ],   // always added
  "env": [ { "name": "npm_config_cache", "value": "{home}/node/npm-cache", "help": "..." } ],
  "options": [ /* KindOption */ ]
}
```

`KindOption`:

| Field | Meaning |
| --- | --- |
| `key`, `label`, `group`, `help`, `placeholder` | What the form shows |
| `common` | Shown up front; the others are under "More settings" (a changed or invalid one is always shown) |
| `type` | `text`, `number`, `bool` (`"true"`/`"false"`), `select`, `multi` (comma-separated), `size`, `lines` (several lines) |
| `default` | The value when the service has none |
| `choices` | `{value, label, args?}` for `select`/`multi`. `args`, when present, is what that choice writes; an empty list writes nothing |
| `choicesFrom` | `phpExtensions`: the choices come from `GET /api/runtimes/{id}/php-extensions` |
| `suggest` | `npmScripts`: suggestions from `GET /api/project/scripts` |
| `min`, `max`, `pattern`, `required` | Checks |
| `show` | `{otherKey: value}` conditions for when the option is visible. `"*"` means set (not empty, not false). For a `multi` option the condition means "contains" |
| `minVersion` | The oldest runtime version the option works with. Older versions leave it out, with a warning |
| `at` | `"end"`: written after the other options (the script, the JAR, the package) |
| `emit` | What the option writes (see below) |

`emit` fields:

| Field | Writes |
| --- | --- |
| `arg` | Argument templates, written when the value is set or the bool is true |
| `argOff` | Arguments written when a bool is false |
| `ini` | A php.ini key |
| `env` | An environment variable set to the value |
| `envSet` | For a bool: variables set when it is true |
| `program` | `{value: "npm"}`: run that program from the runtime's folder instead of the runtime binary |
| `special` | Handled in code: `phpExtensions`, `phpIniBase`, `phpIniLines` |

**Placeholders:**

| Placeholder | Replaced by |
| --- | --- |
| `{v}` | The option's value |
| `{s.<key>}` | Another setting's value |
| `{port}` | The service port |
| `{home}` | The data folder, with forward slashes |
| `{data}` | The service's data folder; also works in custom args |
| `{dataSlash}` | The service's data folder, with forward slashes |
| `{cwd}` | The project folder |
| `{binDir}` | The program's folder, with forward slashes |
| `{ini}`, `{docroot}` | PHP only: the php.ini path and the document root |

### Service fields

| Field | Meaning |
| --- | --- |
| `settings` | `Record<string,string>` of option values. Missing keys use their defaults, so options added later never break a saved service. A bad value is `400`, and the message names the setting's label (see below) |
| `customArgs` | `true`: `args` are the whole argument list, and the settings are not used. `false`: `args` are extra arguments, written after the generated ones; for an npm script, `--` goes before them |
| `cwd` | Required only for kinds whose `folder.use` is `required`. Databases and the web servers run in their data folder, or the program's folder |

These `settings` values are refused with `400`:
- unknown keys
- values of the wrong type
- values outside the choices or range
- line breaks (except in `lines` options)
- NUL characters

**Command line, in order:**
1. The fixed arguments.
2. The visible options, in profile order; those with `at: "end"` go last.
3. `args`.

**Environment, in order** (later entries win):
1. Zero2Dev's variables from the settings.
2. The service's own `env`.
3. `PORT`.

**Compatibility:**
- A request with `args` and no `settings` key comes from a client older than settings. It gets `customArgs: true`, so its args stay the whole command line.
- Services saved before settings existed are migrated the same way when the state file is loaded.
- The managed "Nginx (sites)" service has `customArgs: true`.
- A custom command never has `customArgs`.

### `POST /api/services/preview` body `ServiceInput` (+ optional `id`) -> 200 `CommandPreview`
### `GET /api/services/{id}/command` -> 200 `CommandPreview`

Both return the exact command line, without starting anything or writing any file:

```json
{ "program": "C:\\...\\php.exe", "args": ["-c", "...\\php\\svc_1\\php.ini", "-S", "127.0.0.1:8000", "-t", "C:\\work\\shop\\public"],
  "env": ["PORT=8000"], "workdir": "C:\\work\\shop", "dataDir": "...\\php\\svc_1",
  "files": ["...\\php\\svc_1\\php.ini"], "warnings": [] }
```

- `env` lists only the variables Zero2Dev adds.
- `files` are written before each start.
- Without `id`, the data folder uses `svc_new`.

In the preview, these problems become `warnings` instead of errors:
- no runtime installed
- a missing program
- an invalid setting
- an option that needs a newer runtime version
- a PHP extension that is not in the ext folder

### PHP: the service's own php.ini

A PHP service runs:

```
php -c <data>/php.ini -S 127.0.0.1:{port} -t <project folder>/<document root> [router]
```

The php.ini is written on every start, in this order. Later lines win.

1. The runtime's own `php.ini-development` or `php.ini-production`, chosen by the `iniBase` setting. `none` skips it.
2. A `[PHP]` section with:
   - `extension_dir`: the absolute `ext` folder next to php.exe. PHP's Windows default, `C:\php\ext`, is wrong for Zero2Dev's layout.
   - `session.save_path`: `<data>/sessions`.
   - `upload_tmp_dir` and `sys_temp_dir`: `<data>/tmp`.
3. The settings with `emit.ini`: `memory_limit`, `upload_max_filesize`, `post_max_size`, `max_execution_time`, `display_errors`, `date.timezone`, and the Xdebug settings when Xdebug is ticked.
4. One line per ticked extension:
   - `extension = php_<name>.dll`, or `zend_extension` for OPcache and Xdebug.
   - OPcache also gets `opcache.enable = 1` and `opcache.enable_cli = 1`.
   - Xdebug also gets `xdebug.output_dir = <data>/xdebug`.
5. The `iniLines` setting. Each line must be `name = value` or a `;` comment.

### `GET /api/runtimes/{id}/php-extensions` -> 200

```
{ runtimeId, version, extDir, managed, extensions: [{name, file, zend}], xdebug }
```

- `managed` is false when there is no `ext` folder next to php (a system PHP). That PHP's own php.ini then decides the extensions.
- `400` for a runtime that is not PHP.

### `POST /api/runtimes/{id}/xdebug` (no body) -> 202 Job

Downloads Xdebug for a PHP runtime. The job has `type: "xdebug"`, and its `runtimeId` is the PHP runtime.

1. Read `https://xdebug.org/download`. Each link carries its SHA-256 in its title: `title="SHA256:&nbsp;<hex>"`.
2. Pick the newest stable DLL (no alpha, beta or RC) that is non-thread-safe, x86_64, and built for the PHP version's `major.minor`. Only links to xdebug.org that carry a SHA-256 count.
3. Download it and check the SHA-256. A mismatch fails at `Verifying` and installs nothing.
4. Put it into the runtime's `ext` folder as `php_xdebug.dll`, retrying while antivirus holds the file.

**Errors:**

| Status | When |
| --- | --- |
| `400` | The runtime is not PHP, has no `ext` folder, is older than PHP 8.0, or is not a Windows build |
| `409` | Xdebug is already downloading for the same runtime |

### `GET /api/project/scripts?dir=<absolute path>` -> 200

Returns the script names in `<dir>/package.json`, sorted:

```json
{ "scripts": ["build", "dev", "start"] }
```

- The list is empty when there is no readable package.json.
- `400` for a relative `dir`.

## Amendment: theme and accent (0.0.3)

`Settings` gains two fields for the dashboard's look. `PUT /api/settings` accepts them alone, as a partial body, and changing them restarts nothing.

```json
{ "theme": "system", "accent": "#1677ff" }
```

| Field | Values | Default |
| --- | --- | --- |
| `theme` | `"system"` (follow the OS), `"light"` or `"dark"` | `"system"` (also for an empty value in older state files) |
| `accent` | A `#rrggbb` colour, the seed the UI builds its palette from | `"#1677ff"` |

These are refused with `400 bad_request`:
- a `theme` other than the three values: `theme must be system, light or dark`;
- an `accent` that is not `#` and six hex digits: `accent must be a colour like #1677ff`.

The UI keeps a copy of the built theme in `localStorage` (`zero2dev.theme`) only to avoid a flash before the settings load. The daemon's value wins.

## Amendment: quitting from the UI (0.0.3)

### `POST /api/shutdown` (no body) -> 202 `{ "stopping": true }`

Stops every running service (databases cleanly, as on Ctrl+C) and exits the daemon. The answer is sent first; the daemon quits about 200 ms later. Behind the token like every route; `501` when the daemon was built without a quit handler. The tray icon's **Quit Zero2Dev** and Ctrl+C take the same path.

A second launch acquires neither the home lock nor the listening port and does not modify shared files. It verifies the running instance using `GET /.well-known/zero2dev-instance?challenge=<64 hex characters>`. This Host-checked public endpoint returns hex HMAC-SHA256 of `zero2dev-instance:<challenge>` keyed by the token. The second launch verifies this proof locally without sending the token, then opens the dashboard and exits with code 0.

## Amendment: Stop all (0.0.4)

### `POST /api/services/stop-all[?project=<id>]` (no body) -> 200 `{ "stopped": ["svc_..."], "failed": [{ "id", "name", "error" }] }`

Stops every running service and keeps Zero2Dev running (unlike `POST /api/shutdown`; services can be started again straight away). It answers when the services have stopped or timed out.

- **Order:** apps first (custom, Node, PHP, Go, Java), then databases, then the site server (Caddy, nginx). Services of one group stop at the same time; a group is finished before the next one starts, so an app never loses its database while it is still running. Databases still run their own shutdown command first. Since the start-order amendment below, a service that others depend on (`dependsOn`, `databaseId`) also waits until nothing still running depends on it; with no dependencies set, the order is exactly the one above.
- `?project=<id>` stops only the services of that project (the same order among them). Sites, the site server and every other service are left alone. An unknown project id is `404 not_found`.
- `stopped` lists the services that were running and are now stopped; `failed` those that did not stop (still starting, or not gone in time). Both are arrays, empty when nothing was running. The status is `200` even when `failed` is not empty.
- A service that is waiting for a crash restart (see below) is cancelled too, so Stop all never races a restart timer.
- Behind the token like every route. The dashboard's and the Services page's **Stop all** buttons, a project's **Stop**, and the tray menu's **Stop all services** use it.

## Amendment: projects, start order, crash restart, environment (0.0.4)

Binding for daemon and UI. Everything not listed here is unchanged, except that this **replaces** "No auto-restart in v1" under *Service* and the "Environment, in order" list under *settings for each service type*. Old `state.json` files load unchanged (see *Migration*).

### New Service fields

Added to `Service` and to `ServiceInput` (the body of create / update; every field is optional and a missing one means "not set"):

| Field | Type | Meaning |
| --- | --- | --- |
| `dependsOn` | `string[]` | Ids of the services that must be running before this one starts. Never contains the service itself or a duplicate. At most 64. |
| `databaseId` | `string` | Id of a database service (`mariadb`, `mysql`, `postgres`, `redis`) this service uses, or `""`. It is a dependency too (see `model.Deps`: `dependsOn` plus `databaseId`) and it gives the service the database variables below. |
| `envFile` | `string` | A `.env` file, relative to `cwd`, for any service kind. Replaces the Node-only `envFile` setting. |
| `restartOnCrash` | `bool` | Start the service again when it exits by itself. Default `false`. |
| `restartLimit` | `int` | Most restarts in a row, 1-20. `0` or missing on input means the default, 5. Always 1-20 in a response. |
| `restarts` | `int` | **Output only**, never accepted: how many crash restarts the current streak has had; `0` when none, and again `0` once the service has run for 60 s. |

```json
{
  "id": "svc_a1b2c3d4", "name": "My API", "kind": "node", "port": 3000,
  "dependsOn": ["svc_0f0f0f0f"], "databaseId": "svc_9e9e9e9e",
  "envFile": ".env.local", "restartOnCrash": true, "restartLimit": 5, "restarts": 0,
  "state": "running"
}
```

These are refused with `400 bad_request` (the message names the problem):

| Problem | Message |
| --- | --- |
| a service lists itself in `dependsOn` | `a service cannot depend on itself` |
| `dependsOn` names an unknown id | `dependency <id> not found` |
| more than 64 entries | `too many dependencies (max 64)` |
| `databaseId` is itself, unknown, or not a database | `a service cannot use itself as its database`, `database <id> not found`, `service "<name>" is not a database` |
| a dependency loop (`dependsOn` and `databaseId` both count) | `these services depend on each other in a loop: A -> B -> A` |
| `envFile` is absolute, has a drive or starts with a slash | `envFile must be a path relative to the service folder` |
| `envFile` contains a `..` segment | `envFile must stay inside the service folder (no ..)` |
| `restartLimit` outside 1-20 (0 is the default) | `restartLimit must be 1-20` |

Deleting a service removes it from every `dependsOn` and `databaseId`. A `PUT` is still refused with `409` while the service runs.

### Start order

#### `POST /api/services/start-all[?project=<id>]` (no body) -> 200 `{ "started": ["svc_..."], "failed": [{ "id", "name", "error" }] }`

Starts every service that is not active (or the services of one project), together with the inactive services they depend on, dependencies first. It answers when the services are running, have failed, or `HealthTimeout` (30 s) has passed for a wave.

- **Waves:** the services whose dependencies are all running are ready; the ready ones are started one after another, highest stop tier first (site server, then databases, then apps). A wave is awaited only when something still waiting depends on one of its services; a service nothing waits for counts as `started` as soon as it is spawned.
- Services that are already starting or running are left alone and are not listed. A running dependency counts as satisfied.
- A service that fails to start is listed in `failed` with the reason. Every service that depends on it is listed too, with `error: "waits for <name>"`, and is not started. The status is `200` even when `failed` is not empty. Both arrays are never `null`.
- `?project=<id>` follows the project rules below. An unknown project id is `404 not_found`.
- A dependency loop cannot be saved (see above). If one is found anyway, the services in it fail with `waits for a service that waits for it (dependency loop)`.

#### `POST /api/services/{id}/start` -> 202 `Service`

Still answers `202` with the service in state `starting`, but first starts every inactive dependency (and theirs), each one waited for until it is running or failed. With no inactive dependency it answers as quickly as before; otherwise it blocks, up to about `HealthTimeout` per level of the dependency chain. The target itself is never waited for.

- If a dependency fails to start the answer is `409 conflict` (`<dependency> did not start, so <service> was not started: <reason>`) and the service is not started.
- A dependency that is already starting is waited for.
- `Start` of the target alone keeps its old errors (`409` for a busy port, a missing runtime, "already running").

#### Stop order

`stop-all` (above) and a project's stop use the reverse order: a service stops only when no active service in the same stop depends on it, and among those that are free, the lowest tier goes first. Dependents outside the stop set (the other services of a project, for example) do not hold a service back.

### Projects

A project is a named group of services and sites. It is kept in `state.json` (`projects`).

```json
{ "id": "prj_1a2b3c4d", "name": "Shop", "serviceIds": ["svc_a1b2c3d4", "svc_9e9e9e9e"], "siteIds": ["site_deadbeef"] }
```

IDs have the prefix `prj_`. `ProjectInput` is `{ "name", "serviceIds", "siteIds" }`; both lists may be missing or empty.

| Method | Path | Body | Success |
| --- | --- | --- | --- |
| GET | `/api/projects` | none | `Project[]` (`[]` when there are none) |
| POST | `/api/projects` | `ProjectInput` | `201 Project` |
| PUT | `/api/projects/{id}` | `ProjectInput` | `Project` (full replacement) |
| DELETE | `/api/projects/{id}` | none | `204`. Only the group is removed; its services and sites are not touched and keep running. |

- `name` is required, 1-64 characters (`400 name must be 1-64 characters`). Every id must exist (`400 service <id> not found`, `400 site <id> not found`); duplicates are dropped. An unknown project id is `404 not_found`.
- Deleting a service or a site removes it from every project (the project itself stays).
- **Start** (`POST /api/services/start-all?project=<id>`): switches the project's sites on with one config write and one site-server restart, makes sure the site-server service (Caddy, or the managed nginx service, as set in Settings) is among the services to start when the project has at least one site, then starts the members with their dependencies like `start-all`. Members that were deleted meanwhile are skipped. If the sites cannot be switched on, the services still start, and `failed` gets an extra entry `{ "id": "", "name": "Sites", "error": "could not enable the sites: ..." }`.
- **Start order:** the order of `serviceIds` is the project's start order, and it is kept as sent (duplicates dropped).
  - The daemon's sequence is that list, except that a member moves down to just after the last member it needs (`dependsOn` and `databaseId`, transitively, also through services that are not members). Members in a dependency loop keep their list order.
  - Start goes through the sequence one member at a time. Each one is awaited until it is `running`, has failed, or `HealthTimeout` has passed, before the next starts. Its inactive dependencies start before it as usual.
  - A failed member does not fail the members after it, unless they need it. In that case they are reported as `"waits for <name>"`. The site server is not part of the sequence and starts in the first wave.
- **Stop** (`POST /api/services/stop-all?project=<id>`): stops the project's services only, one at a time in the reverse of the start sequence. Sites, the site server and every other service keep running.
- Event: `projects.changed` (data `{}`) after create, update and delete.

### Switching all sites

#### `POST /api/sites/enable-all[?project=<id>]` and `POST /api/sites/disable-all[?project=<id>]` (no body) -> 200 `{ "changed": 2 }`

Switches every site (or the sites of one project) on or off. `changed` is the number of sites whose state changed; a site that already had that state is not counted. All changes are one store update, one config write and **one** restart of the running site server, instead of one restart per site. When `changed` is `0`, nothing is written and the site server is not restarted. An unknown project id is `404 not_found`; a project without sites changes nothing (`{ "changed": 0 }`). Sites are not validated again, as for `POST /api/sites/{id}/enable`. Event: `sites.changed`.

### Restart on crash

A service **crashes** when it ends without being stopped: it exits by itself (with any exit status, 0 included) or never answers its health check in time. Stopping it, Stop all, deleting it or closing the daemon is never a crash.

When `restartOnCrash` is true (read from the service's current definition at the time of the crash):

- The supervisor starts it again after a growing delay: 1 s, 2 s, 4 s ... doubling, never more than 60 s. The delay before restart number *n* is `min(1 s * 2^(n-1), 60 s)`.
- After `restartLimit` restarts in a row it gives up: the service stays `failed` and the log says `giving up: restarted N times and it keeps failing`.
- The counter starts again at zero when a run has lasted at least 60 s (and `restarts` reads `0` while the service has been up that long).
- A restart that cannot even start the process (the port is taken, the folder is gone) counts as the next crash.
- Each restart is logged as a `system` line (`restarting in 2s (attempt 2/5)`).
- A pending restart is cancelled by Stop, Stop all, a manual Start (which also resets the counter), deleting the service and closing the daemon. The timer re-checks that nothing changed before it starts the service.

`state` is `failed` between a crash and the restart, and the service's `lastError` holds the exit message.

**Event `service.crashed`**, sent for every crash (also for a service without restart on crash):

```json
{ "id": "svc_a1b2c3d4", "name": "My API", "error": "exit status 1", "attempt": 2, "limit": 5, "retryInSec": 2, "gaveUp": false }
```

| Field | Meaning |
| --- | --- |
| `attempt` | The restart now planned, 1-based. With `gaveUp`, equal to `limit`. |
| `limit` | The service's `restartLimit`; **`0` when the service has no restart on crash** (so no restart is planned). |
| `retryInSec` | Seconds until the restart; `0` when `gaveUp` or when no restart is planned. |
| `gaveUp` | `true` when the limit is reached and nothing more will be started. |

`service.status` events and `Service` objects carry `restarts`. The tray shows a balloon on the first crash of a streak (`<name> crashed`, "Zero2Dev is starting it again in N seconds"), when the supervisor gives up (`<name> keeps crashing`) and when a service without restart on crash stops unexpectedly (`<name> stopped unexpectedly`); the restarts in between show no balloon. The dashboard keeps a notice per crashed service until the service is started again by hand or the notice is dismissed.

### The environment of a service

The environment of the child process, **later entries win**:

1. The daemon's own environment, with the runtime's folder put first on `PATH` (as before).
2. `JAVA_HOME` (Java) or `GOROOT` (Go), when the service runs on a runtime Zero2Dev installed or registered: the folder two levels above the binary (`<home>\bin\java.exe` gives `<home>`). Other kinds get nothing.
   **Go tools (0.0.5):** a `go` service with `isolate` on (the default) gets `GOPATH=<home>\go\path` and `<home>\go\path\bin` first on `PATH`, before the runtime's folder, so what `go install` puts there (`air`, `dlv`, `gopls`) is found. With `isolate` off GOPATH is left alone and Go's default bin folder (the first entry of the daemon's `GOPATH`, else `%USERPROFILE%\go\bin`) is put on `PATH` instead. A `custom` service gets each of the two folders that exists. The command preview and Open terminal show the same.
3. The kind's own variables from its settings (`NODE_ENV`, `npm_config_cache` ...).
4. The database variables of `databaseId`.
5. The variables of the `.env` file.
6. The service's own `env` (with `{port}` replaced).
7. `PORT` (when `port > 0`).

Caddy's `XDG_*` and `TMP*` variables are added last, as before.

**Database variables.** Zero2Dev's databases listen on `127.0.0.1` and have no password, so the password is always empty.

| Database | Variables |
| --- | --- |
| MariaDB, MySQL | `DB_CONNECTION=mysql`, `DB_HOST=127.0.0.1`, `DB_PORT=<port>`, `DB_USERNAME=root`, `DB_USER=root`, `DB_PASSWORD=` (empty), `DATABASE_URL=mysql://root@127.0.0.1:<port>/` |
| PostgreSQL | `DB_CONNECTION=pgsql`, `DB_HOST`, `DB_PORT`, `DB_USERNAME=postgres`, `DB_USER=postgres`, `DB_PASSWORD=` (empty), `DATABASE_URL=postgresql://postgres@127.0.0.1:<port>/postgres` |
| Redis | `DB_CONNECTION=redis`, `DB_HOST`, `DB_PORT`, `DB_PASSWORD=` (empty), `REDIS_URL=redis://127.0.0.1:<port>` (no user name, no `DATABASE_URL`) |

A link that cannot be used (the service is gone, is not a database, has no port) adds a warning to the preview and the start log and sets nothing.

**`.env` file** (`envFile`, relative to `cwd`):

- Lines are `KEY=VALUE`, optionally with an `export ` prefix. Blank lines and lines starting with `#` are ignored. Keys are letters, digits, `_`, `.` and `-`, and do not start with a digit.
- An unquoted value ends at a space followed by `#` (an inline comment) and is trimmed. Double quotes understand `\n`, `\r`, `\t`, `\"` and `\\`; single quotes are literal. A quoted value may span lines. There is no `$VAR` expansion. A key set twice keeps the last value.
- A line that cannot be read is skipped with a warning (the line number, never the value). A file over 1 MB is not used (warning).
- A **missing or unreadable file is only a warning**: the service starts without it. A path that leaves the service folder (a `..` segment, or an absolute path from an edited `state.json`) refuses the start with `400`; the preview and Open terminal show it as a warning.
- The start log says `loaded N variables from <file>`. The values are never logged.

**`POST /api/services/preview` and `GET /api/services/{id}/command`** list the whole added environment in `env` (the runtime home, kind variables, database variables, `.env` variables, own variables, `PORT`), in the order above. The value of a `.env` variable is shown as `(hidden)` when its name contains `PASS`, `SECRET`, `TOKEN`, `KEY`, `CREDENTIAL`, `AUTH`, `PRIVATE`, `SIGNATURE`, `COOKIE` or `SESSION`, or when it is a URL with `user:password@`. The process still gets the real value. Problems with the `.env` file or the database link appear in `warnings`.

The Node setting `envFile` (`--env-file`) no longer exists in `GET /api/kinds`; the `.env` file is a service field now.

### Open terminal

#### `POST /api/services/{id}/terminal` (no body) -> 204

Opens a console in the service's folder (the data folder when it has none) with the environment the service would start with: the runtime on `PATH`, `JAVA_HOME` / `GOROOT`, the database variables, the `.env` file, its own variables and `PORT`. Problems that would refuse a start (no runtime installed) are tolerated: the environment is built as far as it can be.

- On Windows it opens **Windows Terminal** (`wt.exe -d <folder> cmd.exe /k`) when `wt.exe` is on `PATH`, otherwise `cmd.exe /k` in a new console window. The console is not supervised: it is not part of the service's job object and it stays open when the daemon stops. Nothing is built from user text; the folder is one argument.
- `404 not_found` for an unknown service. `501` with code `not_supported` where Zero2Dev cannot open a console (every system except Windows). `500` when the console could not be started.
- Not verified in a real window yet: the environment hand-over to Windows Terminal and `cmd.exe` is covered by unit tests only.

### Runtime registration (shims and the user PATH)

The runtimes Zero2Dev installed are not on your Windows `PATH`. Zero2Dev offers two ways to change that: **shims** (always written, nothing outside the data folder changes) and the **Environment** choices below (opt-in, per item, the one exception to "nothing is written to the registry").

**Shims, always on.** `<home>\shims\` holds one small file per command of the **default** runtime of each kind, and three activate scripts. The folder is regenerated at start-up and after every runtime change: install, delete, register and **Make default**. Only the files that changed are rewritten, and only files that carry the line `Generated by Zero2Dev` are ever deleted, so a script you keep there is safe.

| Kind | Commands |
| --- | --- |
| Node | `node`, `npm`, `npx` |
| PHP | `php` |
| Java | `java`, `javac`, `jar` |
| Go | `go`, `gofmt` |
| MySQL | `mysql`, `mysqldump` |
| MariaDB | `mariadb`, `mysql`, `mariadb-dump` |
| PostgreSQL | `psql`, `pg_dump`, `pg_restore` |
| Redis | `redis-cli` |

- Each command is a `<name>.cmd` that runs the tool of the default runtime with the arguments it gets. For `node`, `php`, `java` and `go` the tool is the runtime's own binary; the other commands are looked for next to it and only get a shim when the file exists. Databases get their client tools only, never the server.
- Where two kinds offer the same command (`mysql`), the first in the order Node, PHP, Java, Go, MySQL, MariaDB, PostgreSQL, Redis wins.
- Java and Go shims set `JAVA_HOME` / `GOROOT` for that one run only; the Go shims also set `GOPATH=<home>\go\path`.
- Every `.exe` in `<home>\go\path\bin` and in Go's default bin folder (what `go install` built: `air`, `dlv`, `gopls`) gets a shim `<name>.cmd` that runs it, unless a runtime's shim or Zero2Dev's own folder has the name. They are written by the same refresh as the others, and by **Apply** on the Environment page.
- `activate.cmd`, `activate.ps1` and `activate.sh` put the runtimes' folders first on `PATH` and set `JAVA_HOME` / `GOROOT` **for the terminal they run in**. With a Go runtime they also set `GOPATH` and put `<home>\go\path\bin` (and Go's default bin folder when it exists) on `PATH`, so a tool installed after the last refresh works there at once. Use `call "<home>\shims\activate.cmd"` in Command Prompt, `. '<home>\shims\activate.ps1'` in PowerShell and `. <home>/shims/activate.sh` in Git Bash.

### The Environment (folders and variables, one by one)

Which folders go on your Windows user `PATH` and which user variables get set is a list of **items**, and every item has its own `enabled` switch. Nothing is on by default. The old single `registerPath` setting is gone: `PUT /api/settings` no longer accepts it.

`GET /api/environment` and `PUT /api/environment` both answer:

```json
{
  "config": {
    "seeded": true,
    "path": [
      { "id": "envp_shims", "source": "shims", "enabled": true },
      { "id": "envp_1a2b3c4d", "source": "runtime", "runtimeId": "rt_1", "enabled": false },
      { "id": "envp_5e6f7a8b", "source": "custom", "dir": "C:\\tools\\bin", "enabled": true }
    ],
    "vars": [
      { "id": "envv_java_home", "name": "JAVA_HOME", "source": "default", "kind": "java", "enabled": true },
      { "id": "envv_9c8d7e6f", "name": "NODE_HOME", "source": "runtime", "runtimeId": "rt_2", "enabled": false },
      { "id": "envv_0a1b2c3d", "name": "MY_FLAG", "source": "custom", "value": "1", "enabled": true }
    ]
  },
  "report": {
    "supported": true,
    "shimsDir": "D:\\zero2dev\\data\\shims",
    "path": [ { "id": "envp_shims", "dir": "D:\\zero2dev\\data\\shims", "present": true, "owned": true } ],
    "vars": [ { "id": "envv_java_home", "name": "JAVA_HOME", "value": "D:\\zero2dev\\data\\bin\\jdk-21", "current": "D:\\zero2dev\\data\\bin\\jdk-21", "hasCurrent": true, "owned": true } ]
  }
}
```

**Folder items** (`path`) have a `source`:

| `source` | The folder is | Fields |
| --- | --- | --- |
| `shims` | `<home>\shims`, with `node`, `npm`, `php`, `java`, `go` ... of the default version of each runtime (at most one item) | none |
| `runtime` | the folder of one runtime's program, so only that version's tools | `runtimeId` |
| `custom` | a folder you name (a full path such as `C:\tools\bin`) | `dir` |

**Variable items** (`vars`) have a `name` and a `source`:

| `source` | The value is | Fields |
| --- | --- | --- |
| `default` | the install folder of the **default** version of a kind (`JAVA_HOME` style for Java and Go, the program's folder for the others). It moves with **Make default** | `kind`: `node`, `php`, `java`, `go`, `mysql`, `mariadb`, `postgres` or `redis` |
| `runtime` | the same, for one version you pick | `runtimeId` |
| `custom` | a value you type (at most 2000 characters) | `value` |

On a fresh data folder three items exist, all off: the shims folder, `JAVA_HOME` (default Java) and `GOROOT` (default Go). `seeded` records that, so an item you delete does not come back.

**`PUT /api/environment`** takes `{ "path": [...], "vars": [...] }`, replaces the whole config and applies it. An item without an `id` gets one; the ids you send must look like `envp_...` (folders) or `envv_...` (variables). `seeded` is accepted and ignored. Validation answers `400 bad_request` with the reason and saves nothing:

- a variable name must be letters, digits and `_`, not start with a digit and be at most 64 characters; names are unique ignoring case; `PATH` (it has the folder list) and variables Windows or Zero2Dev depend on (`PATHEXT`, `COMSPEC`, `SYSTEMROOT`, `WINDIR`, `TEMP`, `TMP`, `USERPROFILE`, `APPDATA` ... and `ZERO2DEV_HOME`, `ZERO2DEV_PORT`) are refused;
- a folder must be a full path without `%`, `;` or `"`, and no folder is listed twice;
- a `runtime` item whose runtime is gone is an error only when it is switched on;
- at most 50 folders and 50 variables.

**What applying does** (HKCU only, no administrator rights):

- Zero2Dev's folders are one block on the user `Path`, **in the order of the list** (the first item first, so it wins a command both folders have):
  - the block goes where Zero2Dev's first folder was, or at the **start** when there was none;
  - reordering the list reorders the block, and if you moved the block by hand it stays where you put it;
  - the value keeps its registry type (`REG_EXPAND_SZ` and its `%VARS%` stay; a new `Path` is `REG_EXPAND_SZ`), and the rest of it is not touched;
  - a folder you already had on `Path` is yours: it is not added twice and it is never removed.
- Variables that are switched on are set **only when you have none of that name**. A variable you set yourself is never overwritten; the item then reports `owned: false` and a different `current`. While Zero2Dev's own value is still there it is kept in step: **Make default** moves a `default` variable, and a runtime you delete takes its variables and folder away.
- Switching an item **off** (or deleting it from the list) takes back only what Zero2Dev added: its folder leaves `Path`, and its variable is deleted if it still holds the value Zero2Dev set.
- `<home>\shims\.registry-backup.json` is the record of what Zero2Dev owns and what `Path` held before the first change: `{ "version": 2, "path": { "existed", "type", "value", "owned": [folders] }, "vars": { "NAME": "<value Zero2Dev set>" } }`. It is written before the first change, and removed when Zero2Dev owns nothing any more. Without it nothing is ever taken back. A version 1 file from a 0.0.4 development build is read as owning the shims folder.
- If a write fails the writes already done are undone, `PUT` answers `409 conflict` with `cannot change the user environment: <reason>` and **nothing is saved**, so the config always matches the registry. Off Windows, switching anything on answers `409` the same way.
- `WM_SETTINGCHANGE` is broadcast when something changed, so terminals opened afterwards see it. Open terminals keep what they had.
- After every runtime change (install, delete, register, **Make default**) the same reconcile runs again, but only when something is switched on or Zero2Dev still owns something. A failure there is only logged, because the runtime change itself has succeeded.
- Refused: a `Path` value that is not a string type.

**`report`** is what the user environment holds now, read-only, per item: `present` / `owned` for folders (`owned`: Zero2Dev added it) and `commands`, the commands the folder gives a terminal (sorted; for the shims folder its shims, for a runtime its program and the tools next to it, for your own folder its `.exe` / `.cmd` / `.bat` files, at most 20), `current` / `hasCurrent` / `owned` for variables, `dir` / `value` for what the item stands for ("" when it cannot be worked out, with a `problem` such as `No java runtime is installed.` or `The program of Java 21 is not on disk.`). `supported` is `false` off Windows.

Event: `environment.changed` (no data) after a `PUT` and after every runtime change; clients reload `GET /api/environment`.

**Caveats.**
- The registry code is tested against a fake registry. A check on a real Windows machine is still pending.
- The `.cmd` shims and `activate.cmd` are read with the OEM code page, so a data folder whose path has non-ASCII characters can break them.
- PowerShell's default execution policy can block `activate.ps1`; `Set-ExecutionPolicy -Scope Process Bypass` in that window allows it for that window only.
- If you move the Zero2Dev folder while a folder item is on, the old entry stays on your `Path`. Switch the items off first.


### Events

| Event | Data |
| --- | --- |
| `service.status` | now also `restarts` |
| `service.crashed` | `{ id, name, error, attempt, limit, retryInSec, gaveUp }` |
| `projects.changed` | `{}` |
| `environment.changed` | `{}` (after `PUT /api/environment` and after every runtime change) |

### Migration

`store.normalize` runs on load and after every update. A `state.json` from 0.0.3 loads unchanged: `projects` becomes `[]`, `dependsOn` `[]`, `databaseId` and `envFile` `""`, `restartOnCrash` `false`, `restartLimit` 5. The `envFile` entry of a Node service's `settings` moves to the new `envFile` field (and leaves `settings`). Ids in `dependsOn`, `databaseId` and projects that no longer exist, and a `databaseId` that is not a database, are dropped.

## Amendment: data, databases and environment snapshots (0.0.5)

Binding for daemon and UI. Everything not listed here is unchanged. Old `state.json` files load unchanged: the new fields default to "not set".

Each subsection below is owned by one part of the work. The details marked *(to be completed)* are filled in by that part, without changing what is already written.

### Shared: jobs, new fields, events

- **Jobs.** Database imports and exports, snapshots, restores, clones and migrations are jobs, like runtime installs.
  - The route answers `202 Job`.
  - Progress comes as `job.progress { id, percent, step }` and the end as `job.done`. `GET /api/jobs/{id}` answers for every job.
  - `job.done` now also carries `type`, `serviceId`, `projectId`, `snapshotId`, `migrationId` and `path`.
- **`Job`** gains optional fields, missing when not used:
  - `serviceId`;
  - `projectId` (for a clone, the new project, set when done);
  - `snapshotId`, `migrationId`;
  - `path`: the file a `db-export` wrote.

  New `type`s: `db-import`, `db-export`, `snapshot`, `snapshot-restore`, `snapshot-clone`, `migrate`, `migrate-revert`.
- **One job per thing.** While a job works on a service, a project, a snapshot or a program, a second one on it is `409 conflict` with `details.jobId`.
- **Busy services.** While a snapshot, restore, reset or migration holds a service, starting, editing or deleting it is `409` "`<name> is busy (<what>); try again when it is done`".
- **`Service.adminPassword`** (`string`, `""` for none) is the database administrator's password, and it is in every `Service` response.
  - It is **not** part of `ServiceInput`: create and update ignore it and keep the stored value.
  - Only `PUT .../database/password` changes it, and a reset clears it.
- **`Settings.portable`** `{ "npmGlobal": false, "composer": false, "goCache": false }` says which runtime caches and global package folders live in the data folder. Only Import changes it; `PUT /api/settings` ignores it.
- **New events:** `snapshots.changed` `{ ownerId }` and `migrations.changed` `{}`. A restore or a clone also publishes `services.changed`, `sites.changed` and `projects.changed`.

### File dialog

#### `POST /api/dialog/file` -> 200 `{ "path": "C:\\dumps\\shop.sql", "cancelled": false }`

Body (all optional): `{ "title", "start", "save": false, "defaultName": "shop.sql", "filters": [{ "name": "SQL dumps", "pattern": "*.sql;*.sql.gz" }] }`.

It shows the native Open dialog on the daemon's PC (or Save as, with `save: true`) and answers when the dialog is closed. When the user cancels, the answer is `cancelled: true` and `path: ""`.

Errors:
- `400` for:
  - a title over 100 characters or with a line break;
  - more than 10 filters, or a filter without a name or a pattern;
  - a `defaultName` with a slash, a colon or a line break.
- `409` while another file or folder dialog is open.
- `501 not_supported` off Windows.

Like the folder dialog, it is a convenience: every path field also accepts typed text.

### Databases

For services of kind `mariadb`, `mysql`, `postgres` and `redis`. Any other kind is `400` "`<name> is not a database`".
- **Client programs** (`mariadb`/`mysql`, `mariadb-dump`/`mysqldump`, `psql`, `pg_dump`, `redis-cli`) come from the folder of the service's server binary.
- **Passwords** are passed to them in `MYSQL_PWD`, `PGPASSWORD` or `REDISCLI_AUTH`, never on the command line.
- **Logging:** what each operation ran goes to the service's log as `system` lines, without passwords.

**Names.** A database or user name is 1-64 letters, digits or `_`: `400` "`database name must be 1-64 letters, digits or _`" (and "`user name must be ...`"). The system databases (`mysql`, `information_schema`, `performance_schema`, `sys`, `postgres`, `template0`, `template1`) cannot be created, imported into, exported or dropped through these routes.

| Method | Path | Body | Success | Needs |
| --- | --- | --- | --- | --- |
| GET | `/api/services/{id}/database` | none | `DatabaseInfo` | - |
| POST | `/api/services/{id}/database/databases` | `{ "name" }` | `201 { name }` | running; not redis |
| POST | `/api/services/{id}/database/users` | `{ "user", "password", "database" }` (`database` optional: all privileges on it) | `201 { user, database }` | running; not redis |
| POST | `/api/services/{id}/database/import` | `{ "database", "filePath", "create": false }` | `202 Job` (`db-import`) | running; not redis |
| POST | `/api/services/{id}/database/export` | `{ "database", "filePath", "overwrite": false }` | `202 Job` (`db-export`) | running; not redis |
| POST | `/api/services/{id}/database/reset` | none | `200 Service` | stopped |
| PUT | `/api/services/{id}/database/password` | `{ "password" }` (`""` removes it) | `200 Service` | running |

```json
{
  "serviceId": "svc_9e9e9e9e", "kind": "mariadb", "running": true, "user": "root", "hasPassword": false,
  "databases": [{ "name": "shop", "sizeBytes": 1048576 }], "users": ["root", "shop"],
  "supports": { "databases": true, "dumps": true, "password": true, "reset": true },
  "dataDir": "D:\\zero2dev\\data\\mariadb\\svc_9e9e9e9e"
}
```

`databases` and `users` are `[]` while the server is stopped, and always for Redis. For Redis, `supports.databases` and `supports.dumps` are `false`.

- **Needs running / stopped:** `409` "`start <name> first`" or "`stop <name> first`".
- **Import:**
  - `filePath` is an absolute path on the daemon's PC to a plain `.sql` dump or a gzip-compressed one. Gzip is recognised by its first bytes, whatever the file is called. A missing file is `400`.
  - A database that does not exist is `400`, unless `create: true` creates it first.
  - The job has one step, `Importing`, whose percent follows the bytes read.
  - A SQL error fails the job with the server's message. Whatever was imported before the error stays.
- **Export:**
  - Writes `filePath` (absolute; gzip when it ends in `.gz`) through a temporary `<filePath>.tmp`, renamed at the end, so a failed export never leaves half a file.
  - An existing file is `409` "`<file> exists`", unless `overwrite: true`.
  - MariaDB/MySQL use `--single-transaction --routines --triggers`; PostgreSQL uses `pg_dump --no-owner`.
  - `job.path` is the file that was written.
- **Reset:**
  - Deletes the data folder. The next start sets the database up again as on its first start: empty, no password.
  - Clears `adminPassword`.
  - Refused while a job works on the service.
- **Password:**
  - MariaDB/MySQL: `ALTER USER` for every `root` account. PostgreSQL: the `postgres` user. Redis: `requirepass`, and from then on Zero2Dev starts Redis with `--requirepass`.
  - After that, these use it:
    - the clean stop;
    - the connection variables: `DB_PASSWORD`, `DATABASE_URL` (with the password URL-escaped) and `REDIS_URL`;
    - **Open in** HeidiSQL / DBeaver;
    - the command preview, which shows it as `********`.
  - PostgreSQL still accepts local connections without a password (`trust`). The password is there for apps and tools that insist on one.

#### Client programs and their arguments

Found in the server binary's folder, then its `bin\` subfolder, then the sibling `..\bin\` folder (with `.exe` on Windows). MariaDB tries `mariadb` before `mysql` and `mariadb-dump` before `mysqldump`; MySQL the other way round. They run without a shell and without a console window, and every value is one argument written with `=`.

| What | Program and arguments |
| --- | --- |
| Connection (MariaDB/MySQL) | `--no-defaults --host=127.0.0.1 --port=<port> --protocol=tcp --user=root --default-character-set=utf8mb4`, with `MYSQL_PWD` always set (empty without a password) |
| Connection (PostgreSQL) | `--host=127.0.0.1 --port=<port> --username=postgres --no-password`, with `PGCLIENTENCODING=UTF8` and `PGPASSWORD` when there is a password |
| Connection (Redis) | `-h 127.0.0.1 -p <port>`, with `REDISCLI_AUTH` when there is a password |
| Listing, `CREATE DATABASE`, `CREATE USER`, `ALTER USER`, `DROP DATABASE` | `mariadb <connection> --batch --skip-column-names [--database=<db>]` or `psql <connection> --no-psqlrc --quiet --tuples-only --no-align --field-separator=<tab> --set=ON_ERROR_STOP=1 --dbname=<db or postgres>`. The SQL is written to the program's stdin, so a password inside it is never on a command line. These runs time out after one minute. |
| Import | `mariadb <connection> --max-allowed-packet=1073741824 --database=<db>` or `psql <connection> --no-psqlrc --quiet --set=ON_ERROR_STOP=1 --dbname=<db>`, with the (unzipped) dump on stdin. No time limit. |
| Export | `mariadb-dump <connection> --single-transaction --routines --triggers --max-allowed-packet=1073741824 <db>` or `pg_dump <connection> --no-owner --format=plain --encoding=UTF8 --dbname=<db>`, stdout written to `<filePath>.tmp` (through gzip for `.gz`). No time limit. |
| Redis password | `redis-cli <connection> -x CONFIG SET requirepass`, with the new password on stdin (`-x` reads the last argument from stdin). Any answer but `OK` is an error. |

The SQL Zero2Dev sends:
- **Create a database:** ``CREATE DATABASE `shop` CHARACTER SET utf8mb4;`` or `CREATE DATABASE "shop" ENCODING 'UTF8';`.
- **Create a user (MariaDB/MySQL):** `CREATE USER` for both `'app'@'%'` and `'app'@'localhost'` (so an anonymous `''@'localhost'` account cannot shadow it), then ``GRANT ALL PRIVILEGES ON `shop`.*`` to both, then `FLUSH PRIVILEGES`.
- **Create a user (PostgreSQL):** `CREATE ROLE "app" LOGIN PASSWORD '...'` and `GRANT ALL PRIVILEGES ON DATABASE "shop"`. Then, inside `shop`: `GRANT ALL` on the schema `public` and on all of its tables and sequences, since PostgreSQL 15 and later no longer let every role create tables there.
- **Password (MariaDB/MySQL):** `ALTER USER 'root'@'<host>' IDENTIFIED BY '...'` for every host in `SELECT Host FROM mysql.user WHERE User = 'root'`, then `FLUSH PRIVILEGES`. Removing it uses `IDENTIFIED BY ''`.
- **Password (PostgreSQL):** `ALTER USER postgres WITH PASSWORD '...'`, or `WITH PASSWORD NULL`.
- **Quoting:** names are checked against `^[A-Za-z0-9_]{1,64}$` and quoted with backticks or double quotes. In MariaDB/MySQL strings, `\` becomes `\\` and `'` becomes `''`. In PostgreSQL strings, `'` becomes `''` (`standard_conforming_strings` is on).
- **Passwords** are 1-128 characters on one line (`""` only for `PUT .../password`). The password of a new user is required.
- **Listings** leave out the system databases, and for MariaDB/MySQL also the empty, `PUBLIC`, `mysql.*` and `mariadb.*` accounts. PostgreSQL lists the roles that can log in. Sizes are `data_length + index_length` per schema, or `pg_database_size`.

Each operation writes `system` lines to the service's log: `<what>: <program> <arguments>` before it runs, and `<what> failed: <message>` when it fails. A password in a failure message is replaced by `********`.

#### Error messages

| Status | Message | When |
| --- | --- | --- |
| 404 | `service <id> not found` | |
| 400 | `<name> is not a database` | any other kind |
| 400 | `<name> is Redis, which has no databases to create` (`users to create`, `dumps to import`, `dumps to export`) | Redis on a SQL route |
| 400 | `database name must be 1-64 letters, digits or _` / `user name must be 1-64 letters, digits or _` | |
| 400 | `<db> is a system database; Zero2Dev does not change it` | |
| 400 | `password must be 1-128 characters on one line` (new user) / `password must be at most 128 characters on one line` (admin password) | |
| 400 | `database <db> does not exist` | export, a user's grant |
| 400 | `database <db> does not exist; create it first` | import without `create` |
| 400 | `filePath must be an absolute path on this PC` | |
| 400 | `<file> does not exist` / `<file> is a folder, not a dump file` | import |
| 400 | `folder <dir> does not exist` / `<file> is a folder` | export |
| 409 | `start <name> first` / `stop <name> first` | |
| 409 | `database <db> exists` / `user <user> exists` | |
| 409 | `<file> exists` | export without `overwrite` |
| 409 | `<name> has no port` | |
| 409 | `the <kind> program <program>.exe was not found in <folder>; this <kind> build does not include its client programs` | missing client |
| 409 | `another job is already working on this (<type>, <jobId>)`, with `details.jobId` | a second import or export |
| 409 | `a job is working on <name> (<jobId>); try again when it is done`, with `details.jobId` | reset during an import or export |
| 409 | `<name> is busy (<what>)` | reset while a snapshot, restore or migration holds it |
| 409 | `<name> has its own command line, so Zero2Dev does not know its data folder; reset it yourself` | reset with `customArgs` (`supports.reset` is `false`, `dataDir` is `""`) |
| 409 | `cannot delete <dir>: <error>; is a program still using it?` | reset |
| 409 | `redis-cli: <reply>` | Redis did not answer `OK` |
| 500 | the client's message, e.g. `mariadb: ERROR 1045 (28000): Access denied for user 'root'@'localhost'` | a client program failed (the last 8 lines of its stderr) |

A failed job's `error` is `importing into <db> failed: <message>` or `exporting <db> failed: <message>`.

#### Password threading

- **Clean stop:** `mariadb-admin` / `mysqladmin` get `MYSQL_PWD`, and `redis-cli shutdown` gets `REDISCLI_AUTH`. The password is read when the stop runs, so one set while the server runs is used.
- **Redis:** `--requirepass <password>` is added at the end of `redis-server`'s arguments. The start log and the command preview show it as `--requirepass ********`.
- **Connection variables:** `DB_PASSWORD=<password>`, `DATABASE_URL=mysql://root:<escaped>@127.0.0.1:<port>/` (or `postgresql://postgres:<escaped>@…/postgres`), and `REDIS_URL=redis://:<escaped>@127.0.0.1:<port>`. The command preview shows `DB_PASSWORD=********` and `…://root:********@…`.
- **Open in:** HeidiSQL gets `--password=<password>`; DBeaver gets `|password=<password>` in `-con`, except when the password contains `|`, which `-con` cannot carry (DBeaver then asks for it).
- **First start:** when Zero2Dev creates new MariaDB, MySQL or PostgreSQL data files (for example, after the folder was deleted by hand), a stored password is cleared, because the new database has none. `services.changed` is published.

#### Copying from another server (`dbtools.ServerCopier`, used by Import from this PC)

- `ListRemote` and `CopyFromServer` take the server's `kind`, `host` (default `127.0.0.1`), `port`, `user` (default `root` or `postgres`) and `password`. The client programs come from `ClientDir`, else the folder of the default runtime of that kind (MariaDB and MySQL also try each other's), else the destination service's folder.
- Each database is dumped with the export arguments above and piped straight into the import. No temporary file is written.
- The databases must not exist in the destination yet: `409` "`database <db> already exists in <name>`". Kinds must match (MariaDB/MySQL into MariaDB/MySQL, PostgreSQL into PostgreSQL): `400` otherwise.
- Progress steps are `Copying <db>`, then `Copied`. When a copy fails, the error is the side that failed first (`dumping <db> from <host>:<port> failed: …` or `importing <db> failed: …`), and the databases this call created are dropped again.
- `DropDatabases` runs `DROP DATABASE IF EXISTS` for each name.

### Snapshots

A snapshot is a named copy of either:
- a **project**: its services and their settings, the runtime each one used, its generated `php.ini` files, its sites, and the data folders of its database services;
- one **database service**: its settings and its data folder.

Snapshots live in `<data>\snapshots\<ownerId>\<snapshotId>\` and move with the data folder. Ids have the prefix `snp_`.

| Method | Path | Body | Success |
| --- | --- | --- | --- |
| GET | `/api/projects/{id}/snapshots` | none | `Snapshot[]`, newest first |
| POST | `/api/projects/{id}/snapshots` | `{ "name", "restart": false }` | `202 Job` (`snapshot`) |
| GET | `/api/services/{id}/snapshots` | none | `Snapshot[]` (database services only) |
| POST | `/api/services/{id}/snapshots` | `{ "name", "restart": false }` | `202 Job` (`snapshot`) |
| GET | `/api/snapshots/{snap}` | none | `SnapshotDetail` |
| POST | `/api/snapshots/{snap}/restore` | `{ "restart": false }` | `202 Job` (`snapshot-restore`) |
| POST | `/api/snapshots/{snap}/clone` | `{ "name", "domainSuffix": "-copy" }` | `202 Job` (`snapshot-clone`) |
| DELETE | `/api/snapshots/{snap}` | none | `204` |

```json
{
  "format": 1, "id": "snp_1a2b3c4d", "name": "Before the PHP 8.4 upgrade", "scope": "project", "ownerId": "prj_5e6f7a8b",
  "createdAt": "2026-11-20T10:00:00Z", "version": "0.0.5", "sizeBytes": 73400320,
  "project": { "id": "prj_5e6f7a8b", "name": "Shop", "serviceIds": ["svc_1", "svc_2"], "siteIds": ["site_1"] },
  "sites": [{ "id": "site_1", "name": "Shop", "domain": "shop.localhost", "...": "..." }],
  "services": [
    { "service": { "id": "svc_1", "name": "Shop DB", "kind": "mariadb", "...": "..." },
      "runtime": { "kind": "mariadb", "version": "11.4.4", "catalogId": "mariadb-11.4.4", "binary": "D:\\zero2dev\\bin\\mariadb-11.4.4\\bin\\mariadbd.exe", "followedDefault": true },
      "hasData": true, "phpIni": false }
  ],
  "warnings": []
}
```

`SnapshotDetail` is a `Snapshot` plus `"missing": [{ "kind", "version", "catalogId", "installable", "services": ["Shop API"] }]`: the runtimes it needs that are not installed now.

- **Name:** 1-64 characters (`400`). Two snapshots of one owner may share a name.
- **Taking:**
  - Every service of the snapshot must be stopped; otherwise `409` names the running ones.
  - With `restart: true`, Zero2Dev stops them in the project's stop order, takes the snapshot, and starts again the ones that were running.
  - A database service with its own command line (`customArgs`) has no known data folder. Its settings are kept but its data is not, and `warnings` says so.
  - Steps: `Stopping`, `Copying` (percent follows the bytes copied), `Starting`.
- **Restoring** brings the project back as it was:
  - Every service in the snapshot gets its stored settings back; a deleted one is created again with its old id.
  - Each service is pinned to the runtime it used (`runtimeId`), so a later change of the default does not affect it.
  - Data folders and `php.ini` files are put back, and the sites come back.
  - Services and sites added to the project since are taken out of the project, not deleted.
  - `adminPassword` gets the value from the snapshot.
  - Restoring a service snapshot restores only that service.
  - `409` while a needed runtime is missing (`details.missing`), or while a service runs and `restart` is false.
  - A failed restore puts the data folders back as they were.
- **Cloning** works only on project snapshots (`400` otherwise). It creates a new project named `name`:
  - New ids for the project, its services and its sites. Every link between them (`dependsOn`, `databaseId`, a site's service, the project's lists) points at the copies.
  - Database data is copied into the copies' own data folders.
  - Each copy gets a free port: the next port that no other service uses.
  - Site domains get `domainSuffix` added to their first label (`shop.localhost` -> `shop-copy.localhost`), made unique with `-2`, `-3` and so on.
  - The project folders (`cwd`) are not copied, so both projects point at the same code.
  - `job.projectId` is the new project.
- **Deleting** a project or a service keeps its snapshots. Restoring one of them brings the project or service back.

#### Snapshot jobs in detail

| Job | Steps, in order | `job` fields |
| --- | --- | --- |
| `snapshot` | `Stopping` (only with `restart` and something running), `Copying` (percent = bytes copied), `Starting` (only when something was stopped) | `snapshotId`; `projectId` or `serviceId` |
| `snapshot-restore` | `Stopping` (as above), `Restoring` (percent = bytes copied back), `Starting` (as above) | `snapshotId`; `projectId` or `serviceId` (the owner) |
| `snapshot-clone` | `Copying` (percent = bytes copied), `Creating` | `snapshotId`; `projectId` = the new project, set when done |

- **One job per owner.** A snapshot and a restore of the same project or service share one job slot (`409 conflict` with `details.jobId`). Two clones of one snapshot do too.
- **While a job runs**, the services it works on are held (`Reserve`): starting, editing or deleting them is `409` "`<name> is busy (taking a snapshot)`" or "`(restoring a snapshot)`".
- **Taking:**
  - It is written to `<snapshotId>.partial` and renamed when complete. A failed snapshot leaves nothing behind.
  - A service whose runtime cannot be resolved is kept, with a warning `<name>: its runtime could not be resolved (...), so restoring will not pin one`.
  - A service with its own command (`command`) or of kind `custom` has `runtime: null`.
  - A service that ran a program found on PATH has a `runtime` with only `kind`, `binary` and `followedDefault: true`. A restore does not pin it, and it is missing only when that file is gone.
  - A database whose data folder does not exist yet (never started) has `hasData: false` and no warning.
  - Each service's log gets a `system` line: `snapshot "<name>" (<id>) taken`.
- **Restoring:**
  - The runtime check matches the recorded runtime against what is installed by `catalogId`, then by `kind` + `version`, then by `binary` (a registered program). The match is what `runtimeId` is pinned to.
  - `missing[].catalogId` is the catalog entry that would install it when `installable` (it may differ from the recorded one when only the kind and version match). `missing[].services` names the services that need it.
  - Each data folder is renamed to `<folder>.restore-old`, the snapshot's copy goes in its place, and the old one is deleted only after the state is saved. If anything fails, every data folder and `php.ini` is put back and the state is not changed.
  - Each service's log gets `snapshot "<name>" (<id>) restored`.
- **Cloning:**
  - The copies keep their names, folders, settings and `adminPassword` (the copied data has that password). Each one is pinned to the recorded runtime when it is installed.
  - **Links:** `dependsOn` and `databaseId` that point at a service outside the project still point at it. A proxy site without a `serviceId` whose `upstreamPort` was a member's port gets the copy's port.
  - **Ports:** the next port after the old one (wrapping round to 1024) that no service, site or proxy port setting uses, and that can be listened on at that moment.
  - **Domains:** a first label longer than 63 characters is shortened so the suffix fits.
  - `php.ini` files are not copied: a PHP service writes its own on every start.
- **Errors** (`error.message`):

  | Status | When | Message |
  | --- | --- | --- |
  | `400` | name | `name must be 1-64 characters` |
  | `400` | service snapshot of a non-database | `<name> is not a database` |
  | `400` | clone of a service snapshot | `only project snapshots can be cloned` |
  | `400` | bad `domainSuffix` | `domainSuffix must be 1-32 letters, digits or -, and must not end with -` |
  | `404` | take | `project <id> not found`, `service <id> not found` |
  | `404` | get, restore, clone, delete | `snapshot <id> not found` |
  | `409` | running, without `restart` | `<names> is/are running; stop it/them first, or take the snapshot with restart` (restore: `... or restore with restart`); `details.running` = the ids, comma-separated |
  | `409` | runtime missing | `the snapshot needs <kind version>, which is/are not installed; install it/them first`; `details.missing` = `"php 8.3.14, mariadb 11.4.4"` (the list is in `GET /api/snapshots/{snap}`) |
  | `409` | a snapshot site's domain is taken | `domain <domain> is now used by site "<name>"; change or delete that site first` |
  | `409` | delete while a restore or clone uses it | `snapshot "<name>" is in use (restoring it); try again when it is done` |
  | `409` | leftover from a crashed restore | `<folder>.restore-old is left over from an earlier restore; move it away first` |
  | `409` | clone | `no free port for the copy of <name>` |
  | job failed | copy error | `the snapshot could not be taken: ...`, `the snapshot could not be restored: ...`, `the snapshot could not be cloned: ...` |

- `GET /api/services/{id}/snapshots` of a service that exists and is not a database is `400`. For an id that names nothing (a deleted owner), both lists answer the snapshots that are left, or `[]`.

### Import from this PC (migrations)

Copies or moves the settings and data of programs installed on this PC into Zero2Dev's portable folders, and back. Every migration is recorded in `<data>\migrations\<migrationId>\` (`migration.json` plus the backups). Ids have the prefix `mig_`.

| Method | Path | Body | Success |
| --- | --- | --- | --- |
| GET | `/api/migrations/sources` | none | `MigrateSource[]` |
| POST | `/api/migrations/databases` | `{ "kind", "host", "port", "user", "password" }` | `{ "databases": [{ "name", "sizeBytes" }] }` |
| POST | `/api/migrations` | `MigrateRequest` | `202 Job` (`migrate`) |
| GET | `/api/migrations` | none | `Migration[]`, newest first |
| POST | `/api/migrations/{id}/revert` | `{ "how": "discard" \| "copy-back" \| "move-back" }` | `202 Job` (`migrate-revert`) |
| POST | `/api/migrations/{id}/cleanup` | none | `202 Job` (`migrate-cleanup`): removes the originals of a finished copy |
| DELETE | `/api/migrations/{id}/backups` | none | `200 Migration`: deletes `backup-target\` and `backup-original\` |
| DELETE | `/api/migrations/{id}` | none | `204`: removes a reverted or failed migration and its backups |

**Added after the first 0.0.5 draft.**
- `MigrateSource.targetFreeBytes`: free bytes on the drive the data goes to (the bin folder's drive for a `runtime`); `0` when unknown.
- `Migration` gains `finishedAt` (when its last job ended), `backupBytes` (what the backup folders hold), `canCleanup` and `originalsBytes` (what a clean-up frees). The last three are worked out on every `GET`.
- **Clean-up** removes the originals a copy left behind. It is `409` unless the migration is a `done` copy of a `dir`, `file` or `registry` program whose originals are still there (never for `runtime` or `database`), or while the program runs (`Close X first`) or a job runs. Every copy is verified first (file counts and bytes; for VS Code's extensions the file count, since `extensions.json` was rewritten); one mismatch fails the job with `nothing was removed: ...` and touches nothing. A registry key is saved (`registry.json`, `.reg`) before it is deleted. Afterwards the record is a move: `mode: "move"`, `revertOptions: ["move-back"]`, so Revert puts the originals back from Zero2Dev's copy.
- **Delete backups** is `409` while a job runs on the migration. The portable data and the originals are not touched; a later revert has nothing to bring back from them.
- **Delete a record** is `409` unless the state is `reverted` or `failed` with `revertOptions: []`.

```json
{
  "id": "vscode", "program": "vscode", "label": "VS Code", "found": true,
  "items": [
    { "label": "Settings", "kind": "dir", "from": "C:\\Users\\me\\AppData\\Roaming\\Code\\User", "to": "D:\\zero2dev\\data\\code\\user-data\\User", "sizeBytes": 2048000, "exists": true },
    { "label": "Extensions", "kind": "dir", "from": "C:\\Users\\me\\.vscode\\extensions", "to": "D:\\zero2dev\\data\\code\\extensions", "sizeBytes": 412000000, "exists": true }
  ],
  "modes": ["copy", "move"], "copyOnlyReason": "", "blockers": [], "targetHasData": false,
  "note": "Logins kept by Windows (Settings Sync) are not moved."
}
```

Sources (`program`): `vscode`, `vscodium`, `notepadpp`, `heidisql`, `dbeaver`, `runtime`, `npm-global`, `npm-cache`, `go-cache`, `composer`, `database`. There is one `runtime` source per registered runtime, with the `id` `runtime:<runtimeId>`.

`MigrateRequest` is `{ "source": "vscode", "mode": "copy", "items": ["Settings"], "database": null }`. An empty `items` means all of them. For `database`, it is `"database": { "kind", "host", "port", "user", "password", "databases": ["shop"], "targetServiceId": "svc_..." }`.

- **Copy-only sources:** `runtime` and `database` have `modes: ["copy"]`, and `copyOnlyReason` says why. `mode: "move"` for them is `400`.
  - A runtime's files belong to the system installer.
  - Zero2Dev does not remove databases from a server it does not own.
- **Blockers**, such as "Install VS Code in Zero2Dev first" or "Close VS Code first", make `POST /api/migrations` a `409` with the blocker as the message.
- **Backups:**
  - Whatever the portable place held before is saved first. `targetHasData` says whether it holds something.
  - A move or a copy-back saves the originals before touching them.
  - A move to another drive copies, checks the copy, and only then deletes the original.
- **Revert** (`revertOptions` lists what a migration accepts):
  - `move-back` puts moved items back, saving first whatever is there by then.
  - `discard` removes the portable copy and brings back what the portable place held before.
  - `copy-back` copies the portable data over the originals, saved first.
  - An adopted runtime goes back to the registered program it came from.
  - A database copy drops the databases it created in the Zero2Dev service.
- `Settings.portable` is switched on by a migration of `npm-global`, `composer` or `go-cache`, and off by its revert.

#### Items per program

`<data>` is the data folder. `items[].label` is what `MigrateRequest.items` names.

| Program | Item (`label`, `kind`) | From | To |
| --- | --- | --- | --- |
| `vscode` | `Settings` (dir) | `%APPDATA%\Code\User` | `<data>\code\user-data\User` |
| | `Extensions` (dir) | `%USERPROFILE%\.vscode\extensions` | `<data>\code\extensions` |
| `vscodium` | `Settings` (dir) | `%APPDATA%\VSCodium\User` | `<data>\codium\user-data\User` |
| | `Extensions` (dir) | `%USERPROFILE%\.vscode-oss\extensions` | `<data>\codium\extensions` |
| `notepadpp` | `Settings` (dir) | `%APPDATA%\Notepad++` | `<data>\notepadpp` |
| `dbeaver` | `Workspace` (dir) | `%APPDATA%\DBeaverData\workspace6` | `<data>\dbeaver\workspace` |
| `heidisql` | `Sessions and settings` (registry) | `HKEY_CURRENT_USER\Software\HeidiSQL` | `<data>\heidisql\portable_settings.txt` |
| `npm-global` | `Global packages` (dir) | `%APPDATA%\npm` | `<data>\node\global` |
| `npm-cache` | `Download cache` (dir) | `%LOCALAPPDATA%\npm-cache` | `<data>\node\npm-cache` |
| `go-cache` | `Module cache` (dir) | `%USERPROFILE%\go\pkg\mod` | `<data>\go\path\pkg\mod` |
| | `Build cache` (dir) | `%LOCALAPPDATA%\go-build` | `<data>\go\cache` |
| `composer` | `Composer home` (dir) | `%APPDATA%\Composer` | `<data>\composer` |
| `runtime` | `Program` (runtime) | the runtime's installation folder | `<bin>\adopted\<kind>-<version>` |
| `database` | `Databases` (databases) | `<kind>://<host>:<port>` | the target service's name |

The targets are the folders Zero2Dev's own editors use (`VSCODE_PORTABLE`, `-settingsDir`, `--psettings`, `-data`). The Go folders are the ones the Go setting "Keep Go's downloads and build cache in the data folder" uses.

- **Found, size, target:**
  - `found` is true when at least one item exists, and the database source is always found.
  - `sizeBytes` is the size of the files, or for HeidiSQL the size of the file it would write.
  - Only items that exist are imported. When none of the chosen items exists, the answer is `409` "`nothing of <label> was found on this PC`".
  - `targetHasData` means a target is a file with bytes in it, or a folder that is not empty.
- **Blockers:**
  - "`Install <program> in Zero2Dev first`": the editors and database tools need a Zero2Dev-installed runtime of that kind.
  - "`Close <program> first`": the process check looks for `Code.exe`, `VSCodium.exe`, `notepad++.exe`, `heidisql.exe` or `dbeaver.exe`, and for an adopted editor its own exe.
  - "`<label> was already imported (mig_...); revert that first`": only one live migration per source. The database source has no such limit.
  - Runtimes: "`The program <binary> is missing`", or a folder that is not the program's own (a drive root, the Windows folder, a shim folder such as `chocolatey\bin` or `scoop\shims`).
  - Databases: "`Add a MariaDB, MySQL or PostgreSQL service in Zero2Dev first`", or "`Database tools are not available in this build`".
- **Other errors:**
  - `400`: `mode` other than `copy`/`move`, an unknown item label, or a `move` of a copy-only source.
  - `404`: an unknown `source`, or an unknown migration on revert.
  - `409`: a blocker; a second job on the same source (`details.jobId`); a revert of a reverted migration or of one that is still running.
  - Revert with a `how` not in `revertOptions` is `400`.
- **Revert options:**
  - A copy offers `["discard", "copy-back"]` and a move offers `["move-back"]`.
  - A runtime or database migration offers `["discard"]`.
  - A failed migration is rolled back at once. It is listed as `failed` with `revertOptions: []`. When the rollback also failed, it keeps its options, so the user can finish the job.
- **Order of an import:**
  1. The current target is moved to `backup-target\<n>-<label>\`.
  2. A copy is copied and then checked: the same number of files and bytes.
  3. A move is a rename on one volume. Across volumes it is copied, checked, and the original is deleted only then. If the delete fails, the original is put back from the copy.
- **What each revert does:**
  - **discard** deletes the target and puts `backup-target` back.
  - **copy-back** first moves whatever is at the original place to `backup-original\<n>-<label>\`. Then it copies the portable data there. The portable copy stays, and the migration is `reverted`.
  - **move-back** saves the original place the same way, moves the portable data back, and then puts `backup-target` back.
- **VS Code extensions.** `extensions\extensions.json` holds absolute paths, which are rewritten to the new folder, and back on copy-back and move-back:
  - `location.fsPath`, with the drive letter in lower case;
  - `location.path` (`/d:/...`);
  - `location.external` (`file:///d%3A/...`), when it was there.

  An entry is rewritten when it has a `relativeLocation`, or when its location is inside the old folder.
- **`migration.json`** is the `Migration` plus `format: 1`, the steps with their backup paths, the adopted runtime's paths, and the database server (`kind`, `host`, `port`, `user`; never the password). While the job runs, the record has `state: "running"` and is left out of `GET /api/migrations`. A record still `running` after a restart is listed as `failed`.

#### HeidiSQL's settings format

HeidiSQL keeps its settings in `HKCU\Software\HeidiSQL`. Its portable build, and its "Export settings" command, use a text file instead: UTF-8 (a BOM is accepted on reading), one value per line, CRLF between lines:

```
MainWinMaximized<|||>3<|||>1
Servers\Local\Host<|||>1<|||>127.0.0.1
Servers\Local\StartupScript<|||>1<|||>line1<{{{><}}}>line2
```

- The fields are separated by `<|||>`: the value's path relative to the HeidiSQL key (subkeys joined with `\`), the type, and the value.
- The type is Delphi's `TRegDataType`:
  - `1` is a string. CR is written as `<{{{>` and LF as `<}}}>`.
  - `3` is an integer: a `REG_DWORD` as a signed decimal (`0xFFFFFFFF` is `-1`).
- `REG_EXPAND_SZ` is written as a string. Other types (binary, multi-string, QWORD) cannot go in the file. They are left out of it and kept in the backup.
- After writing the file, Zero2Dev reads it back and compares it with the key.
- **Move:** before the key is deleted (`RegDeleteTree`), it is saved in `backup-original\<n>-sessions-and-settings\` as:
  - `registry.json`, every value with its raw bytes;
  - `HeidiSQL.reg`, a Registry Editor 5.00 file that can be double-clicked to restore the key.
- **Move-back** writes the file's values (with what HeidiSQL changed since) into the key again. The values the file could not hold come from `registry.json`. Whatever the key held by then is saved first.
- **Copy-back** writes the file's values over the key, after saving it.

Only keys below `HKCU\Software\` are ever opened. The registry is reached through `advapi32.dll`, with no dependencies.

#### Runtime adoption

- **Sources.** There is one `runtime:<id>` source per runtime with `source: "registered"`, except binaries already inside the bin folder.
- **The installation folder** is the binary's folder (symlinks resolved), or the folder above it when that one is called `bin`: Go, Java, MariaDB, PostgreSQL. For example, `C:\Program Files\nodejs\node.exe` gives `C:\Program Files\nodejs`, and `C:\Program Files\Go\bin\go.exe` gives `C:\Program Files\Go`.
- **The copy:**
  - It goes to `<bin>\adopted\<kind>-<version>`, with `-2`, `-3` and so on when the name is taken. A version that is not known is `unknown`.
  - For a database server, a top-level `data` folder is left out: Zero2Dev keeps its own data.
  - The copy is checked like any other copy.
- **The runtime keeps its `id`.** Only these change:
  - `binary` becomes the same file inside the copy;
  - `source` becomes `"installed"`.

  Services, defaults and environment entries that name the runtime keep working, and `runtimes.changed` is published. The installed program is not touched.
- **Discard** puts back the registered `binary` and `source: "registered"`, removes the copy, and publishes `runtimes.changed`.
  - It is `409` "`the original program <binary> is gone; keep the adopted copy`" when the original is gone.
  - A runtime deleted in the meantime only has its copy removed.

#### Databases

- **`POST /api/migrations/databases`** and the `database` source check:
  - `kind` (`mariadb`, `mysql` or `postgres`), `host` and `user` are required;
  - `port` is 1-65535;
  - the password has no line breaks.

  Any of these is `400`.
- **The client programs** come from the target service's runtime. For the list, they come from a MariaDB/MySQL (or PostgreSQL) runtime in Zero2Dev, else the answer is `409` "`Install MariaDB or MySQL in Zero2Dev first: its client programs are needed`".
- **Before the job:**
  - Names follow the database rules, and system databases are `400`.
  - The target service must exist (`404`) and be of a matching kind: MariaDB/MySQL for `mariadb`/`mysql`, PostgreSQL for `postgres` (`400`). It must not be the source server itself (`400`).
  - Zero2Dev lists the target's databases (`409` "`cannot reach <service>; start it first`"). A database that already exists there is `409`. That way the revert drops only databases this migration created.
- **The job** calls `CopyFromServer`, and its percent and step are the job's.
- **Discard** calls `DropDatabases` for the copied databases. A service deleted in the meantime has nothing to drop.

#### Portable caches

`profiles.json` gives the `node`, `php` and `go` kinds a `portable` list: `{ setting, env: [{ name, value, help }], path?, help }`, where `{home}` is the data folder. While `Settings.portable.<setting>` is on, a service of that kind (and its terminal) gets these, after the kind's own variables and before the database, `.env` and user variables:

| Setting | Variables | First on `PATH` |
| --- | --- | --- |
| `npmGlobal` | `npm_config_prefix=<data>\node\global` | `<data>\node\global` |
| `composer` | `COMPOSER_HOME=<data>\composer` | `<data>\composer\vendor\bin` |
| `goCache` | `GOMODCACHE=<data>\go\path\pkg\mod`, `GOCACHE=<data>\go\cache` | - |

The preview shows the `PATH` entry as `<folders>;%PATH%`.

## Amendment: languages, tasks and new projects (0.0.6)

New kinds, one-off tasks of a service, project detection, the shipped frameworks list and New project. Everything here is `501 not_supported` until its work is in (Wave 0 left only the shapes).

### Kinds

- **`python`** is a service kind and a runtime kind (category `language`, data folder `<home>\python`). Python is installed from python-build-standalone (`install_only` `.tar.gz`, `stripComponents` 1, binary `python.exe`; it has `venv` and `pip`). A service of this kind runs in a virtual environment of its own at `<home>\python\venvs\<serviceId>`, made before the first start (and again, with a log line, when the runtime changed) and removed with the service.
- **`composer`** is a runtime kind only (category `tool`, data folder `<home>\composer`). It is one downloaded file, `composer.phar`, put in as it is with a `composer.bat` next to it that runs `php "%~dp0composer.phar" %*`. The runtime's binary is `composer.bat`. A task runs Composer with the service's PHP: the program `composer` of a task is resolved to `php <composer.phar>`.
- **Catalog:** a download whose file name ends in `.phar` is a single file, not an archive (the installer's `file` format). Its checksum works as for archives.
- **Job types** `task` and `new-project`. `job.done` carries `serviceId` for a task and `projectId` and `path` (the project folder) for a new project, like the other jobs.
- **Kind profiles** (`GET /api/kinds`, `internal/kinds/profiles.json`) gain `tasks`: `[ { id, label, program, args, help?, confirm?, when?, timeout? } ]`. `program` is run without a shell, first next to the service's runtime (`npm.cmd` beside `node.exe`, `pip.exe` in the virtual environment's `Scripts`), else on the service's `PATH`. `args` may use `{s.<setting>}` and `{port}`. `confirm` is a text the dashboard asks first. `when` is a file or folder that must exist in the service's folder for the task to be offered. `timeout` is in seconds (default 600).

### One-off tasks

| Method | Path | Body | Response |
| --- | --- | --- | --- |
| GET | `/api/services/{id}/tasks` | none | `TaskList` |
| POST | `/api/services/{id}/tasks` | `{ "preset": "npm-install" }` or `{ "program": "npm", "args": ["run", "build"] }` | `202 Job` (`task`) |

`TaskList` is `{ "presets": [kind task ...], "scripts": [ { "name", "command" } ], "running": "job_..." }`. `presets` are the tasks of the service's kind whose `when` exists; `scripts` are the `scripts` of a Node service's `package.json` (run with the service's package manager); `running` is the id of the task job running for the service, `""` when none.

- A task runs in the service's folder with the environment the service starts with (`Supervisor.Environ`: the runtime on `PATH`, `JAVA_HOME`/`GOROOT`, the database variables, the `.env` file, the user's variables). A service that is running may have a task; nothing is stopped.
- **No shell.** The program and each argument are passed as they are. A program is a name without a path or a file below the service's runtime folders; arguments are text without a NUL or a line break.
- Every output line (stdout and stderr, in the order they came) is written to the service's log with the stream `task` and published as `log.line`; the first line is `$ program args`. The job's `step` is the label of the task, its percent stays at 0 until it ends. A non-zero exit fails the job with `<program> exited with code N` and the last output lines.
- **Errors:** `404` unknown service or preset; `400` both or neither of `preset` and `program`, a bad program or argument; `409` a task of the service runs (`details.jobId`), the service is busy (another operation holds it), or a needed program is missing (`composer` is not installed: the message says to install it from Runtimes).
- Timeout: the preset's `timeout`, else 10 minutes; the process tree is ended and the job fails with `timed out after ...`.

### Project detection

`GET /api/project/detect?dir=<folder>` returns

```json
{ "kind": "node", "framework": "nextjs", "name": "my-shop", "confidence": "high",
  "settings": { "package": "..." }, "envFile": ".env", "port": 3000,
  "markers": ["package.json", "next.config.js"] }
```

`kind` is `""` (and the rest empty) when the folder says nothing. The markers are, in order: `composer.json` (`laravel/framework`, `symfony/*`, WordPress files), `package.json` (`next`, `vite`, `@nestjs/core`, `astro`, `express` and its `scripts`), `go.mod` (`gin-gonic/gin`, `gofiber/fiber`), `pom.xml` or `build.gradle` (Spring Boot), `manage.py` / `requirements.txt` / `pyproject.toml` (Django, FastAPI, Flask), `index.html`. `400` for a folder that does not exist. It only reads the folder.

### Frameworks

`GET /api/frameworks` returns `{ "frameworks": [Framework ...] }`, the list shipped with Zero2Dev (`internal/frameworks/frameworks.json`) or `<data>\frameworks.json` when that exists (`409` when it is damaged). Shape (`format: 1`):

```json
{ "id": "laravel", "label": "Laravel", "language": "php", "summary": "...", "homepage": "https://laravel.com",
  "runtime": { "kind": "php", "min": "8.2" }, "composer": true,
  "database": { "kinds": ["mariadb", "mysql", "postgres"], "default": "mariadb", "optional": true },
  "steps": [ { "type": "run", "label": "Creating the Laravel project", "program": "composer", "args": ["create-project", "laravel/laravel", "{slug}"], "in": "parent" } ],
  "services": [ { "name": "{name}", "kind": "php", "port": 8000, "usesDatabase": true } ],
  "site": { "type": "php", "root": "public", "https": true },
  "env": [ { "key": "DB_DATABASE", "value": "{db}" } ], "envFile": ".env" }
```

- `language`: `php`, `javascript`, `python`, `go`, `java`, `static`. `runtime.kind` is a runtime kind or `""`; `min`/`max` are inclusive version bounds.
- Step `type`: `run` (a program with `args`, run in `in` = `parent` (default) or `project`, with the runtime of the framework; `composer`, `npm`, `npx`, `pip` ... are the runtime's own tools), `download` (`url` https, `checksum` `sha256:<hex>`, `sha1:<hex>` or a https URL of a file holding it, `strip`) or `files` (`files`: relative path with `/` to content).
- Placeholders in strings: `{name}` `{slug}` `{dir}` `{parent}` `{port}` `{domain}` `{db}` `{dbUser}` `{dbPassword}` `{dbHost}` `{dbPort}` `{dbUrl}` `{appKey}` `{random}`. Values are checked before use (a name becomes a slug of letters, digits and `-`); arguments are never joined into a shell line.
- `env` lines go to `envFile` (default `.env`); `ifMissing` writes a line only when the key is not there.
- Validation on load: unique ids, known languages and kinds, a `run` step names a program, a `download` step has https and a checksum, `files` paths are relative without `..`, a service that `usesDatabase` needs a `database`, and a framework has a service or a site.

### New project

| Method | Path | Body | Response |
| --- | --- | --- | --- |
| POST | `/api/new-project/plan` | `NewProjectRequest` | `Plan` |
| POST | `/api/new-project` | `NewProjectRequest` | `202 Job` (`new-project`) |

`NewProjectRequest`: `{ "framework": "laravel", "name": "My Shop", "parent": "D:\\www", "databaseKind": "mariadb", "domain": "", "port": 0, "openEditor": true }`. The project folder is `<parent>\<slug>`; `domain` defaults to `<slug>.localhost`; `port` 0 picks a free one.

`Plan` is `{ "dir", "slug", "domain", "port", "installs": [ { "kind", "version", "catalogId", "name", "reason" } ], "database": { "kind", "name", "user", "serviceId" } | null, "steps": ["Installing PHP 8.4.1", ...], "creates": ["Service My Shop (PHP, port 8000)", ...], "problems": [...], "warnings": [...] }`. Nothing is changed. A plan with `problems` cannot be started (`409` with the first problem as the message): the folder exists and is not empty, the parent does not exist, the domain or port is taken, no runtime version fits the framework, a database kind is not offered.

**The job** (`new-project`) does, in this order, and reports each as its `step` with a percent:
1. Checks the plan again.
2. Installs what is missing (each runtime and Composer through the installer, one after another).
3. Starts the database service (created first when none exists) and creates the database and a user with all privileges on it only (a random password, stored only in the `.env`).
4. Runs the steps of the framework in order (their output goes to the log of the new project's first service once it exists, before that to the job's log).
5. Writes the `.env`, keeping lines the generator wrote.
6. Creates the services, the site (the site server is set up when needed) and the project; the services use the database.
7. Opens the folder in the first installed editor when `openEditor`.

On success `job.done` has `projectId` and `path`. A failure stops the job, removes what it created (services, site, project, database and user), keeps the generated folder, and says so in the error (`The folder D:\www\my-shop was kept.`). Events: `services.changed`, `sites.changed`, `projects.changed`.

### Pre-steps and finding programs (internal contract)

- `kinds.Plan.Pre` is a list of `PreStep { Label, Program, Args, Skip, Dir, Timeout }`: programs that run before the service's own program (a Python service's virtual environment, the Corepack folder of a pnpm project). The supervisor runs them in order after the folders and files of the plan exist, with the service's environment, skipping a step whose `Skip` file exists; each step and its output go to the service's log, and a failing step stops the start (`409`). `Supervisor.PrepareService(svc)` runs the same steps without starting the service: a one-off task calls it first.
- `supervisor.FindProgram(name, env)` finds a program the way the service would: a plain name is looked up in the `PATH` of `env` (as `Supervisor.Environ` builds it, so a virtual environment's `Scripts` folder wins) with the extensions of `PATHEXT`. A name with a path separator is refused; an absolute path must exist.

#### Composer and package manager details

- **Catalog.** Built-in entries `composer-<version>-windows-amd64` and `composer-<version>-windows-arm64` (Composer does not depend on the CPU, so each release has one entry per Windows architecture; the catalog only offers the one that matches the machine). URL `https://getcomposer.org/download/<version>/composer.phar`, `binaryRelPath` `composer.bat`, checksum `sha256` (built in) or `shasums` with the publisher's `composer.phar.sha256sum` (remote). The version source of kind `composer` is `https://getcomposer.org/versions` (its `stable` list: the current release with line `2` and channel `stable`, the long-term-support one with line `2.2` and channel `lts`; only paths `/download/<version>/composer.phar` on getcomposer.org are taken). It appears in `GET /api/catalog/status` like the other sources.
- **`composer.cmd` shim and activate scripts.** With a default PHP and a Composer runtime whose `composer.phar` exists, the shims folder gets `composer.cmd`: `"<default php binary>" "<composer.phar>" %*`, run with `COMPOSER_HOME` and `COMPOSER_CACHE_DIR` set (the activate scripts set these two as well). Without a PHP there is no shim and Composer's folder is not put on PATH.
- **Environment.** Every service of kind `php` (and Open terminal on it) gets `COMPOSER_HOME=<home>\composer` and `COMPOSER_CACHE_DIR=<home>\composer\cache`, always (a profile `portable` rule with `"always": true`). The `composer` switch of `Settings.Portable` stays and only adds `<home>\composer\vendor\bin` to PATH. A `portable` rule may also have `"when": { "setting": "packageManager", "not": "npm" }`: it applies while that setting of the service (its effective value, default included) differs from `not`; a service with `customArgs` has no setting, so no such rule applies.
- **`Supervisor.ComposerCommand(svc) (bin string, args []string, err error)`** is what a task whose program is `composer` runs: `bin` is the PHP of the service (the default PHP for a service that is not a PHP one) and `args` is `[<composer.phar>]` of the default Composer runtime (the first one when none is the default; a runtime whose `composer.phar` is gone does not count). The caller appends the task's own arguments and runs it with the service's environment. `409 conflict` "Composer is not installed: install it on the Runtimes page" when none is installed; the error of the PHP lookup when there is no PHP.
- **Node option `packageManager`** (`select`: `npm` default, `pnpm`, `yarn`; group Run). It emits no argument. For `pnpm` and `yarn` the plan gets: the folder `<home>\node\corepack\bin`; `Plan.Pre` step 1 "Installing Corepack": `npm install --prefix <home>\node\corepack corepack` (skipped when `<home>\node\corepack\node_modules\corepack\dist\corepack.js` exists; `npm` is the one next to the service's node); step 2 "Making the pnpm and yarn commands": `<home>\node\corepack\node_modules\.bin\corepack.cmd enable --install-directory <home>\node\corepack\bin pnpm yarn` (skipped when `bin\pnpm.cmd` exists); and for `run = npm` the program is `<home>\node\corepack\bin\<pm>.cmd` instead of `npm`, without npm's `--` before the user's arguments. That program may not exist yet at the start: the check that a program exists is left to the pre-steps when the plan has any. A task program `{s.packageManager}` (`npm`, `pnpm`, `yarn`) is found on the service's PATH, where Corepack's `bin` folder comes first.
- **Node environment** while the package manager is not npm: `COREPACK_HOME=<home>\node\corepack-home`, `COREPACK_ENABLE_DOWNLOAD_PROMPT=0`, `npm_config_store_dir=<home>\node\pnpm-store`, `YARN_CACHE_FOLDER=<home>\node\yarn-cache`, and `<home>\node\corepack\bin` first on PATH (before the runtime's folder).
- The `corepack` package comes from npm: the first start needs an internet connection, and the pnpm or Yarn version is downloaded by Corepack on first use (the one the project's `packageManager` field names, else the latest).

#### Tasks and detection details

**How a task finds its program.**

1. A preset's `program` and `args` are expanded: `{s.<setting>}` is the service's setting (an unknown one is empty; `packageManager` is `npm` when not set), `{port}` the service's port (empty without one).
2. The program must be a plain name: no path separator (`/` or backslash), NUL or line break (400). Each argument has no NUL or line break, and there are at most 200 (400). A preset takes no extra `args`.
3. `composer` is run as the service's PHP with `composer.phar` in front of the arguments (`Manager.Composer`, to be connected to `supervisor.ComposerCommand`); without an installed Composer the request is `409` ("Composer is not installed: install it on the Runtimes page"). Every other name is looked up with `supervisor.FindProgram` on the `PATH` of `Supervisor.Environ(service)`, after `Supervisor.PrepareService(service)` ran. A name that is not found fails the job (not the request), because the pre-steps (a virtual environment's `pip`) may be what makes it.
4. The process runs in the service's folder with that environment, no shell, no console window. When the timeout runs out (the preset's `timeout`, else 10 minutes) the whole process tree is ended.

**Log format.** Lines go to `Supervisor.Log(serviceId, "task", text)`, so they are stored and published as `log.line` with `stream: "task"`. The first is `$ <program> <args>`; then each output line of stdout and stderr in the order it came (a trailing carriage return is dropped, a progress line that redraws itself keeps its last state); when the task fails one more line says why (`npm exited with code 1`, `npm timed out after 10m0s`). The program's own output is never reworded. The service's preparation (virtual environment, Corepack) logs its own lines after the first one.

**Errors.**

| Status | When |
| --- | --- |
| 400 | neither or both of `preset` and `program`; `args` with a preset; a program name with a path separator, NUL or line break; an argument with a NUL or line break; on detect: no `dir`, a `dir` that is not an absolute path or not a folder |
| 404 | unknown service; the service's kind has no such preset |
| 409 | a task of the service runs (`details.jobId`); the service is held by another operation (snapshot, restore, reset, migration); the preset's `when` file is missing; the service has no project folder or it does not exist; Composer is not installed |
| job failed | `<program> exited with code N: <last lines>`; `<program> timed out after <t>`; `<program>: <error>` when it could not start; the service's preparation failed; `<program>: <program> was not found on the service's PATH` |

A failed job is `state: "failed"` with the message in `error`; its `step` is the preset's label, or `program args` for your own program.

**Detection rules.** The strongest evidence wins (a framework dependency is `high`, only the marker file `medium`); on a tie the earlier row. Settings are kept only when the kind's profile has that option and accepts the value.

| Marker (in the folder) | Kind | Framework | Notes |
| --- | --- | --- | --- |
| `composer.json` requires `laravel/framework` | php | `laravel` | port 8000, `docroot` `public`, marker `artisan` |
| `composer.json` requires `symfony/framework-bundle`, `symfony/symfony` or `symfony/http-kernel` | php | `symfony` | port 8000, `docroot` `public` |
| `wp-config.php`, `wp-load.php` or `wp-content` | php | `wordpress` | high, no port |
| other `composer.json` | php | | medium, port 8000, name from the package (`vendor/name`) |
| `package.json` with `next` | node | `nextjs` | port 3000 |
| `@nestjs/core` | node | `nestjs` | port 3000 |
| `astro` | node | `astro` | port 4321 |
| `vite` (with `react`, `vue` or `svelte`) | node | `vite-react`, `vite-vue`, `vite-svelte`, else `vite` | port 5173 |
| `express` | node | `express` | port 3000 |
| other `package.json` | node | | medium; name from `name` (scope dropped) |
| `go.mod` with `gin-gonic/gin` | go | `gin` | port 8080 |
| `go.mod` with `gofiber/fiber` | go | `fiber` | port 3000 |
| other `go.mod` | go | `go-nethttp` | medium, port 8080, name is the last element of the module path (not `v2`); `package` is `./cmd/<x>` when that is the only `cmd/*/main.go` and there is no `main.go` |
| `pom.xml`, `build.gradle(.kts)` mentioning Spring Boot | java | `springboot` | high, port 8080; name from `artifactId` (not the parent's) or `rootProject.name` |
| other `pom.xml` / `build.gradle` | java | | medium |
| `manage.py`, or `django` in `requirements.txt` / `pyproject.toml` | python | `django` | port 8000 |
| `fastapi` | python | `fastapi` | port 8000 |
| `flask` | python | `flask` | port 5000 |
| other `requirements.txt` / `pyproject.toml` | python | | medium; name from `pyproject.toml` |
| only `index.html` | `""` | | `markers: ["index.html"]`, nothing else filled in |

Node settings: `run`=`npm` and `npmScript`=`dev` (else `start`) when `package.json` has such a script; otherwise `run`=`script` and `script`=`main` or the first of `server.js`, `index.js`, `app.js`, `main.js`; `packageManager`=`pnpm` or `yarn` from `pnpm-lock.yaml` or `yarn.lock` once the kind has that option. `name` falls back to the folder's name; `envFile` is `.env` when that file exists. Framework ids other than `go-nethttp` and `html` must match `frameworks.json` as it grows.

#### Python details

- **Catalog.** Python comes from python-build-standalone, `https://github.com/astral-sh/python-build-standalone`. The built-in entries are pinned to the release `20261026`: `python-3.13.18-windows-amd64`, `python-3.13.18-windows-arm64`, `python-3.12.16-windows-amd64` and `python-3.12.16-windows-arm64` (version line `3.13` / `3.12`). Each is the plain `install_only` archive `cpython-<version>+<build>-<x86_64|aarch64>-pc-windows-msvc-install_only.tar.gz` (not free-threaded, not `_stripped`), `checksum.type` `sha256` with the value from the release's `SHA256SUMS`, `stripComponents` 1 (the root folder is `python`), `binaryRelPath` `python.exe`. `pip` is `Scripts\pip.exe` below the runtime folder.
- **Remote discovery.** `POST /api/catalog/refresh` has a source `python` (`github.com/astral-sh/python-build-standalone`). It reads the newest 3 releases (`.../releases?per_page=3`; one release holds every Python and about 2 MB of asset list), and for each of the newest four minor lines (`3.14`, `3.13`, `3.12`, `3.11`) keeps the newest patch with its Windows `amd64` and `arm64` builds. Pre-releases (`3.16.0a1`), free-threaded and `_stripped` builds and other platforms are ignored. The checksum is the SHA-256 digest GitHub shows for the asset; an asset without one becomes an unverified entry. Ids are `python-<version>-windows-<arch>`; the same id in the built-in manifest wins.
- **Run modes** (setting `run`, `GET /api/kinds`). The arguments after the program are:

  | `run` | Arguments | Settings used |
  | --- | --- | --- |
  | `script` (default) | `<script>` | `script` (`main.py`) |
  | `module` | `-m <module>` | `module` |
  | `uvicorn` | `-m uvicorn --host <host> --port {port} <app>` | `app` (`main:app`), `host` |
  | `flask` | `-m flask run --host <host> --port {port}` | `flaskApp` (set as `FLASK_APP`, optional), `host` |
  | `django` | `<manage> runserver <host>:{port}` | `manage` (`manage.py`), `host` |

  The user's own arguments come last. `host` (default `127.0.0.1`) is also set as `HOST` in every mode. Every Python service gets `PYTHONUNBUFFERED=1` and the usual `PORT`.
- **Virtual environment.** A service of kind `python` that does not use `customArgs` (and has no `command`) runs `<home>\python\venvs\<serviceId>\Scripts\python.exe` (`kinds.VenvDir`, `VenvPython`, `VenvScripts`), with `VIRTUAL_ENV=<home>\python\venvs\<serviceId>` and that `Scripts` folder first on `PATH` (before the runtime's folder). So `pip` and `python` in a task, `Environ` and the terminal are the environment's. `GET /api/services/{id}/command` and the preview show that program even before the environment exists (no "not found" warning).
  - *Creation.* `Plan.Pre` has one step, "Creating the virtual environment": the runtime's `python.exe -m venv <dir>` with `Skip` = the environment's `python.exe`, so it runs once. A start (or `PrepareService`, so a task too) runs it.
  - *Runtime changed.* If `<dir>\pyvenv.cfg` has a `home` that is not the folder of the service's current runtime (compared as paths, ignoring case), the step becomes `-m venv --clear <dir>` and is not skipped, and the plan has a warning (logged as a `note:` at the start, shown in the preview): the environment is made again and its packages are gone.
  - *Removal.* `Supervisor.Remove` (service delete) removes `<home>\python\venvs\<serviceId>` after the process stopped. A failure to delete (a locked file) is ignored.
  - *Snapshots.* A snapshot, a restore and a clone never copy a virtual environment (`internal/snapshot` copies only database data folders and `php.ini`): it holds absolute paths. After a restore or a clone the first start makes an empty one.
  - A service that uses `customArgs`, or that has its own `command`, has no pre-step and runs as it was written.
- **Tasks** of the kind (`GET /api/kinds`, `tasks`): `pip-install` (`pip install -r requirements.txt`, `when` `requirements.txt`), `pip-list` (`pip list`), `django-migrate` (`python manage.py migrate`, with a `confirm`) and `django-collectstatic` (`python manage.py collectstatic --noinput`), both `when` `manage.py`. Their programs `pip` and `python` must be found on the service's `PATH` first (the environment's `Scripts` folder), not next to the runtime: the runtime's own `python.exe` has no Django.
- **Shims.** `python` and `pip` get shims for the default Python runtime; `pip` is looked for next to `python.exe` and in `Scripts\` below it. When `pip` is in `Scripts\`, the activate scripts put that folder on `PATH` after the runtime's folder.

#### New project details

**Schema additions** (`format` stays 1; all validated by `frameworks.Parse`):

- A `run` step may have `env` (name to value, placeholders allowed): variables added to the runtime's environment for that program (`CI=1`, `npm_config_yes=true`, `MAVEN_USER_HOME`).
- A `download` step has either `checksum` or `unverified: true` with a `reason`, never both. An unverified download (start.spring.io builds the project on request and publishes no checksum) is a warning in the plan: `<step label>: <reason>.`
- A `run` program is a plain name, or `./name` for a file in the folder the step runs in (`./mvnw.cmd`). No path separators, no `..`, no spaces.
- Placeholders: `{venv}` (Python frameworks only: `<home>\python\venvs\<serviceId>`) and `{home}` (the data folder) are added. A placeholder that is not on the list is refused when the list is loaded, and the database placeholders need a `database`. `{appKey}` and `{random}` give a new value at every use. Without a database the database placeholders are empty and an `env` line that uses one is left out.
- A `php` or `proxy` site needs a service with a port (a `php` site a `php` service); `cwd`, site `root`, `envFile` and file paths are relative without `..`.
- The project name is limited to letters, digits, spaces and `. _ - ( )` (at most 64, starting with a letter or digit): it is put into page titles and source files as it is. The slug is the name in lower case ASCII, words joined with `-`, at most 40 characters; Windows device names (`con`, `nul`, `com1` ...) are a problem. The database and its user are the slug with `_` (the user at most 32 characters; `mysql`, `postgres`, `root` and the like are a problem).

**Plan.** A request error (`400`: unknown framework, body not JSON; `409`: the frameworks list is damaged) is an error; everything about the request itself is a `problems` entry of a `200` plan. Runtime choice: an installed runtime of the kind inside `runtime.min`/`max` is used (the default of the kind when it fits, else the newest); otherwise the newest catalog entry that this machine can run and that fits is installed (entries of the `mainline` channel only when nothing else fits). A bound with fewer parts than the version compares those parts (`max: "8.4"` takes 8.4.9). Composer and the database server are installed the same way when missing. An installed database service of the kind is reused (a running one first); otherwise a service is created on the next free port from 3306 (PostgreSQL 5432). `Plan.steps` are the job's steps in words and `Plan.creates` what exists afterwards.

**The job** (key `newproject:<folder>`, one job per folder). Percent: checking 0-5, installs 5-35, database 35-45, generator 45-80, `.env` 80, creating 85, editor 95. `step` is the text of `Plan.steps`.

- *Installs*: `Installer.Start(catalogId)` and wait for its job. An entry that is already installed is skipped.
- *Database*: create the server service when needed (validated like the API), start it with its dependencies and wait up to 2 minutes for `running`, then create the database and the user (a password of 24 random letters and digits). A database or user that exists already fails the job (nothing of the other is dropped).
- *Generator steps*, in the folder their `in` says (`parent` or `project`; the project folder is made first for `project`):
  - `run`: the program is `composer` (the framework's PHP plus `composer.phar`, as `supervisor.ComposerCommand`), `./name` (a file of the step's folder), or a name found on the PATH of a service of the runtime's kind that is not stored: the runtime's folder first, `JAVA_HOME`/`GOROOT`/Composer folders as for a real service, a Python framework's virtual environment `Scripts` first (the environment is made first by `Supervisor.PrepareService`, for the service id that the Python service will get). No shell and no console window; timeout 15 minutes (or `timeout`); a non-zero exit fails the step with the last output lines. Output lines are kept and written to the log of the new project's first service (stream `task`, first line `$ program args`, the database password replaced by `********`); when the job fails before that, to `<home>\logs\newproject-<slug>.log`.
  - `download`: HTTPS to `<home>\tmp`, the checksum checked while it is written (`sha256:<hex>`, `sha1:<hex>` or a HTTPS URL of a file whose first word is the digest), then unpacked with the installer's zip extractor (entries that leave the folder are refused, links ignored) and `strip`; the file in `tmp` is always removed.
  - `files`: relative paths only, parents are made, a file that exists is **never overwritten** (the step logs `Kept the existing ...`).
- *.env*: `env` lines are set, `ifMissing` lines only when the key is not there; every other line, the comments and the line ending stay. Services with the project folder as `cwd` get `envFile`.
- *Creating*: the services (validated by `internal/validate`, the same code as `POST /api/services`; `usesDatabase` sets `databaseId` and `dependsOn`), the site (`sites.Validate`, then `EnsureServers` and a regenerate) and the project (the new services and the site) are added in **one** state update. The first service with a port gets the planned port, the next ones the next free port.
- *Editor*: the first installed VS Code, else VSCodium, through `launch` with the same data arguments as `POST /api/runtimes/{id}/launch`. A failure to open it is logged, not a failure of the job.
- `job.done`: `projectId` and `path`.

**Failure.** The job stops at the first failing step (a crash too). Removed, in reverse order: the project, the site and the services (stopped, their logs and virtual environment removed), the database, its user, and the database service when this job made it. Kept: the generated folder (an empty folder that this job made is removed; no file is ever deleted) and the installed runtimes. The error says what failed, then `The folder D:\www\my-shop was kept.`, the generator output file when there is one, and what could not be cleaned up.

**Python frameworks.** Django: `requirements.txt` (`Django`), `pip install -r`, `django-admin startproject config .`, `python manage.py migrate`, all in the service's virtual environment. FastAPI and Flask: a starter and `requirements.txt`, then `pip install -r requirements.txt` in the environment, so the service runs at once.

**Frameworks:**

| Id | Language | Runtime | Database | Generator | Service |
| --- | --- | --- | --- | --- | --- |
| `laravel` | php | php 8.2+, Composer | MariaDB or MySQL, optional | `composer create-project laravel/laravel` | php, port 8000, docroot `public`; `.env` DB_*, APP_URL, file sessions and cache |
| `symfony` | php | php 8.2+, Composer | MariaDB or MySQL, optional | `composer create-project symfony/skeleton` | php, 8000, `public`; `DATABASE_URL` |
| `wordpress` | php | php 7.4+ | MariaDB or MySQL, required | `latest.zip` with the SHA-1 of `latest.zip.sha1`, strip 1, `wp-config.php` | php, 8000, php site |
| `php` | php | php 8.1+ | none | `index.php` | php, 8000 |
| `express` | javascript | node 20+ | none | `package.json`, `server.js`, `npm install` | node `npm run dev`, 3000 |
| `nestjs` | javascript | node 20+ | none | `npx --yes @nestjs/cli@latest new --package-manager npm --skip-git` | `npm run start:dev`, 3000 |
| `nextjs` | javascript | node 20.9+ | none | `npx --yes create-next-app@latest --yes --use-npm --disable-git` | `npm run dev`, 3000 |
| `vite-react`, `vite-vue`, `vite-svelte` | javascript | node 20.19+ | none | `npm create vite@latest -- --template <t>-ts --no-interactive`, `npm install` | `npm run dev -- --host 127.0.0.1 --port <port> --strictPort`, 5173 |
| `astro` | javascript | node 22.12+ | none | `npm create astro@latest -- --template minimal --install --no-git --skip-houston --yes` | `npm run dev`, 4321 |
| `django`, `fastapi`, `flask` | python | python 3.10+ | none | see above | python `django` / `uvicorn` / `flask`, 8000 / 8000 / 5000 |
| `go-nethttp` | go | go 1.21+ | none | `go mod init`, `main.go` | go, 8080 |
| `gin` | go | go 1.24+ | none | `go mod init`, `main.go`, `go mod tidy` | go, 8080 |
| `fiber` | go | go 1.26+ | none | the same with Fiber v3 | go, 3000 |
| `springboot` | java | java 17+ | none | start.spring.io `starter.zip` (Maven, Spring Web, unverified), `./mvnw.cmd -q -DskipTests package` | java `run: jar`, `springPort`, 8080 |
| `html` | static | none | none | `index.html`, `style.css` | static site of the folder |

PHP projects offer MariaDB and MySQL only: the default extension list has `pdo_mysql` and `mysqli`, not `pdo_pgsql`. Vite and Astro are started with `--host 127.0.0.1`, because the site server proxies to `127.0.0.1` and their default host is `localhost`.

**Internal contract.** `newproject.Manager` takes its collaborators as interfaces (`InstallerAPI`, `DatabaseAPI`, `SupervisorAPI`, `SitesAPI`) and the hooks `Run`, `Launch`, `Catalog`, `PortFree`, `HTTP`, `Rand`, so tests use fakes. `dbtools.Manager.DropUser(ctx, serviceID, user)` is new (used by the clean-up). Shared pieces: `sites.CheckDomain` and `sites.Validate`, `installer.ExtractZip`, `launch.ToolDataArgs`, `validate.Service/Links/Project`, `netutil.PortFree/NextFreePort`, `envfile.Apply`.

## Amendment: 0.0.6, part 2 (Vite+, Rust, API clients, project files, CPU and memory)

Everything here is the contract of the second half of 0.0.6. Routes answer `501 not_supported` until their work is in.

### Kinds

- **`rust`** is a service kind and a runtime kind (category `language`, data folder `<home>\rust`). The toolchain is installed with **rustup** from a catalog entry whose `url` is `rustup-init.exe` (the installer's `.exe` type, below): the **GNU** toolchain (`x86_64-pc-windows-gnu`), so no administrator install is needed. The runtime's binary is `<bin>\<id>\cargo\bin\cargo.exe`; `rustc` and `rustup` are next to it. A service of this kind runs `cargo`; env `RUSTUP_HOME`, `CARGO_HOME` (both inside the runtime's folder) and `CARGO_TARGET_DIR=<home>\rust\target\<serviceId>`; `<CARGO_HOME>\bin` is on `PATH`.
- **Installer `.exe` type.** A catalog download whose file name ends in `.exe` is a program to run, not an archive. It is verified like any download, copied into the install folder and **run** (no window, a timeout, its process tree ended on a timeout) with the arguments and environment of the kind (rustup: `RUSTUP_HOME`, `CARGO_HOME`, `--default-host x86_64-pc-windows-gnu --default-toolchain stable --profile minimal --no-modify-path -y`); its output lines become the job's step and percent. The runtime is registered only when the file named by `binaryRelPath` exists afterwards. A failing install removes the folder.
- **`viteplus`** is a runtime kind (category `tool`, data folder `<home>\vite-plus`): the Vite+ command line (`vp`). It is installed only if Vite+'s global program can be fetched with a published checksum and kept portable with `VP_HOME=<home>\vite-plus`; otherwise the kind stays registered but has no catalog entry, and the Node-from setting below answers why it is off.
- **`bruno`** and **`hoppscotch`** are runtime kinds of a new category **`apiclient`** (Launchable like the editors: opened, not supervised; `GET /api/runtimes` and the Runtimes page group "API clients"). Each is a portable zip from GitHub (the asset's `digest` is its sha256). Their data is moved into `<home>\bruno` and `<home>\hoppscotch` by `launch.ToolDataArgs` (Bruno `--user-data-dir=<dir>`; Hoppscotch Desktop's portable build keeps its data next to the program's working directory, which is `<home>\hoppscotch`). A tool that cannot be moved into the data folder is not offered. `POST /api/runtimes/{id}/launch` works for them with no folder argument (Bruno has none).

### Node: Vite+

The Node profile gains, in the **Run** group:
- run mode `vp` (with the settings `vpCommand` = `dev` | `build` | `test` | `run`, and `vpScript` for `run`): the service runs `<project>\node_modules\.bin\vp.cmd <command>`; port 5173 by default.
- setting **`nodeFrom`** = `zero2dev` (default) | `viteplus`: where Node comes from. `zero2dev` runs on a Zero2Dev Node runtime. `viteplus` runs through Vite+'s own program with `VP_HOME=<home>\vite-plus`, so Vite+ selects (and installs) the Node the project pins (`.node-version`, `devEngines.runtime`, `engines.node`, `.nvmrc`). The service's command preview and its page say in words which of the two the service uses. Choosing `viteplus` without the Vite+ program installed is a `409` on start with the message "Install the Vite+ command line on the Runtimes page".
- **Pin warning:** a Node service whose folder pins a Node version that the service's Node runtime does not satisfy gets a warning in its command preview and start log, and the form offers the installed version that does, or a link to install one.
- `GET /api/project/detect` recognises Vite+ (a `vite-plus` dependency, a `vite.config` that imports `vite-plus`, or a `vp` script) and sets `run=vp` and `vpCommand=dev`.

### CPU and memory per service

- `model.Service` (so `GET /api/services` and `GET /api/services/{id}`) gains `cpu` (percent of the whole machine, 0-100, one decimal) and `memBytes` (the working set summed over the service's whole process tree). Both are `0` while the service is stopped or before the first sample, and are never stored or accepted as input.
- The supervisor samples every running service every 2 seconds. On Windows the numbers come from the service's job object (every service already runs in one): CPU from the job's accounting (user plus kernel time since the last sample, divided by the wall time and the number of processors), memory from the processes in the job. Off Windows there is no job: both stay `0`.
- **Event `service.usage`:** one batch for all running services, `[ { "id", "cpu", "memBytes" } ]`, published after each sample; a service missing from the batch has stopped (the dashboard sets its numbers to `0`).

### Project file

`zero2dev.json` (`format: 1`) describes a project without anything that belongs to one PC:

```json
{ "format": 1, "name": "My Shop",
  "runtimes": [ { "kind": "php", "min": "8.2" } ],
  "services": [
    { "ref": "app", "name": "My Shop", "kind": "php", "cwd": ".", "port": 8000, "settings": { "docroot": "public" },
      "env": { "APP_ENV": "local" }, "envFile": ".env", "dependsOn": [], "database": "db", "runtime": { "kind": "php", "min": "8.2" } },
    { "ref": "db", "name": "MariaDB", "kind": "mariadb", "cwd": "", "port": 3306 } ],
  "sites": [ { "name": "My Shop", "domain": "my-shop", "type": "php", "root": "public", "service": "app", "https": true, "enabled": true } ] }
```

- **Paths** are relative to the folder of the file, with `/` (`.` is that folder). A service folder outside it is skipped on export with a warning (an absolute path is never written). **Runtimes** are a kind and a version range, never a path, an id or a catalog id. **Services refer to each other by `ref`** (`dependsOn`, `database`, a site's `service`); no Zero2Dev id is in the file. A **domain** is the label before the suffix.
- **Never written:** ids, runtime binaries, absolute paths, a database service's administrator password, the contents of `.env`, data folders, and every `env` variable whose key looks like a secret (contains `PASSWORD`, `SECRET`, `TOKEN`, `KEY`, `CREDENTIAL`, `AUTH`, `PRIVATE`, `SIGNATURE`, `COOKIE` or `SESSION`, case-insensitive): each one is named in the export's `warnings`.
- A file with another `format`, an unknown service kind or runtime kind, a `ref` used twice, a `dependsOn`/`database`/`service` that names nothing, or a dependency loop is refused (a plan `problem`).

| Method | Path | Body | Response |
| --- | --- | --- | --- |
| POST | `/api/projects/{id}/export` | `{ "path": "D:\\work\\shop\\zero2dev.json", "overwrite": false }` | `{ "path", "warnings": [] }` |
| POST | `/api/projects/import/plan` | `{ "path": "D:\\work\\shop", "name": "" }` | `ProjectImportPlan` |
| POST | `/api/projects/import` | the same | `202 Job` (`project-import`) |

- **Export:** `path` is a file, or a folder (then `<folder>\zero2dev.json`). `404` unknown project; `409` the file exists and `overwrite` is not set (`<file> exists`). The folder of the file becomes the base of every relative path, so export next to the project's code.
- **Import plan:** `path` is a `zero2dev.json` or a folder that holds one (a cloned repository). `{ "path", "dir", "name", "services": ["Shop API (PHP, port 8000)"], "sites": ["https://shop.localhost -> Shop API"], "installs": [ { "kind", "version", "catalogId", "name", "reason" } ], "changes": ["Port 8000 is taken: Shop API uses 8001"], "problems": [], "warnings": [] }`. Nothing is changed. Problems stop the import (`409` on start with the first one as the message).
- **Import job** (`project-import`, `job.done` carries `projectId` and `path`): checks the plan again; installs the missing runtimes (the same installer and selection as New project); creates the services, the sites and the project in **one** change, with new ids, free ports (the file's port when free), unique domains, and runtimes matched by kind and range (a service that followed the default follows it); a failure leaves nothing created. No file in the project's folder is written. Events: `services.changed`, `sites.changed`, `projects.changed`.
- The dashboard: **Export** on a project's row (the Save as dialog) and **Import project** on the Projects page (`#/projects/import`: the file or folder picker, the plan, the progress, the result).

#### CPU and memory details

- **Sampler:** one goroutine of the supervisor, started by the daemon (`Supervisor.StartUsage`) and ended with `StopAll`. Every 2 seconds it takes a sample of each service in the state `running` (a service that is still `starting` or already `stopping` has no numbers). The tick, the reader and the processor count are fields of the supervisor, so the tests feed fake samples.
- **CPU:** `(CPU time now - CPU time at the last sample) / (wall time between the two samples x number of logical processors) x 100`, clamped to 0-100 and rounded to one decimal. The first sample of a run has nothing to compare with and gives `0`.
- **Windows:** the CPU time is `QueryInformationJobObject` with `JobObjectBasicAccountingInformation` (user plus kernel time in 100 ns units; it includes processes that already exited, so it only grows). The memory is `JobObjectBasicProcessIdList` for the processes in the job, then for each `OpenProcess(PROCESS_QUERY_LIMITED_INFORMATION)` and `K32GetProcessMemoryInfo`, summing `WorkingSetSize`; the handles are closed again. A service whose job could not be created (the `taskkill` fallback) or whose tree is closed has no numbers (`0`).
- **Other systems:** there is no job object, so both numbers stay `0` and no event is published.
- **Events:** `service.usage` is published once per tick when at least one service reported numbers, and once more as an **empty array** when the last service stops (so the dashboard clears the numbers); with nothing running nothing is published. `GET /api/services` and `GET /api/services/{id}` carry the last sample (`0` for a service that is not running); `cpu` and `memBytes` are never stored in `state.json` and ignored in a request body.
