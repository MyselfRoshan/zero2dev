package detect

import (
	"regexp"
	"strings"
)

// ---- Rust: Cargo.toml ----------------------------------------------------------------------

var (
	tomlSection   = regexp.MustCompile(`^\[\[?\s*([^\]]+?)\s*\]\]?\s*(?:#.*)?$`)
	tomlStringKey = regexp.MustCompile(`^([A-Za-z0-9_-]+)\s*=\s*"([^"]*)"`)
	tomlAnyKey    = regexp.MustCompile(`^"?([A-Za-z0-9_-]+)"?\s*=`)
)

// cargoFacts are what the detection reads from a Cargo.toml.
type cargoFacts struct {
	name, defaultRun string
	axum             bool
}

// readCargo scans Cargo.toml line by line (no TOML library: the standard library has none). It
// keeps the [package] name and default-run, and whether axum is a dependency: a key of a
// [dependencies], [dev-dependencies], [build-dependencies] or [workspace.dependencies] table
// (also under [target.<cfg>.dependencies]), or a [dependencies.axum] table of its own.
func readCargo(text string) cargoFacts {
	var f cargoFacts
	section := ""
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if m := tomlSection.FindStringSubmatch(line); m != nil {
			section = m[1]
			if isDepTable(section, true) && lastPart(section) == "axum" {
				f.axum = true // [dependencies.axum]
			}
			continue
		}
		switch {
		case section == "package":
			if m := tomlStringKey.FindStringSubmatch(line); m != nil {
				switch m[1] {
				case "name":
					f.name = m[2]
				case "default-run":
					f.defaultRun = m[2]
				}
			}
		case isDepTable(section, false):
			if m := tomlAnyKey.FindStringSubmatch(line); m != nil && m[1] == "axum" {
				f.axum = true
			}
		}
	}
	return f
}

func lastPart(section string) string {
	i := strings.LastIndex(section, ".")
	return strings.Trim(section[i+1:], `" `)
}

// isDepTable tells whether a section is a dependency table (withCrate: or the table of one crate,
// [dependencies.axum]).
func isDepTable(section string, withCrate bool) bool {
	for _, t := range []string{"dependencies", "dev-dependencies", "build-dependencies"} {
		if section == t || section == "workspace."+t || (strings.HasPrefix(section, "target.") && strings.HasSuffix(section, "."+t)) {
			return !withCrate
		}
		if withCrate && (strings.HasPrefix(section, t+".") || strings.HasPrefix(section, "workspace."+t+".")) {
			return true
		}
	}
	return false
}

func probeRust(d *folder) *Result {
	if !d.has("Cargo.toml") {
		return nil
	}
	f := readCargo(d.read("Cargo.toml"))
	r := &Result{Kind: "rust", Confidence: "medium", Port: 8080, Name: f.name, Markers: []string{"Cargo.toml"}}
	if f.axum {
		r.Framework, r.Confidence, r.Port = "axum", "high", 3000
	}
	st := newSettings("rust")
	st.set("bin", f.defaultRun)
	r.Settings = st.done()
	return r
}
