package main

import (
	"os"
	"strings"
	"testing"
)

// TestHTMXAssetIsVersion400 guards the self-hosted htmx asset: it must report
// the 4.0.0 final release, never a beta. htmx.min.js is committed (unlike the
// gitignored vendor/ and generated templ/CSS), so a stray `cp` of a beta build
// would otherwise ship silently; the two builds share a public API, so only the
// version marker distinguishes them.
//
// Note: this upgrade did NOT fix the same-id attribute-restore trap (#1267).
// The relevant upstream source (`__startCSSTransitions` and the same-id restore
// path) is byte-identical between `4.0.0-beta6` and `4.0.0` final. #1267 is
// fixed by the `hx-alpine-compat` extension adopted in #1275, loaded after htmx
// and before deferred Alpine (see `ui.templ`); its e2e regression is
// `tests/e2e/specs/js/htmx-alpine-compat.spec.ts`.
//
// Config note: `defaultSwapEmpty` was renamed to `allowEmptySwapAfterOOB`
// between beta6 and final. This app uses neither.
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
