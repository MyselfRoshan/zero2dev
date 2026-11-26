package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"zero2dev/internal/model"
)

// DefaultComposerURL lists the Composer releases; its "stable" list has the current release and
// the long-term-support one.
const DefaultComposerURL = "https://getcomposer.org/versions"

// composerPathRE is the only download path taken from the list.
var composerPathRE = regexp.MustCompile(`^/download/(\d+\.\d+\.\d+)/composer\.phar$`)

type composerVersions struct {
	Stable []struct {
		Path    string `json:"path"`
		Version string `json:"version"`
		LTS     bool   `json:"lts"`
	} `json:"stable"`
}

func (r *Remote) fetchComposer(ctx context.Context) ([]model.CatalogEntry, error) {
	src := or(r.ComposerURL, DefaultComposerURL)
	b, err := r.get(ctx, src)
	if err != nil {
		return nil, err
	}
	u, err := url.Parse(src)
	if err != nil || u.Host == "" {
		return nil, errors.New("bad composer list url")
	}
	return parseComposer(b, u.Scheme+"://"+u.Host)
}

// parseComposer turns the "stable" list of getcomposer.org/versions into entries. composer.phar
// does not depend on the CPU, so each release is offered for Windows on x64 and on arm64 (the
// catalog only offers an entry that matches the machine). The checksum is the file the publisher
// puts next to the download. The long-term-support release has the line "2.2", the current one "2".
func parseComposer(b []byte, base string) ([]model.CatalogEntry, error) {
	var v composerVersions
	if err := json.Unmarshal(b, &v); err != nil {
		return nil, fmt.Errorf("unexpected composer versions: %v", err)
	}
	if !hostOK(base, "getcomposer.org") {
		return nil, errors.New("composer download base points to an unexpected host")
	}
	base = strings.TrimSuffix(base, "/")
	var out []model.CatalogEntry
	for _, rel := range v.Stable {
		m := composerPathRE.FindStringSubmatch(rel.Path)
		if m == nil || m[1] != rel.Version {
			continue
		}
		line, ch, name := "2", "stable", "Composer "+rel.Version
		if rel.LTS {
			line, ch, name = "2.2", "lts", name+" (LTS)"
		}
		for _, arch := range []string{"amd64", "arm64"} {
			out = append(out, model.CatalogEntry{
				ID: "composer-" + rel.Version + "-windows-" + arch, Kind: "composer", Version: rel.Version, Name: name,
				URL: base + rel.Path, Checksum: model.Checksum{Type: "shasums", URL: base + rel.Path + ".sha256sum"},
				BinaryRelPath: "composer.bat", OS: "windows", Arch: arch, Homepage: base + "/download/",
				Line: line, Channel: ch, Source: "remote",
			})
		}
	}
	if len(out) == 0 {
		return nil, errors.New("no Composer releases found")
	}
	return out, nil
}
