package webcrawl

import (
	"testing"
	"time"
)

func TestParseRobotsGroups(t *testing.T) {
	body := []byte(`
User-agent: *
Disallow: /private/

User-agent: googlebot
Disallow: /secret/
Allow: /secret/public/
Crawl-delay: 3.5

Sitemap: https://example.com/sitemap.xml
`)
	txt := parseRobots(body)
	if len(txt.groups) != 2 {
		t.Fatalf("groups = %d, want 2", len(txt.groups))
	}
	if got := txt.sitemaps; len(got) != 1 || got[0] != "https://example.com/sitemap.xml" {
		t.Errorf("sitemaps = %v", got)
	}

	// The generic UA is governed by the * group.
	if !txt.allowed("somebot", "/public/x") {
		t.Error("somebot must be allowed /public/x")
	}
	if txt.allowed("somebot", "/private/x") {
		t.Error("somebot must be disallowed /private/x")
	}

	// googlebot matches the longer token, so its group governs.
	if !txt.allowed("googlebot", "/private/x") {
		t.Error("googlebot must be allowed /private/x (googlebot group has no such rule)")
	}
	if !txt.allowed("googlebot", "/secret/public/x") {
		t.Error("googlebot must be allowed /secret/public/x via Allow")
	}
	if txt.allowed("googlebot", "/secret/x") {
		t.Error("googlebot must be disallowed /secret/x")
	}
	if got := txt.crawlDelay("googlebot"); got != 3500*time.Millisecond {
		t.Errorf("crawlDelay = %v, want 3.5s", got)
	}
}

func TestRobotsLongestMatchAndAllowTie(t *testing.T) {
	body := []byte(`
User-agent: *
Allow: /a/b
Disallow: /a
`)
	txt := parseRobots(body)
	// /a/b matches both "Allow: /a/b" (len 4) and "Disallow: /a" (len 2):
	// the longest wins, so it is allowed.
	if !txt.allowed("bot", "/a/b") {
		t.Error("/a/b must be allowed (longest match)")
	}
	// /a/c matches only "Disallow: /a".
	if txt.allowed("bot", "/a/c") {
		t.Error("/a/c must be disallowed")
	}
}

func TestRobotsAllowWinsTie(t *testing.T) {
	body := []byte(`
User-agent: *
Allow: /x
Disallow: /x
`)
	txt := parseRobots(body)
	if !txt.allowed("bot", "/x") {
		t.Error("equal-length Allow must win over Disallow")
	}
}

func TestRobotsPatternMatching(t *testing.T) {
	txt := parseRobots([]byte(`
User-agent: *
Disallow: /tmp/
Disallow: /*.pdf$
Allow: /public/
`))
	cases := map[string]bool{
		"/tmp/x":     false,
		"/tmp":       true,  // no trailing slash: /tmp/ does not match /tmp
		"/a/b.pdf":   false, // $ anchor, full match
		"/a/b.pdf/x": true,  // not a full .pdf match
		"/public/x":  true,
	}
	for path, want := range cases {
		if got := txt.allowed("bot", path); got != want {
			t.Errorf("allowed(%q) = %v, want %v", path, got, want)
		}
	}
}

func TestRobotsDefaultAllowWithNoMatch(t *testing.T) {
	txt := parseRobots([]byte(`User-agent: *
Disallow: /private/
`))
	if !txt.allowed("bot", "/public/x") {
		t.Error("unmatched path must default to allow")
	}
	if txt.allowed("bot", "/private/x") {
		t.Error("/private/x must be disallowed")
	}
}
