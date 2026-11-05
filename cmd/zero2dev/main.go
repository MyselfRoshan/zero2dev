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
	"zero2dev/internal/model"
	"zero2dev/internal/runtimes"
	"zero2dev/internal/sites"
	"zero2dev/internal/store"
	"zero2dev/internal/supervisor"
	"zero2dev/internal/webui"
)

// version is overridden with -ldflags "-X main.version=...".
var version = "0.0.1"

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
	// Relocatable: re-point installed runtimes if the folder was moved.
	if n, err := st.RebaseRuntimes(cfg.Home); err != nil {
		return err
	} else if n > 0 {
		fmt.Printf("Data folder moved: re-pointed %d installed runtime(s)\n", n)
	}
	clearDir(cfg.TmpDir()) // leftovers of an interrupted install
	hub := events.New()
	sup := supervisor.New(cfg.Home, st, hub)
	rt := runtimes.New(cfg.Home, st, hub)
	site := sites.New(cfg.Home, st, hub, sup)
	inst := installer.New(cfg.Home, st, rt, hub)
	inst.Version = version
	// A new caddy runtime gets a Caddy service automatically.
	rt.OnChange = func() { _, _ = site.EnsureCaddy() }

	if _, err := site.EnsureCaddy(); err != nil {
		return err
	}
	if err := site.Regenerate(); err != nil {
		return fmt.Errorf("cannot write Caddyfile: %w", err)
	}

	srv := api.New(api.Deps{
		Cfg: cfg, Store: st, Hub: hub, Sup: sup, Runtimes: rt, Installer: inst, Sites: site,
		Catalog: func() ([]model.CatalogEntry, error) { return catalog.Load(cfg.Home) },
		UI:      webui.FS(), Version: version, Started: time.Now(),
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
	kctx, kcancel := context.WithTimeout(context.Background(), 15*time.Second)
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
