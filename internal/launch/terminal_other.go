//go:build !windows

package launch

// OpenTerminal opens a terminal in dir with the environment env. Only
// implemented on Windows (see terminal_windows.go); elsewhere it returns
// ErrNoTerminal.
func OpenTerminal(dir string, env []string) error { return ErrNoTerminal }
