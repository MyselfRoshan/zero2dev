# Using Zero2Dev

A step-by-step guide for 0.0.1. Zero2Dev is one program: start it, and the control panel opens in your browser at `http://127.0.0.1:7000`.

> The screenshots were taken on a Linux test machine, so paths look like `/tmp/...`. On Windows you will see paths such as `C:\work\my-app`. Everything else is identical.

Contents: [1. Start](#1-start-zero2dev) · [2. Install Caddy](#2-install-a-runtime-caddy-node-go) · [3. Run an app](#3-run-an-app-as-a-service) · [4. Give it a local domain](#4-give-it-a-local-domain-sites) · [5. Logs](#5-watch-the-logs) · [6. Settings](#6-settings) · [7. Portable data](#7-your-data-moving-backing-up-resetting) · [8. Troubleshooting](#8-troubleshooting)

---

## 1. Start Zero2Dev

1. Put `zero2dev.exe` in a folder you own, for example `D:\zero2dev\`. (Download it from the GitHub release, or build it with `build.ps1`.)
2. Double-click it, or run it from PowerShell: `.\zero2dev.exe`. Keep that window open; closing it stops Zero2Dev and everything it started.
3. The first run creates a `data` folder next to the exe and opens your browser at a link like `http://127.0.0.1:7000/#token=...`. The token is your password; the page saves it and removes it from the address bar.

**If your browser shows this page instead**, it has no token yet. Open the full link printed in the terminal window, or paste the token from `data\token`:

![First run page asking for the access token](screenshots/01-first-run.png)

You land on the **Dashboard**. It shows how many services, runtimes and sites you have, and offers the three things you will do first:

![Empty dashboard with Create a service, Install a runtime and Create a site](screenshots/02-dashboard-empty.png)

The left menu has five areas: **Dashboard**, **Services**, **Runtimes**, **Sites**, **Logs**, plus **Settings**. The green *Connected* box at the bottom left means the page is talking to the program.

---

## 2. Install a runtime (Caddy, Node, Go)

A *runtime* is a program Zero2Dev can run for you. **Caddy** is the web server that gives your apps local domains; Node and Go run your code.

1. Open **Runtimes**.
2. In the **Catalog**, find your platform (`windows/amd64`) and click **Install** next to **Caddy**.
3. Zero2Dev downloads it, checks the download against the published checksum, and unpacks it into `data\runtimes\`. When it finishes, Caddy appears under **Installed runtimes** and a **Caddy** service is created for you.

![Runtimes page: installed runtimes on top, catalog with Install buttons below](screenshots/03-runtimes.png)

Other ways to get a runtime:

| Button | Use it when |
| --- | --- |
| **Detect on this PC** | Node, PHP, Go or Java is already installed. Zero2Dev finds it on your PATH and lists it. |
| **Register by path** | The program lives somewhere unusual, e.g. `C:\php\php.exe`. |
| **Import file** (in the catalog row) | Downloads are blocked on your network. Save the archive in your browser from the row's *Download page* link, then import it. Zero2Dev still verifies it. |

Nothing is installed system-wide: runtimes stay inside `data\`, and you can delete one by removing it from the list.

---

## 3. Run an app as a service

A *service* is a command Zero2Dev starts, stops and watches for you, with its output in one place.

1. Open **Services** and click **New service**.
2. Fill in the form:

   | Field | What to enter |
   | --- | --- |
   | **Name** | Anything, e.g. `My API` |
   | **Kind** | `node`, `php`, `go`, `java`, `caddy`, or `custom` for any program |
   | **Runtime** | Which installed runtime to use. Leave on *Automatic* to use the first one registered, or whatever is on PATH |
   | **Command** | Only for `custom`: the program to run, e.g. `python` |
   | **Arguments** | One per row (click **Add argument**). `{port}` is replaced with the port you set |
   | **Working directory** | The folder your app lives in, e.g. `C:\work\my-api` |
   | **Port** | The port your app listens on, or `0` if it has none |
   | **Health path** | Optional, e.g. `/health`. Without it Zero2Dev checks the port accepts connections |
   | **Environment variables** | Optional `NAME` / `value` pairs |
   | **Start automatically…** | Tick to start it whenever Zero2Dev starts |

   ![New service form filled in for a Python static server](screenshots/04-service-form.png)

3. Click **Create service**. It appears in the list, stopped:

   ![Services list with one stopped service](screenshots/05-services-list.png)

4. Click **Start**. The state changes to *starting*, then *running* once the port answers. **Stop** and **Restart** work the same way. Stopping a service stops the whole process tree.

Typical examples:

- **Node app**: Kind `node`, Arguments `server.js`, Working directory = your project.
- **PHP**: register `php.exe` first, then Kind `php`. Leave Arguments empty to use PHP's built-in server on your port.
- **Go app**: Kind `go`, leave Arguments empty to run `go run .` in your project.
- **Anything else**: Kind `custom`, Command = the program, Arguments = its options.

Click a service's name to open its page: command, folder, process id, start time, port link and live output.

![Service page showing running state, port link and live log lines](screenshots/06-service-running-logs.png)

---

## 4. Give it a local domain (Sites)

A *site* maps a name like `demo.localhost` to a folder (a static website) or to a running service. Caddy does the work, so **install Caddy first** (step 2).

1. Open **Sites** and click **New site**.
2. Fill in:
   - **Name**: a label for you.
   - **Domain**: lowercase, ending in `.localhost`, e.g. `demo.localhost`.
   - **Type**: *static* serves a folder; *proxy* forwards to a service or port; *PHP* serves through a PHP service.
   - **Folder to serve** (static) or the **service** to forward to.
   - **Use HTTPS** if you want `https://` (see [Settings](#6-settings)).

   ![New site form: name, domain, type static, folder to serve](screenshots/07-site-form.png)

3. Click **Create site**. It is listed as *enabled* with its address:

   ![Sites list showing demo.localhost on port 8080](screenshots/08-sites-list.png)

4. Go to **Services** and **Start** the **Caddy** service (created when you installed Caddy).
5. Open `http://demo.localhost:8080` in your browser (HTTPS sites use port `8443`).

Browsers resolve `*.localhost` to your own computer, so no hosts-file edit and no administrator rights are needed. Edits to sites are applied to Caddy automatically.

---

## 5. Watch the logs

- Each service page has **Live output**: type in **Filter lines** to search, **Pause** to stop scrolling, **Copy** to copy the lines, **Clear** to empty the view.
- The **Logs** page shows output from all services in one stream.
- Logs are also saved as files in `data\logs\`, one per service, so they survive a restart.

![Logs page with output from all services](screenshots/09-logs.png)

---

## 6. Settings

![Settings page: domain suffix, proxy ports, HTTPS certificate, download proxy](screenshots/10-settings.png)

| Setting | What it does |
| --- | --- |
| **Domain suffix** | The ending your site domains must use. `localhost` works everywhere with no setup |
| **HTTP / HTTPS proxy port** | Ports Caddy listens on (`8080` / `8443`). Change them if something else uses those ports; a running Caddy restarts for you |
| **Let Caddy trust its local certificate authority** | For `https://` sites. **Off by default**: Zero2Dev then changes nothing outside its folder, and your browser shows a certificate warning for HTTPS sites. Turning it on makes Caddy add its certificate to Windows (you may get a security prompt) |
| **Download proxy** | Used when installing runtimes on a company network, e.g. `http://proxy.company.com:8080`. Zero2Dev does not read the Windows proxy setting |

---

## 7. Your data: moving, backing up, resetting

Everything lives in the `data` folder next to `zero2dev.exe`:

```
zero2dev.exe
data\
  token          your access token
  state.json     your services, runtimes, sites and settings
  runtimes\      Node, Go, Caddy installed from the catalog
  caddy\         Caddy's own data (nothing goes to AppData)
  config\        generated Caddyfile
  logs\          service logs and installer.log
```

- **Move or copy** the whole folder to another drive or a USB stick. Stop Zero2Dev first.
- **Back up** by copying `data\` while Zero2Dev is stopped.
- **Reset** by stopping Zero2Dev and deleting `data\`.
- **Different location**: set the `ZERO2DEV_HOME` environment variable to another folder.

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
| Service stays *failed* | Open its page: the last error and the live output say why (wrong folder, missing runtime, command not found) |

Environment variables: `ZERO2DEV_HOME` (data folder), `ZERO2DEV_PORT` (panel port, default 7000), `ZERO2DEV_NO_BROWSER` (do not open the browser).
