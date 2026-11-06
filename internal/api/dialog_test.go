package api

import (
	"errors"
	"testing"

	"zero2dev/internal/winpick"
)

func TestFolderDialogEndpoint(t *testing.T) {
	e := newEnv(t)

	// No picker on this OS: 501, and status says so (the UI hides Browse).
	var st struct {
		FolderPicker bool `json:"folderPicker"`
	}
	e.do("GET", "/api/status", nil, &st)
	if st.FolderPicker {
		t.Fatal("folderPicker must be false without a picker")
	}
	if c := e.errCode("POST", "/api/dialog/folder", nil, 501); c != "not_supported" {
		t.Fatal(c)
	}

	var gotTitle, gotStart string
	result := struct {
		path string
		ok   bool
		err  error
	}{`C:\work\app`, true, nil}
	e.srv.PickFolder = func(title, start string) (string, bool, error) {
		gotTitle, gotStart = title, start
		return result.path, result.ok, result.err
	}
	e.do("GET", "/api/status", nil, &st)
	if !st.FolderPicker {
		t.Fatal("folderPicker must be true with a picker")
	}

	var out struct {
		Path      string `json:"path"`
		Cancelled bool   `json:"cancelled"`
	}
	// empty body: default title
	if r := e.do("POST", "/api/dialog/folder", nil, &out); r.StatusCode != 200 || out.Path != `C:\work\app` || out.Cancelled || gotTitle != "Select a folder" {
		t.Fatalf("%d %+v %q", r.StatusCode, out, gotTitle)
	}
	e.do("POST", "/api/dialog/folder", map[string]string{"title": " Project folder ", "start": ` C:\work `}, &out)
	if gotTitle != "Project folder" || gotStart != `C:\work` {
		t.Fatalf("title %q start %q", gotTitle, gotStart)
	}

	result.path, result.ok = "", false
	out = struct {
		Path      string `json:"path"`
		Cancelled bool   `json:"cancelled"`
	}{}
	e.do("POST", "/api/dialog/folder", nil, &out)
	if !out.Cancelled || out.Path != "" {
		t.Fatalf("cancel: %+v", out)
	}

	result.err = winpick.ErrBusy
	if c := e.errCode("POST", "/api/dialog/folder", nil, 409); c != "conflict" {
		t.Fatal(c)
	}
	result.err = errors.New("boom")
	e.errCode("POST", "/api/dialog/folder", nil, 500)
	result.err = nil
	e.errCode("POST", "/api/dialog/folder", map[string]string{"title": "a\nb"}, 400)
	e.errCode("POST", "/api/dialog/folder", map[string]any{"nope": 1}, 400)
}

func TestSiteEnableDisable(t *testing.T) {
	e := newEnv(t)
	var site struct {
		ID      string `json:"id"`
		Enabled bool   `json:"enabled"`
		URL     string `json:"url"`
	}
	if r := e.do("POST", "/api/sites", map[string]any{"name": "x", "domain": "x.localhost", "type": "proxy", "upstreamPort": 3000}, &site); r.StatusCode != 201 {
		t.Fatalf("create: %d", r.StatusCode)
	}
	if r := e.do("POST", "/api/sites/"+site.ID+"/disable", nil, &site); r.StatusCode != 200 || site.Enabled {
		t.Fatalf("disable: %d %+v", r.StatusCode, site)
	}
	if r := e.do("POST", "/api/sites/"+site.ID+"/enable", nil, &site); r.StatusCode != 200 || !site.Enabled || site.URL == "" {
		t.Fatalf("enable: %d %+v", r.StatusCode, site)
	}
	if c := e.errCode("POST", "/api/sites/site_nope/enable", nil, 404); c != "not_found" {
		t.Fatal(c)
	}
}
