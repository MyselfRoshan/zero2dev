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
	"syscall"
	"time"

	"zero2dev/internal/api"
	"zero2dev/internal/catalog"
	"zero2dev/internal/config"
	"zero2dev/internal/events"
	"zero2dev/internal/installer"
	"zero2dev/internal/launch"
	"zero2dev/internal/model"
	"zero2dev/internal/runtimes"
	"zero2dev/internal/sites"
	"zero2dev/internal/store"
	"zero2dev/internal/supervisor"
	"zero2dev/internal/webui"
	"zero2dev/internal/winpick"
)

// version is overridden with -ldflags "-X main.version=...".
var version = "0.0.3"

func main() {
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()
	if *showVersion {
		fmt.Println("zero2dev", version)
		return
	}
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "zero2dev:", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.FromEnv(os.Getenv)
	if err != nil {
		return err
	}
	// Everything the daemon (and children that inherit its env) writes to
	// temp goes below <home>/tmp, never the system temp folder.
	cfg.ApplyTempEnv()
	st, err := store.Open(filepath.Join(cfg.Home, "state.json"))
	if err != nil {
		return err
	}
	if n := migrateLegacyRuntimes(cfg.Home, cfg.BinDir); n > 0 {
		fmt.Printf("Moved %d installed runtime(s) from data\\runtimes to %s\n", n, cfg.BinDir)
	}
	// Relocatable: re-point installed runtimes if the folder was moved.
	if n, err := st.RebaseRuntimes(cfg.BinDir); err != nil {
		return err
	} else if n > 0 {
		fmt.Printf("Data folder moved: re-pointed %d installed runtime(s)\n", n)
	}
	clearDir(cfg.TmpDir()) // leftovers of an interrupted install
	hub := events.New()
	sup := supervisor.New(cfg.Home, st, hub)
	rt := runtimes.New(cfg.Home, st, hub)
	rt.Bin = cfg.BinDir
	site := sites.New(cfg.Home, st, hub, sup)
	inst := installer.New(cfg.Home, st, rt, hub)
	inst.Bin = cfg.BinDir
	inst.Version = version
	// A new caddy (or, when nginx serves the sites, nginx) runtime gets its
	// site server service automatically.
	rt.OnChange = func() { _, _ = site.EnsureServers() }

	if _, err := site.EnsureServers(); err != nil {
		return err
	}
	if err := site.Regenerate(); err != nil {
		return fmt.Errorf("cannot write the site server config: %w", err)
	}

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
	srv := api.New(api.Deps{
		Cfg: cfg, Store: st, Hub: hub, Sup: sup, Runtimes: rt, Installer: inst, Sites: site,
		Catalog: func() ([]model.CatalogEntry, error) { return catalog.Load(cfg.Home) }, Remote: remote,
		UI: webui.FS(), Version: version, Started: time.Now(),
		PickFolder: folderPicker(),
		Launch:     launch.Start,
	})

	// Bind loopback only; never 0.0.0.0.
	ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(cfg.Port)))
	if err != nil {
		return fmt.Errorf("cannot listen on 127.0.0.1:%d (is Zero2Dev already running?): %w", cfg.Port, err)
	}
	httpSrv := &http.Server{Handler: srv, ReadHeaderTimeout: 10 * time.Second}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	errc := make(chan error, 1)
	go func() { errc <- httpSrv.Serve(ln) }()

	url := fmt.Sprintf("http://127.0.0.1:%d/#token=%s", cfg.Port, cfg.Token)
	fmt.Printf("Zero2Dev %s listening on 127.0.0.1:%d\n%s\nOpen: %s\n", version, cfg.Port, cfg.Describe(), url)
	if !cfg.NoBrowser && st.Snapshot().Settings.OpenBrowserOnStart {
		openBrowser(url)
	}
	go sup.AutostartAll()

	select {
	case <-ctx.Done():
		fmt.Println("shutting down...")
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
		fmt.Println("could not open the browser automatically; open the URL above manually")
		return
	}
	go func() { _ = cmd.Wait() }()
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
