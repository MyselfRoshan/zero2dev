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
| editor | `vscode`, `vscodium`, `notepadpp` | no: opened with `launch` |
| dbtool | `heidisql`, `dbeaver` | no: opened with `launch` |

`model.KindCategory`, `model.Launchable(kind)` and `model.IsDatabase(kind)` hold this table. The UI mirrors it in `RUNTIME_GROUPS` (`web/src/lib/catalog.ts`).

- **Detection:** `mariadbd`, `mysqld`, `postgres` and `redis-server` are probed with `--version`; `code` / `codium` (the `.cmd` wrappers) too.
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
| redis, vscodium, notepadpp, heidisql, dbeaver | GitHub releases (`api.github.com/repos/<repo>/releases?per_page=10`), newest stable assets matching the Windows ZIP pattern | sha256 from the asset's `digest`; none means unverified |
| mysql | none: entries are pinned in the built-in manifest (dev.mysql.com pages are not meant for programs) | md5 |

**Archive layouts** (checked against the real archives):

| Kind | Strip | Binary |
| --- | --- | --- |
| mariadb | 1 | `bin/mariadbd.exe` |
| mysql | 1 | `bin/mysqld.exe` |
| postgres | 1 | `bin/postgres.exe` |
| redis | 1 | `redis-server.exe` |
| dbeaver | 1 | `dbeaver.exe` |
| vscode | 0 | `Code.exe` |
| vscodium | 0 | `VSCodium.exe` |
| notepadpp | 0 | `notepad++.exe` |
| heidisql | 0 | `heidisql.exe` |

**Bin holds programs only.** Tool data goes to `<home>/<model.DataDirName(kind)>`: `code`, `codium`, `notepadpp`, `heidisql`, `dbeaver`, and for databases `mariadb`, `mysql`, `postgres`, `redis`. Nothing is created inside `bin`.

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
| vscode, vscodium | env `VSCODE_PORTABLE=<dir>` (portable mode with the data outside the program folder) | `<home>/code`, `<home>/codium` (`user-data`, `extensions`) |
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
- Java and Go shims set `JAVA_HOME` / `GOROOT` for that one run only.
- `activate.cmd`, `activate.ps1` and `activate.sh` put the runtimes' folders first on `PATH` and set `JAVA_HOME` / `GOROOT` **for the terminal they run in**. Use `call "<home>\shims\activate.cmd"` in Command Prompt, `. '<home>\shims\activate.ps1'` in PowerShell and `. <home>/shims/activate.sh` in Git Bash.

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
