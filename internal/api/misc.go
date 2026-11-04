package api

import (
	"fmt"
	"net/http"
	"runtime"
	"time"

	"zero2dev/internal/apperr"
	"zero2dev/internal/model"
	"zero2dev/internal/sites"
	"zero2dev/internal/store"
)

func (s *Server) status(w http.ResponseWriter, r *http.Request) error {
	d := s.Store.Snapshot()
	running := 0
	for _, sv := range d.Services {
		if s.Sup.State(sv.ID) == model.StateRunning {
			running++
		}
	}
	writeJSON(w, 200, map[string]any{
		"version": s.Version, "uptimeSeconds": int64(time.Since(s.Started).Seconds()),
		"os": runtime.GOOS, "arch": runtime.GOARCH, "homeDir": s.Cfg.Home,
		"counts": map[string]int{"services": len(d.Services), "running": running, "runtimes": len(d.Runtimes), "sites": len(d.Sites)},
	})
	return nil
}

func (s *Server) settings() model.Settings {
	st := s.Store.Snapshot().Settings
	st.APIPort, st.HomeDir = s.Cfg.Port, s.Cfg.Home
	return st
}

func (s *Server) getSettings(w http.ResponseWriter, r *http.Request) error {
	writeJSON(w, 200, s.settings())
	return nil
}

// settingsInput: every field optional (nil = unchanged). apiPort/homeDir are
// accepted and ignored because the UI echoes whole Settings objects back.
type settingsInput struct {
	APIPort            *int    `json:"apiPort"`
	HomeDir            *string `json:"homeDir"`
	DomainSuffix       *string `json:"domainSuffix"`
	ProxyHTTPPort      *int    `json:"proxyHttpPort"`
	ProxyHTTPSPort     *int    `json:"proxyHttpsPort"`
	OpenBrowserOnStart *bool   `json:"openBrowserOnStart"`
}

func (s *Server) putSettings(w http.ResponseWriter, r *http.Request) error {
	var in settingsInput
	if err := decode(w, r, &in); err != nil {
		return err
	}
	changedProxy := false
	err := s.Store.Update(func(d *store.Data) error {
		st := &d.Settings
		if in.DomainSuffix != nil {
			suf := lower(*in.DomainSuffix)
			if !sites.SuffixRE.MatchString(suf) {
				return apperr.BadRequest("domainSuffix %q is not a valid domain suffix", *in.DomainSuffix)
			}
			st.DomainSuffix = suf
		}
		if in.ProxyHTTPPort != nil {
			st.ProxyHTTPPort, changedProxy = *in.ProxyHTTPPort, true
		}
		if in.ProxyHTTPSPort != nil {
			st.ProxyHTTPSPort, changedProxy = *in.ProxyHTTPSPort, true
		}
		for _, p := range []int{st.ProxyHTTPPort, st.ProxyHTTPSPort} {
			if p < 1 || p > 65535 {
				return apperr.BadRequest("proxy ports must be 1-65535")
			}
		}
		if st.ProxyHTTPPort == st.ProxyHTTPSPort {
			return apperr.BadRequest("proxyHttpPort and proxyHttpsPort must differ")
		}
		if st.ProxyHTTPPort == s.Cfg.Port || st.ProxyHTTPSPort == s.Cfg.Port {
			return apperr.BadRequest("proxy ports must differ from the API port %d", s.Cfg.Port)
		}
		if in.OpenBrowserOnStart != nil {
			st.OpenBrowserOnStart = *in.OpenBrowserOnStart
		}
		return nil
	})
	if err != nil {
		return err
	}
	if changedProxy {
		if err := s.Sites.Regenerate(); err != nil {
			return apperr.Internal("cannot write Caddyfile: %v", err)
		}
		s.Hub.Publish("sites.changed", struct{}{})
	}
	writeJSON(w, 200, s.settings())
	return nil
}

func (s *Server) getJob(w http.ResponseWriter, r *http.Request) error {
	j, ok := s.Installer.Get(r.PathValue("id"))
	if !ok {
		return apperr.NotFound("job %s not found", r.PathValue("id"))
	}
	writeJSON(w, 200, j)
	return nil
}

func (s *Server) eventTicket(w http.ResponseWriter, r *http.Request) error {
	writeJSON(w, 200, map[string]string{"ticket": s.Hub.NewTicket()})
	return nil
}

// eventStream serves text/event-stream. The ticket is the credential.
func (s *Server) eventStream(w http.ResponseWriter, r *http.Request) error {
	if !s.Hub.RedeemTicket(r.URL.Query().Get("ticket")) {
		return apperr.Unauthorized("invalid, expired or already used ticket")
	}
	fl, ok := w.(http.Flusher)
	if !ok {
		return apperr.Internal("streaming unsupported")
	}
	sub := s.Hub.Subscribe()
	if sub == nil {
		return apperr.Conflict("daemon is shutting down")
	}
	defer s.Hub.Unsubscribe(sub)
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(200)
	fmt.Fprint(w, ": connected\n\n")
	fl.Flush()
	ping := time.NewTicker(15 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return nil
		case frame, ok := <-sub.C:
			if !ok { // dropped as slow consumer, or shutdown
				return nil
			}
			if _, err := w.Write(frame); err != nil {
				return nil
			}
			fl.Flush()
		case <-ping.C:
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
				return nil
			}
			fl.Flush()
		}
	}
}
