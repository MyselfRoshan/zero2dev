package main

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

// On Windows zero2dev.exe is a GUI-subsystem program: double-clicking it
// opens no console window. Started from a terminal it attaches to that
// terminal and prints there; otherwise its messages go to
// data/logs/zero2dev.log (output_windows.go).

var (
	outMu sync.Mutex
	out   io.Writer = os.Stdout
	// interactive is true when the messages reach a terminal; startup errors are
	// then printed, otherwise they are shown in a message box.
	interactive = true
)

// say prints one message line, with a timestamp in the log file.
func say(format string, args ...any) {
	outMu.Lock()
	defer outMu.Unlock()
	line := fmt.Sprintf(format, args...)
	if !interactive {
		line = time.Now().Format("2006-01-02 15:04:05 ") + line
	}
	fmt.Fprintln(out, line)
}

// logToFile sends the messages to <home>/logs/zero2dev.log (rotated once at 1 MB).
func logToFile(home string) {
	dir := filepath.Join(home, "logs")
	_ = os.MkdirAll(dir, 0o755)
	path := filepath.Join(dir, "zero2dev.log")
	if st, err := os.Stat(path); err == nil && st.Size() > 1<<20 {
		_ = os.Rename(path, path+".1")
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return
	}
	outMu.Lock()
	out = f
	outMu.Unlock()
}

// fail reports a startup error where the user will see it.
func fail(err error) {
	say("zero2dev: %v", err)
	if !interactive {
		alert("Zero2Dev could not start", err.Error())
	}
}

// alreadyRunning verifies a fresh challenge using the shared token without sending it.
func alreadyRunning(port int, token string, c *http.Client) bool {
	if token == "" {
		return false
	}
	nonce := make([]byte, 32)
	if _, err := rand.Read(nonce); err != nil {
		return false
	}
	challenge := hex.EncodeToString(nonce)
	req, err := http.NewRequest("GET", "http://127.0.0.1:"+strconv.Itoa(port)+"/.well-known/zero2dev-instance?challenge="+challenge, nil)
	if err != nil {
		return false
	}
	// Do not follow redirects or route loopback traffic through an environment proxy.
	client := *c
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	defer transport.CloseIdleConnections()
	client.Transport = transport
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	proof, err := io.ReadAll(io.LimitReader(resp.Body, 65))
	mac := hmac.New(sha256.New, []byte(token))
	mac.Write([]byte("zero2dev-instance:" + challenge))
	return err == nil && resp.StatusCode == http.StatusOK && hmac.Equal(proof, []byte(hex.EncodeToString(mac.Sum(nil))))
}
