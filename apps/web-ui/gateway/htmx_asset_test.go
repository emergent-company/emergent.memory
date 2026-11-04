package main

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"strings"
	"testing"
)

// htmxAssetSHA256 is the SHA-256 of the exact upstream bundle we vendor:
// https://unpkg.com/htmx.org@4.0.1/dist/htmx.min.js
//
// htmx.min.js is committed (unlike the gitignored vendor/ tree and the
// generated templ/CSS), so a stray `cp` of a different build would ship
// silently: the bundles share a public API and only the version marker
// distinguishes them. The hash makes the vendored revision unambiguous.
const htmxAssetSHA256 = "a0abb04f2f45a1f89d1d4844023245537e5e6014c87b144553e0607b4cc32130"

// TestHTMXAssetIsVersion401 guards the self-hosted htmx asset: it must report
// the 4.0.1 final release, never a beta, and must be the exact bundle we
// vendored. 4.0.1 is a bugfix release over 4.0.0: it fixes a response with only
// `hx-partial` blanking the body, `hx-sync` stale-response overwrites,
// `hx-trigger` on boosted elements, the `changed` modifier, and adds a
// reconnect timeout to `hx-sse`/`hx-multipart`. Those fixes are load-bearing
// for the board's `hx-sse` live refresh (see `board.go` boardStream).
//
// Note: the same-id attribute-restore trap (#1267) is still fixed by the
// `hx-alpine-compat` extension (#1275), loaded after htmx and before deferred
// Alpine (see `ui.templ`); its e2e regression is
// `tests/e2e/specs/js/htmx-alpine-compat.spec.ts`.
//
// The minified bundle self-reports its version as `this.version="4.0.1"`, so
// we assert the exact marker plus the absence of any `4.0.1-beta` string.
func TestHTMXAssetIsVersion401(t *testing.T) {
	data, err := os.ReadFile("webui/static/js/htmx.min.js")
	if err != nil {
		t.Fatalf("read htmx asset: %v", err)
	}
	s := string(data)

	if !strings.Contains(s, `version="4.0.1"`) {
		t.Errorf(`htmx asset does not report version="4.0.1" (got no match)`)
	}
	if strings.Contains(s, "4.0.1-beta") {
		t.Errorf(`htmx asset is a beta build (found "4.0.1-beta"); re-vendor the 4.0.1 final release`)
	}
	sum := sha256.Sum256(data)
	if got := hex.EncodeToString(sum[:]); got != htmxAssetSHA256 {
		t.Errorf("htmx asset sha256 = %s, want %s (re-vendor https://unpkg.com/htmx.org@4.0.1/dist/htmx.min.js)", got, htmxAssetSHA256)
	}
}

// TestHTMXSSEExtensionIsVendored guards the vendored hx-sse extension: the
// board's live refresh depends on it, and it must stay in lockstep with the
// core bundle (the extension is version-coupled to htmx 4) and keep the local
// `cancelReader` patch that stops a navigation away from an SSE element
// surfacing an uncaught AbortError (upstream still calls reader.cancel()
// unguarded in 4.0.1).
func TestHTMXSSEExtensionIsVendored(t *testing.T) {
	data, err := os.ReadFile("webui/static/js/hx-sse.js")
	if err != nil {
		t.Fatalf("read hx-sse asset: %v", err)
	}
	s := string(data)

	for _, want := range []struct {
		needle string
		reason string
	}{
		{"htmx.org@4.0.1", "vendored from a different htmx version than the core bundle"},
		{"connectTimeout", "missing the 4.0.1 reconnect-timeout fix"},
		{"function cancelReader(connection)", "missing the local AbortError guard"},
		{"hx-sse:connect", "not the htmx 4 SSE extension"},
	} {
		if !strings.Contains(s, want.needle) {
			t.Errorf("hx-sse asset is missing %q (%s)", want.needle, want.reason)
		}
	}
	if strings.Contains(s, "4.0.0") {
		t.Errorf("hx-sse asset references 4.0.0; re-vendor https://unpkg.com/htmx.org@4.0.1/dist/ext/hx-sse.js")
	}
}
