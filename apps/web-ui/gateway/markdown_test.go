package main

import (
	"strings"
	"testing"
)

func TestRenderMarkdown(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string // substrings that must be present
		not  []string // substrings that must be absent
	}{
		{"bold", "**bold**", []string{"<strong>bold</strong>"}, nil},
		{"code", "`code`", []string{"<code>code</code>"}, nil},
		{"script", "<script>alert(1)</script>", nil, []string{"<script>"}},
		{"js-link", "[x](javascript:alert(1))", nil, []string{"javascript:"}},
		{"fence", "```go\nfmt.Println(\"hi\")\n```", []string{"<pre", "fmt.Println"}, nil},
		{"table", "|a|b|\n|-|-|\n|1|2|", []string{"<table"}, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := renderMarkdown(c.in)
			for _, w := range c.want {
				if !strings.Contains(got, w) {
					t.Errorf("renderMarkdown(%q) missing %q in %q", c.in, w, got)
				}
			}
			for _, n := range c.not {
				if strings.Contains(got, n) {
					t.Errorf("renderMarkdown(%q) unexpectedly contains %q in %q", c.in, n, got)
				}
			}
		})
	}
}

func TestRenderMarkdownDeterministic(t *testing.T) {
	a := renderMarkdown("**x**")
	b := renderMarkdown("**x**")
	if a != b {
		t.Errorf("renderMarkdown not deterministic: %q vs %q", a, b)
	}
}

func TestNeutralizeCitationLinks(t *testing.T) {
	cited := "11111111-1111-1111-1111-111111111111"
	other := "22222222-2222-2222-2222-222222222222"
	rel := "33333333-3333-3333-3333-333333333333"
	cites := []citation{
		{Kind: "object", ID: cited, Type: "Person", Label: "Acme"},
		{Kind: "relationship", ID: rel, Type: "works_at"},
	}
	cases := []struct {
		name    string
		in      string
		want    []string
		notWant []string
		exact   bool
	}{
		{
			name: "cited object link preserved",
			in:   `<p><a href="/objects/` + cited + `">Acme</a></p>`,
			want: []string{`href="/objects/` + cited + `"`, ">Acme<"},
		},
		{
			name:    "uncited object link demoted to label text",
			in:      `<p><a href="/objects/` + other + `">Ghost</a></p>`,
			want:    []string{"<p>Ghost</p>"},
			notWant: []string{"<a ", "/objects/"},
		},
		{
			name:    "uncited relationship fragment dropped, object link kept",
			in:      `<p><a href="/objects/` + cited + `#relationship-` + other + `">A —rel→ B</a></p>`,
			want:    []string{`href="/objects/` + cited + `"`, "A —rel→ B"},
			notWant: []string{"#relationship-"},
		},
		{
			name: "cited relationship link preserved even when source object uncited",
			in:   `<p><a href="/objects/` + other + `#relationship-` + rel + `">A —rel→ B</a></p>`,
			want: []string{`href="/objects/` + other + `#relationship-` + rel + `"`},
		},
		{
			name:    "uncited relationship and unknown object fully demoted",
			in:      `<p><a href="/objects/` + other + `#relationship-` + other + `">A —rel→ B</a></p>`,
			want:    []string{"<p>A —rel→ B</p>"},
			notWant: []string{"<a ", "/objects/"},
		},
		{
			name:  "non-object anchors untouched (byte-identical)",
			in:    `<p><a href="https://example.com">x</a> <strong>y</strong></p>`,
			exact: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := neutralizeCitationLinks(tc.in, cites)
			if tc.exact && got != tc.in {
				t.Errorf("unchanged input altered:\n got %q\nwant %q", got, tc.in)
			}
			for _, w := range tc.want {
				if !strings.Contains(got, w) {
					t.Errorf("missing %q in %q", w, got)
				}
			}
			for _, n := range tc.notWant {
				if strings.Contains(got, n) {
					t.Errorf("unexpected %q in %q", n, got)
				}
			}
		})
	}
}

// TestNeutralizeCitationLinksCodeFence proves the rule applies to real anchors
// only: markdown code that merely looks like an object link renders as text and
// is left untouched.
func TestNeutralizeCitationLinksCodeFence(t *testing.T) {
	other := "22222222-2222-2222-2222-222222222222"
	rendered := renderMarkdown("```\n[Ghost](/objects/" + other + ")\n```")
	if got := neutralizeCitationLinks(rendered, nil); got != rendered {
		t.Errorf("code fence altered:\n got %q\nwant %q", got, rendered)
	}
}

// TestParseObjectRef locks the ref grammar: UUID refs, key refs containing "/",
// an optional relationship fragment, and rejection of empty / query refs.
func TestParseObjectRef(t *testing.T) {
	cases := []struct {
		name      string
		href      string
		wantRef   string
		wantRel   string
		wantIsObj bool
	}{
		{"uuid", "/objects/11111111-1111-1111-1111-111111111111", "11111111-1111-1111-1111-111111111111", "", true},
		{"key with slash", "/objects/lov/2005-06-17-90", "lov/2005-06-17-90", "", true},
		{"uuid with relationship", "/objects/11111111-1111-1111-1111-111111111111#relationship-r1", "11111111-1111-1111-1111-111111111111", "r1", true},
		{"key with relationship", "/objects/lov/2005-06-17-90#relationship-r1", "lov/2005-06-17-90", "r1", true},
		{"empty ref", "/objects/", "", "", false},
		{"query rejected", "/objects/lov/x?q=1", "", "", false},
		{"other path", "/pages/1", "", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ref, rel, isObj := parseObjectRef(tc.href)
			if ref != tc.wantRef || rel != tc.wantRel || isObj != tc.wantIsObj {
				t.Errorf("parseObjectRef(%q) = (%q, %q, %v), want (%q, %q, %v)",
					tc.href, ref, rel, isObj, tc.wantRef, tc.wantRel, tc.wantIsObj)
			}
		})
	}
}

// TestNeutralizeCitationLinksKeyRefs covers the key-ref rule: a link to a cited
// object's key is kept and re-targeted to the canonical id, an unknown key is
// demoted, and a key link carrying a cited relationship fragment keeps the link
// (re-targeted) with the fragment intact.
func TestNeutralizeCitationLinksKeyRefs(t *testing.T) {
	const (
		canonical = "11111111-1111-1111-1111-111111111111"
		key       = "lov/2005-06-17-90"
		rel       = "33333333-3333-3333-3333-333333333333"
	)
	cites := []citation{
		{Kind: "object", ID: canonical, Type: "Law", Label: "arbeidsmiljøloven", URL: "/objects/" + canonical, Key: key},
		{Kind: "relationship", ID: rel, Type: "amends"},
	}
	cases := []struct {
		name    string
		in      string
		want    []string
		notWant []string
		exact   bool
	}{
		{
			name:    "cited key kept and re-targeted to canonical id",
			in:      `<p><a href="/objects/` + key + `">arbeidsmiljøloven</a></p>`,
			want:    []string{`href="/objects/` + canonical + `"`, ">arbeidsmiljøloven<"},
			notWant: []string{`href="/objects/` + key},
		},
		{
			name:    "unknown key demoted to label text",
			in:      `<p><a href="/objects/nope/does-not-exist">Nope</a></p>`,
			want:    []string{"<p>Nope</p>"},
			notWant: []string{"<a ", "/objects/"},
		},
		{
			name:  "uuid id ref unchanged (byte-identical)",
			in:    `<p><a href="/objects/` + canonical + `">x</a></p>`,
			exact: true,
		},
		{
			name: "key link with cited relationship fragment kept and re-targeted",
			in:   `<p><a href="/objects/` + key + `#relationship-` + rel + `">A —amends→ B</a></p>`,
			want: []string{`href="/objects/` + canonical + `#relationship-` + rel + `"`},
		},
		{
			name:    "key link with unknown relationship fragment dropped, object re-targeted",
			in:      `<p><a href="/objects/` + key + `#relationship-nope">A —amends→ B</a></p>`,
			want:    []string{`href="/objects/` + canonical + `"`, "A —amends→ B"},
			notWant: []string{"#relationship-"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := neutralizeCitationLinks(tc.in, cites)
			if tc.exact && got != tc.in {
				t.Errorf("unchanged input altered:\n got %q\nwant %q", got, tc.in)
			}
			for _, w := range tc.want {
				if !strings.Contains(got, w) {
					t.Errorf("missing %q in %q", w, got)
				}
			}
			for _, n := range tc.notWant {
				if strings.Contains(got, n) {
					t.Errorf("unexpected %q in %q", n, got)
				}
			}
		})
	}
}

// TestNeutralizeCitationLinksKeyRefRenderedMarkdown runs a key ref through the
// real markdown renderer, proving goldmark/bluemonday leave the slash-bearing
// key href intact so the rewrite can recognize it.
func TestNeutralizeCitationLinksKeyRefRenderedMarkdown(t *testing.T) {
	const (
		canonical = "11111111-1111-1111-1111-111111111111"
		key       = "lov/2005-06-17-90"
	)
	cites := []citation{{Kind: "object", ID: canonical, Type: "Law", Label: "L", Key: key}}
	rendered := renderMarkdown("[L](/objects/" + key + ")")
	got := neutralizeCitationLinks(rendered, cites)
	if !strings.Contains(got, `href="/objects/`+canonical+`"`) {
		t.Errorf("key ref not re-targeted in rendered markdown: %q", got)
	}
}

// TestNeutralizeCitationLinksAdversarial proves the citation render boundary is
// injection-safe with model-generated labels/ids: a raw HTML label, a quote
// breakout in a cited label, a quote/tag breakout in an uncited href, an
// uncited raw anchor, and a javascript: scheme all render inert — no element,
// script, or script-bearing URL can survive. The input always goes through
// renderMarkdown first, exactly as the live snapshot and history render do.
func TestNeutralizeCitationLinksAdversarial(t *testing.T) {
	const (
		cited = "11111111-1111-1111-1111-111111111111"
		rel   = "33333333-3333-3333-3333-333333333333"
	)
	cites := []citation{
		{Kind: "object", ID: cited, Type: "Person", Label: "Acme"},
		{Kind: "relationship", ID: rel, Type: "works_at"},
	}
	cases := []struct{ name, in, notWant string }{
		{"raw html in label", `[<img src=x onerror=alert(1)>](/objects/22222222-2222-2222-2222-222222222222)`, "/objects/2222"},
		{"quote breakout in cited label", `["><img src=x onerror=alert(1)>](/objects/` + cited + `)`, "<img"},
		{"quote breakout in uncited href", `[x](/objects/"><img src=x onerror=alert(1)>)`, "<img"},
		{"uncited raw anchor stripped", `<a href="/objects/22222222-2222-2222-2222-222222222222">z</a>`, "/objects/2222"},
		{"javascript scheme stripped", `[javascript](javascript:alert(1))`, "javascript:"},
		{"encoded breakout with cited relationship", `[x](/objects/"><img/src=x/onerror=alert(1)#relationship-` + rel + `)`, "<img"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := neutralizeCitationLinks(renderMarkdown(tc.in), cites)
			for _, bad := range []string{"<img", "<script", "javascript:", " onerror="} {
				if strings.Contains(got, bad) {
					t.Errorf("injection survived (%q): %s", bad, got)
				}
			}
			if tc.notWant != "" && strings.Contains(got, tc.notWant) {
				t.Errorf("unexpected %q in %s", tc.notWant, got)
			}
		})
	}
}

func TestSplitLeadingReasoning(t *testing.T) {
	cases := []struct {
		name          string
		in            string
		wantReasoning string
		wantAnswer    string
	}{
		{"single line", "just an answer", "", "just an answer"},
		{"empty", "", "", ""},
		{"cot plus answer", "reasoning here\nactual answer", "reasoning here", "actual answer"},
		{"cot blank markdown", "thinking\n\nthe **answer**", "thinking", "the **answer**"},
		{"trailing newline only", "no answer\n", "", "no answer\n"},
		{"leading newline", "\nanswer", "", "\nanswer"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			reasoning, answer := splitLeadingReasoning(c.in)
			if reasoning != c.wantReasoning || answer != c.wantAnswer {
				t.Errorf("splitLeadingReasoning(%q) = (%q, %q), want (%q, %q)",
					c.in, reasoning, answer, c.wantReasoning, c.wantAnswer)
			}
		})
	}
}
