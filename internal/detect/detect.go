// Package detect recognises what kind of project a folder holds (package.json, composer.json,
// go.mod, pom.xml, manage.py ...), so the New service form can fill itself in.
//
// Contract (docs/API.md, "Amendment: languages, tasks and new projects (0.0.6)"). Wave 0 stub.
package detect

import "errors"

// ErrNotImplemented is returned by the Wave 0 stub (the API answers 501).
var ErrNotImplemented = errors.New("not implemented yet")

// Result is GET /api/project/detect?dir=.
type Result struct {
	// Kind is the service kind that fits ("node", "php", "go", "java", "python"), or "" when the
	// folder says nothing (then nothing else is filled in).
	Kind string `json:"kind"`
	// Framework is the framework id from the shipped list ("laravel", "nextjs", "django" ...) or "".
	Framework string `json:"framework"`
	// Name is a name for the service: the project's own name, else the folder's.
	Name string `json:"name"`
	// Confidence is "high" (a marker file and a framework dependency), "medium" (a marker file) or "".
	Confidence string `json:"confidence"`
	// Settings are values for the kind's options (script, package, start script ...), only the
	// ones that were worked out.
	Settings map[string]string `json:"settings"`
	// EnvFile is the .env file of the project when there is one (".env"), else "".
	EnvFile string `json:"envFile"`
	// Port is the port the framework usually listens on, 0 when unknown.
	Port int `json:"port"`
	// Markers are the files that were looked at and found, for the UI to say why ("package.json").
	Markers []string `json:"markers"`
}

// Dir looks at a folder. A folder that does not exist is a 400.
func Dir(dir string) (Result, error) { return Result{}, ErrNotImplemented }
