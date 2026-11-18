package supervisor

import (
	"fmt"
	"strconv"

	"zero2dev/internal/launch"
	"zero2dev/internal/model"
	"zero2dev/internal/store"
)

// dbConnection is the DB_CONNECTION value (the Laravel names) per database kind.
var dbConnection = map[string]string{"mariadb": "mysql", "mysql": "mysql", "postgres": "pgsql", "redis": "redis"}

// databaseEnv returns the variables that point a service at its linked
// database service (Service.DatabaseID): DB_CONNECTION, DB_HOST, DB_PORT,
// DB_USERNAME, DB_USER, DB_PASSWORD, DATABASE_URL and, for Redis, REDIS_URL.
// Zero2Dev's databases listen on 127.0.0.1 only and have no password, so none
// is ever written here. A link that cannot be used (the service is gone, is not
// a database, has no port) is returned as a warning, not an error.
func databaseEnv(svc model.Service, d store.Data) (vars []string, warning string) {
	if svc.DatabaseID == "" {
		return nil, ""
	}
	var db *model.Service
	for i := range d.Services {
		if d.Services[i].ID == svc.DatabaseID {
			db = &d.Services[i]
			break
		}
	}
	switch {
	case db == nil:
		return nil, fmt.Sprintf("database service %s not found, no database variables were set", svc.DatabaseID)
	case dbConnection[db.Kind] == "":
		return nil, fmt.Sprintf("%s is not a database, no database variables were set", db.Name)
	case db.Port <= 0:
		return nil, fmt.Sprintf("database %s has no port, no database variables were set", db.Name)
	}
	port := strconv.Itoa(db.Port)
	user := launch.DBUser(db.Kind)
	vars = []string{"DB_CONNECTION=" + dbConnection[db.Kind], "DB_HOST=127.0.0.1", "DB_PORT=" + port}
	switch db.Kind {
	case "mariadb", "mysql":
		vars = append(vars, "DB_USERNAME="+user, "DB_USER="+user, "DB_PASSWORD=", "DATABASE_URL=mysql://"+user+"@127.0.0.1:"+port+"/")
	case "postgres":
		vars = append(vars, "DB_USERNAME="+user, "DB_USER="+user, "DB_PASSWORD=", "DATABASE_URL=postgresql://"+user+"@127.0.0.1:"+port+"/postgres")
	case "redis":
		vars = append(vars, "DB_PASSWORD=", "REDIS_URL=redis://127.0.0.1:"+port)
	}
	return vars, ""
}
