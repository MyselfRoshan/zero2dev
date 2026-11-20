//go:build !windows

package migrate

// Off Windows there is no registry and no process check.

func systemRegistry() Registry { return emptyRegistry{} }

func processRunning(string) bool { return false }
