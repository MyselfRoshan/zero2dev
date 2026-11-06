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

// DBUser is the administrator account Zero2Dev's databases are set up with
// (no password; they only listen on 127.0.0.1).
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
		con := "driver=" + driver + "|host=127.0.0.1|port=" + port + "|user=" + user + "|name=" + svc.Name + "|connect=true"
		if svc.Kind == "postgres" {
			con += "|database=postgres"
		}
		return []string{"-con", con}, nil
	}
	return nil, ErrNoConnection
}
