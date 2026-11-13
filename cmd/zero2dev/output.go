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

// instanceProof answers a fresh challenge without disclosing the bearer token.
func instanceProof(token, challenge string) string {
	mac := hmac.New(sha256.New, []byte(token))
	mac.Write([]byte("zero2dev-instance:" + challenge))
	return hex.EncodeToString(mac.Sum(nil))
}

func instanceHandler(token string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" && r.URL.Path == "/.well-known/zero2dev-instance" {
			challenge := r.URL.Query().Get("challenge")
			if b, err := hex.DecodeString(challenge); err != nil || len(b) != 32 {
				http.Error(w, "invalid challenge", 400)
				return
			}
			w.Header().Set("Cache-Control", "no-store")
			fmt.Fprint(w, instanceProof(token, challenge))
			return
		}
		next.ServeHTTP(w, r)
	})
}

// alreadyRunning verifies knowledge of our token without sending it to the listener.
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
	// Never follow a redirect to an unrelated listener.
	client := *c
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	proof, err := io.ReadAll(io.LimitReader(resp.Body, 65))
	return err == nil && resp.StatusCode == http.StatusOK && hmac.Equal(proof, []byte(instanceProof(token, challenge)))
}
