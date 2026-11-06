package installer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"
)

// ValidateProxy checks the downloadProxy setting: empty, or an http(s) URL
// with a host (optionally user:pass@).
func ValidateProxy(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", nil
	}
	u, err := url.Parse(s)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" ||
		(u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("downloadProxy must look like http://host:port or https://host:port")
	}
	if p := u.Port(); p != "" {
		var n int
		if _, err := fmt.Sscanf(p, "%d", &n); err != nil || n < 1 || n > 65535 {
			return "", fmt.Errorf("downloadProxy has an invalid port")
		}
	}
	return s, nil
}

// newClient builds the HTTP client: explicit timeouts, proxy from the
// setting (else HTTP_PROXY/HTTPS_PROXY/NO_PROXY). There is deliberately no
// overall client timeout (archives are large); stalls are caught separately.
func newClient(proxy string) *http.Client { return HTTPClient(proxy) }

// HTTPClient builds the client used for downloads and version checks (see newClient).
func HTTPClient(proxy string) *http.Client {
	tr := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		TLSHandshakeTimeout:   15 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
		ForceAttemptHTTP2:     true,
	}
	if proxy != "" {
		if u, err := url.Parse(proxy); err == nil {
			tr.Proxy = http.ProxyURL(u)
		}
	}
	return &http.Client{Transport: tr}
}

func (in *Installer) client() *http.Client {
	if in.Client != nil {
		return in.Client
	}
	return newClient(in.st.Snapshot().Settings.DownloadProxy)
}

// fatal marks an error that must not be retried.
type fatal struct{ error }

func (f fatal) Unwrap() error { return f.error }

// stallReader re-arms the stall timer whenever bytes arrive.
type stallReader struct {
	r io.Reader
	t *time.Timer
	d time.Duration
}

func (s *stallReader) Read(p []byte) (int, error) {
	n, err := s.r.Read(p)
	if n > 0 {
		s.t.Reset(s.d)
	}
	return n, err
}

// get performs a GET with the User-Agent, stall detection and up to
// len(RetryDelays)+1 attempts. Network errors and HTTP 5xx are retried;
// HTTP 4xx and errors wrapped in fatal are not. handle consumes the body.
func (in *Installer) get(ctx context.Context, job, rawURL string, handle func(resp *http.Response, body io.Reader) error) error {
	var last error
	for attempt := 0; ; attempt++ {
		if attempt > 0 {
			if attempt > len(in.RetryDelays) || ctx.Err() != nil {
				return last
			}
			in.log.printf(job, "retry %d after: %v", attempt, last)
			in.Sleep(in.RetryDelays[attempt-1])
		}
		err, retry := in.getOnce(ctx, job, rawURL, handle)
		if err == nil {
			return nil
		}
		last = err
		if !retry {
			return err
		}
	}
}

func (in *Installer) getOnce(ctx context.Context, job, rawURL string, handle func(*http.Response, io.Reader) error) (error, bool) {
	actx, cancel := context.WithCancel(ctx)
	defer cancel()
	var stalled atomic.Bool
	timer := time.AfterFunc(in.StallTimeout, func() { stalled.Store(true); cancel() })
	defer timer.Stop()
	wrap := func(err error) error {
		if stalled.Load() {
			return fmt.Errorf("no data received for %s from %s", in.StallTimeout, rawURL)
		}
		return err
	}

	req, err := http.NewRequestWithContext(actx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err, false
	}
	req.Header.Set("User-Agent", "Zero2Dev/"+in.Version)
	resp, err := in.client().Do(req)
	if err != nil {
		return wrap(err), true
	}
	defer resp.Body.Close()
	final := redactURL(resp.Request.URL)
	in.log.printf(job, "GET %s -> HTTP %d (final URL %s)", rawURL, resp.StatusCode, final)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d %s from %s", resp.StatusCode, http.StatusText(resp.StatusCode), final), resp.StatusCode >= 500
	}
	timer.Reset(in.StallTimeout)
	err = handle(resp, &stallReader{r: resp.Body, t: timer, d: in.StallTimeout})
	if err != nil {
		var f fatal
		return wrap(err), !errors.As(err, &f) || stalled.Load()
	}
	return nil, false
}

// redactURL drops the query string: release hosts redirect to signed,
// short-lived URLs whose tokens have no place in a log or an error message.
func redactURL(u *url.URL) string {
	c := *u
	c.RawQuery, c.Fragment = "", ""
	return c.String()
}
