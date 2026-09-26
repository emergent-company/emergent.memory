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

func TestRefIsZero(t *testing.T) {
	if !(Ref{}).IsZero() {
		t.Fatal("zero Ref should be IsZero")
	}
	if !(Ref{Provider: "openai"}).IsZero() {
		t.Fatal("missing model should be IsZero")
	}
	if (Ref{Provider: "openai", Model: "gpt-4o"}).IsZero() {
		t.Fatal("complete Ref should not be IsZero")
	}
}
