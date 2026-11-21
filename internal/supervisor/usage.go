package supervisor

import (
	"math"
	"runtime"
	"sort"
	"time"

	"zero2dev/internal/model"
)

// CPU and memory per service (0.0.6). One goroutine, started by StartUsage, samples every running
// service each UsageInterval and publishes ONE batched "service.usage" event per tick:
// [{id, cpu, memBytes}] for the services that run. A service that is not in the batch has
// stopped (the dashboard sets its numbers to 0). When the last service stops, one final empty batch
// is published so the dashboard clears the numbers; with nothing running, nothing is published.

// DefaultUsageInterval is the time between two samples.
const DefaultUsageInterval = 2 * time.Second

// Sampler reads the CPU time (user plus kernel, only ever growing) and the memory (working set) of a
// service's whole process tree. ok is false when there are no numbers (no job object, closed tree).
type Sampler func(t *tree) (cpuTime time.Duration, memBytes uint64, ok bool)

func realSample(t *tree) (time.Duration, uint64, bool) {
	if t == nil {
		return 0, 0, false
	}
	return t.usage()
}

// usageState is what the sampler remembers of one run of a service. Guarded by Supervisor.mu.
type usageState struct {
	have    bool // a first sample exists (a delta needs two)
	lastCPU time.Duration
	lastAt  time.Time
	cpu     float64
	mem     uint64
}

// StartUsage starts the sampler goroutine (once). It ends with StopAll (the daemon closing).
func (s *Supervisor) StartUsage() {
	s.mu.Lock()
	if s.usageStop != nil || s.closing {
		s.mu.Unlock()
		return
	}
	stop, done := make(chan struct{}), make(chan struct{})
	s.usageStop, s.usageDone = stop, done
	every := s.UsageInterval
	s.mu.Unlock()
	if every <= 0 {
		every = DefaultUsageInterval
	}
	go func() {
		defer close(done)
		tick := time.NewTicker(every)
		defer tick.Stop()
		for {
			select {
			case <-stop:
				return
			case now := <-tick.C:
				s.sampleUsage(now)
			}
		}
	}()
}

// stopUsage ends the sampler; the caller holds s.mu.
func (s *Supervisor) stopUsageLocked() {
	if s.usageStop != nil {
		select {
		case <-s.usageStop:
		default:
			close(s.usageStop)
		}
	}
}

// sampleUsage takes one sample of every running service at time now and publishes the batch.
func (s *Supervisor) sampleUsage(now time.Time) {
	sample := s.Sample
	if sample == nil {
		sample = realSample
	}
	cpus := s.NumCPU
	if cpus <= 0 {
		cpus = runtime.NumCPU()
	}
	type target struct {
		id string
		p  *proc
		tr *tree
	}
	s.mu.Lock()
	var targets []target
	for id, p := range s.procs {
		if p.state == model.StateRunning {
			targets = append(targets, target{id, p, p.tree})
		}
	}
	s.mu.Unlock()
	sort.Slice(targets, func(i, j int) bool { return targets[i].id < targets[j].id })

	type result struct {
		cpu time.Duration
		mem uint64
		ok  bool
	}
	res := make([]result, len(targets))
	for i, t := range targets { // outside s.mu: the samples are system calls
		res[i].cpu, res[i].mem, res[i].ok = sample(t.tr)
	}

	batch := []model.Usage{}
	s.mu.Lock()
	for i, t := range targets {
		if t.p.state != model.StateRunning || !res[i].ok {
			continue // stopped while sampling, or no numbers
		}
		u := &t.p.usage
		u.cpu = 0
		if u.have {
			u.cpu = cpuPercent(res[i].cpu-u.lastCPU, now.Sub(u.lastAt), cpus)
		}
		u.have, u.lastCPU, u.lastAt, u.mem = true, res[i].cpu, now, res[i].mem
		batch = append(batch, model.Usage{ID: t.id, CPU: u.cpu, MemBytes: u.mem})
	}
	publish := len(batch) > 0 || s.usageSent
	s.usageSent = len(batch) > 0
	s.mu.Unlock()
	if publish {
		s.em.Publish("service.usage", batch)
	}
}

// cpuPercent is the share of the whole machine a process tree used: CPU time over wall time and
// processors, clamped to 0-100, one decimal.
func cpuPercent(cpu, wall time.Duration, cpus int) float64 {
	if wall <= 0 || cpus <= 0 || cpu <= 0 {
		return 0
	}
	v := float64(cpu) / (float64(wall) * float64(cpus)) * 100
	return math.Round(math.Min(math.Max(v, 0), 100)*10) / 10
}
