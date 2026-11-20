package supervisor

import (
	"fmt"
	"net/url"
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
// Zero2Dev's databases listen on 127.0.0.1 only and have no password unless
// one was set (Service.AdminPassword): then DB_PASSWORD holds it and the URLs
// carry it URL-escaped (mysql://root:p%40ss@127.0.0.1:3306/,
// redis://:p%40ss@127.0.0.1:6379). A link that cannot be used (the service is
// gone, is not a database, has no port) is returned as a warning, not an error.
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
	pw := db.AdminPassword
	// auth is the user-info part of a URL: "root@", "root:p%40ss@", ":p%40ss@" or "".
	auth := func(u string) string {
		switch {
		case pw != "":
			return url.UserPassword(u, pw).String() + "@"
		case u != "":
			return url.User(u).String() + "@"
		}
		return ""
	}
	vars = []string{"DB_CONNECTION=" + dbConnection[db.Kind], "DB_HOST=127.0.0.1", "DB_PORT=" + port}
	switch db.Kind {
	case "mariadb", "mysql":
		vars = append(vars, "DB_USERNAME="+user, "DB_USER="+user, "DB_PASSWORD="+pw, "DATABASE_URL=mysql://"+auth(user)+"127.0.0.1:"+port+"/")
	case "postgres":
		vars = append(vars, "DB_USERNAME="+user, "DB_USER="+user, "DB_PASSWORD="+pw, "DATABASE_URL=postgresql://"+auth(user)+"127.0.0.1:"+port+"/postgres")
	case "redis":
		vars = append(vars, "DB_PASSWORD="+pw, "REDIS_URL=redis://"+auth("")+"127.0.0.1:"+port)
	}
	return vars, ""
}
