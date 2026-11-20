//go:build !windows

package migrate

// Off Windows there is no registry and no process check.

func systemRegistry() Registry { return emptyRegistry{} }

func processRunning(string) bool { return false }

// diskFree: unknown off Windows (0).
func diskFree(string) int64 { return 0 }
