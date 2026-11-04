package webcrawl

import (
	"fmt"
	"net/url"
	"strings"
)

// trackingParams are query parameters that carry attribution/analytics state
// rather than content identity, so they are dropped to make two URLs that point
// at the same document collapse to one canonical key.
var trackingParams = map[string]bool{
	"fbclid": true,
	"gclid":  true,
	"gclsrc": true,
	"mc_cid": true,
	"mc_eid": true,
	"ref":    true,
	"source": true,
	"_hsenc": true,
}

// Canonical normalizes a URL (possibly a relative reference resolved against
// base) into a stable identity key:
//
//   - resolves relative references against base (when non-nil);
//   - drops the fragment and userinfo;
//   - lowercases the scheme and host;
//   - strips the default port;
//   - collapses an index.html / index.htm filename to its directory form;
//   - drops a single trailing slash (except at the root);
//   - drops tracking parameters (utm_* and a fixed denylist) and sorts the rest.
//
// Non-http(s) schemes are rejected.
func Canonical(raw string, base *url.URL) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("parse %q: %w", raw, err)
	}
	if base != nil {
		u = base.ResolveReference(u)
	}

	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("unsupported scheme %q (only http/https)", u.Scheme)
	}

	u.Scheme = strings.ToLower(u.Scheme)
	u.User = nil
	u.Fragment = ""

	// Normalize the empty path to the root so "http://a.com" and "http://a.com/"
	// collapse to one key.
	if u.Path == "" {
		u.Path = "/"
	}

	// Strip the default port for the scheme, preserving IPv6 brackets.
	host := strings.ToLower(u.Hostname())
	port := u.Port()
	if (u.Scheme == "http" && port == "80") || (u.Scheme == "https" && port == "443") {
		if strings.Contains(host, ":") {
			u.Host = "[" + host + "]"
		} else {
			u.Host = host
		}
	} else {
		u.Host = strings.ToLower(u.Host)
	}

	// Collapse an explicit index file to the directory form.
	for _, idx := range []string{"index.html", "index.htm"} {
		if strings.HasSuffix(u.Path, idx) {
			u.Path = u.Path[:len(u.Path)-len(idx)]
			break
		}
	}
	// Drop a single trailing slash, except the root.
	if u.Path != "/" {
		u.Path = strings.TrimSuffix(u.Path, "/")
	}

	q := u.Query()
	for k := range q {
		if k == "" {
			q.Del(k)
			continue
		}
		if trackingParams[k] || strings.HasPrefix(k, "utm_") {
			q.Del(k)
		}
	}
	u.RawQuery = q.Encode() // url.Values.Encode sorts keys

	return u.String(), nil
}
