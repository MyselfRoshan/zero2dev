package installer

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"zero2dev/internal/model"
)

// The installer's .exe type: a catalog download whose file name ends in .exe is a program to run,
// not an archive. It is verified like any download, copied into the stage folder, moved to its
// final place and RUN there (rustup writes absolute paths, so it must run where it stays). The
// runtime is registered only when the file named by binaryRelPath exists afterwards.

// rustupArgs is the command line of rustup-init.exe. The flags come from rustup's documentation
// and source (src/cli/setup_mode.rs: -y/--yes, --default-host, --default-toolchain, --profile,
// --no-modify-path). The whole line, as run by Zero2Dev, is still "to be verified on a real
// Windows desktop" (docs/API.md "Rust details"): it is kept here, in ONE place, to fix it easily.
//
//   - GNU host: no administrator install and no Visual Studio Build Tools are needed.
//   - minimal profile: rustc, rust-std and cargo only (no docs, clippy or rustfmt).
//   - --no-modify-path: the user's PATH and registry are not touched (the shims do that portably).
var rustupArgs = []string{
	"--default-host", "x86_64-pc-windows-gnu",
	"--default-toolchain", "stable",
	"--profile", "minimal",
	"--no-modify-path",
	"-y",
}

// exeRun describes how the program of one kind is run.
type exeRun struct {
	// Args is the command line.
	Args []string
	// Env returns KEY=VALUE variables for the run, given the final install folder.
	Env func(final string) []string
	// Timeout is how long the run may take.
	Timeout time.Duration
	// Remove is deleted from the folder after a successful run (the downloaded program itself).
	Remove bool
}

const (
	exeBandFrom = 85 // the job's percent when the program starts
	exeBandTo   = 94 // never reached before "Registering" (95)
)

// exeKinds lists, per runtime kind, how a downloaded .exe is run. Another kind with an
// installer program only needs an entry here.
var exeKinds = map[string]exeRun{
	"rust": {
		Args: rustupArgs,
		Env: func(final string) []string {
			return []string{"RUSTUP_HOME=" + filepath.Join(final, "rustup"), "CARGO_HOME=" + filepath.Join(final, "cargo")}
		},
		Timeout: 20 * time.Minute,
		Remove:  true,
	},
}

// ExeRunner runs program with args and env in dir (hidden, its process tree ended when ctx ends)
// and calls onLine for every line of its output. Injectable so that tests use a fake.
type ExeRunner func(ctx context.Context, program string, args, env []string, dir string, onLine func(string)) error

// runExe is the ExeRunner of the real program.
func runExe(ctx context.Context, program string, args, env []string, dir string, onLine func(string)) error {
	cmd := exec.CommandContext(ctx, program, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	configureExe(cmd)
	cmd.Stdin = nil
	pr, pw, err := os.Pipe()
	if err != nil {
		return err
	}
	cmd.Stdout, cmd.Stderr = pw, pw
	if err := cmd.Start(); err != nil {
		pr.Close()
		pw.Close()
		return err
	}
	pw.Close() // the child holds the only write end
	done := make(chan struct{})
	go func() {
		defer close(done)
		sc := bufio.NewScanner(pr)
		sc.Buffer(make([]byte, 64*1024), 1<<20)
		sc.Split(splitLines)
		for sc.Scan() {
			if l := strings.TrimSpace(sc.Text()); l != "" {
				onLine(l)
			}
		}
	}()
	werr := cmd.Wait()
	pr.Close()
	<-done
	return werr
}

// splitLines splits on \n, \r\n and a bare \r (rustup redraws its progress bars with \r).
func splitLines(data []byte, atEOF bool) (int, []byte, error) {
	if i := bytes.IndexAny(data, "\r\n"); i >= 0 {
		return i + 1, data[:i], nil
	}
	if atEOF && len(data) > 0 {
		return len(data), data, nil
	}
	return 0, nil, nil
}

// tailLines keeps the last n lines.
type tailLines struct {
	n     int
	lines []string
}

func (t *tailLines) add(l string) {
	t.lines = append(t.lines, l)
	if len(t.lines) > t.n {
		t.lines = t.lines[len(t.lines)-t.n:]
	}
}

func (t *tailLines) String() string { return strings.Join(t.lines, " | ") }

// exeStep turns an output line into the job's step text: "Installing: downloading component
// 'cargo'". Lines of rustup look like "info: downloading component 'cargo'".
func exeStep(line string) string {
	l := strings.TrimSpace(line)
	for _, p := range []string{"info:", "warning:", "error:"} {
		l = strings.TrimSpace(strings.TrimPrefix(l, p))
	}
	if len(l) > 70 {
		l = l[:70] + "..."
	}
	if l == "" {
		return "Installing"
	}
	return "Installing: " + l
}

// runInstallerProgram runs the program of an .exe download in the final folder and checks that
// it made the runtime's binary. A failure carries the last lines of the output.
func (in *Installer) runInstallerProgram(ctx context.Context, j *job, run exeRun, final, program, binary string) error {
	timeout := run.Timeout
	if timeout <= 0 {
		timeout = 20 * time.Minute
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	env := run.Env(final)
	if px := in.st.Snapshot().Settings.DownloadProxy; px != "" {
		env = append(env, "HTTPS_PROXY="+px, "HTTP_PROXY="+px)
	}
	tail := &tailLines{n: 6}
	pct, lines := exeBandFrom, 0
	in.log.printf(j.ID, "running %s %s", filepath.Base(program), strings.Join(run.Args, " "))
	in.progress(j, pct, "Installing")
	err := in.Run(ctx, program, run.Args, env, final, func(l string) {
		in.log.printf(j.ID, "| %s", l)
		tail.add(l)
		lines++
		if p := exeBandFrom + lines; p > pct && p <= exeBandTo {
			pct = p
		}
		in.progress(j, pct, exeStep(l))
	})
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return fmt.Errorf("the installer did not finish within %d minutes and was stopped. Last output: %s", int(timeout/time.Minute), tail)
	case err != nil:
		return fmt.Errorf("the installer failed: %v. Last output: %s", err, tail)
	}
	if st, serr := os.Stat(binary); serr != nil || st.IsDir() {
		return fmt.Errorf("the installer finished but %s was not made. Last output: %s", filepath.Base(binary), tail)
	}
	if run.Remove {
		_ = os.Remove(program)
	}
	return nil
}

// processProgram is process for an .exe download: the verified program is copied into the stage
// folder, the folder is moved to its final place, and the program runs there (rustup records
// absolute paths). A failure removes the folder.
func (in *Installer) processProgram(ctx context.Context, j *job, e model.CatalogEntry, archive, stage string) (string, error) {
	run, ok := exeKinds[e.Kind]
	if !ok {
		return "", stepErr("Extracting", fmt.Errorf("%s has no installer program: a .exe download is not an archive", e.Kind))
	}
	if err := installFile(archive, stage, e.FileName); err != nil {
		return "", stepErr("Extracting", err)
	}
	if err := os.MkdirAll(in.Bin, 0o755); err != nil {
		return "", stepErr("Installing", err)
	}
	final := filepath.Join(in.Bin, e.ID)
	if err := os.RemoveAll(final); err != nil {
		return "", stepErr("Installing", err)
	}
	if err := in.moveDir(j.ID, stage, final); err != nil {
		return "", stepErr("Installing", err)
	}
	binary := filepath.Join(final, filepath.FromSlash(e.BinaryRelPath))
	if err := in.runInstallerProgram(ctx, j, run, final, filepath.Join(final, e.FileName), binary); err != nil {
		_ = os.RemoveAll(final)
		return "", stepErr("Installing", err)
	}
	in.progress(j, 95, "Registering")
	rt := model.Runtime{
		ID: model.NewID("rt_"), Kind: e.Kind, Version: e.Version, Name: e.Name, CatalogID: e.ID,
		Binary: binary, Source: "installed", InstalledAt: time.Now().UTC().Truncate(time.Second),
	}
	if err := in.rt.Add(rt); err != nil {
		_ = os.RemoveAll(final)
		return "", stepErr("Registering", err)
	}
	return rt.ID, nil
}
