package modelref

import "testing"

func TestParse(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		want    Ref
		wantErr bool
	}{
		{name: "simple", in: "openai-main/gpt-4o", want: Ref{Provider: "openai-main", Model: "gpt-4o"}},
		{name: "vertex resource path", in: "google-vertex/publishers/google/models/gemini-2.5-flash", want: Ref{Provider: "google-vertex", Model: "publishers/google/models/gemini-2.5-flash"}},
		{name: "trims whitespace", in: "  openai/gpt-4o  ", want: Ref{Provider: "openai", Model: "gpt-4o"}},
		{name: "missing provider", in: "gpt-4o", wantErr: true},
		{name: "empty model", in: "openai/", wantErr: true},
		{name: "whitespace model", in: "openai/   ", wantErr: true},
		{name: "empty", in: "", wantErr: true},
		{name: "only slash", in: "/", wantErr: true},
		// #1466: a doubled separator yields a model segment starting with '/';
		// the reference is malformed even though the model segment is non-empty.
		{name: "double slash", in: "openai//gpt-4o", wantErr: true},
		{name: "double slash empty model", in: "openai//", wantErr: true},
		{name: "triple slash", in: "openai///x", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Parse(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("Parse(%q) = %+v, want error", tc.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("Parse(%q) unexpected error: %v", tc.in, err)
			}
			if got != tc.want {
				t.Fatalf("Parse(%q) = %+v, want %+v", tc.in, got, tc.want)
			}
		})
	}
}

func TestParseWithProvider(t *testing.T) {
	cases := []struct {
		name     string
		in       string
		provider string
		want     Ref
		wantErr  bool
	}{
		{name: "prefixed only", in: "openai/gpt-4o", want: Ref{Provider: "openai", Model: "gpt-4o"}},
		{name: "structured bare name", in: "gpt-4o", provider: "openai", want: Ref{Provider: "openai", Model: "gpt-4o"}},
		{name: "structured redundant provider", in: "openai/gpt-4o", provider: "openai", want: Ref{Provider: "openai", Model: "gpt-4o"}},
		{name: "vertex multi-slash model", in: "google-vertex/publishers/google/models/gemini-2.5-flash", want: Ref{Provider: "google-vertex", Model: "publishers/google/models/gemini-2.5-flash"}},
		{name: "no override", in: "", want: Ref{}},
		{name: "bare name without provider", in: "gpt-4o", wantErr: true},
		{name: "provider without model", in: "", provider: "openai", wantErr: true},
		{name: "conflicting providers", in: "openai/gpt-4o", provider: "google", wantErr: true},
		{name: "provider slug with slash", in: "gpt-4o", provider: "org/openai", wantErr: true},
		// A malformed prefixed form must not be silently rescued by prepending
		// the structured provider.
		{name: "double slash not rescued", in: "openai//gpt-4o", provider: "openai", wantErr: true},
		{name: "leading slash not rescued", in: "/gpt-4o", provider: "openai", wantErr: true},
		{name: "empty model segment not rescued", in: "openai/", provider: "openai", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseWithProvider(tc.in, tc.provider)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("ParseWithProvider(%q, %q) = %+v, want error", tc.in, tc.provider, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseWithProvider(%q, %q) unexpected error: %v", tc.in, tc.provider, err)
			}
			if got != tc.want {
				t.Fatalf("ParseWithProvider(%q, %q) = %+v, want %+v", tc.in, tc.provider, got, tc.want)
			}
		})
	}
}

func TestRefStringRoundTrip(t *testing.T) {
	cases := []string{
		"openai-main/gpt-4o",
		"google-vertex/publishers/google/models/gemini-2.5-flash",
	}
	for _, s := range cases {
		ref, err := Parse(s)
		if err != nil {
			t.Fatalf("Parse(%q): %v", s, err)
		}
		if got := ref.String(); got != s {
			t.Fatalf("round trip: String() = %q, want %q", got, s)
		}
	}
}
