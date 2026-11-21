// Package launch opens desktop programs (code editors, database tools) for
// the user. Unlike services they are not supervised: they are not put in the
// supervisor's job object, so they keep running when Zero2Dev stops, and
// nobody waits for them.
package launch

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"zero2dev/internal/model"
)

// Start runs bin with args in dir and returns once the program was started.
// env holds extra "NAME=value" entries added to the daemon's environment.
func Start(bin string, args []string, dir string, env []string) error {
	cmd := exec.Command(bin, args...) // never a shell
	cmd.Dir = dir
	if len(env) > 0 {
		cmd.Env = append(os.Environ(), env...)
	}
	detach(cmd)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("cannot start %s: %w", filepath.Base(bin), err)
	}
	return cmd.Process.Release()
}

// ErrNoTerminal: this system has no terminal Zero2Dev can open (the API answers 501).
var ErrNoTerminal = errors.New("opening a terminal is only supported on Windows")

// DBUser is the administrator account Zero2Dev's databases are set up with
// (no password until one is set, Service.AdminPassword; they only listen on
// 127.0.0.1).
func DBUser(kind string) string {
	switch kind {
	case "mariadb", "mysql":
		return "root"
	case "postgres":
		return "postgres"
	}
	return ""
}

// ErrNoConnection: the tool cannot open this kind of database.
var ErrNoConnection = errors.New("this database tool cannot open that kind of database")

// ConnectArgs are the command-line arguments that make a database tool open a
// connection to svc (a MariaDB, MySQL or PostgreSQL service with a port).
func ConnectArgs(tool string, svc model.Service) ([]string, error) {
	if svc.Port <= 0 {
		return nil, errors.New("the database service has no port")
	}
	port := strconv.Itoa(svc.Port)
	user := DBUser(svc.Kind)
	switch tool {
	case "heidisql":
		// Long options with "=" work in every HeidiSQL version (short ones changed in v13).
		// Network types: 0 = MariaDB/MySQL over TCP/IP, 8 = PostgreSQL over TCP/IP.
		args := []string{"--description=" + svc.Name, "--host=127.0.0.1", "--port=" + port, "--user=" + user}
		if svc.AdminPassword != "" {
			args = append(args, "--password="+svc.AdminPassword)
		}
		switch svc.Kind {
		case "mariadb", "mysql":
			return append([]string{"--nettype=0"}, args...), nil
		case "postgres":
			return append([]string{"--nettype=8"}, append(args, "--databases=postgres")...), nil
		}
	case "dbeaver":
		driver := map[string]string{"mariadb": "MariaDB", "mysql": "MySQL", "postgres": "PostgreSQL"}[svc.Kind]
		if driver == "" {
			break
		}
		con := "driver=" + driver + "|host=127.0.0.1|port=" + port + "|user=" + user
		// The -con string has no escaping: a password with "|" is left out and
		// DBeaver asks for it.
		if pw := svc.AdminPassword; pw != "" && !strings.Contains(pw, "|") {
			con += "|password=" + pw
		}
		con += "|name=" + svc.Name + "|connect=true"
		if svc.Kind == "postgres" {
			con += "|database=postgres"
		}
		return []string{"-con", con}, nil
	}
	return nil, ErrNoConnection
}

// ToolDataArgs keeps the settings, extensions and workspaces of a tool that
// Zero2Dev installed in <home>/<name> (data\code, data\codium, ...), so the
// bin folder only holds programs. Each tool has its own switch for it. Tools
// registered by path are the user's own installs and keep their usual
// settings, so they get nothing.
func ToolDataArgs(rt model.Runtime, home string) (args, env []string, err error) {
	name := model.DataDirName(rt.Kind)
	if rt.Source != "installed" || name == "" {
		return nil, nil, nil
	}
	dir := filepath.Join(home, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, nil, err
	}
	switch rt.Kind {
	case "vscode", "vscodium":
		// Portable mode with the data outside the program folder: VS Code and
		// VSCodium keep user-data and extensions in $VSCODE_PORTABLE.
		return nil, []string{"VSCODE_PORTABLE=" + dir}, nil
	case "notepadpp":
		return []string{"-settingsDir=" + dir}, nil, nil
	case "heidisql":
		// The portable build writes portable_settings.txt next to heidisql.exe
		// unless told otherwise; the file is created so the setting is used.
		// O_EXCL: two launches at once must never truncate settings the
		// other (or HeidiSQL itself) already wrote.
		f := filepath.Join(dir, "portable_settings.txt")
		fh, err := os.OpenFile(f, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err == nil {
			err = fh.Close()
		} else if errors.Is(err, os.ErrExist) {
			err = nil
		}
		if err != nil {
			return nil, nil, err
		}
		return []string{"--psettings=" + f}, nil, nil
	case "dbeaver":
		ws := filepath.Join(dir, "workspace")
		if err := os.MkdirAll(ws, 0o755); err != nil {
			return nil, nil, err
		}
		return []string{"-data", ws}, nil, nil
	}
	return nil, nil, nil
}
