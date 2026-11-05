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
3. Zero2Dev downloads it, checks the download against the published checksum, and unpacks it into the `bin` folder next to `zero2dev.exe`. When it finishes, Caddy shows as installed on its card and a **Caddy** service is created for you.

![Runtimes page: installed runtimes on top, catalog with Install buttons below](screenshots/03-runtimes.png)

Other ways to get a runtime:

| Button | Use it when |
| --- | --- |
| **Detect on this PC** | Node, PHP, Go or Java is already installed. Zero2Dev finds it on your PATH and lists it. |
| **Register by path** | The program lives somewhere unusual, e.g. `C:\php\php.exe`. |
| **Import file** (in the catalog row) | Downloads are blocked on your network. Save the archive in your browser from the row's *Download page* link, then import it. Zero2Dev still verifies it. |

Nothing is installed system-wide: runtimes stay in the `bin` folder next to `zero2dev.exe`, and you can delete one by removing it from the list.

### Install runtimes and keep several versions side by side

Every runtime (Node.js, Go, Caddy, PHP, Java, nginx) has its own card on the **Runtimes** page. You can install **as many versions as you like at the same time**, for example PHP 7.1 for an old project and PHP 8.4 for a new one.

1. Open **Runtimes**. Press **Check for new versions** to ask the publishers (nodejs.org, go.dev, GitHub for Caddy, windows.php.net, Adoptium, nginx.org) which versions exist. It tells you how many new ones it found; a site that cannot be reached is named in a red line and the other cards still work. The page asks once automatically the first time.
2. In a card, pick a version from the list and press **Install**. Tags: *latest* (newest of that version line), *LTS* / *stable* / *mainline*, *installed*, *unverified*.

   ![Runtimes page with one card per runtime, each with a version list](screenshots/12-runtimes-versions.png)

3. **Some downloads cannot be verified.** nginx.org publishes no checksum, and some old PHP releases (7.x) have none either. Zero2Dev tells you and asks you to confirm. It still downloads over HTTPS from the publisher only, and writes the SHA-256 it computed to `data\logs\installer.log`. If you have a SHA-256 you trust, use **Import file** and paste it instead; then it is verified like everything else.

   ![Confirmation: nginx publishes no checksum, install anyway?](screenshots/13-nginx-unverified-confirm.png)

   ![Confirmation for an old PHP line without a published checksum](screenshots/14-php-old-unverified.png)

4. Install a second version the same way. Both appear in the card. The first version you install becomes the **Default**; press **Make default** on another one to change it. **Remove** deletes one version only; if you remove the default, another installed version becomes the default.

   ![PHP card with two versions installed](screenshots/15-php-two-versions.png)

   ![After Make default: PHP 8.4.1 is the default](screenshots/16-php-default-changed.png)

5. **Choose a version per project.** In a service's form, the **Version** list shows every installed version of that type. Leave it on *Default (...)* to follow the default, or pick e.g. *PHP 7.1.33* for that service only. Another service can use PHP 8.4.1 at the same time.

Good to know:
- **Removing a version in use** is refused while a service picked that exact version; the message names the service.
- **Windows only** for nginx and PHP (the publishers provide no Linux builds). Java (Eclipse Temurin) is available on Windows and Linux.
- **PHP needs the Microsoft Visual C++ Redistributable** (x64) installed on Windows. If `php.exe` will not start, install it from Microsoft.
- **nginx settings live inside the nginx folder** (`bin\nginx-<version>\conf`). Every nginx version has its own folder, so keep your own configuration in a folder of your own and pass it to the service with `-p` and `-c` arguments.
- Runtimes you added with **Register by path** are never touched by installs or removals (Forget only removes them from the list).

---

## 3. Run an app as a service

A *service* is a command Zero2Dev starts, stops and watches for you, with its output in one place.

1. Open **Services** and click **New service**.
2. Fill in the form:

   | Field | What to enter |
   | --- | --- |
   | **Name** | Anything, e.g. `My API` |
   | **Type** | Node.js, PHP, Go, Java, Caddy, nginx, or *Custom command* for any other program |
   | **Project folder** | The folder your app lives in, e.g. `C:\work\my-api`. Click **Browse...** to pick it in the Windows folder dialog, or type the path |
   | **Program** | Only for *Custom command*: the program to run, e.g. `python` |
   | **Arguments** | One per row (click **Add argument**). `{port}` is replaced with the port you set. The hint under the field says what happens when you leave it empty |
   | **Port** | Optional: the port your app listens on. Your app also gets it as `PORT` |
   | **Version** | Which installed version to use. Leave on *Default (...)* to follow the default of that type |
   | **Start automatically…** | Tick to start it whenever Zero2Dev starts |
   | **Advanced** | Rarely needed, folded away: a different **Program** (e.g. `npm.cmd`), a **Health check path** such as `/health` (otherwise Zero2Dev waits until the port accepts connections), and **Environment variables** |

   ![New service form filled in for a Python static server](screenshots/04-service-form.png)

3. Click **Create service**. It appears in the list, stopped:

   ![Services list with one stopped service](screenshots/05-services-list.png)

4. Click **Start**. The state changes to *starting*, then *running* once the port answers. While it runs you get **Stop** and **Restart**. Stopping a service stops the whole process tree.

Typical examples:

- **Node app**: Type *Node.js*, Arguments `server.js`, Project folder = your project.
- **PHP**: Type *PHP*. Leave Arguments empty to use PHP's built-in server on your port.
- **Go app**: Type *Go*, leave Arguments empty to run `go run .` in your project.
- **Anything else**: Type *Custom command*, Program = the program, Arguments = its options.

Click a service's name to open its page: the exact command it runs (e.g. `node.exe server.js --port 3000`), folder, process id, start time, address and live output.

![Service page showing running state, port link and live log lines](screenshots/06-service-running-logs.png)

---

## 4. Give it a local domain (Sites)

A *site* maps a name like `demo.localhost` to a folder (a static website) or to a running service. Caddy does the work, so **install Caddy first** (step 2).

1. Open **Sites** and click **New site**.
2. Fill in:
   - **Name**: a label for you. The domain is filled in from it (`My Shop` becomes `my-shop.localhost`) until you edit the domain yourself.
   - **Domain**: lowercase, ending in `.localhost`, e.g. `demo.localhost`.
   - **Points to**: *A service* (one of your services; the site follows its port), *A port* (an app you start yourself), or *A folder* (static files served as they are).
   - **Use HTTPS instead of HTTP** if you want `https://` (see [Settings](#6-settings)).

   ![New site form: name, domain, type static, folder to serve](screenshots/07-site-form.png)

3. Click **Create site**. It is listed as *enabled* with its address:

   ![Sites list showing demo.localhost on port 8080](screenshots/08-sites-list.png)

4. Go to **Services** and **Start** the **Caddy** service (created when you installed Caddy).
5. Open `http://demo.localhost:8080` in your browser (HTTPS sites use port `8443`).

Browsers resolve `*.localhost` to your own computer, so no hosts-file edit and no administrator rights are needed. Edits to sites are applied to Caddy automatically.

To take a site offline without deleting it, click **Disable** in its row; **Enable** brings it back. It works like Stop and Start for services.

**Prefer nginx?** Install nginx on the **Runtimes** page, then pick **nginx** under **Settings > Site server** and save. Zero2Dev creates an **Nginx (sites)** service, stops Caddy if it was running and starts nginx in its place. Sites, domains and ports stay the same. nginx only listens on `127.0.0.1`.

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
| **Site server** | **Caddy** (default) or **nginx** serves your sites. Switching stops the running one and starts the other; both use the same ports |
| **Domain suffix** | The ending your site domains must use. `localhost` works everywhere with no setup |
| **HTTP / HTTPS proxy port** | Ports the site server listens on (`8080` / `8443`). Change them if something else uses those ports; a running Caddy or nginx restarts for you |
| **Let Caddy trust its local certificate authority** | Caddy only, for `https://` sites. With nginx, Zero2Dev makes its own CA at `data\nginx\certs\ca.crt`; trust that file yourself to remove the browser warning. For Caddy: **Off by default**: Zero2Dev then changes nothing outside its folder, and your browser shows a certificate warning for HTTPS sites. Turning it on makes Caddy add its certificate to Windows (you may get a security prompt) |
| **Download proxy** | Used when installing runtimes on a company network, e.g. `http://proxy.company.com:8080`. Zero2Dev does not read the Windows proxy setting |

---

## 7. Your data: moving, backing up, resetting

Zero2Dev keeps two folders next to `zero2dev.exe`: `data` for your own information and `bin` for the downloaded runtimes.

```
zero2dev.exe
bin\             downloaded runtimes, one folder per version (php-8.4.1, node-22.12.0, ...)
data\
  token          your access token
  state.json     your services, runtimes, sites and settings
  caddy\         Caddy's own data (nothing goes to AppData)
  config\        generated Caddyfile and nginx.conf
  nginx\         used by the Nginx (sites) service: logs, temp files, certs (local CA)
  logs\          service logs and installer.log
```

- **Move or copy** the whole folder (exe, `bin` and `data`) to another drive or a USB stick. Stop Zero2Dev first.
- **Back up** by copying `data\` while Zero2Dev is stopped. `bin\` only holds downloads that can be installed again.
- **Reset** by stopping Zero2Dev and deleting `data\` (and `bin\` to remove the runtimes too).
- **Upgrading from 0.0.1**: runtimes you installed before are moved from `data\runtimes` to `bin` the first time 0.0.2 starts.
- **Different location**: set `ZERO2DEV_HOME` to another data folder (runtimes then go to its `bin` subfolder) and, if you like, `ZERO2DEV_BIN` to another runtimes folder.

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
