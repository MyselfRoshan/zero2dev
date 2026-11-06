package catalog

import (
	"errors"
	"fmt"
	"html"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// XdebugPage lists Xdebug's Windows builds. Every link carries its SHA-256 in
// its title: <a title="SHA256:&nbsp;<hex>" href='/files/php_xdebug-3.5.3-8.4-nts-vs17-x86_64.dll'>.
const XdebugPage = "https://xdebug.org/download"

// XdebugBuild is one Windows DLL of Xdebug.
type XdebugBuild struct {
	Version  string // 3.5.3
	PHP      string // 8.4
	URL      string
	SHA256   string
	FileName string
}

var (
	anchorRE   = regexp.MustCompile(`(?is)<a\s[^>]*>`)
	attrRE     = regexp.MustCompile(`(?is)\b(href|title)\s*=\s*("[^"]*"|'[^']*')`)
	xdebugDLL  = regexp.MustCompile(`^php_xdebug-(\d+\.\d+\.\d+)-(\d+\.\d+)-(nts|ts)-(?:vs|vc)\d+-x86_64\.dll$`)
	sha256Text = regexp.MustCompile(`(?i)SHA256:\s*([0-9a-f]{64})`)
)

// ParseXdebug finds the newest stable non-thread-safe x64 Xdebug for PHP
// phpMinor ("8.4") on the download page. Pre-releases (3.6.0alpha1) are skipped,
// and only links to xdebug.org with a SHA-256 count.
func ParseXdebug(page, pageURL, phpMinor string) (XdebugBuild, error) {
	base, err := url.Parse(pageURL)
	if err != nil {
		return XdebugBuild{}, err
	}
	var best XdebugBuild
	for _, tag := range anchorRE.FindAllString(page, -1) {
		var href, title string
		for _, m := range attrRE.FindAllStringSubmatch(tag, -1) {
			v := html.UnescapeString(m[2][1 : len(m[2])-1])
			if strings.EqualFold(m[1], "href") {
				href = v
			} else {
				title = v
			}
		}
		u, err := base.Parse(href)
		if err != nil || href == "" {
			continue
		}
		name := u.Path[strings.LastIndex(u.Path, "/")+1:]
		m := xdebugDLL.FindStringSubmatch(name)
		if m == nil || m[2] != phpMinor || m[3] != "nts" {
			continue
		}
		sum := sha256Text.FindStringSubmatch(strings.ReplaceAll(title, " ", " "))
		if sum == nil || u.Scheme != "https" || (u.Hostname() != "xdebug.org" && !isLoopback(u.Hostname())) {
			continue
		}
		if best.Version == "" || newer(m[1], best.Version) {
			best = XdebugBuild{Version: m[1], PHP: m[2], URL: u.String(), SHA256: strings.ToLower(sum[1]), FileName: name}
		}
	}
	if best.Version == "" {
		return best, fmt.Errorf("xdebug.org lists no Windows build of Xdebug for PHP %s (non-thread-safe, 64-bit)", phpMinor)
	}
	return best, nil
}

func isLoopback(h string) bool { return h == "127.0.0.1" || h == "localhost" || h == "::1" }

// newer compares x.y.z versions.
func newer(a, b string) bool {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(pa) && i < len(pb); i++ {
		x, _ := strconv.Atoi(pa[i])
		y, _ := strconv.Atoi(pb[i])
		if x != y {
			return x > y
		}
	}
	return len(pa) > len(pb)
}

// PHPMinor returns "8.4" for a PHP version such as "8.4.13".
func PHPMinor(version string) (string, error) {
	p := strings.Split(strings.TrimSpace(version), ".")
	if len(p) < 2 {
		return "", errors.New("unknown PHP version " + version)
	}
	if _, err := strconv.Atoi(p[0]); err != nil {
		return "", errors.New("unknown PHP version " + version)
	}
	minor := p[1]
	for i, c := range minor {
		if c < '0' || c > '9' {
			minor = minor[:i]
			break
		}
	}
	if minor == "" {
		return "", errors.New("unknown PHP version " + version)
	}
	return p[0] + "." + minor, nil
}
