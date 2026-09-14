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
