package supervisor

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"zero2dev/internal/model"
)

const (
	ringCap      = 2000
	maxLogFile   = 5 * 1024 * 1024
	maxLineBytes = 16 * 1024
)

// svcLog keeps the last ringCap lines in memory and appends every line to a
// rotating file (<logs>/<id>.log, rotated at 5 MB to .log.1).
type svcLog struct {
	mu    sync.Mutex
	lines []model.LogLine
	path  string
	f     *os.File
	size  int64
	max   int64
}

func newSvcLog(path string) *svcLog {
	l := &svcLog{path: path, max: maxLogFile}
	l.lines = readTail(path, ringCap) // survive daemon restarts
	return l
}

func (l *svcLog) add(line model.LogLine) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lines = append(l.lines, line)
	if len(l.lines) >= ringCap*2 {
		l.lines = append([]model.LogLine(nil), l.lines[len(l.lines)-ringCap:]...)
	}
	l.writeFile(line)
}

func (l *svcLog) writeFile(line model.LogLine) {
	rec := line.Ts + "\t" + line.Stream + "\t" + line.Text + "\n"
	if l.f != nil && l.size+int64(len(rec)) > l.max {
		l.f.Close()
		l.f = nil
		_ = os.Remove(l.path + ".1")
		_ = os.Rename(l.path, l.path+".1")
	}
	if l.f == nil {
		if err := os.MkdirAll(filepath.Dir(l.path), 0o755); err != nil {
			return
		}
		f, err := os.OpenFile(l.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if err != nil {
			return
		}
		st, _ := f.Stat()
		l.f = f
		if st != nil {
			l.size = st.Size()
		}
		if l.size+int64(len(rec)) > l.max && l.size > 0 { // oversized leftover
			l.f.Close()
			l.f = nil
			_ = os.Remove(l.path + ".1")
			_ = os.Rename(l.path, l.path+".1")
			f, err := os.OpenFile(l.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
			if err != nil {
				return
			}
			l.f, l.size = f, 0
		}
	}
	n, _ := l.f.WriteString(rec)
	l.size += int64(n)
}

func (l *svcLog) tail(n int) []model.LogLine {
	l.mu.Lock()
	defer l.mu.Unlock()
	src := l.lines
	if len(src) > ringCap {
		src = src[len(src)-ringCap:]
	}
	if n > len(src) {
		n = len(src)
	}
	return append([]model.LogLine{}, src[len(src)-n:]...)
}

// close releases the file handle (needed before deleting on Windows).
func (l *svcLog) close() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.f != nil {
		l.f.Close()
		l.f = nil
	}
}

func readTail(path string, n int) []model.LogLine {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []model.LogLine
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		p := strings.SplitN(sc.Text(), "\t", 3)
		if len(p) != 3 {
			continue
		}
		out = append(out, model.LogLine{Ts: p[0], Stream: p[1], Text: p[2]})
		if len(out) > n*2 {
			out = out[len(out)-n:]
		}
	}
	if len(out) > n {
		out = out[len(out)-n:]
	}
	return out
}

func tsNow() string { return time.Now().UTC().Format("2006-01-02T15:04:05.000Z") }

// lineWriter splits a byte stream into lines and calls emit for each.
type lineWriter struct {
	mu   sync.Mutex
	buf  []byte
	emit func(string)
}

func (w *lineWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.buf = append(w.buf, p...)
	for {
		i := indexByte(w.buf, '\n')
		if i < 0 {
			break
		}
		w.emit(cleanLine(w.buf[:i]))
		w.buf = w.buf[i+1:]
	}
	if len(w.buf) > maxLineBytes { // a line with no newline: flush in chunks
		w.emit(cleanLine(w.buf))
		w.buf = nil
	}
	return len(p), nil
}

// Flush emits any trailing partial line.
func (w *lineWriter) Flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.buf) > 0 {
		w.emit(cleanLine(w.buf))
		w.buf = nil
	}
}

func indexByte(b []byte, c byte) int {
	for i, v := range b {
		if v == c {
			return i
		}
	}
	return -1
}

func cleanLine(b []byte) string {
	s := strings.TrimRight(string(b), "\r")
	if !utf8.ValidString(s) {
		s = strings.ToValidUTF8(s, "�")
	}
	if len(s) > maxLineBytes {
		s = s[:maxLineBytes] + "…"
	}
	return s
}
