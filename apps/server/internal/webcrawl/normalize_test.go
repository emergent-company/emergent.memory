package webcrawl

import (
	"net/url"
	"testing"
)

func TestCanonical(t *testing.T) {
	base, _ := url.Parse("http://example.com/dir/page.html")
	tests := []struct {
		name string
		raw  string
		base *url.URL
		want string
	}{
		{"lowercases scheme and host", "HTTP://Example.COM/Path", nil, "http://example.com/Path"},
		{"drops fragment", "http://example.com/x#section", nil, "http://example.com/x"},
		{"drops userinfo", "http://user:pass@example.com/x", nil, "http://example.com/x"},
		{"strips default http port", "http://example.com:80/x", nil, "http://example.com/x"},
		{"strips default https port", "https://example.com:443/x", nil, "https://example.com/x"},
		{"keeps non-default port", "http://example.com:8080/x", nil, "http://example.com:8080/x"},
		{"collapses index.html", "http://example.com/foo/index.html", nil, "http://example.com/foo"},
		{"collapses index.htm to root", "http://example.com/index.htm", nil, "http://example.com/"},
		{"drops trailing slash", "http://example.com/foo/", nil, "http://example.com/foo"},
		{"keeps root slash", "http://example.com/", nil, "http://example.com/"},
		{"empty path becomes root", "http://example.com", nil, "http://example.com/"},
		{"drops utm and sorts query", "http://example.com/x?utm_source=a&b=2&a=1&utm_medium=c", nil, "http://example.com/x?a=1&b=2"},
		{"drops fbclid gclid", "http://example.com/x?fbclid=1&gclid=2&keep=3", nil, "http://example.com/x?keep=3"},
		{"resolves relative ref", "../up", base, "http://example.com/up"},
		{"resolves absolute ignoring base", "http://other.com/x", base, "http://other.com/x"},
		{"drops all tracking leaves empty query", "http://example.com/x?utm_a=1&ref=2&source=3", nil, "http://example.com/x"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Canonical(tt.raw, tt.base)
			if err != nil {
				t.Fatalf("Canonical(%q) = error %v", tt.raw, err)
			}
			if got != tt.want {
				t.Errorf("Canonical(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

func TestCanonicalRejectsBadSchemes(t *testing.T) {
	for _, raw := range []string{
		"ftp://example.com/x",
		"file:///etc/passwd",
		"javascript:alert(1)",
		"mailto:foo@example.com",
		"//example.com/x", // scheme-less relative with no base
	} {
		if _, err := Canonical(raw, nil); err == nil {
			t.Errorf("Canonical(%q) = nil error, want rejection", raw)
		}
	}
}

func TestCanonicalDedupesEquivalentURLs(t *testing.T) {
	equiv := []string{
		"http://Example.com:80",
		"http://example.com/",
		"http://example.com/index.html",
		"http://example.com/?utm_source=x#frag",
	}
	first, err := Canonical(equiv[0], nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range equiv[1:] {
		got, err := Canonical(raw, nil)
		if err != nil {
			t.Fatalf("Canonical(%q): %v", raw, err)
		}
		if got != first {
			t.Errorf("Canonical(%q) = %q, want %q (all equivalents must collapse)", raw, got, first)
		}
	}
}
