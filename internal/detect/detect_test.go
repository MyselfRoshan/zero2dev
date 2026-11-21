package detect

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"zero2dev/internal/apperr"
)

func TestDir(t *testing.T) {
	cases := []struct {
		name  string
		files map[string]string // relative path with "/" -> content ("" for a folder when it ends with "/")
		want  Result
	}{
		{"empty", nil, Result{Markers: []string{}}},
		{"static site", map[string]string{"index.html": "<html>"}, Result{Markers: []string{"index.html"}}},
		{"laravel", map[string]string{
			"composer.json": `{"name":"acme/my-shop","require":{"php":"^8.2","laravel/framework":"^11.0"}}`,
			"artisan":       "", ".env": "A=1", "package.json": `{"name":"x","devDependencies":{"vite":"^5"}}`,
		}, Result{Kind: "php", Framework: "laravel", Name: "my-shop", Confidence: "high", Port: 8000, EnvFile: ".env",
			Settings: map[string]string{"docroot": "public"}, Markers: []string{"composer.json", "artisan"}}},
		{"symfony", map[string]string{"composer.json": `{"name":"acme/blog","require":{"symfony/framework-bundle":"^7"}}`},
			Result{Kind: "php", Framework: "symfony", Name: "blog", Confidence: "high", Port: 8000, Settings: map[string]string{"docroot": "public"}, Markers: []string{"composer.json"}}},
		{"only symfony components is plain php", map[string]string{"composer.json": `{"name":"acme/cli","require":{"symfony/console":"^7"}}`},
			Result{Kind: "php", Name: "cli", Confidence: "medium", Port: 8000, Markers: []string{"composer.json"}}},
		{"wordpress", map[string]string{"wp-config.php": "<?php", "wp-content/": ""},
			Result{Kind: "php", Framework: "wordpress", Confidence: "high", Markers: []string{"wp-config.php", "wp-content"}}},
		{"plain php with public", map[string]string{"composer.json": `{}`, "public/index.php": "<?php"},
			Result{Kind: "php", Confidence: "medium", Port: 8000, Settings: map[string]string{"docroot": "public"}, Markers: []string{"composer.json"}}},
		{"next", map[string]string{
			"package.json":   `{"name":"@acme/web-shop","scripts":{"dev":"next dev","build":"next build"},"dependencies":{"next":"14","react":"18"}}`,
			"next.config.js": "", "pnpm-lock.yaml": "",
		}, Result{Kind: "node", Framework: "nextjs", Name: "web-shop", Confidence: "high", Port: 3000,
			// pnpm-lock.yaml: the package manager is pnpm (the option exists since the Composer work)
			Settings: map[string]string{"run": "npm", "npmScript": "dev", "packageManager": "pnpm"}, Markers: []string{"package.json", "next.config.js"}}},
		{"vite react", map[string]string{"package.json": `{"name":"ui","scripts":{"dev":"vite"},"devDependencies":{"vite":"5","react":"18"}}`, "vite.config.ts": ""},
			Result{Kind: "node", Framework: "vite-react", Name: "ui", Confidence: "high", Port: 5173,
				Settings: map[string]string{"run": "npm", "npmScript": "dev"}, Markers: []string{"package.json", "vite.config.ts"}}},
		{"vite vue", map[string]string{"package.json": `{"name":"ui","dependencies":{"vue":"3"},"devDependencies":{"vite":"5"}}`},
			Result{Kind: "node", Framework: "vite-vue", Name: "ui", Confidence: "high", Port: 5173, Markers: []string{"package.json"}}},
		{"nest", map[string]string{"package.json": `{"name":"api","scripts":{"start":"nest start"},"dependencies":{"@nestjs/core":"10"}}`, "nest-cli.json": "{}"},
			Result{Kind: "node", Framework: "nestjs", Name: "api", Confidence: "high", Port: 3000,
				Settings: map[string]string{"run": "npm", "npmScript": "start"}, Markers: []string{"package.json", "nest-cli.json"}}},
		{"astro", map[string]string{"package.json": `{"name":"site","scripts":{"dev":"astro dev"},"dependencies":{"astro":"4"}}`},
			Result{Kind: "node", Framework: "astro", Name: "site", Confidence: "high", Port: 4321,
				Settings: map[string]string{"run": "npm", "npmScript": "dev"}, Markers: []string{"package.json"}}},
		{"express with an entry file", map[string]string{"package.json": `{"name":"srv","main":"./server.js","dependencies":{"express":"4"}}`, "server.js": ""},
			Result{Kind: "node", Framework: "express", Name: "srv", Confidence: "high", Port: 3000,
				Settings: map[string]string{"run": "script", "script": "server.js"}, Markers: []string{"package.json"}}},
		{"plain node, nothing to run", map[string]string{"package.json": `{"scripts":{"build":"tsc"}}`},
			Result{Kind: "node", Confidence: "medium", Markers: []string{"package.json"}}},
		{"damaged package.json", map[string]string{"package.json": `{ nope`},
			Result{Kind: "node", Confidence: "medium", Markers: []string{"package.json"}}},
		{"go net/http", map[string]string{"go.mod": "module example.com/acme/tool\n\ngo 1.24\n", "main.go": ""},
			Result{Kind: "go", Framework: "go-nethttp", Name: "tool", Confidence: "medium", Port: 8080, Markers: []string{"go.mod"}}},
		{"go gin in cmd", map[string]string{"go.mod": "module example.com/acme/api/v2\n\nrequire github.com/gin-gonic/gin v1.9.1\n", "cmd/api/main.go": ""},
			Result{Kind: "go", Framework: "gin", Name: "api", Confidence: "high", Port: 8080, Settings: map[string]string{"package": "./cmd/api"}, Markers: []string{"go.mod"}}},
		{"go fiber", map[string]string{"go.mod": "module shop\n\nrequire (\n\tgithub.com/gofiber/fiber/v2 v2.52.0\n)\n"},
			Result{Kind: "go", Framework: "fiber", Name: "shop", Confidence: "high", Port: 3000, Markers: []string{"go.mod"}}},
		{"spring boot maven", map[string]string{"pom.xml": `<project><parent><artifactId>spring-boot-starter-parent</artifactId></parent><artifactId>orders</artifactId></project>`},
			Result{Kind: "java", Framework: "springboot", Name: "orders", Confidence: "high", Port: 8080, Markers: []string{"pom.xml"}}},
		{"spring boot gradle", map[string]string{"build.gradle": "plugins { id 'org.springframework.boot' version '3.3.0' }", "settings.gradle": "rootProject.name = 'billing'"},
			Result{Kind: "java", Framework: "springboot", Name: "billing", Confidence: "high", Port: 8080, Markers: []string{"build.gradle"}}},
		{"plain maven", map[string]string{"pom.xml": `<project><artifactId>lib</artifactId></project>`},
			Result{Kind: "java", Name: "lib", Confidence: "medium", Port: 8080, Markers: []string{"pom.xml"}}},
		{"django", map[string]string{"manage.py": "", "requirements.txt": "Django==5.0\npsycopg\n"},
			Result{Kind: "python", Framework: "django", Confidence: "high", Port: 8000, Settings: map[string]string{"run": "django", "manage": "manage.py"}, Markers: []string{"manage.py", "requirements.txt"}}},
		{"django by manage.py only", map[string]string{"manage.py": ""},
			Result{Kind: "python", Framework: "django", Confidence: "medium", Port: 8000, Settings: map[string]string{"run": "django", "manage": "manage.py"}, Markers: []string{"manage.py"}}},
		{"fastapi in pyproject", map[string]string{"pyproject.toml": "[project]\nname = \"api-svc\"\ndependencies = [\"fastapi>=0.110\", \"uvicorn\"]\n"},
			Result{Kind: "python", Framework: "fastapi", Name: "api-svc", Confidence: "high", Port: 8000, Settings: map[string]string{"run": "uvicorn"}, Markers: []string{"pyproject.toml"}}},
		{"fastapi with main.py", map[string]string{"requirements.txt": "fastapi\nuvicorn\n", "main.py": ""},
			Result{Kind: "python", Framework: "fastapi", Confidence: "high", Port: 8000, Settings: map[string]string{"run": "uvicorn", "app": "main:app"}, Markers: []string{"requirements.txt"}}},
		{"fastapi with app.py", map[string]string{"requirements.txt": "fastapi\n", "app.py": ""},
			Result{Kind: "python", Framework: "fastapi", Confidence: "high", Port: 8000, Settings: map[string]string{"run": "uvicorn", "app": "app:app"}, Markers: []string{"requirements.txt"}}},
		{"fastapi with app/main.py", map[string]string{"requirements.txt": "fastapi\n", "app/main.py": ""},
			Result{Kind: "python", Framework: "fastapi", Confidence: "high", Port: 8000, Settings: map[string]string{"run": "uvicorn", "app": "app.main:app"}, Markers: []string{"requirements.txt"}}},
		{"flask", map[string]string{"requirements.txt": "flask\n"},
			Result{Kind: "python", Framework: "flask", Confidence: "high", Port: 5000, Settings: map[string]string{"run": "flask"}, Markers: []string{"requirements.txt"}}},
		{"flask with app.py", map[string]string{"requirements.txt": "flask\n", "app.py": ""},
			Result{Kind: "python", Framework: "flask", Confidence: "high", Port: 5000, Settings: map[string]string{"run": "flask", "flaskApp": "app"}, Markers: []string{"requirements.txt"}}},
		{"flask with wsgi.py", map[string]string{"requirements.txt": "flask\n", "wsgi.py": ""},
			Result{Kind: "python", Framework: "flask", Confidence: "high", Port: 5000, Settings: map[string]string{"run": "flask", "flaskApp": "wsgi"}, Markers: []string{"requirements.txt"}}},
		{"flask-cors is not flask", map[string]string{"requirements.txt": "flask-cors\n"},
			Result{Kind: "python", Confidence: "medium", Markers: []string{"requirements.txt"}}},
		{"plain python", map[string]string{"requirements.txt": "requests\n"},
			Result{Kind: "python", Confidence: "medium", Markers: []string{"requirements.txt"}}},
		{"a django project with frontend tooling is django", map[string]string{"manage.py": "", "requirements.txt": "django", "package.json": `{"name":"fe"}`},
			Result{Kind: "python", Framework: "django", Confidence: "high", Port: 8000, Settings: map[string]string{"run": "django", "manage": "manage.py"}, Markers: []string{"manage.py", "requirements.txt"}}},
		{"a .env folder is not an env file", map[string]string{".env/": "", "go.mod": "module x\n"},
			Result{Kind: "go", Framework: "go-nethttp", Name: "x", Confidence: "medium", Port: 8080, Markers: []string{"go.mod"}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "my-folder")
			os.MkdirAll(dir, 0o755)
			for rel, content := range c.files {
				p := filepath.Join(dir, filepath.FromSlash(rel))
				if rel[len(rel)-1] == '/' {
					os.MkdirAll(p, 0o755)
					continue
				}
				os.MkdirAll(filepath.Dir(p), 0o755)
				if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			got, err := Dir(dir)
			if err != nil {
				t.Fatal(err)
			}
			want := c.want
			if want.Settings == nil {
				want.Settings = map[string]string{}
			}
			if want.Name == "" && want.Kind != "" {
				want.Name = "my-folder" // the folder's own name
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("got  %+v\nwant %+v", got, want)
			}
		})
	}
}

func TestDirErrors(t *testing.T) {
	file := filepath.Join(t.TempDir(), "f.txt")
	os.WriteFile(file, []byte("x"), 0o644)
	for name, dir := range map[string]string{
		"missing":  filepath.Join(t.TempDir(), "nope"),
		"a file":   file,
		"empty":    "",
		"relative": "some/folder",
	} {
		_, err := Dir(dir)
		var ae *apperr.Error
		if !errors.As(err, &ae) || ae.Status != 400 {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// Dir only reads: the folder is the same afterwards.
func TestDirReadsOnly(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"name":"a"}`), 0o644)
	if _, err := Dir(dir); err != nil {
		t.Fatal(err)
	}
	es, _ := os.ReadDir(dir)
	if len(es) != 1 {
		t.Fatalf("detect wrote into the folder: %v", es)
	}
}
