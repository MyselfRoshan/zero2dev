//go:build windows

package launch

import (
	"fmt"
	"os/exec"
	"syscall"
)

const createNewConsole = 0x00000010 // createNoWindow is in detach_windows.go

// OpenTerminal opens a console in dir with the environment env (a full
// KEY=VALUE list, as supervisor.Environ returns it; an empty env means the
// daemon's own). It prefers Windows Terminal (`wt.exe -d <dir> cmd.exe /k`)
// when wt.exe is on PATH, otherwise cmd.exe in a new console window. The
// console is not supervised and outlives the daemon.
func OpenTerminal(dir string, env []string) error {
	if wt, err := exec.LookPath("wt.exe"); err == nil {
		// wt.exe is only a launcher: no console of its own is needed.
		if err := startTerminal(wt, dir, env); err == nil {
			return nil
		}
		// fall through to cmd.exe if Windows Terminal could not be started
	}
	return startTerminal("", dir, env)
}

func startTerminal(wtPath, dir string, env []string) error {
	prog, args, newConsole := terminalArgs(wtPath, dir)
	flags := uint32(createNoWindow)
	if newConsole {
		flags = createNewConsole
	}
	cmd := exec.Command(prog, args...)
	cmd.Dir = dir
	if len(env) > 0 {
		cmd.Env = env
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: flags}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("cannot open a terminal: %w", err)
	}
	return cmd.Process.Release()
}
