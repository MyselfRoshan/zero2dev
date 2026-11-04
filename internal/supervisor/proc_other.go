//go:build !windows

package supervisor

import (
	"os/exec"
	"sync"
	"syscall"
)

// configure puts the child in its own process group so the whole tree can be
// signalled with kill(-pgid).
func configure(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// tree represents a started process and its descendants.
type tree struct {
	mu     sync.Mutex
	pgid   int
	closed bool
}

func attach(cmd *exec.Cmd) (*tree, error) { return &tree{pgid: cmd.Process.Pid}, nil }

// terminate asks the group to exit (SIGTERM).
func (t *tree) terminate() { t.signal(syscall.SIGTERM) }

// kill force-kills the group (SIGKILL).
func (t *tree) kill() { t.signal(syscall.SIGKILL) }

func (t *tree) signal(sig syscall.Signal) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed { // after close the pgid could have been recycled
		return
	}
	_ = syscall.Kill(-t.pgid, sig)
}

// close sweeps leftover descendants and disarms the tree.
func (t *tree) close() {
	t.kill()
	t.mu.Lock()
	t.closed = true
	t.mu.Unlock()
}
