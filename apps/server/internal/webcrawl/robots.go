package webcrawl

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// robotsTxt is a parsed robots.txt, shaped after RFC 9309. Groups hold the
// allow/disallow rules scoped to one or more user-agent tokens; sitemaps are
// collected document-wide.
type robotsTxt struct {
	groups   []robotsGroup
	sitemaps []string
}

type robotsGroup struct {
	userAgents []string
	rules      []robotsRule
	crawlDelay time.Duration
}

type robotsRule struct {
	allow   bool
	pattern string
}

// parseRobots parses a robots.txt body. It is RFC 9309-shaped rather than a
// full validator: comments (#) and blank lines are ignored, User-agent starts a
// group, Allow/Disallow/Crawl-delay accumulate into the current group, and
// Sitemap lines are collected document-wide. Malformed lines are skipped.
func parseRobots(body []byte) *robotsTxt {
	txt := &robotsTxt{}
	sc := bufio.NewScanner(strings.NewReader(string(body)))
	var cur *robotsGroup

	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		if i := strings.IndexByte(line, '#'); i >= 0 {
			line = line[:i]
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		key, val, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key = strings.ToLower(strings.TrimSpace(key))
		val = strings.TrimSpace(val)

		switch key {
		case "user-agent":
			txt.groups = append(txt.groups, robotsGroup{})
			cur = &txt.groups[len(txt.groups)-1]
			cur.userAgents = append(cur.userAgents, strings.ToLower(val))
		case "allow", "disallow":
			if cur == nil {
				cur = &robotsGroup{userAgents: []string{"*"}}
				txt.groups = append(txt.groups, *cur)
				cur = &txt.groups[len(txt.groups)-1]
			}
			if val == "" {
				// An empty Allow/Disallow value matches nothing.
				continue
			}
			cur.rules = append(cur.rules, robotsRule{allow: key == "allow", pattern: val})
		case "crawl-delay":
			if cur != nil {
				if secs, err := strconv.ParseFloat(val, 64); err == nil && secs >= 0 {
					cur.crawlDelay = time.Duration(secs * float64(time.Second))
				}
			}
		case "sitemap":
			txt.sitemaps = append(txt.sitemaps, val)
		}
	}
	return txt
}

// groupFor returns the group that governs ua: the group whose user-agent token
// is the longest case-insensitive substring of ua wins; a "*" group is the
// fallback when no specific token matches.
func (t *robotsTxt) groupFor(ua string) *robotsGroup {
	ua = strings.ToLower(ua)
	var best *robotsGroup
	bestLen := -1
	var fallback *robotsGroup

	for i := range t.groups {
		g := &t.groups[i]
		for _, tok := range g.userAgents {
			tok = strings.TrimSpace(tok)
			if tok == "*" {
				if fallback == nil {
					fallback = g
				}
				continue
			}
			if tok != "" && strings.Contains(ua, tok) {
				if len(tok) > bestLen {
					best = g
					bestLen = len(tok)
				}
			}
		}
	}
	if best != nil {
		return best
	}
	return fallback
}

// allowed reports whether path is allowed for ua. Among rules that match, the
// longest pattern wins; on a tie an Allow wins over a Disallow. With no
// matching rule the default is allow.
func (t *robotsTxt) allowed(ua, path string) bool {
	g := t.groupFor(ua)
	if g == nil {
		return true
	}
	bestLen := -1
	allowed := true
	for _, rule := range g.rules {
		if !robotsMatch(rule.pattern, path) {
			continue
		}
		if len(rule.pattern) > bestLen {
			bestLen = len(rule.pattern)
			allowed = rule.allow
		} else if len(rule.pattern) == bestLen && rule.allow {
			allowed = true
		}
	}
	return allowed
}

// crawlDelay returns the crawl delay for ua, or 0.
func (t *robotsTxt) crawlDelay(ua string) time.Duration {
	if g := t.groupFor(ua); g != nil {
		return g.crawlDelay
	}
	return 0
}

// robotsMatch implements simplified RFC 9309 pattern matching: "*" matches any
// sequence (including empty and "/"), a trailing "$" anchors the end, and a
// pattern without "$" matches any path that begins with the pattern.
func robotsMatch(pattern, path string) bool {
	if strings.HasSuffix(pattern, "$") {
		pattern = strings.TrimSuffix(pattern, "$")
		return wildcardMatch(pattern, path)
	}
	return wildcardMatch(pattern+"*", path)
}

// wildcardMatch matches pattern against the entire string s, where "*" matches
// any sequence including empty.
func wildcardMatch(p, s string) bool {
	for len(p) > 0 {
		if p[0] == '*' {
			for len(p) > 0 && p[0] == '*' {
				p = p[1:]
			}
			if len(p) == 0 {
				return true
			}
			for i := 0; i <= len(s); i++ {
				if wildcardMatch(p, s[i:]) {
					return true
				}
			}
			return false
		}
		if len(s) == 0 || p[0] != s[0] {
			return false
		}
		p = p[1:]
		s = s[1:]
	}
	return len(s) == 0
}

// robotsChecker caches parsed robots.txt per origin for the duration of a crawl
// and applies the policy for each fetch.
type robotsChecker struct {
	cache map[string]*robotsResult
}

type robotsResult struct {
	txt     *robotsTxt
	warning error // non-nil on a fail-open (5xx/network), see allowed()
}

func newRobotsChecker() *robotsChecker {
	return &robotsChecker{cache: map[string]*robotsResult{}}
}

// check returns whether fetching path from origin is permitted for o.UserAgent.
//
// Fetch policy (a documented deviation from RFC 9309's fail-closed): a 404/410
// allows everything (no robots.txt), and a 5xx or network error fails OPEN — the
// fetch is allowed and a non-nil warning is returned so the caller can record it
// — rather than blocking an entire site because its robots.txt is momentarily
// unavailable. robots.txt is fetched once per origin per crawl.
func (rc *robotsChecker) check(ctx context.Context, f Fetcher, o Options, origin, path string) (bool, error) {
	res, ok := rc.cache[origin]
	if !ok {
		res = &robotsResult{txt: &robotsTxt{}}
		robotsURL := origin + "/robots.txt"
		resp, err := f.Get(ctx, robotsURL)
		switch {
		case err != nil:
			res.warning = fmt.Errorf("robots.txt fetch failed (fail-open): %w", err)
		case resp.Status == http.StatusNotFound || resp.Status == http.StatusGone:
			// No robots.txt: allow everything.
		case resp.Status >= 500:
			res.warning = fmt.Errorf("robots.txt returned HTTP %d (fail-open)", resp.Status)
		default:
			res.txt = parseRobots(resp.Body)
		}
		rc.cache[origin] = res
	}
	if res.warning != nil {
		return true, res.warning
	}
	return res.txt.allowed(o.UserAgent, path), nil
}

// sitemaps returns the sitemap URLs declared in the cached robots.txt for origin
// (empty when there are none or the fetch has not happened yet).
func (rc *robotsChecker) sitemaps(origin string) []string {
	if res, ok := rc.cache[origin]; ok && res != nil && res.txt != nil {
		return res.txt.sitemaps
	}
	return nil
}

// originOf returns the "scheme://host" origin for a parsed URL.
func originOf(u *url.URL) string {
	return u.Scheme + "://" + u.Host
}
