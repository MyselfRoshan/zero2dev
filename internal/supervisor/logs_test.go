package supervisor

import (
	"os"
	"path/filepath"
	"testing"

	"zero2dev/internal/model"
)

// A line that arrives after close (the "stopped" line comes after the process
// is reaped, and so after StopAll closed the logs) must not reopen the file:
// an open file cannot be deleted on Windows.
func TestClosedLogDoesNotReopenFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "logs", "svc.log")
	l := newSvcLog(p)
	l.add(model.LogLine{Ts: "t1", Stream: "system", Text: "starting"})
	l.close()
	l.add(model.LogLine{Ts: "t2", Stream: "system", Text: "stopped"})

	if l.f != nil {
		t.Fatal("close must be final: the file was opened again")
	}
	if got := l.tail(10); len(got) != 2 || got[1].Text != "stopped" {
		t.Fatalf("lines after close still belong in memory: %+v", got)
	}
	if err := os.Remove(p); err != nil {
		t.Fatalf("the log file must be removable after close: %v", err)
	}
}

// After StopAll, a log created for the first time is closed from the start.
func TestLogsAfterStopAllStayInMemory(t *testing.T) {
	s := &Supervisor{home: t.TempDir(), logs: map[string]*svcLog{}, procs: map[string]*proc{}, crash: map[string]*crashState{}, em: nopEmitter{}}
	s.StopAll(t.Context())
	s.logLine("svc_late", "system", "late line")
	if _, err := os.Stat(s.logPath("svc_late")); !os.IsNotExist(err) {
		t.Fatalf("no log file may be created after StopAll: %v", err)
	}
	if got := s.Logs("svc_late", 10); len(got) != 1 {
		t.Fatalf("the line is still kept in memory: %+v", got)
	}
}

type nopEmitter struct{}

func (nopEmitter) Publish(string, any) {}
