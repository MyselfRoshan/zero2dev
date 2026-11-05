package main

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestNoWritesOutsideHome builds the real daemon, copies it to a temp
// folder, and runs it WITHOUT ZERO2DEV_HOME while HOME, USERPROFILE,
// APPDATA, LOCALAPPDATA, XDG_* and TMPDIR all point at one empty directory.
// After starting/stopping a service and importing a (fake) caddy runtime
// whose service is started too, that directory must still be empty and the
// data folder must sit next to the copied binary.
func TestNoWritesOutsideHome(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses /bin/sh and sleep")
	}
	goBin, err := exec.LookPath("go")
	if err != nil {
		goBin = filepath.Join(runtime.GOROOT(), "bin", "go")
	}
	root := t.TempDir()
	binDir := filepath.Join(root, "app", "bin")
	os.MkdirAll(binDir, 0o755)
	exe := filepath.Join(binDir, "zero2dev")
	if out, err := exec.Command(goBin, "build", "-o", exe, ".").CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	if out, err := exec.Command(exe, "-version").CombinedOutput(); err != nil || strings.TrimSpace(string(out)) != "zero2dev 0.0.1" {
		t.Fatalf("version: %q %v", out, err)
	}

	empty := filepath.Join(root, "empty")
	os.MkdirAll(empty, 0o755)
	l, _ := net.Listen("tcp", "127.0.0.1:0")
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()

	cmd := exec.Command(exe)
	cmd.Dir = root // not the exe dir, to prove the data folder follows the executable
	cmd.Env = []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + empty, "USERPROFILE=" + empty, "APPDATA=" + empty, "LOCALAPPDATA=" + empty,
		"XDG_DATA_HOME=" + empty, "XDG_CONFIG_HOME=" + empty, "XDG_CACHE_HOME=" + empty, "XDG_STATE_HOME=" + empty,
		"TMPDIR=" + empty, "TMP=" + empty, "TEMP=" + empty,
		fmt.Sprintf("ZERO2DEV_PORT=%d", port), "ZERO2DEV_NO_BROWSER=1",
	}
	var outBuf bytes.Buffer
	cmd.Stdout, cmd.Stderr = &outBuf, &outBuf
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	stopped := false
	stop := func() {
		if stopped {
			return
		}
		stopped = true
		cmd.Process.Signal(syscall.SIGTERM)
		cmd.Wait()
	}
	defer stop()

	data := filepath.Join(binDir, "data")
	var token string
	for i := 0; i < 100; i++ {
		b, _ := os.ReadFile(filepath.Join(data, "token"))
		token = strings.TrimSpace(string(b))
		if token != "" {
			if c, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port)); err == nil {
				c.Close()
				break
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	if token == "" {
		t.Fatalf("daemon did not start in %s:\n%s", data, outBuf.String())
	}

	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	call := func(method, path string, body any, out any) int {
		var rd io.Reader
		if body != nil {
			b, _ := json.Marshal(body)
			rd = bytes.NewReader(b)
		}
		req, _ := http.NewRequest(method, base+path, rd)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if out != nil {
			json.NewDecoder(resp.Body).Decode(out)
		}
		return resp.StatusCode
	}
	waitState := func(id, want string) {
		t.Helper()
		var sv struct{ State string }
		for i := 0; i < 100; i++ {
			call("GET", "/api/services/"+id, nil, &sv)
			if sv.State == want {
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
		t.Fatalf("service %s state %q, want %q\n%s", id, sv.State, want, outBuf.String())
	}

	var status map[string]any
	call("GET", "/api/status", nil, &status)
	if status["homeSource"] != "exe" || status["portable"] != true {
		t.Fatalf("status: %v", status)
	}
	if got, _ := filepath.EvalSymlinks(status["homeDir"].(string)); got != mustReal(t, data) {
		t.Fatalf("homeDir %v, want %s", status["homeDir"], data)
	}

	// 1. a plain service
	var sv struct{ ID string }
	call("POST", "/api/services", map[string]any{"name": "sleeper", "kind": "custom", "command": "sleep", "args": []string{"300"}, "cwd": root}, &sv)
	call("POST", "/api/services/"+sv.ID+"/start", nil, nil)
	waitState(sv.ID, "running")
	call("POST", "/api/services/"+sv.ID+"/stop", nil, nil)
	waitState(sv.ID, "stopped")

	// 2. import a fake caddy (a shell script) and run the auto-created service
	script := "#!/bin/sh\nenv > \"$XDG_DATA_HOME/env.txt\"\nexec sleep 300\n"
	var zb bytes.Buffer
	zw := zip.NewWriter(&zb)
	hdr := &zip.FileHeader{Name: "caddy", Method: zip.Deflate}
	hdr.SetMode(0o755)
	w, _ := zw.CreateHeader(hdr)
	w.Write([]byte(script))
	zw.Close()
	h := sha256.Sum256(zb.Bytes())
	sum := hex.EncodeToString(h[:])
	catalog := fmt.Sprintf(`[{"id":"caddy-fake","kind":"caddy","version":"2.8.4","name":"Caddy fake","url":"https://example.invalid/caddy_fake.zip",
	  "checksum":{"type":"sha256","value":%q},"binaryRelPath":"caddy","stripComponents":0,"os":%q,"arch":%q,"homepage":"https://example.invalid/"}]`, sum, runtime.GOOS, runtime.GOARCH)
	os.WriteFile(filepath.Join(data, "catalog.json"), []byte(catalog), 0o644)
	dl := filepath.Join(root, "dl")
	os.MkdirAll(dl, 0o755)
	file := filepath.Join(dl, "caddy_fake.zip")
	os.WriteFile(file, zb.Bytes(), 0o644)
	var job struct{ ID, State, Error string }
	if code := call("POST", "/api/runtimes/import", map[string]any{"catalogId": "caddy-fake", "filePath": file}, &job); code != 202 {
		t.Fatalf("import: %d", code)
	}
	for i := 0; i < 100 && (job.State == "" || job.State == "running"); i++ {
		time.Sleep(50 * time.Millisecond)
		call("GET", "/api/jobs/"+job.ID, nil, &job)
	}
	if job.State != "done" {
		t.Fatalf("import job: %+v\n%s", job, outBuf.String())
	}
	var svcs []struct{ ID, Kind string }
	call("GET", "/api/services", nil, &svcs)
	var caddyID string
	for _, s := range svcs {
		if s.Kind == "caddy" {
			caddyID = s.ID
		}
	}
	if caddyID == "" {
		t.Fatal("caddy service not auto-created")
	}
	call("POST", "/api/services/"+caddyID+"/start", nil, nil)
	waitState(caddyID, "running")
	var envTxt []byte
	for i := 0; i < 100 && len(envTxt) == 0; i++ {
		envTxt, _ = os.ReadFile(filepath.Join(data, "caddy", "data", "env.txt"))
		time.Sleep(30 * time.Millisecond)
	}
	for _, want := range []string{"XDG_DATA_HOME=" + filepath.Join(data, "caddy", "data"), "XDG_CONFIG_HOME=" + filepath.Join(data, "caddy", "config"), "TMPDIR=" + filepath.Join(data, "tmp")} {
		if !strings.Contains(string(envTxt), want) {
			t.Errorf("caddy env lacks %s:\n%s", want, envTxt)
		}
	}
	if cf, _ := os.ReadFile(filepath.Join(data, "config", "Caddyfile")); !strings.Contains(string(cf), "admin off") {
		t.Errorf("Caddyfile: %s", cf)
	}

	stop()
	ents, _ := os.ReadDir(empty)
	if len(ents) != 0 {
		var names []string
		for _, e := range ents {
			names = append(names, e.Name())
		}
		t.Fatalf("daemon wrote outside its data folder: %v", names)
	}
	if !strings.Contains(outBuf.String(), "Data folder: "+data+" (portable, next to the program)") &&
		!strings.Contains(outBuf.String(), "(portable, next to the program)") {
		t.Fatalf("startup message missing:\n%s", outBuf.String())
	}
	for _, d := range []string{"tmp", "caddy/data", "caddy/config", "runtimes", "logs", "state.json"} {
		if _, err := os.Stat(filepath.Join(data, d)); err != nil {
			t.Errorf("data/%s missing", d)
		}
	}
	if ents, _ := os.ReadDir(filepath.Join(data, "tmp")); len(ents) != 0 {
		t.Errorf("tmp not cleaned: %v", ents)
	}
}

func mustReal(t *testing.T, p string) string {
	r, err := filepath.EvalSymlinks(p)
	if err != nil {
		t.Fatal(err)
	}
	return r
}
