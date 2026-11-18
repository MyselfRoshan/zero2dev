//go:build !windows

package envreg

// openRegistry is the non-Windows version: there is no user environment to change.
func openRegistry() registry { return nil }

// broadcastEnvChange has nothing to tell anyone off Windows.
func broadcastEnvChange() {}
