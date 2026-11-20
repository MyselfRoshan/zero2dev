package dbtools

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// stderrLines is how many of the last stderr lines an error carries.
const stderrLines = 8

// DefaultRunner runs c with os/exec: never through a shell, with no console
// window on Windows, stopped when ctx ends or c.Timeout passes. A failure
// carries the last lines the program wrote to stderr.
func DefaultRunner(ctx context.Context, c Cmd) error {
	if c.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.Timeout)
		defer cancel()
	}
	cmd := exec.CommandContext(ctx, c.Bin, c.Args...) // never a shell
	cmd.Env = append(os.Environ(), c.Env...)          // later entries win
	cmd.Dir = c.Dir
	if cmd.Dir == "" {
		cmd.Dir = filepath.Dir(c.Bin)
	}
	cmd.Stdin = c.Stdin
	cmd.Stdout = c.Stdout
	if cmd.Stdout == nil {
		cmd.Stdout = io.Discard
	}
	tail := &tailWriter{max: stderrLines}
	cmd.Stderr = tail
	cmd.WaitDelay = 2 * time.Second
	hideWindow(cmd)
	err := cmd.Run()
	name := progName(c.Bin)
	if ctx.Err() == context.DeadlineExceeded {
		return fmt.Errorf("%s timed out after %s", name, c.Timeout)
	}
	if err == nil {
		return nil
	}
	if msg := tail.String(); msg != "" {
		return fmt.Errorf("%s: %s", name, msg)
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return fmt.Errorf("%s exited with code %d", name, ee.ExitCode())
	}
	return fmt.Errorf("%s: %v", name, err)
}

// progName is the program's file name without .exe, for messages.
func progName(bin string) string {
	b := filepath.Base(bin)
	return strings.TrimSuffix(b, filepath.Ext(b))
}

// tailWriter keeps the last max non-empty lines written to it.
type tailWriter struct {
	mu    sync.Mutex
	max   int
	part  []byte
	lines []string
}

func (w *tailWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.part = append(w.part, p...)
	for {
		i := bytes.IndexByte(w.part, '\n')
		if i < 0 {
			break
		}
		w.add(string(w.part[:i]))
		w.part = w.part[i+1:]
	}
	if len(w.part) > 64*1024 { // a very long line: keep its start
		w.add(string(w.part[:4096]))
		w.part = w.part[:0]
	}
	return len(p), nil
}

func (w *tailWriter) add(line string) {
	line = strings.TrimSpace(line)
	if line == "" {
		return
	}
	w.lines = append(w.lines, line)
	if len(w.lines) > w.max {
		w.lines = w.lines[len(w.lines)-w.max:]
	}
}

// String is the kept lines, joined with "; ".
func (w *tailWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.part) > 0 {
		w.add(string(w.part))
		w.part = nil
	}
	return strings.Join(w.lines, "; ")
}

// lineLogger sends whole lines to a log function.
type lineLogger struct {
	mu   sync.Mutex
	emit func(string)
	part []byte
}

func (w *lineLogger) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.part = append(w.part, p...)
	for {
		i := bytes.IndexByte(w.part, '\n')
		if i < 0 {
			break
		}
		if t := strings.TrimRight(string(w.part[:i]), "\r"); strings.TrimSpace(t) != "" {
			w.emit(t)
		}
		w.part = w.part[i+1:]
	}
	if len(w.part) > 16*1024 {
		w.emit(string(w.part))
		w.part = w.part[:0]
	}
	return len(p), nil
}

func (w *lineLogger) flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if t := strings.TrimSpace(string(w.part)); t != "" {
		w.emit(t)
	}
	w.part = nil
}
