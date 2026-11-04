package supervisor

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"zero2dev/internal/model"
	"zero2dev/internal/store"
)

// The test binary doubles as a fake server: when Z2D_HELPER is set, TestMain
// acts according to the mode instead of running tests.
func TestMain(m *testing.M) {
	switch os.Getenv("Z2D_HELPER") {
	case "":
		os.Exit(m.Run())
	case "serve": // listen on $PORT, print, wait for SIGTERM
		l, err := net.Listen("tcp", "127.0.0.1:"+os.Getenv("PORT"))
		if err != nil {
			fmt.Fprintln(os.Stderr, "listen failed:", err)
			os.Exit(2)
		}
		go func() {
			for {
				c, err := l.Accept()
				if err != nil {
					return
				}
				c.Close()
			}
		}()
		fmt.Println("listening on", os.Getenv("PORT"))
		fmt.Fprintln(os.Stderr, "this is stderr")
		select {}
	case "exit":
		fmt.Println("about to fail")
		os.Exit(3)
	case "idle": // no port, runs forever
		fmt.Println("idle up")
		select {}
	case "ignoreterm": // ignores SIGTERM so only SIGKILL stops it
		ignoreTerm()
		fmt.Println("ignoring term")
		select {}
	case "parent": // spawns a sleeping child and reports its pid
		c := exec.Command("sleep", "300")
		_ = c.Start()
		fmt.Println("child", c.Process.Pid)
		select {}
	case "noport": // claims a port but never listens
		select {}
	}
}

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

type recorder struct {
	mu     sync.Mutex
	events []string
}

func (r *recorder) Publish(typ string, data any) {
	r.mu.Lock()
	r.events = append(r.events, typ)
	r.mu.Unlock()
}
func (r *recorder) count(typ string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, e := range r.events {
		if e == typ {
			n++
		}
	}
	return n
}

func setup(t *testing.T, mode string, port int) (*Supervisor, *store.Store, *recorder, string) {
	t.Helper()
	home := t.TempDir()
	st, err := store.Open(home + "/state.json")
	if err != nil {
		t.Fatal(err)
	}
	rec := &recorder{}
	sup := New(home, st, rec)
	sup.KillGrace = 800 * time.Millisecond
	sup.HealthTimeout = 3 * time.Second
	id := model.NewID("svc_")
	st.Update(func(d *store.Data) error {
		d.Services = append(d.Services, model.Service{
			ID: id, Name: "t", Kind: "custom", Command: os.Args[0], Args: []string{"-test.run=^$"},
			Cwd: home, Env: map[string]string{"Z2D_HELPER": mode}, Port: port,
		})
		return nil
	})
	t.Cleanup(func() { sup.StopAll(t.Context()) })
	return sup, st, rec, id
}

func waitState(t *testing.T, s *Supervisor, id, want string, d time.Duration) {
	t.Helper()
	end := time.Now().Add(d)
	for time.Now().Before(end) {
		if s.State(id) == want {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("state = %q, want %q (logs: %v)", s.State(id), want, s.Logs(id, 20))
}

func hasLog(s *Supervisor, id, stream, sub string) bool {
	for _, l := range s.Logs(id, 100) {
		if l.Stream == stream && strings.Contains(l.Text, sub) {
			return true
		}
	}
	return false
}

func TestStartStopWithPort(t *testing.T) {
	port := freePort(t)
	s, _, rec, id := setup(t, "serve", port)
	if err := s.Start(id); err != nil {
		t.Fatal(err)
	}
	if st := s.State(id); st != "starting" && st != "running" {
		t.Fatalf("unexpected immediate state %s", st)
	}
	waitState(t, s, id, "running", 5*time.Second)
	svc := s.Apply(model.Service{ID: id})
	if svc.Pid == 0 || svc.StartedAt == nil {
		t.Fatalf("pid/startedAt not set: %+v", svc)
	}
	time.Sleep(100 * time.Millisecond)
	if !hasLog(s, id, "stdout", "listening on "+strconv.Itoa(port)) || !hasLog(s, id, "stderr", "this is stderr") {
		t.Fatalf("missing output lines: %v", s.Logs(id, 50))
	}
	if err := s.StopWait(id); err != nil {
		t.Fatal(err)
	}
	if st := s.State(id); st != "stopped" {
		t.Fatalf("state after stop = %s", st)
	}
	if s.Apply(model.Service{ID: id}).LastError != "" {
		t.Fatal("clean stop must not set lastError")
	}
	if rec.count("service.status") < 3 {
		t.Fatalf("expected >=3 status events, got %d", rec.count("service.status"))
	}
	if rec.count("log.line") == 0 {
		t.Fatal("no log.line events")
	}
}

func TestPortConflict(t *testing.T) {
	l, _ := net.Listen("tcp", "127.0.0.1:0")
	defer l.Close()
	port := l.Addr().(*net.TCPAddr).Port
	s, _, _, id := setup(t, "serve", port)
	err := s.Start(id)
	if err == nil || !strings.Contains(err.Error(), strconv.Itoa(port)) {
		t.Fatalf("want port conflict naming %d, got %v", port, err)
	}
	if s.State(id) != "stopped" {
		t.Fatal("state must stay stopped")
	}
}

func TestUnexpectedExitFails(t *testing.T) {
	s, _, _, id := setup(t, "exit", 0)
	if err := s.Start(id); err != nil {
		t.Fatal(err)
	}
	waitState(t, s, id, "failed", 5*time.Second)
	if le := s.Apply(model.Service{ID: id}).LastError; le != "exit status 3" {
		t.Fatalf("lastError = %q", le)
	}
	if !hasLog(s, id, "stdout", "about to fail") {
		t.Fatal("missing output")
	}
	// A failed service can be started again.
	if err := s.Start(id); err != nil {
		t.Fatal(err)
	}
}

func TestNoPortRunsAfterDelay(t *testing.T) {
	s, _, _, id := setup(t, "idle", 0)
	if err := s.Start(id); err != nil {
		t.Fatal(err)
	}
	waitState(t, s, id, "running", 3*time.Second)
	if err := s.Start(id); err == nil {
		t.Fatal("double start must conflict")
	}
}

func TestHealthTimeoutFails(t *testing.T) {
	port := freePort(t)
	s, _, _, id := setup(t, "noport", port)
	s.HealthTimeout = 700 * time.Millisecond
	if err := s.Start(id); err != nil {
		t.Fatal(err)
	}
	waitState(t, s, id, "failed", 6*time.Second)
	if le := s.Apply(model.Service{ID: id}).LastError; !strings.Contains(le, "health check timed out") {
		t.Fatalf("lastError = %q", le)
	}
}

func TestRestart(t *testing.T) {
	port := freePort(t)
	s, _, _, id := setup(t, "serve", port)
	s.Start(id)
	waitState(t, s, id, "running", 5*time.Second)
	pid1 := s.Apply(model.Service{ID: id}).Pid
	if err := s.Restart(id); err != nil {
		t.Fatal(err)
	}
	waitState(t, s, id, "running", 5*time.Second)
	if pid2 := s.Apply(model.Service{ID: id}).Pid; pid2 == pid1 || pid2 == 0 {
		t.Fatalf("pid not renewed %d -> %d", pid1, pid2)
	}
}

func TestSigkillAfterGrace(t *testing.T) {
	s, _, _, id := setup(t, "ignoreterm", 0)
	s.Start(id)
	waitState(t, s, id, "running", 3*time.Second)
	time.Sleep(200 * time.Millisecond)
	start := time.Now()
	if err := s.StopWait(id); err != nil {
		t.Fatal(err)
	}
	if time.Since(start) < s.KillGrace-100*time.Millisecond {
		t.Fatal("process should have survived SIGTERM until grace expired")
	}
	if s.State(id) != "stopped" {
		t.Fatal("not stopped")
	}
}

func TestKillsProcessTree(t *testing.T) {
	if _, err := exec.LookPath("sleep"); err != nil {
		t.Skip("no sleep binary")
	}
	s, _, _, id := setup(t, "parent", 0)
	s.Start(id)
	waitState(t, s, id, "running", 3*time.Second)
	var child int
	for i := 0; i < 50 && child == 0; i++ {
		for _, l := range s.Logs(id, 20) {
			if strings.HasPrefix(l.Text, "child ") {
				child, _ = strconv.Atoi(strings.TrimPrefix(l.Text, "child "))
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	if child == 0 {
		t.Fatal("helper did not report child pid")
	}
	s.StopWait(id)
	for i := 0; i < 50; i++ { // child is reparented; it must be gone
		if !pidAlive(child) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("grandchild %d survived Stop", child)
}

func TestMissingBinaryAndNodeNeedsArgs(t *testing.T) {
	s, st, _, id := setup(t, "idle", 0)
	st.Update(func(d *store.Data) error { d.Services[0].Command = "/definitely/not/here"; return nil })
	if err := s.Start(id); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("want not found, got %v", err)
	}
	st.Update(func(d *store.Data) error {
		d.Services[0].Command, d.Services[0].Kind, d.Services[0].Args = os.Args[0], "node", nil
		return nil
	})
	if err := s.Start(id); err == nil || !strings.Contains(err.Error(), "args required") {
		t.Fatalf("want args required, got %v", err)
	}
}

func TestPlaceholdersAndEnv(t *testing.T) {
	s, st, _, id := setup(t, "idle", 4567)
	st.Update(func(d *store.Data) error {
		d.Services[0].Args = []string{"a-{port}"}
		d.Services[0].Env["X"] = "p{port}"
		return nil
	})
	r, err := s.resolve(st.Snapshot().Services[0], st.Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	if r.args[0] != "a-4567" {
		t.Fatalf("args %v", r.args)
	}
	joined := strings.Join(r.env, "\n")
	if !strings.Contains(joined, "\nX=p4567") || !strings.Contains(joined, "\nPORT=4567") {
		t.Fatal("env placeholders / PORT missing")
	}
	_ = id
}

func TestDefaultArgs(t *testing.T) {
	s, st, _, _ := setup(t, "idle", 8000)
	sv := st.Snapshot().Services[0]
	sv.Kind, sv.Args = "php", nil
	r, err := s.resolve(sv, st.Snapshot())
	if err != nil || strings.Join(r.args, " ") != "-S 127.0.0.1:{port} -t "+sv.Cwd && strings.Join(r.args, " ") != "-S 127.0.0.1:8000 -t "+sv.Cwd {
		t.Fatalf("php default args: %v %v", r, err)
	}
	sv.Kind = "caddy"
	r, err = s.resolve(sv, st.Snapshot())
	if err != nil || r.args[0] != "run" || !strings.HasSuffix(r.args[2], "Caddyfile") {
		t.Fatalf("caddy args %v %v", r, err)
	}
}

func TestLogFileRotationAndTail(t *testing.T) {
	dir := t.TempDir()
	l := newSvcLog(dir + "/x.log")
	l.max = 300
	for i := 0; i < 20; i++ {
		l.add(model.LogLine{Ts: "t", Stream: "stdout", Text: fmt.Sprintf("line %02d padding padding", i)})
	}
	l.close()
	if _, err := os.Stat(dir + "/x.log.1"); err != nil {
		t.Fatal("rotation file missing")
	}
	if got := l.tail(3); len(got) != 3 || got[2].Text != "line 19 padding padding" {
		t.Fatalf("tail %v", got)
	}
	// reload from disk after "restart"
	l2 := newSvcLog(dir + "/x.log")
	if len(l2.lines) == 0 || l2.lines[len(l2.lines)-1].Text != "line 19 padding padding" {
		t.Fatal("did not reload tail from file")
	}
}

func TestRingCap(t *testing.T) {
	l := newSvcLog(t.TempDir() + "/r.log")
	for i := 0; i < ringCap*3; i++ {
		l.add(model.LogLine{Text: strconv.Itoa(i)})
	}
	l.close()
	if got := l.tail(5000); len(got) != ringCap || got[len(got)-1].Text != strconv.Itoa(ringCap*3-1) {
		t.Fatalf("ring len=%d", len(got))
	}
}

func TestRemoveDeletesLogs(t *testing.T) {
	s, _, _, id := setup(t, "exit", 0)
	s.Start(id)
	waitState(t, s, id, "failed", 5*time.Second)
	if _, err := os.Stat(s.logPath(id)); err != nil {
		t.Fatal("log file should exist")
	}
	if err := s.Remove(id); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(s.logPath(id)); err == nil {
		t.Fatal("log file should be gone")
	}
}
