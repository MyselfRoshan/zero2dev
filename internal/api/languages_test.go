package api

import "testing"

// Wave 0: the routes exist and answer what the contract says until their work is in.
func TestLanguageRoutesWave0(t *testing.T) {
	e := newEnv(t)
	// not wired: 501
	e.errCode("GET", "/api/services/svc_x/tasks", nil, 501)
	e.errCode("POST", "/api/services/svc_x/tasks", map[string]any{"preset": "npm-install"}, 501)
	e.errCode("POST", "/api/new-project/plan", map[string]any{"framework": "html"}, 501)
	e.errCode("POST", "/api/new-project", map[string]any{"framework": "html"}, 501)
	// detect: the folder is required; the stub answers 501 for the rest
	e.errCode("GET", "/api/project/detect", nil, 400)
	e.errCode("GET", "/api/project/detect?dir="+t.TempDir(), nil, 501)

	// the shipped list is served as {frameworks: [...]}
	var got struct {
		Frameworks []struct {
			ID, Language string
		}
	}
	if resp := e.do("GET", "/api/frameworks", nil, &got); resp.StatusCode != 200 || len(got.Frameworks) < 2 {
		t.Fatalf("frameworks %d %+v", resp.StatusCode, got)
	}
}
