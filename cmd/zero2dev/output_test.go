package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"zero2dev/internal/api"
	"zero2dev/internal/config"
)

func TestAlreadyRunning(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Error("probe leaked credentials")
		}
		if r.URL.Path == "/.well-known/zero2dev-instance" {
			mac := hmac.New(sha256.New, []byte("good"))
			mac.Write([]byte("zero2dev-instance:" + r.URL.Query().Get("challenge")))
			io.WriteString(w, hex.EncodeToString(mac.Sum(nil)))
			return
		}
		w.WriteHeader(404)
	}))
	defer srv.Close()
	port := srv.Listener.Addr().(*net.TCPAddr).Port
	c := &http.Client{Timeout: time.Second}
	if !alreadyRunning(port, "good", c) {
		t.Error("a Zero2Dev answering with our token is running")
	}
	if alreadyRunning(port, "other", c) {
		t.Error("another Zero2Dev (other token) or another program is not ours")
	}
	l, _ := net.Listen("tcp", "127.0.0.1:0")
	free := l.Addr().(*net.TCPAddr).Port
	l.Close()
	if alreadyRunning(free, "good", c) {
		t.Error("nothing listening: not running")
	}
}

func TestLogToFile(t *testing.T) {
	home := t.TempDir()
	saved, savedI := out, interactive
	defer func() { out, interactive = saved, savedI }()
	interactive = false
	logToFile(home)
	say("hello %d", 42)
	b, err := os.ReadFile(filepath.Join(home, "logs", "zero2dev.log"))
	if err != nil || !strings.Contains(string(b), "hello 42") || !strings.HasPrefix(string(b), "20") {
		t.Fatalf("log %q %v", b, err)
	}
	if f, ok := out.(*os.File); ok {
		f.Close()
	}
}

func TestAlreadyRunningUntrustedListener(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Error("token leaked")
		}
		http.Redirect(w, r, "/api/status", http.StatusFound)
	}))
	defer srv.Close()
	if alreadyRunning(srv.Listener.Addr().(*net.TCPAddr).Port, "secret", srv.Client()) {
		t.Fatal("accepted redirect")
	}
}

func TestAlreadyRunningActualServer(t *testing.T) {
	cfg := &config.Config{Token: "shared-token"}
	srv := httptest.NewUnstartedServer(nil)
	cfg.Port = srv.Listener.Addr().(*net.TCPAddr).Port
	srv.Config.Handler = api.New(api.Deps{Cfg: cfg})
	srv.Start()
	defer srv.Close()
	if !alreadyRunning(cfg.Port, cfg.Token, srv.Client()) {
		t.Fatal("actual server proof rejected")
	}
	if alreadyRunning(cfg.Port, "wrong-token", srv.Client()) {
		t.Fatal("accepted wrong owner")
	}
}

func TestAlreadyRunningDoesNotDiscloseToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" || strings.Contains(r.URL.String(), "secret") {
			t.Error("token disclosed")
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	if alreadyRunning(srv.Listener.Addr().(*net.TCPAddr).Port, "secret", srv.Client()) {
		t.Fatal("unverified listener accepted")
	}
}
