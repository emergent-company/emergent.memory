package main

import (
	"strings"
	"testing"
)

// cssRuleBlock returns the compiled block for selector in app.css, for the tests
// that assert a stylesheet contract the markup alone cannot express (a safelisted
// utility, a shared lane grid). It lived in the retired board's layout test until
// the board moved; the objects, theme and sidebar contracts all read through it,
// so it is shared rather than owned by one surface.
func cssRuleBlock(t *testing.T, css, selector string) string {
	t.Helper()
	start := strings.Index(css, selector+" {")
	if start < 0 {
		t.Fatalf("selector %q not found in app.css", selector)
	}
	rest := css[start:]
	end := strings.Index(rest, "}")
	if end < 0 {
		t.Fatalf("unterminated block for %q", selector)
	}
	return rest[:end]
}
