//go:build windows

package envreg

import "testing"

// Read-only: checks that the syscall reader returns what the registry holds.
// Nothing in the registry is written by any test.
func TestWindowsRegistryReadOnly(t *testing.T) {
	v, typ, ok, err := hkcuEnv{}.Get("Path")
	if err != nil {
		t.Fatal(err)
	}
	if ok && typ != regSZ && typ != regExpandSZ {
		t.Fatalf("unexpected Path type %d", typ)
	}
	if ok && v == "" {
		t.Log("user Path is empty")
	}
	if _, _, ok, err := (hkcuEnv{}).Get("ZERO2DEV_SURELY_MISSING_VALUE"); err != nil || ok {
		t.Fatalf("a missing value must be (ok=false, nil): ok=%v err=%v", ok, err)
	}
}
