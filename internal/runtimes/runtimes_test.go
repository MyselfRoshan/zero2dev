package runtimes

import "testing"

func TestVersionRegexp(t *testing.T) {
	cases := map[string]string{
		"v22.11.0\n":                          "22.11.0",
		"PHP 8.3.1 (cli) (built: Dec 2023)":   "8.3.1",
		"go version go1.23.3 linux/amd64":     "1.23.3",
		`openjdk version "21.0.1" 2023-10-17`: "21.0.1",
		"v2.8.4 h1:abc":                       "2.8.4",
		"nginx version: nginx/1.24.0":         "1.24.0",
		"no digits here":                      "",
	}
	for in, want := range cases {
		if got := versionRE.FindString(in); got != want {
			t.Errorf("%q -> %q, want %q", in, got, want)
		}
	}
}

func TestDisplayName(t *testing.T) {
	if DisplayName("node", "22.11.0", "x") != "Node.js 22.11.0" || DisplayName("other", "1.0", "/usr/sbin/nginx") != "nginx 1.0" {
		t.Fatal("display names")
	}
}
