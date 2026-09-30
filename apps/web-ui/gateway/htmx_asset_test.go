package main

import (
	"os"
	"strings"
	"testing"
)

// TestHTMXAssetIsVersion400 guards the self-hosted htmx asset: it must report
// the 4.0.0 final release, never a beta. htmx.min.js is committed (unlike the
// gitignored vendor/ and generated templ/CSS), so a stray `cp` of a beta build
// would otherwise ship silently — the beta has the same public API but the
// `4.0.0-beta*` attribute-restore semantics fixed in #1267 / the
// hx-alpine-compat extension (#1275).
//
// The minified bundle self-reports its version as `this.version="4.0.0"` (or
// `"4.0.0-beta6"` for the old beta), so we assert the exact marker plus the
// absence of any `4.0.0-beta` string.
func TestHTMXAssetIsVersion400(t *testing.T) {
	data, err := os.ReadFile("webui/static/js/htmx.min.js")
	if err != nil {
		t.Fatalf("read htmx asset: %v", err)
	}
	s := string(data)

	if !strings.Contains(s, `version="4.0.0"`) {
		t.Errorf(`htmx asset does not report version="4.0.0" (got no match)`)
	}
	if strings.Contains(s, "4.0.0-beta") {
		t.Errorf(`htmx asset is a beta build (found "4.0.0-beta"); re-vendor the 4.0.0 final release`)
	}
}
