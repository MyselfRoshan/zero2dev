package supervisor

import (
	"reflect"
	"strings"
	"testing"

	"zero2dev/internal/model"
	"zero2dev/internal/store"
)

func TestDatabaseEnvPerKind(t *testing.T) {
	cases := []struct {
		kind string
		want []string
	}{
		{"mariadb", []string{"DB_CONNECTION=mysql", "DB_HOST=127.0.0.1", "DB_PORT=3307", "DB_USERNAME=root", "DB_USER=root", "DB_PASSWORD=", "DATABASE_URL=mysql://root@127.0.0.1:3307/"}},
		{"mysql", []string{"DB_CONNECTION=mysql", "DB_HOST=127.0.0.1", "DB_PORT=3307", "DB_USERNAME=root", "DB_USER=root", "DB_PASSWORD=", "DATABASE_URL=mysql://root@127.0.0.1:3307/"}},
		{"postgres", []string{"DB_CONNECTION=pgsql", "DB_HOST=127.0.0.1", "DB_PORT=3307", "DB_USERNAME=postgres", "DB_USER=postgres", "DB_PASSWORD=", "DATABASE_URL=postgresql://postgres@127.0.0.1:3307/postgres"}},
		{"redis", []string{"DB_CONNECTION=redis", "DB_HOST=127.0.0.1", "DB_PORT=3307", "DB_PASSWORD=", "REDIS_URL=redis://127.0.0.1:3307"}},
	}
	for _, c := range cases {
		t.Run(c.kind, func(t *testing.T) {
			d := store.Data{Services: []model.Service{{ID: "db", Name: "DB", Kind: c.kind, Port: 3307}}}
			got, warn := databaseEnv(model.Service{ID: "app", DatabaseID: "db"}, d)
			if warn != "" || !reflect.DeepEqual(got, c.want) {
				t.Errorf("got %q (%q)\nwant %q", got, warn, c.want)
			}
		})
	}
}

func TestDatabaseEnvUnusableLink(t *testing.T) {
	d := store.Data{Services: []model.Service{
		{ID: "web", Name: "Web", Kind: "node", Port: 3000},
		{ID: "noport", Name: "NoPort", Kind: "postgres"},
	}}
	if v, w := databaseEnv(model.Service{}, d); v != nil || w != "" {
		t.Errorf("no link: %q %q", v, w)
	}
	for id, want := range map[string]string{"gone": "not found", "web": "not a database", "noport": "no port"} {
		v, w := databaseEnv(model.Service{DatabaseID: id}, d)
		if v != nil || !strings.Contains(w, want) {
			t.Errorf("%s: %q %q", id, v, w)
		}
	}
}

func TestDatabaseEnvWithPassword(t *testing.T) {
	cases := []struct {
		kind string
		want []string
	}{
		{"mariadb", []string{"DB_CONNECTION=mysql", "DB_HOST=127.0.0.1", "DB_PORT=3307", "DB_USERNAME=root", "DB_USER=root", "DB_PASSWORD=p@ss/w:rd", "DATABASE_URL=mysql://root:p%40ss%2Fw%3Ard@127.0.0.1:3307/"}},
		{"postgres", []string{"DB_CONNECTION=pgsql", "DB_HOST=127.0.0.1", "DB_PORT=3307", "DB_USERNAME=postgres", "DB_USER=postgres", "DB_PASSWORD=p@ss/w:rd", "DATABASE_URL=postgresql://postgres:p%40ss%2Fw%3Ard@127.0.0.1:3307/postgres"}},
		{"redis", []string{"DB_CONNECTION=redis", "DB_HOST=127.0.0.1", "DB_PORT=3307", "DB_PASSWORD=p@ss/w:rd", "REDIS_URL=redis://:p%40ss%2Fw%3Ard@127.0.0.1:3307"}},
	}
	for _, c := range cases {
		d := store.Data{Services: []model.Service{{ID: "db", Name: "DB", Kind: c.kind, Port: 3307, AdminPassword: "p@ss/w:rd"}}}
		got, warn := databaseEnv(model.Service{ID: "app", DatabaseID: "db"}, d)
		if warn != "" || !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: got %q (%q)\nwant %q", c.kind, got, warn, c.want)
		}
	}
}
