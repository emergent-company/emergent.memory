package blueprints

import "testing"

// TestSortVersionsNewestFirst covers the ordering contract advertised by the
// blueprint-versions MCP tool and the REST versions endpoint: newest first,
// semver-aware, deterministic for unparseable versions.
func TestSortVersionsNewestFirst(t *testing.T) {
	versions := func(vs ...string) []Blueprint {
		out := make([]Blueprint, len(vs))
		for i, v := range vs {
			out[i] = Blueprint{Version: v}
		}
		return out
	}
	names := func(bps []Blueprint) []string {
		out := make([]string, len(bps))
		for i, bp := range bps {
			out[i] = bp.Version
		}
		return out
	}
	equal := func(t *testing.T, got []Blueprint, want []string) {
		t.Helper()
		gotNames := names(got)
		if len(gotNames) != len(want) {
			t.Fatalf("got %v, want %v", gotNames, want)
		}
		for i := range want {
			if gotNames[i] != want[i] {
				t.Fatalf("got %v, want %v", gotNames, want)
			}
		}
	}

	t.Run("numeric major/minor/patch, not string order", func(t *testing.T) {
		bps := versions("1.9.0", "1.10.0", "2.0.0", "1.10.1")
		sortVersionsNewestFirst(bps)
		equal(t, bps, []string{"2.0.0", "1.10.1", "1.10.0", "1.9.0"})
	})

	t.Run("release sorts above its prereleases", func(t *testing.T) {
		bps := versions("1.0.0-beta.2", "1.1.0-rc.1", "1.0.0", "1.0.0-beta.10")
		sortVersionsNewestFirst(bps)
		equal(t, bps, []string{"1.1.0-rc.1", "1.0.0", "1.0.0-beta.2", "1.0.0-beta.10"})
	})

	t.Run("leading v and build metadata are tolerated", func(t *testing.T) {
		bps := versions("v1.2.0", "1.10.0", "1.2.0+build.5")
		sortVersionsNewestFirst(bps)
		if got := names(bps)[0]; got != "1.10.0" {
			t.Fatalf("got %v, want 1.10.0 first", names(bps))
		}
	})

	t.Run("non-semver falls back to descending string order", func(t *testing.T) {
		bps := versions("alpha", "2026.10", "banana")
		sortVersionsNewestFirst(bps)
		equal(t, bps, []string{"banana", "alpha", "2026.10"})
	})

	t.Run("parseable versions always sort above unparseable ones", func(t *testing.T) {
		// Regression: comparing only within the parseable pair left a junk
		// version first and ordered a semver below another one.
		bps := versions("1abc", "10.0.0", "2.0.0", "latest")
		sortVersionsNewestFirst(bps)
		equal(t, bps, []string{"10.0.0", "2.0.0", "latest", "1abc"})
	})

	t.Run("result does not depend on incoming order", func(t *testing.T) {
		inputs := [][]string{
			{"1abc", "10.0.0", "2.0.0", "latest"},
			{"latest", "2.0.0", "1abc", "10.0.0"},
			{"2.0.0", "latest", "10.0.0", "1abc"},
		}
		want := []string{"10.0.0", "2.0.0", "latest", "1abc"}
		for _, in := range inputs {
			bps := versions(in...)
			sortVersionsNewestFirst(bps)
			equal(t, bps, want)
		}
	})

	t.Run("contract fixture shared with the web UI gallery", func(t *testing.T) {
		// The gallery renders this exact order verbatim (issue #15): release
		// above prerelease, lexicographic prerelease (beta.2 above beta.10),
		// numeric core, and unparseable last.
		bps := versions("latest", "1.0.0-beta.10", "1.0.0-beta.2", "1.0.0", "1.9.0", "1.10.0")
		sortVersionsNewestFirst(bps)
		equal(t, bps, []string{"1.10.0", "1.9.0", "1.0.0", "1.0.0-beta.2", "1.0.0-beta.10", "latest"})
	})

	t.Run("equal versions keep incoming order", func(t *testing.T) {
		bps := versions("1.0.0", "1.0.0", "1.0.0")
		bps[0].ID, bps[1].ID, bps[2].ID = "first", "second", "third"
		sortVersionsNewestFirst(bps)
		if bps[0].ID != "first" || bps[1].ID != "second" || bps[2].ID != "third" {
			t.Fatalf("equal versions were reordered: %v", []string{bps[0].ID, bps[1].ID, bps[2].ID})
		}
	})

	t.Run("empty and single element are no-ops", func(t *testing.T) {
		sortVersionsNewestFirst(nil)
		bps := versions("1.0.0")
		sortVersionsNewestFirst(bps)
		equal(t, bps, []string{"1.0.0"})
	})
}
