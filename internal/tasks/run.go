package tasks

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

// ExitError is what a Runner returns when the program ran and ended with a non-zero code.
type ExitError struct{ Code int }

func (e *ExitError) Error() string { return fmt.Sprintf("exit code %d", e.Code) }

// DefaultRunner runs c with os/exec: never through a shell, with no console window on Windows,
// its stdout and stderr merged into c.Out, the whole process tree ended when ctx ends.
func DefaultRunner(ctx context.Context, c Cmd) error {
	cmd := exec.CommandContext(ctx, c.Bin, c.Args...) // never a shell
	cmd.Env = c.Env
	if len(cmd.Env) == 0 {
		cmd.Env = os.Environ()
	}
	cmd.Dir = c.Dir
	var out io.Writer = c.Out
	if out == nil {
		out = io.Discard
	}
	cmd.Stdout, cmd.Stderr = out, out // the same writer: one pipe, lines in the order they came
	cmd.WaitDelay = 2 * time.Second
	configure(cmd)
	err := cmd.Run()
	var ee *exec.ExitError
	if errors.As(err, &ee) && ctx.Err() == nil {
		return &ExitError{Code: ee.ExitCode()}
	}
	return err
}

// lineWriter sends whole lines to emit and keeps the last few of them.
type lineWriter struct {
	mu    sync.Mutex
	emit  func(string)
	part  []byte
	tail  []string
	limit int
}

func (w *lineWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.part = append(w.part, p...)
	for {
		i := bytes.IndexByte(w.part, '\n')
		if i < 0 {
			break
		}
		w.line(string(w.part[:i]))
		w.part = w.part[i+1:]
	}
	if len(w.part) > 16*1024 { // a very long line without a break
		w.line(string(w.part))
		w.part = w.part[:0]
	}
	return len(p), nil
}

func (w *lineWriter) line(s string) {
	s = strings.TrimRight(s, "\r")
	if i := strings.LastIndexByte(s, '\r'); i >= 0 { // a progress line redraws itself: keep the last state
		s = s[i+1:]
	}
	if strings.TrimSpace(s) == "" {
		return
	}
	w.emit(s)
	w.tail = append(w.tail, strings.TrimSpace(s))
	if len(w.tail) > w.limit {
		w.tail = w.tail[len(w.tail)-w.limit:]
	}
}

// flush writes the last, unterminated line.
func (w *lineWriter) flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.part) > 0 {
		w.line(string(w.part))
		w.part = nil
	}
}

func (w *lineWriter) lastLines() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return strings.Join(w.tail, "; ")
}

// progName is the program's file name without its extension, for messages.
func progName(bin string) string {
	b := filepath.Base(bin)
	return strings.TrimSuffix(b, filepath.Ext(b))
}
