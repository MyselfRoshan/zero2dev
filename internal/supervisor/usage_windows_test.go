//go:build windows

package supervisor

import (
	"os/exec"
	"testing"
)

// The real job object: a started process shows a working set; a closed tree has no numbers.
func TestTreeUsageReadsTheJobObject(t *testing.T) {
	cmd := exec.Command("cmd", "/c", "ping -n 5 127.0.0.1 >nul")
	configure(cmd)
	if err := cmd.Start(); err != nil {
		t.Skip(err)
	}
	tr, err := attach(cmd)
	if err != nil {
		t.Skip("no job object here:", err)
	}
	defer func() { tr.close(); _ = cmd.Wait() }()
	cpu, mem, ok := tr.usage()
	if !ok || mem == 0 || cpu < 0 {
		t.Fatalf("usage = %v, %d, %v", cpu, mem, ok)
	}
	tr.close()
	if _, _, ok := tr.usage(); ok {
		t.Fatal("a closed tree has no numbers")
	}
	if _, _, ok := (&tree{}).usage(); ok {
		t.Fatal("no job, no numbers")
	}
}
