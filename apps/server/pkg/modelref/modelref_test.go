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

type fakeResolver struct {
	instances map[string]bool
	dialects  map[string]bool
	defaults  map[string]string
	infer     map[string]string
}

func (f fakeResolver) IsInstance(slug string) bool { return f.instances[slug] }
func (f fakeResolver) IsDialect(d string) bool     { return f.dialects[d] }
func (f fakeResolver) DefaultInstanceForDialect(d string) (string, bool) {
	s, ok := f.defaults[d]
	return s, ok
}
func (f fakeResolver) InferDialectForModel(m string) (string, bool) {
	d, ok := f.infer[m]
	return d, ok
}

func TestNormalizeLegacy(t *testing.T) {
	r := fakeResolver{
		instances: map[string]bool{"openai-main": true, "openai": true},
		dialects:  map[string]bool{"openai": true, "google": true, "google-vertex": true},
		defaults:  map[string]string{"openai": "openai", "google-vertex": "google-vertex"},
		infer: map[string]string{
			"publishers/google/models/gemini-2.5-flash": "google-vertex",
			"gpt-4o": "openai",
		},
	}

	cases := []struct {
		name string
		in   string
		want Ref
		ok   bool
	}{
		{name: "instance slug prefix", in: "openai-main/gpt-4o", want: Ref{Provider: "openai-main", Model: "gpt-4o"}, ok: true},
		{name: "dialect prefix -> default instance", in: "openai/gpt-4o", want: Ref{Provider: "openai", Model: "gpt-4o"}, ok: true},
		{name: "bare model inferred", in: "gpt-4o", want: Ref{Provider: "openai", Model: "gpt-4o"}, ok: true},
		{name: "unqualified multi-segment preserved", in: "publishers/google/models/gemini-2.5-flash", want: Ref{Provider: "google-vertex", Model: "publishers/google/models/gemini-2.5-flash"}, ok: true},
		{name: "unknown and uninferable", in: "mystery/model", want: Ref{}, ok: false},
		{name: "empty", in: "", want: Ref{}, ok: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := NormalizeLegacy(tc.in, r)
			if ok != tc.ok {
				t.Fatalf("NormalizeLegacy(%q) ok = %v, want %v (got %+v)", tc.in, ok, tc.ok, got)
			}
			if ok && got != tc.want {
				t.Fatalf("NormalizeLegacy(%q) = %+v, want %+v", tc.in, got, tc.want)
			}
		})
	}
}
