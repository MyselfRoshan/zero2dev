package main

import (
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAlreadyRunning(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/status" && r.Header.Get("Authorization") == "Bearer good" {
			w.WriteHeader(200)
			return
		}
		w.WriteHeader(401)
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
