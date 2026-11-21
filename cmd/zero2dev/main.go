// Command zero2dev is the Zero2Dev daemon: it serves the dashboard and API
// on 127.0.0.1 and supervises local services.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"zero2dev/internal/api"
	"zero2dev/internal/catalog"
	"zero2dev/internal/config"
	"zero2dev/internal/dbtools"
	"zero2dev/internal/envreg"
	"zero2dev/internal/events"
	"zero2dev/internal/installer"
	"zero2dev/internal/jobs"
	"zero2dev/internal/launch"
	"zero2dev/internal/migrate"
	"zero2dev/internal/model"
	"zero2dev/internal/newproject"
	"zero2dev/internal/projectfile"
	"zero2dev/internal/runtimes"
	"zero2dev/internal/sites"
	"zero2dev/internal/snapshot"
	"zero2dev/internal/store"
	"zero2dev/internal/supervisor"
	"zero2dev/internal/tasks"
	"zero2dev/internal/tray"
	"zero2dev/internal/webui"
	"zero2dev/internal/winpick"
)

// version is overridden with -ldflags "-X main.version=...".
var version = "0.0.6"

func main() {
	setupOutput()
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()
	if *showVersion {
		say("zero2dev %s", version)
		return
	}
	if err := run(); err != nil {
		fail(err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.ResolveEnv(os.Getenv)
	if err != nil {
		return err
	}
	// Bind before any shared writes; the listener owns this port until shutdown.
	url := fmt.Sprintf("http://127.0.0.1:%d/#token=", cfg.Port)
	ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(cfg.Port)))
	if err != nil {
		b, readErr := os.ReadFile(filepath.Join(cfg.Home, "token"))
		token := strings.TrimSpace(string(b))
		if readErr == nil && alreadyRunning(cfg.Port, token, &http.Client{Timeout: 3 * time.Second}) {
			say("Zero2Dev is already running; opening the dashboard.")
			if !cfg.NoBrowser {
				openBrowser(url + token)
			}
			return nil
		}
		return fmt.Errorf("port %d on 127.0.0.1 is used by another program, so Zero2Dev cannot start. "+
			"Close that program, or set the ZERO2DEV_PORT environment variable to another port: %w", cfg.Port, err)
	}
	defer ln.Close()
	release, err := lockHome(cfg.Home)
	if err != nil {
		return err
	}
	defer release()
	if err := cfg.Initialize(); err != nil {
		return err
	}
	url += cfg.Token
	// Everything the daemon (and children that inherit its env) writes to
	// temp goes below <home>/tmp, never the system temp folder.
	cfg.ApplyTempEnv()
	if !interactive {
		logToFile(cfg.Home)
	}
	st, err := store.Open(filepath.Join(cfg.Home, "state.json"))
	if err != nil {
		return err
	}
	if n := migrateLegacyRuntimes(cfg.Home, cfg.BinDir); n > 0 {
		say("Moved %d installed runtime(s) from data\\runtimes to %s", n, cfg.BinDir)
	}
	// Relocatable: re-point installed runtimes if the folder was moved.
	if n, err := st.RebaseRuntimes(cfg.BinDir); err != nil {
		return err
	} else if n > 0 {
		say("Data folder moved: re-pointed %d installed runtime(s)", n)
	}
	clearDir(cfg.TmpDir()) // leftovers of an interrupted install
	hub := events.New()
	sup := supervisor.New(cfg.Home, st, hub)
	rt := runtimes.New(cfg.Home, st, hub)
	rt.Bin = cfg.BinDir
	// VS Code and VSCodium in bin get (or, after the folder moved, fix) their
	// "data" link, so a double-click on Code.exe uses Zero2Dev's settings.
	for _, err := range rt.RepairEditorData() {
		say("Editor data: %v", err)
	}
	site := sites.New(cfg.Home, st, hub, sup)
	inst := installer.New(cfg.Home, st, rt, hub)
	inst.Bin = cfg.BinDir
	inst.Version = version
	// A new caddy (or, when nginx serves the sites, nginx) runtime gets its
	// site server service automatically.
	// Every runtime change also refreshes the shims folder and the items of the Environment
	// page that follow a runtime (JAVA_HOME of the default Java, ...), when the user turned
	// any on.
	refreshShims := func() {
		if err := envreg.Refresh(cfg.Home, st.Snapshot()); err != nil {
			say("shims: %v", err)
		}
	}
	rt.OnChange = func() {
		_, _ = site.EnsureServers()
		refreshShims()
		hub.Publish("environment.changed", struct{}{})
	}

	if _, err := site.EnsureServers(); err != nil {
		return err
	}
	if err := site.Regenerate(); err != nil {
		return fmt.Errorf("cannot write the site server config: %w", err)
	}
	refreshShims()

	remote := &catalog.Remote{
		Home: cfg.Home, Version: version,
		Client: func() *http.Client { return installer.HTTPClient(st.Snapshot().Settings.DownloadProxy) },
		// Loopback-only overrides for tests and offline mirrors; the defaults are the publishers' own sites.
		PHPURL: os.Getenv("ZERO2DEV_PHP_RELEASES_URL"), AdoptiumURL: os.Getenv("ZERO2DEV_ADOPTIUM_URL"),
		NginxPage: os.Getenv("ZERO2DEV_NGINX_PAGE_URL"), NginxBase: os.Getenv("ZERO2DEV_NGINX_BASE_URL"),
		NodeURL: os.Getenv("ZERO2DEV_NODE_URL"), NodeBase: os.Getenv("ZERO2DEV_NODE_BASE_URL"),
		GoURL: os.Getenv("ZERO2DEV_GO_URL"), GoBase: os.Getenv("ZERO2DEV_GO_BASE_URL"),
		CaddyURL: os.Getenv("ZERO2DEV_CADDY_URL"), PHPArchiveURL: os.Getenv("ZERO2DEV_PHP_ARCHIVE_URL"),
	}
	// Crash balloons go to the tray icon once it is up (it is created after the API starts).
	var trayIcon atomic.Value // tray.Icon
	sup.OnCrash = func(c supervisor.Crash) {
		if title, text := crashNotice(c); title != "" {
			if ti, ok := trayIcon.Load().(tray.Icon); ok {
				ti.Notify(title, text)
			}
		}
	}
	// One way to stop for all: Ctrl+C, the tray's Quit, and POST /api/shutdown.
	quitCtx, quit := context.WithCancel(context.Background())
	defer quit()
	shutdownDone := make(chan struct{})
	removeConsoleQuit := consoleQuit(quit, shutdownDone)
	defer removeConsoleQuit()
	defer close(shutdownDone)
	// 0.0.5: database tools, snapshots and Import share one job registry.
	loadCatalog := func() ([]model.CatalogEntry, error) { return catalog.Load(cfg.Home) }
	jobReg := jobs.New(hub)
	dbm := dbtools.New(st, sup, jobReg, cfg.Home)
	snaps := snapshot.New(cfg.Home, version, st, sup, jobReg, site, hub.Publish, loadCatalog)
	migr := migrate.New(cfg.Home, cfg.BinDir, st, jobReg, dbm, hub.Publish)
	// 0.0.6: one-off tasks and New project.
	taskMgr := tasks.New(cfg.Home, st, sup, jobReg)
	newProj := newproject.New(cfg.Home, st, sup, inst, rt, site, dbm, jobReg, hub.Publish)
	newProj.Launch = launch.Start
	newProj.Catalog = loadCatalog
	projFile := projectfile.New(cfg.Home, st, sup, inst, rt, site, jobReg, hub.Publish)
	srv := api.New(api.Deps{
		Cfg: cfg, Store: st, Hub: hub, Sup: sup, Runtimes: rt, Installer: inst, Sites: site,
		Catalog: loadCatalog, Remote: remote,
		UI: webui.FS(), Version: version, Started: time.Now(),
		PickFolder: folderPicker(),
		PickFile:   filePicker(),
		Launch:     launch.Start,
		Terminal:   launch.OpenTerminal,
		EnvApply:   func(d store.Data) error { return envreg.Apply(cfg.Home, d) },
		EnvReport:  func(d store.Data) envreg.Report { return envreg.Inspect(cfg.Home, d) },
		Quit:       quit,
		Jobs:       jobReg, DB: dbm, Snapshots: snaps, Migrate: migr,
		Tasks: taskMgr, NewProject: newProj, ProjectFile: projFile,
	})

	httpSrv := &http.Server{Handler: srv, ReadHeaderTimeout: 10 * time.Second}

	ctx, stop := signal.NotifyContext(quitCtx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	errc := make(chan error, 1)
	go func() { errc <- httpSrv.Serve(ln) }()

	say("Zero2Dev %s listening on 127.0.0.1:%d", version, cfg.Port)
	say("%s", cfg.Describe())
	say("Open: %s", url)
	openOnStart := !cfg.NoBrowser && st.Snapshot().Settings.OpenBrowserOnStart

	// The tray icon is how Zero2Dev is found and stopped without a console window.
	var ti tray.Icon
	ti, terr := tray.Start(tray.Menu{
		Tooltip:  "Zero2Dev",
		Taskbar:  true, // a taskbar button: Windows shows it as running
		OnOpen:   func() { openBrowser(url) },
		OnFolder: func() { openFolder(cfg.Home) },
		OnStopAll: func() {
			title, text := stopAllNotice(sup.StopAllOrdered())
			if ti != nil {
				ti.Notify(title, text)
			}
		},
		OnQuit: quit,
	})
	if terr == nil {
		trayIcon.Store(ti)
		defer ti.Close()
		go trayTooltip(ctx, ti, st, sup)
		if !openOnStart {
			ti.Notify("Zero2Dev is running", "Click here or the Zero2Dev icon to open the dashboard.")
		}
	} else if !errors.Is(terr, tray.ErrUnsupported) {
		say("tray icon unavailable: %v", terr)
	}
	if openOnStart {
		openBrowser(url)
	}
	go sup.AutostartAll()

	select {
	case <-ctx.Done():
		say("shutting down...")
		if ti != nil {
			ti.SetTooltip("Zero2Dev: stopping services...")
		}
	case err := <-errc:
		if !errors.Is(err, http.ErrServerClosed) {
			sup.StopAll(context.Background())
			return err
		}
	}
	hub.Close() // ends SSE handlers so Shutdown can finish
	sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = httpSrv.Shutdown(sctx)
	// Children must never outlive the daemon.
	// Databases get supervisor.DBGrace to shut down cleanly, so allow a bit more.
	kctx, kcancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer kcancel()
	sup.StopAll(kctx)
	return nil
}

// openBrowser launches the default browser without a shell.
func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	if err := cmd.Start(); err != nil {
		say("could not open the browser automatically; open %s manually", url)
		return
	}
	go func() { _ = cmd.Wait() }()
}

// openFolder shows dir in the file manager.
func openFolder(dir string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("explorer", dir)
	case "darwin":
		cmd = exec.Command("open", dir)
	default:
		cmd = exec.Command("xdg-open", dir)
	}
	if cmd.Start() == nil {
		go func() { _ = cmd.Wait() }()
	}
}

// stopAllNotice words the tray balloon shown after "Stop all services".
func stopAllNotice(rep supervisor.StopReport) (title, text string) {
	plural := func(n int) string {
		if n == 1 {
			return "1 service"
		}
		return fmt.Sprintf("%d services", n)
	}
	switch {
	case len(rep.Failed) > 0:
		names := make([]string, 0, 3)
		for i, f := range rep.Failed {
			if i == 3 {
				names = append(names, fmt.Sprintf("and %d more", len(rep.Failed)-3))
				break
			}
			names = append(names, f.Name)
		}
		return "Some services did not stop", fmt.Sprintf("Stopped %d. Still running: %s.", len(rep.Stopped), strings.Join(names, ", "))
	case len(rep.Stopped) == 0:
		return "Nothing to stop", "No services were running."
	}
	return "Services stopped", "Stopped " + plural(len(rep.Stopped)) + ". Zero2Dev is still running."
}

// trayTooltip keeps the tray's hover text on the number of running services.
func trayTooltip(ctx context.Context, ti tray.Icon, st *store.Store, sup *supervisor.Supervisor) {
	t := time.NewTicker(3 * time.Second)
	defer t.Stop()
	for {
		n := 0
		for _, sv := range st.Snapshot().Services {
			if sup.State(sv.ID) == model.StateRunning {
				n++
			}
		}
		switch n {
		case 0:
			ti.SetTooltip("Zero2Dev: no services running")
		case 1:
			ti.SetTooltip("Zero2Dev: 1 service running")
		default:
			ti.SetTooltip(fmt.Sprintf("Zero2Dev: %d services running", n))
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// clearDir removes the contents of dir (not dir itself).
func clearDir(dir string) {
	ents, _ := os.ReadDir(dir)
	for _, e := range ents {
		_ = os.RemoveAll(filepath.Join(dir, e.Name()))
	}
}

// folderPicker is the native folder dialog when this OS has one, else nil
// (the UI then hides its Browse buttons).
func folderPicker() func(title, start string) (string, bool, error) {
	if !winpick.Supported() {
		return nil
	}
	return winpick.Folder
}

func filePicker() func(o winpick.FileOptions) (string, bool, error) {
	if !winpick.Supported() {
		return nil
	}
	return winpick.File
}
