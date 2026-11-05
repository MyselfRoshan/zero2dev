package installer

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const maxInstallerLog = 1 << 20 // rotate at 1 MB

// fileLog appends timestamped lines to <home>/logs/installer.log. The file
// is opened per write: writes are rare and this avoids holding a handle
// (which would block rotation on Windows).
type fileLog struct {
	mu   sync.Mutex
	path string
}

func (l *fileLog) printf(job, format string, a ...any) {
	msg := strings.ReplaceAll(fmt.Sprintf(format, a...), "\n", " ")
	line := time.Now().UTC().Format(time.RFC3339) + " [" + job + "] " + msg + "\n"
	l.mu.Lock()
	defer l.mu.Unlock()
	if st, err := os.Stat(l.path); err == nil && st.Size()+int64(len(line)) > maxInstallerLog {
		_ = os.Remove(l.path + ".1")
		_ = os.Rename(l.path, l.path+".1")
	}
	if err := os.MkdirAll(filepath.Dir(l.path), 0o755); err != nil {
		return
	}
	f, err := os.OpenFile(l.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.WriteString(line)
}

// Tail returns the last n lines (reaching into the rotated file if needed).
func (l *fileLog) Tail(n int) []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	var lines []string
	for _, p := range []string{l.path + ".1", l.path} {
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		for _, s := range strings.Split(strings.TrimRight(string(b), "\n"), "\n") {
			if s != "" {
				lines = append(lines, s)
			}
		}
	}
	if n < len(lines) {
		lines = lines[len(lines)-n:]
	}
	if lines == nil {
		lines = []string{}
	}
	return lines
}
