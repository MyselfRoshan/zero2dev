// Package model holds the JSON data types shared by the daemon packages.
// Field names and tags follow docs/API.md exactly.
package model

import (
	"crypto/rand"
	"encoding/hex"
	"time"
)

// NewID returns prefix + 8 random hex chars (e.g. svc_a1b2c3d4).
func NewID(prefix string) string {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		panic(err) // the OS RNG failing is unrecoverable
	}
	return prefix + hex.EncodeToString(b)
}

// Service states.
const (
	StateStopped  = "stopped"
	StateStarting = "starting"
	StateRunning  = "running"
	StateStopping = "stopping"
	StateFailed   = "failed"
)

// Service is a managed process definition plus its runtime status.
// State, Pid, StartedAt and LastError are filled in by the supervisor.
type Service struct {
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	Kind       string            `json:"kind"`
	Command    string            `json:"command"`
	Args       []string          `json:"args"`
	Cwd        string            `json:"cwd"`
	Env        map[string]string `json:"env"`
	Port       int               `json:"port"`
	HealthPath string            `json:"healthPath"`
	RuntimeID  string            `json:"runtimeId"`
	Autostart  bool              `json:"autostart"`
	State      string            `json:"state"`
	Pid        int               `json:"pid"`
	StartedAt  *time.Time        `json:"startedAt"`
	LastError  string            `json:"lastError"`
}

// ServiceInput is the body of create/update.
type ServiceInput struct {
	Name       string            `json:"name"`
	Kind       string            `json:"kind"`
	Command    string            `json:"command"`
	Args       []string          `json:"args"`
	Cwd        string            `json:"cwd"`
	Env        map[string]string `json:"env"`
	Port       int               `json:"port"`
	HealthPath string            `json:"healthPath"`
	RuntimeID  string            `json:"runtimeId"`
	Autostart  bool              `json:"autostart"`
}

// Runtime is an installed or registered language runtime / tool.
type Runtime struct {
	ID          string    `json:"id"`
	Kind        string    `json:"kind"`
	Version     string    `json:"version"`
	Name        string    `json:"name"`
	Binary      string    `json:"binary"`
	Source      string    `json:"source"`
	InstalledAt time.Time `json:"installedAt"`
}

// Checksum describes how an archive is verified.
type Checksum struct {
	Type  string `json:"type"` // sha256 | shasums
	Value string `json:"value,omitempty"`
	URL   string `json:"url,omitempty"`
}

// CatalogEntry is a downloadable runtime.
type CatalogEntry struct {
	ID              string   `json:"id"`
	Kind            string   `json:"kind"`
	Version         string   `json:"version"`
	Name            string   `json:"name"`
	URL             string   `json:"url"`
	Checksum        Checksum `json:"checksum"`
	BinaryRelPath   string   `json:"binaryRelPath"`
	StripComponents int      `json:"stripComponents"`
	OS              string   `json:"os"`
	Arch            string   `json:"arch"`
	Homepage        string   `json:"homepage"`
	FileName        string   `json:"fileName"`
	Installed       bool     `json:"installed"`
	Supported       bool     `json:"supported"`
}

// Site maps a local domain to a folder or an upstream.
type Site struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Domain       string `json:"domain"`
	Type         string `json:"type"`
	Root         string `json:"root"`
	ServiceID    string `json:"serviceId"`
	UpstreamPort int    `json:"upstreamPort"`
	HTTPS        bool   `json:"https"`
	Enabled      bool   `json:"enabled"`
}

// SiteView is the API response shape (Site plus computed url).
type SiteView struct {
	Site
	URL string `json:"url"`
}

// SiteInput is the body of site create/update. Enabled defaults to true.
type SiteInput struct {
	Name         string `json:"name"`
	Domain       string `json:"domain"`
	Type         string `json:"type"`
	Root         string `json:"root"`
	ServiceID    string `json:"serviceId"`
	UpstreamPort int    `json:"upstreamPort"`
	HTTPS        bool   `json:"https"`
	Enabled      *bool  `json:"enabled"`
}

// Job is a long running operation (runtime install).
type Job struct {
	ID        string `json:"id"`
	Type      string `json:"type"`
	State     string `json:"state"` // running | done | failed
	Percent   int    `json:"percent"`
	Step      string `json:"step"`
	Error     string `json:"error"`
	RuntimeID string `json:"runtimeId"`
}

// Settings are user-editable daemon settings.
type Settings struct {
	APIPort            int    `json:"apiPort"`
	HomeDir            string `json:"homeDir"`
	DomainSuffix       string `json:"domainSuffix"`
	ProxyHTTPPort      int    `json:"proxyHttpPort"`
	ProxyHTTPSPort     int    `json:"proxyHttpsPort"`
	OpenBrowserOnStart bool   `json:"openBrowserOnStart"`
	TrustLocalCA       bool   `json:"trustLocalCA"`
	DownloadProxy      string `json:"downloadProxy"`
}

// LogLine is one captured output line.
type LogLine struct {
	Ts     string `json:"ts"`
	Stream string `json:"stream"` // stdout | stderr | system
	Text   string `json:"text"`
}

// ServiceKinds and RuntimeKinds are the accepted enums.
var (
	ServiceKinds = []string{"custom", "node", "php", "go", "java", "caddy"}
	RuntimeKinds = []string{"node", "php", "go", "java", "caddy", "other"}
)

// In reports whether s is in list.
func In(s string, list []string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
