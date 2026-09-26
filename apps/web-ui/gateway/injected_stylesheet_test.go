package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// injectedStylesheetCSS returns the CSS the gateway injects at runtime from
// chat-components.js — the concatenated string literals assigned to
// st.textContent. It is the only stylesheet the client adds to <head> after the
// compiled app.css link. An empty result means no injection was found.
func injectedStylesheetCSS(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("webui", "static", "js", "chat-components.js"))
	if err != nil {
		t.Fatalf("read chat-components.js: %v", err)
	}
	js := string(b)

	const marker = "st.textContent ="
	idx := strings.Index(js, marker)
	if idx < 0 {
		t.Fatal("chat-components.js: no st.textContent assignment found")
	}
	rest := js[idx+len(marker):]

	// The assignment ends at the terminating `;`, immediately before
	// document.head.appendChild(st).
	end := strings.Index(rest, "document.head.appendChild")
	if end < 0 {
		t.Fatal("chat-components.js: injected sheet has no document.head.appendChild terminator")
	}
	rest = rest[:end]

	// Concatenate the double-quoted string literals; the CSS itself uses single
	// quotes for attribute selectors, so double quotes only ever delimit JS
	// literals here.
	var css strings.Builder
	for _, m := range regexp.MustCompile(`"([^"]*)"`).FindAllStringSubmatch(rest, -1) {
		css.WriteString(m[1])
	}
	return css.String()
}

// TestInjectedStylesheetIsNotASecondStylesheet guards the "runtime-injected
// styles are not a second stylesheet" requirement. The chat run-control surface
// — live run status, typed run markers, turn footers, copy affordances, the
// composer queue, the pending-work dock, the session todo card, and the rail
// badge — is owned by webui/css/app.css. chat-components.js must not re-inject a
// copy of those rules: a duplicate would be a second source of truth kept in
// sync by convention, which the requirement forbids.
func TestInjectedStylesheetIsNotASecondStylesheet(t *testing.T) {
	css := injectedStylesheetCSS(t)
	if css == "" {
		t.Fatal("injected stylesheet is empty")
	}

	// app.css-owned selector families that must never reappear as an injected
	// rule. Derived from the #890 measurement that found 47 of the injected
	// sheet's 84 selectors duplicated in app.css.
	for _, sel := range []string{
		".memory-run-status", ".memory-run-marker", ".memory-turn-footer",
		".memory-copy-btn", ".memory-copy-msg", ".memory-code-wrap",
		".memory-queue", ".memory-rail-badge", "#chat-dock", "#chat-todos",
		".dock-count", ".dock-card", ".dock-head", ".dock-question",
		".dock-approval", "memory-pulse",
	} {
		if strings.Contains(css, sel) {
			t.Errorf("injected stylesheet re-declares %q — app.css is the single source of truth", sel)
		}
	}

	// The runtime-only badge shell stays injected: it is the documented subset
	// app.css deliberately does not define. The single intentional overlap — the
	// `.memory-tool-chip[data-status]` border override that keeps the tool-chip
	// row borderless — also remains, but re-declaring it here is what the
	// blocklist above guards against for every app.css-owned selector.
	for _, want := range []string{
		".memory-badge{",
		".memory-badge-open{",
		".memory-badge-detail{",
		".memory-badge-live .memory-badge-label{",
	} {
		if !strings.Contains(css, want) {
			t.Errorf("injected stylesheet lost its runtime-only badge rule %q", want)
		}
	}
}
