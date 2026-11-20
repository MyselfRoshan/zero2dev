package supervisor

import (
	"context"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"zero2dev/internal/model"
	"zero2dev/internal/store"
)

func TestStopTier(t *testing.T) {
	apps, dbs, front := StopTier("node"), StopTier("postgres"), StopTier("caddy")
	if !(apps < dbs && dbs < front) {
		t.Fatalf("order must be apps < databases < site server, got %d %d %d", apps, dbs, front)
	}
	for _, k := range []string{"custom", "php", "go", "java", "node", "mailpit", ""} {
		if StopTier(k) != apps {
			t.Errorf("%q should stop with the apps", k)
		}
	}
	for _, k := range []string{"mariadb", "mysql", "postgres", "redis"} {
		if StopTier(k) != dbs {
			t.Errorf("%q should stop with the databases", k)
		}
	}
	if StopTier("nginx") != front {
		t.Error("nginx is a site server")
	}
}

// stateLog remembers every service.status change in order and can look at the
// supervisor the moment a given change is published.
type stateLog struct {
	mu    sync.Mutex
	seq   []string     // "<id>:<state>"
	check func(string) // set before any service starts
}

func (l *stateLog) Publish(typ string, data any) {
	if m, ok := data.(map[string]any); ok && typ == "service.status" {
		l.mu.Lock()
		entry := m["id"].(string) + ":" + m["state"].(string)
		l.seq = append(l.seq, entry)
		l.mu.Unlock()
		if l.check != nil {
			l.check(entry)
		}
	}
}

func TestStopAllOrdered(t *testing.T) {
	home := t.TempDir()
	st, err := store.Open(home + "/state.json")
	if err != nil {
		t.Fatal(err)
	}
	rec := &stateLog{}
	sup := New(home, st, rec)
	sup.KillGrace = 800 * time.Millisecond
	sup.HealthTimeout = 3 * time.Second
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		sup.StopAll(ctx)
	})

	// The test binary is the fake server. CustomArgs keeps the arguments as written,
	// so a "caddy" service runs it too.
	mk := func(kind, name string) string {
		id := model.NewID("svc_")
		port := freePort(t)
		st.Update(func(d *store.Data) error {
			d.Services = append(d.Services, model.Service{
				ID: id, Name: name, Kind: kind, Command: os.Args[0], Args: []string{"-test.run=^$"}, CustomArgs: true,
				Cwd: home, Env: map[string]string{"Z2D_HELPER": "serve"}, Port: port,
			})
			return nil
		})
		return id
	}
	app, app2, front, idle := mk("custom", "app"), mk("custom", "app 2"), mk("caddy", "front door"), mk("custom", "idle")
	// The moment the site server is asked to stop, both apps must already be gone.
	var appsLeft []string
	rec.check = func(entry string) {
		if entry == front+":stopping" {
			for _, id := range []string{app, app2} {
				if s := sup.State(id); s != "stopped" {
					appsLeft = append(appsLeft, id+" is "+s)
				}
			}
		}
	}
	for _, id := range []string{app, app2, front} {
		if err := sup.Start(id); err != nil {
			t.Fatal(err)
		}
		waitState(t, sup, id, "running", 5*time.Second)
	}

	rep := sup.StopAllOrdered()
	if len(rep.Failed) != 0 {
		t.Fatalf("failed: %+v", rep.Failed)
	}
	want := []string{app, app2, front}
	slices.Sort(want)
	if !slices.Equal(rep.Stopped, want) {
		t.Fatalf("stopped %v, want %v (the idle service must not be listed)", rep.Stopped, want)
	}
	for _, id := range []string{app, app2, front} {
		if s := sup.State(id); s != "stopped" {
			t.Fatalf("%s is %s after Stop all", id, s)
		}
	}
	if sup.State(idle) != "stopped" {
		t.Fatal("a service that never ran stays stopped")
	}
	if len(appsLeft) != 0 {
		t.Fatalf("the site server started stopping while apps were still up: %v (events %v)", appsLeft, rec.seq)
	}

	// Unlike daemon shutdown, services can be started again.
	if err := sup.Start(app); err != nil {
		t.Fatalf("start after Stop all: %v", err)
	}
	waitState(t, sup, app, "running", 5*time.Second)
}

func TestStopAllOrderedNothingRunning(t *testing.T) {
	s, _, _, _ := setup(t, "serve", freePort(t))
	rep := s.StopAllOrdered()
	if rep.Stopped == nil || rep.Failed == nil || len(rep.Stopped)+len(rep.Failed) != 0 {
		t.Fatalf("want two empty, non-nil lists, got %+v", rep)
	}
}

func TestStopOrderedFollowsDependencies(t *testing.T) {
	e := newOrdEnv(t)
	// The apps are one tier: only the dependencies decide their order. The database
	// is a database kind so that the database link of appB is kept by the store.
	db := e.add("redis", "db", "serve")
	app := e.add("custom", "app", "serve", db)
	worker := e.add("custom", "worker", "serve", app)
	appB := e.add("custom", "app b", "serve") // database link
	e.link(appB, db)
	idle := e.add("custom", "idle", "serve")

	var problems []string
	e.expect(&problems, db+":stopping", model.StateStopped, app, worker, appB)
	e.expect(&problems, app+":stopping", model.StateStopped, worker)
	for _, id := range []string{db, app, worker, appB} {
		if err := e.sup.Start(id); err != nil {
			t.Fatal(err)
		}
		waitState(t, e.sup, id, model.StateRunning, 5*time.Second)
	}

	rep := e.sup.StopOrdered(nil)
	if len(rep.Failed) != 0 {
		t.Fatalf("failed: %+v", rep.Failed)
	}
	want := []string{db, app, worker, appB}
	slices.Sort(want)
	if !slices.Equal(rep.Stopped, want) {
		t.Fatalf("stopped %v, want %v (idle %s must not be listed)", rep.Stopped, want, idle)
	}
	if len(problems) != 0 {
		t.Fatalf("something stopped before its dependents: %v (events %v)", problems, e.log.seq)
	}
}

func TestStopOrderedSubsets(t *testing.T) {
	e := newOrdEnv(t)
	db := e.add("custom", "db", "serve")
	app := e.add("custom", "app", "serve", db)
	worker := e.add("custom", "worker", "serve", app)
	for _, id := range []string{db, app, worker} {
		if err := e.sup.Start(id); err != nil {
			t.Fatal(err)
		}
		waitState(t, e.sup, id, model.StateRunning, 5*time.Second)
	}

	if rep := e.sup.StopOrdered([]string{}); len(rep.Stopped)+len(rep.Failed) != 0 || rep.Stopped == nil || rep.Failed == nil {
		t.Fatalf("empty ids must stop nothing: %+v", rep)
	}
	if got := e.states(db, app, worker); strings.Count(got, "running") != 3 {
		t.Fatalf("nothing may stop: %s", got)
	}

	rep := e.sup.StopOrdered([]string{app, worker, "svc_nope"})
	if len(rep.Failed) != 0 || len(rep.Stopped) != 2 || !slices.Contains(rep.Stopped, app) || !slices.Contains(rep.Stopped, worker) {
		t.Fatalf("report %+v", rep)
	}
	if e.sup.State(db) != model.StateRunning {
		t.Fatalf("the database is not in the subset: %s", e.sup.State(db))
	}

	// a dependent outside the subset does not hold a service back
	if err := e.sup.Start(app); err != nil {
		t.Fatal(err)
	}
	waitState(t, e.sup, app, model.StateRunning, 5*time.Second)
	rep = e.sup.StopOrdered([]string{db})
	if len(rep.Failed) != 0 || !slices.Equal(rep.Stopped, []string{db}) {
		t.Fatalf("report %+v", rep)
	}
	if e.sup.State(app) != model.StateRunning {
		t.Fatalf("app is outside the subset: %s", e.sup.State(app))
	}
}

func TestStopOrderedDependencyLoopStillStops(t *testing.T) {
	e := newOrdEnv(t)
	a := e.add("custom", "a", "serve")
	b := e.add("custom", "b", "serve", a)
	for _, id := range []string{a, b} {
		if err := e.sup.Start(id); err != nil {
			t.Fatal(err)
		}
		waitState(t, e.sup, id, model.StateRunning, 5*time.Second)
	}
	e.st.Update(func(d *store.Data) error { d.Services[0].DependsOn = []string{b}; return nil })

	done := make(chan StopReport, 1)
	go func() { done <- e.sup.StopOrdered(nil) }()
	select {
	case rep := <-done:
		if len(rep.Failed) != 0 || len(rep.Stopped) != 2 {
			t.Fatalf("report %+v", rep)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("StopOrdered did not return on a dependency loop")
	}
}

func TestStopOrderedTiersStillWinWithoutDependencies(t *testing.T) {
	e := newOrdEnv(t)
	front := e.add("caddy", "front door", "serve")
	app := e.add("custom", "app", "serve")
	var problems []string
	e.expect(&problems, front+":stopping", model.StateStopped, app)
	for _, id := range []string{front, app} {
		if err := e.sup.Start(id); err != nil {
			t.Fatal(err)
		}
		waitState(t, e.sup, id, model.StateRunning, 5*time.Second)
	}
	if rep := e.sup.StopOrdered(nil); len(rep.Failed) != 0 || len(rep.Stopped) != 2 {
		t.Fatalf("report %+v", rep)
	}
	if len(problems) != 0 {
		t.Fatal(problems)
	}
}
