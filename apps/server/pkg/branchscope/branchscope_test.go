package branchscope

import (
	"context"
	"strings"
	"testing"
)

func mustScope(t *testing.T, cfg Config) *Scope {
	t.Helper()
	s, err := New(cfg)
	if err != nil {
		t.Fatalf("New(%+v): %v", cfg, err)
	}
	return s
}

func TestNewDefaultsToMainOnly(t *testing.T) {
	for _, cfg := range []Config{{}, {Unmerged: false, Origins: []string{"extraction"}}} {
		s, err := New(cfg)
		if err != nil {
			t.Fatalf("New(%+v): %v", cfg, err)
		}
		if s != nil {
			t.Fatalf("New(%+v) = %+v, want nil (main only)", cfg, s)
		}
		if !s.IsMainOnly() {
			t.Fatalf("nil scope must be main only")
		}
	}
}

func TestNewValidation(t *testing.T) {
	tests := []struct {
		name string
		cfg  Config
		want string
	}{
		{"unknown origin", Config{Unmerged: true, Origins: []string{"docs"}}, "branchScope.origins"},
		{"blank glob", Config{Unmerged: true, Branches: []string{"  "}}, "branchScope.branches"},
		{"max below one", Config{Unmerged: true, MaxBranches: -1}, "branchScope.maxBranches"},
		{"max above ceiling", Config{Unmerged: true, MaxBranches: MaxMaxBranches + 1}, "branchScope.maxBranches"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := New(tt.cfg); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("New(%+v) error = %v, want one mentioning %q", tt.cfg, err, tt.want)
			}
		})
	}
}

func TestNewNormalizes(t *testing.T) {
	s := mustScope(t, Config{
		Unmerged: true,
		Origins:  []string{" Extraction ", "extraction", "AGENT"},
		Branches: []string{"plan/*"},
	})
	if got, want := s.Origins(), []string{"agent", "extraction"}; !equalStrings(got, want) {
		t.Fatalf("Origins() = %v, want %v", got, want)
	}
	if got := s.MaxBranches(); got != DefaultMaxBranches {
		t.Fatalf("MaxBranches() = %d, want default %d", got, DefaultMaxBranches)
	}
}

func TestAllowsOrigin(t *testing.T) {
	restricted := mustScope(t, Config{Unmerged: true, Origins: []string{"extraction"}})
	if !restricted.AllowsOrigin("extraction") {
		t.Fatal("extraction branch must be allowed")
	}
	if !restricted.AllowsOrigin("EXTRACTION") {
		t.Fatal("origin match must be case-insensitive")
	}
	if restricted.AllowsOrigin("manual") {
		t.Fatal("manual branch must not be allowed")
	}
	if restricted.AllowsOrigin("") {
		t.Fatal("empty origin must normalise to manual and be excluded")
	}

	unrestricted := mustScope(t, Config{Unmerged: true})
	for _, o := range []string{"extraction", "agent", "manual", "system", ""} {
		if !unrestricted.AllowsOrigin(o) {
			t.Fatalf("unrestricted scope must allow origin %q", o)
		}
	}
	var nilScope *Scope
	if nilScope.AllowsOrigin("extraction") || nilScope.AllowsName("any") {
		t.Fatal("nil scope must allow nothing")
	}
}

func TestAllowsNameGlobs(t *testing.T) {
	tests := []struct {
		pattern string
		name    string
		want    bool
	}{
		{"*", "anything/at/all", true},
		{"plan/*", "plan/q3", true},
		{"plan/*", "plan/q3/nested", true}, // `*` crosses `/`
		{"plan/*", "plans/q3", false},
		{"extraction/*", "extraction/1413e4cf/20fa1f14", true},
		{"extraction/*", "agent/1413e4cf", false},
		{"*/invoice*", "extraction/abc/invoice-2024.pdf", true},
		{"*-draft", "plan/q3-draft", true},
		{"*-draft", "plan/q3", false},
		{"plan/q3", "plan/q3", true},
		{"plan/q3", "plan/q4", false},
	}
	for _, tt := range tests {
		if got := GlobMatch(tt.pattern, tt.name); got != tt.want {
			t.Fatalf("GlobMatch(%q, %q) = %v, want %v", tt.pattern, tt.name, got, tt.want)
		}
	}

	s := mustScope(t, Config{Unmerged: true, Branches: []string{"plan/*", "extraction/*"}})
	if !s.AllowsName("extraction/a/b") || !s.AllowsName("plan/q3") {
		t.Fatal("both patterns must be honoured")
	}
	if s.AllowsName("agent/x") {
		t.Fatal("unmatched name must be excluded")
	}
}

func TestNarrow(t *testing.T) {
	extractionOnly := mustScope(t, Config{Unmerged: true, Origins: []string{"extraction"}, MaxBranches: 5})
	extractionAndAgent := mustScope(t, Config{Unmerged: true, Origins: []string{"extraction", "agent"}, MaxBranches: 2})

	got := Narrow(extractionOnly, extractionAndAgent)
	if got.IsMainOnly() {
		t.Fatal("narrowed scope must still read branches")
	}
	if want := []string{"extraction"}; !equalStrings(got.Origins(), want) {
		t.Fatalf("narrowed origins = %v, want %v", got.Origins(), want)
	}
	if got.MaxBranches() != 2 {
		t.Fatalf("narrowed max = %d, want the smaller 2", got.MaxBranches())
	}

	// A parent that is main only must not gain branch reads by delegating.
	if Narrow(nil, extractionAndAgent) != nil {
		t.Fatal("main-only parent must narrow the child to nil")
	}
	if Narrow(extractionOnly, nil) != nil {
		t.Fatal("main-only child must stay nil")
	}
	// A nil-inherited, nil-declared pair is main only.
	if Narrow(nil, nil) != nil {
		t.Fatal("nil/nil must be main only")
	}
}

func TestNarrowKeepsRestrictionWhenIntersectionIsEmpty(t *testing.T) {
	extractionOnly := mustScope(t, Config{Unmerged: true, Origins: []string{"extraction"}})
	agentOnly := mustScope(t, Config{Unmerged: true, Origins: []string{"agent"}})

	got := Narrow(extractionOnly, agentOnly)
	if got.IsMainOnly() {
		t.Fatal("narrowed scope should still be a branch-read scope")
	}
	if got.AllowsOrigin("extraction") || got.AllowsOrigin("agent") || got.AllowsOrigin("manual") {
		t.Fatal("an empty origin intersection must match nothing, not become unrestricted")
	}
}

func TestNarrowGlobsAreConservative(t *testing.T) {
	parent := mustScope(t, Config{Unmerged: true, Branches: []string{"extraction/*"}})

	covered := mustScope(t, Config{Unmerged: true, Branches: []string{"extraction/invoice*"}})
	kept := Narrow(parent, covered)
	if !kept.AllowsName("extraction/invoice-1") {
		t.Fatal("a declared pattern covered by the parent must survive")
	}
	if kept.AllowsName("extraction/other") {
		t.Fatal("narrowing must keep the narrower pattern")
	}

	uncovered := mustScope(t, Config{Unmerged: true, Branches: []string{"plan/*"}})
	none := Narrow(parent, uncovered)
	if none.IsMainOnly() {
		t.Fatal("scope should remain a branch-read scope")
	}
	if none.AllowsName("plan/q3") || none.AllowsName("extraction/a/b") {
		t.Fatal("no declared pattern is covered by the parent, so nothing matches")
	}

	// One side unrestricted: the restricted side wins.
	anyName := mustScope(t, Config{Unmerged: true})
	if got := Narrow(parent, anyName); !got.AllowsName("extraction/a/b") || got.AllowsName("plan/q3") {
		t.Fatal("child must inherit the parent's pattern restriction")
	}
}

func TestContextRoundTrip(t *testing.T) {
	ctx := context.Background()
	if FromContext(ctx) != nil {
		t.Fatal("empty context must be main only")
	}
	if got := ContextWith(ctx, nil); got != ctx {
		t.Fatal("installing a nil scope must not allocate a context value")
	}

	scope := mustScope(t, Config{Unmerged: true, Origins: []string{"extraction"}})
	ctx = ContextWith(ctx, scope)
	if FromContext(ctx) != scope {
		t.Fatal("scope must round-trip through context")
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
