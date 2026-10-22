package webcrawl

import (
	"net/url"
	"strings"
)

// SameOrigin returns a predicate reporting whether u shares scheme and host
// with base. Comparison is case-insensitive and normalizes the default port
// (so "http://x:80" and "http://x" match), because callers may pass URLs that
// have not been canonicalized.
func SameOrigin(base *url.URL) func(*url.URL) bool {
	baseScheme := strings.ToLower(base.Scheme)
	baseHost := normalizedHostPort(base)
	return func(u *url.URL) bool {
		if u == nil {
			return false
		}
		return strings.ToLower(u.Scheme) == baseScheme && normalizedHostPort(u) == baseHost
	}
}

// normalizedHostPort returns "host:port" with the default port filled in and
// IPv6 hosts bracketed, so equivalent origins compare equal.
func normalizedHostPort(u *url.URL) string {
	host := strings.ToLower(u.Hostname())
	port := u.Port()
	if port == "" {
		switch strings.ToLower(u.Scheme) {
		case "http":
			port = "80"
		case "https":
			port = "443"
		}
	}
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	return host + ":" + port
}

// Glob matches a path against a pattern:
//
//   - "*" matches any sequence of characters within a single path segment
//     (it does not cross "/");
//   - "**" matches zero or more whole segments (crossing "/").
//
// Both pattern and path are split on "/", so "/docs/*" matches "/docs/intro"
// but not "/docs/a/b", while "/docs/**" matches both.
func Glob(pattern, path string) bool {
	// modelref:allow — splits URL path segments for glob matching, not a routed
	// model name; no provider identity is being reconstructed.
	return matchSegments(strings.Split(pattern, "/"), strings.Split(path, "/"))
}

func matchSegments(pat, segs []string) bool {
	for len(pat) > 0 {
		p := pat[0]
		if p == "**" {
			// Zero or more segments.
			for i := 0; i <= len(segs); i++ {
				if matchSegments(pat[1:], segs[i:]) {
					return true
				}
			}
			return false
		}
		if len(segs) == 0 || !wildcardMatch(p, segs[0]) {
			return false
		}
		pat, segs = pat[1:], segs[1:]
	}
	return len(segs) == 0
}

// InScope reports whether u should be followed given o. Precedence: an
// ExcludePattern match rejects outright (exclude wins), then an IncludePattern
// set must have a match, then SameOrigin (against any seed origin) applies.
func InScope(u *url.URL, o Options) bool {
	if u == nil {
		return false
	}
	path := u.Path
	if path == "" {
		path = "/"
	}

	for _, pat := range o.ExcludePatterns {
		if Glob(pat, path) {
			return false
		}
	}
	if len(o.IncludePatterns) > 0 {
		matched := false
		for _, pat := range o.IncludePatterns {
			if Glob(pat, path) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	if o.SameOrigin {
		return sameOriginAsSeeds(u, o.Seeds)
	}
	return true
}

func sameOriginAsSeeds(u *url.URL, seeds []string) bool {
	for _, s := range seeds {
		base, err := url.Parse(s)
		if err != nil {
			continue
		}
		if SameOrigin(base)(u) {
			return true
		}
	}
	return false
}

// sameOriginPredicate returns a predicate reporting whether u shares scheme and
// host with any seed. Seeds are parsed once up front so per-candidate calls do
// no re-parsing; the normalization (case, default port, IPv6 bracketing) lives
// in SameOrigin and is not reimplemented here.
func sameOriginPredicate(seeds []string) func(*url.URL) bool {
	bases := make([]*url.URL, 0, len(seeds))
	for _, s := range seeds {
		if b, err := url.Parse(s); err == nil {
			bases = append(bases, b)
		}
	}
	return func(u *url.URL) bool {
		for _, b := range bases {
			if SameOrigin(b)(u) {
				return true
			}
		}
		return false
	}
}
