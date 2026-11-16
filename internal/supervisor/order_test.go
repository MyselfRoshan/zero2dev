package supervisor

import (
	"context"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"zero2dev/internal/apperr"
	"zero2dev/internal/model"
	"zero2dev/internal/store"
)

func TestFindCycle(t *testing.T) {
	svc := func(id string, deps ...string) model.Service { return model.Service{ID: id, DependsOn: deps} }
	if c := FindCycle(nil); c != nil {
		t.Fatalf("empty: %v", c)
	}
	// a chain and a diamond are not loops; links to unknown services are ignored
	if c := FindCycle([]model.Service{svc("a", "b", "c"), svc("b", "d"), svc("c", "d"), svc("d", "gone")}); c != nil {
		t.Fatalf("diamond: %v", c)
	}
	c := FindCycle([]model.Service{svc("a", "b"), svc("b", "c"), svc("c", "a"), svc("x")})
	if len(c) != 4 || c[0] != c[3] || !slices.Contains(c, "a") || !slices.Contains(c, "b") || !slices.Contains(c, "c") {
		t.Fatalf("loop must be a closed path over a, b, c: %v", c)
	}
	if c := FindCycle([]model.Service{svc("a", "a")}); !slices.Equal(c, []string{"a", "a"}) {
		t.Fatalf("self loop: %v", c)
	}
	// the database link is a dependency too
	db := model.Service{ID: "db", Kind: "postgres", DependsOn: []string{"app"}}
	app := model.Service{ID: "app", DatabaseID: "db"}
	if c := FindCycle([]model.Service{db, app}); c == nil {
		t.Fatal("db <-> app through databaseId must be a loop")
	}
}

// ordEnv is a supervisor whose services are the test binary in "serve" mode (or
// "exit"), built the way the stop tests build theirs.
type ordEnv struct {
	t    *testing.T
	home string
	st   *store.Store
	sup  *Supervisor
	log  *stateLog
}

func newOrdEnv(t *testing.T) *ordEnv {
	t.Helper()
	home := t.TempDir()
	st, err := store.Open(home + "/state.json")
	if err != nil {
		t.Fatal(err)
	}
	log := &stateLog{}
	sup := New(home, st, log)
	sup.KillGrace = 800 * time.Millisecond
	sup.HealthTimeout = 3 * time.Second
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		sup.StopAll(ctx)
		// A service logs "stopped" just after it is reaped, which reopens its log file
		// after StopAll closed it; close the logs again so the folder can be removed.
		time.Sleep(300 * time.Millisecond)
		sup.StopAll(ctx)
	})
	return &ordEnv{t: t, home: home, st: st, sup: sup, log: log}
}

// add defines a service. mode "serve" listens on a port, "exit" dies at once.
func (e *ordEnv) add(kind, name, mode string, deps ...string) string {
	id := model.NewID("svc_")
	port := 0
	if mode == "serve" {
		port = freePort(e.t)
	}
	e.st.Update(func(d *store.Data) error {
		d.Services = append(d.Services, model.Service{
			ID: id, Name: name, Kind: kind, Command: os.Args[0], Args: []string{"-test.run=^$"}, CustomArgs: true,
			Cwd: e.home, Env: map[string]string{"Z2D_HELPER": mode}, Port: port, DependsOn: deps,
		})
		return nil
	})
	return id
}

func (e *ordEnv) link(id, db string) {
	e.st.Update(func(d *store.Data) error {
		for i := range d.Services {
			if d.Services[i].ID == id {
				d.Services[i].DatabaseID = db
			}
		}
		return nil
	})
}

func (e *ordEnv) states(ids ...string) string {
	var out []string
	for _, id := range ids {
		out = append(out, id+"="+e.sup.State(id))
	}
	return strings.Join(out, " ")
}

func (e *ordEnv) pid(id string) int { return e.sup.Apply(model.Service{ID: id}).Pid }

func (e *ordEnv) index(entry string) int {
	e.log.mu.Lock()
	defer e.log.mu.Unlock()
	return slices.Index(e.log.seq, entry)
}

// expect notes a problem when entry ("<id>:<state>") is published while one of
// ids is not in the state want at that moment.
func (e *ordEnv) expect(problems *[]string, entry, want string, ids ...string) {
	prev := e.log.check
	e.log.check = func(got string) {
		if prev != nil {
			prev(got)
		}
		if got != entry {
			return
		}
		for _, id := range ids {
			if s := e.sup.State(id); s != want {
				*problems = append(*problems, entry+": "+id+" is "+s+" (events "+strings.Join(e.log.seq, ",")+")")
			}
		}
	}
}

func TestStartOrderedWaves(t *testing.T) {
	e := newOrdEnv(t)
	db := e.add("redis", "db", "serve") // a database kind, or the store drops the database link
	app := e.add("custom", "app", "serve", db)
	appB := e.add("custom", "app b", "serve") // uses the database through the database link only
	e.link(appB, db)
	front := e.add("caddy", "front door", "serve", app)
	solo := e.add("custom", "solo", "serve")

	var problems []string
	e.expect(&problems, app+":starting", model.StateRunning, db)
	e.expect(&problems, appB+":starting", model.StateRunning, db)
	e.expect(&problems, front+":starting", model.StateRunning, app)

	rep := e.sup.StartOrdered(nil)
	if len(rep.Failed) != 0 {
		t.Fatalf("failed: %+v", rep.Failed)
	}
	for _, id := range []string{db, app, appB, front, solo} {
		if !slices.Contains(rep.Started, id) {
			t.Errorf("%s is not reported started: %v", id, rep.Started)
		}
		waitState(t, e.sup, id, model.StateRunning, 5*time.Second)
	}
	if len(problems) != 0 {
		t.Fatalf("a service started before what it needs was running: %v", problems)
	}
}

func TestStartOrderedTierOrderWithoutDependencies(t *testing.T) {
	e := newOrdEnv(t)
	app := e.add("custom", "app", "serve")
	front := e.add("caddy", "front door", "serve")
	app2 := e.add("custom", "app 2", "serve")
	rep := e.sup.StartOrdered(nil)
	if len(rep.Failed) != 0 || len(rep.Started) != 3 {
		t.Fatalf("report %+v", rep)
	}
	if !(e.index(front+":starting") < e.index(app+":starting") && e.index(app+":starting") < e.index(app2+":starting")) {
		t.Fatalf("the site server starts first, then the apps in the order they are defined: %v", e.log.seq)
	}
}

func TestStartOrderedPullsInDependenciesAndSubsets(t *testing.T) {
	e := newOrdEnv(t)
	db := e.add("custom", "db", "serve")
	mid := e.add("custom", "mid", "serve", db)
	app := e.add("custom", "app", "serve", mid)
	other := e.add("custom", "other", "serve")

	// a non-nil empty list is nothing
	rep := e.sup.StartOrdered([]string{})
	if len(rep.Started)+len(rep.Failed) != 0 || rep.Started == nil || rep.Failed == nil {
		t.Fatalf("empty ids must start nothing: %+v", rep)
	}
	if got := e.states(db, mid, app, other); strings.Contains(got, "running") || strings.Contains(got, "starting") {
		t.Fatalf("something started: %s", got)
	}

	rep = e.sup.StartOrdered([]string{app})
	if len(rep.Failed) != 0 {
		t.Fatalf("failed: %+v", rep.Failed)
	}
	if want := []string{db, mid, app}; !slices.Equal(rep.Started, want) { // dependencies first
		t.Fatalf("started %v, want %v", rep.Started, want)
	}
	if e.sup.State(other) != model.StateStopped {
		t.Fatalf("an unrelated service must not start: %s", e.sup.State(other))
	}
}

func TestStartOrderedRunningDependencyIsSatisfied(t *testing.T) {
	e := newOrdEnv(t)
	db := e.add("custom", "db", "serve")
	app := e.add("custom", "app", "serve", db)
	if err := e.sup.Start(db); err != nil {
		t.Fatal(err)
	}
	waitState(t, e.sup, db, model.StateRunning, 5*time.Second)
	pid := e.pid(db)

	rep := e.sup.StartOrdered([]string{db, app})
	if len(rep.Failed) != 0 || !slices.Equal(rep.Started, []string{app}) {
		t.Fatalf("only the app is started and reported: %+v", rep)
	}
	if got := e.pid(db); got != pid {
		t.Fatalf("the running dependency was restarted: pid %d -> %d", pid, got)
	}
}

func TestStartOrderedFailedDependency(t *testing.T) {
	e := newOrdEnv(t)
	db := e.add("custom", "broken db", "exit")
	app := e.add("custom", "app", "serve", db)
	worker := e.add("custom", "worker", "serve", app) // two levels down
	free := e.add("custom", "free", "serve")

	rep := e.sup.StartOrdered(nil)
	if !slices.Equal(rep.Started, []string{free}) {
		t.Fatalf("started %v, want only the independent service", rep.Started)
	}
	if len(rep.Failed) != 3 {
		t.Fatalf("failed: %+v", rep.Failed)
	}
	byID := map[string]StartFailure{}
	for _, f := range rep.Failed {
		byID[f.ID] = f
	}
	if f := byID[db]; !strings.Contains(f.Error, "exit status 3") {
		t.Errorf("the root cause is reported with the exit message: %+v", f)
	}
	if f := byID[app]; f.Error != "waits for broken db" {
		t.Errorf("app: %+v", f)
	}
	if f := byID[worker]; f.Error != "waits for app" {
		t.Errorf("worker: %+v", f)
	}
	if rep.Failed[0].ID != db {
		t.Errorf("the root cause comes first: %+v", rep.Failed)
	}
	for _, id := range []string{app, worker} {
		if e.sup.State(id) != model.StateStopped {
			t.Errorf("%s must not have been started: %s", id, e.sup.State(id))
		}
	}
}

func TestStartOrderedDependencyLoopEndsAndFailsTheLoop(t *testing.T) {
	e := newOrdEnv(t)
	a := e.add("custom", "a", "serve")
	b := e.add("custom", "b", "serve", a)
	e.st.Update(func(d *store.Data) error { d.Services[0].DependsOn = []string{b}; return nil })
	c := e.add("custom", "c", "serve")
	d := e.add("custom", "d", "serve", a) // hangs off the loop

	done := make(chan StartReport, 1)
	go func() { done <- e.sup.StartOrdered(nil) }()
	select {
	case rep := <-done:
		if !slices.Equal(rep.Started, []string{c}) {
			t.Fatalf("started %v, want only %s", rep.Started, c)
		}
		failed := []string{}
		for _, f := range rep.Failed {
			failed = append(failed, f.ID)
		}
		slices.Sort(failed)
		want := []string{a, b, d}
		slices.Sort(want)
		if !slices.Equal(failed, want) {
			t.Fatalf("failed %v, want %v", failed, want)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("StartOrdered did not return on a dependency loop")
	}
	if e.sup.State(a) != model.StateStopped || e.sup.State(b) != model.StateStopped {
		t.Fatal("services in a loop must not be started")
	}
}

func TestStartWithDeps(t *testing.T) {
	e := newOrdEnv(t)
	db := e.add("custom", "db", "serve")
	app := e.add("custom", "app", "serve", db)

	var problems []string
	e.expect(&problems, app+":starting", model.StateRunning, db)
	if err := e.sup.StartWithDeps(app); err != nil {
		t.Fatal(err)
	}
	if e.sup.State(db) != model.StateRunning {
		t.Fatalf("the dependency must be running when the call returns: %s", e.states(db, app))
	}
	if s := e.sup.State(app); s != model.StateStarting && s != model.StateRunning {
		t.Fatalf("the target is spawned when the call returns: %s", s)
	}
	waitState(t, e.sup, app, model.StateRunning, 5*time.Second)
	if len(problems) != 0 {
		t.Fatal(problems)
	}
	// already running: Start's own conflict
	err := e.sup.StartWithDeps(app)
	var ae *apperr.Error
	if !errors.As(err, &ae) || ae.Status != 409 {
		t.Fatalf("starting a running service: %v", err)
	}
	if err := e.sup.StartWithDeps("svc_nope"); err == nil {
		t.Fatal("unknown service must fail")
	}
}

func TestStartWithDepsFailedDependency(t *testing.T) {
	e := newOrdEnv(t)
	db := e.add("custom", "broken db", "exit")
	app := e.add("custom", "app", "serve", db)
	err := e.sup.StartWithDeps(app)
	var ae *apperr.Error
	if !errors.As(err, &ae) || ae.Status != 409 || !strings.Contains(err.Error(), "broken db") {
		t.Fatalf("want a 409 naming the dependency, got %v", err)
	}
	if e.sup.State(app) != model.StateStopped {
		t.Fatalf("the app must not start: %s", e.sup.State(app))
	}
}

func TestStartWithDepsWithoutDependenciesIsPlainStart(t *testing.T) {
	e := newOrdEnv(t)
	solo := e.add("custom", "solo", "serve")
	if err := e.sup.StartWithDeps(solo); err != nil {
		t.Fatal(err)
	}
	waitState(t, e.sup, solo, model.StateRunning, 5*time.Second)
}
