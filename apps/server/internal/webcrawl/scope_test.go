package webcrawl

import (
	"net/url"
	"testing"
)

func TestGlob(t *testing.T) {
	tests := []struct {
		pattern string
		path    string
		want    bool
	}{
		{"*", "docs", true},
		{"*", "docs/intro", false}, // single segment only
		{"/docs/*", "/docs/intro", true},
		{"/docs/*", "/docs/a/b", false},
		{"/docs/**", "/docs/a/b", true},
		{"/docs/**", "/docs", true}, // ** matches zero segments
		{"/docs/**", "/docs/", true},
		{"/**", "/anything/deep/path", true},
		{"/a/b", "/a/b", true},
		{"/a/b", "/a/c", false},
		{"*.html", "page.html", true},
		{"*.html", "page.txt", false},
		{"/static/*", "/static/style.css", true},
		{"/static/*", "/static/css/style.css", false},
	}
	for _, tt := range tests {
		t.Run(tt.pattern+"|"+tt.path, func(t *testing.T) {
			if got := Glob(tt.pattern, tt.path); got != tt.want {
				t.Errorf("Glob(%q, %q) = %v, want %v", tt.pattern, tt.path, got, tt.want)
			}
		})
	}
}

func TestSameOrigin(t *testing.T) {
	base, _ := url.Parse("http://example.com/")
	pred := SameOrigin(base)

	cases := map[string]bool{
		"http://example.com/x":     true,
		"http://example.com:80/x":  true,
		"http://EXAMPLE.com/x":     true,
		"https://example.com/x":    false,
		"http://sub.example.com/x": false,
		"http://example.org/x":     false,
	}
	for raw, want := range cases {
		u, err := url.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		if got := pred(u); got != want {
			t.Errorf("SameOrigin(%q) = %v, want %v", raw, got, want)
		}
	}
}

func TestInScope(t *testing.T) {
	seed := "http://example.com/"
	mustURL := func(s string) *url.URL {
		u, err := url.Parse(s)
		if err != nil {
			t.Fatal(err)
		}
		return u
	}

	tests := []struct {
		name string
		o    Options
		u    *url.URL
		want bool
	}{
		{"no restrictions allows anything", Options{}, mustURL("http://any.com/x"), true},
		{"same origin allows", Options{SameOrigin: true, Seeds: []string{seed}}, mustURL("http://example.com/x"), true},
		{"same origin rejects other host", Options{SameOrigin: true, Seeds: []string{seed}}, mustURL("http://other.com/x"), false},
		{"include match", Options{IncludePatterns: []string{"/docs/**"}}, mustURL("http://x/docs/a"), true},
		{"include miss", Options{IncludePatterns: []string{"/docs/**"}}, mustURL("http://x/other"), false},
		{"exclude wins over include", Options{IncludePatterns: []string{"/docs/**"}, ExcludePatterns: []string{"/docs/private/**"}}, mustURL("http://x/docs/private/a"), false},
		{"exclude wins over same origin", Options{SameOrigin: true, Seeds: []string{seed}, ExcludePatterns: []string{"/private/**"}}, mustURL("http://example.com/private/a"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := InScope(tt.u, tt.o); got != tt.want {
				t.Errorf("InScope(%s) = %v, want %v", tt.u, got, tt.want)
			}
		})
	}
}
