package main

import (
	"strings"
	"testing"

	"github.com/a-h/templ"
	ui "github.com/emergent-company/go-daisy/components/ui"
)

// TestGoDaisyInitRuntimeWired guards the init-runtime wiring that the go-daisy
// v0.16.0 bump made mandatory. In v0.16.0, component inline scripts call
// window.gdOnce / window.gdBehaviour (and htmx after-settle runs window.gdRescan)
// to self-stamp their one-time and swap-safe behaviour guards. If the head omits
// the behaviours runtime (staticfs/js/go-daisy-init.js), those globals are
// undefined and the components fail SILENTLY at runtime: clipboard copy,
// spinners, the sidebar, and the data-table just stop working with no error.
//
// This is a presence/order assertion, not a count ratchet, so it is deliberately
// absent from the adoption/consistency budget tables.
func TestGoDaisyInitRuntimeWired(t *testing.T) {
	// Render the app shell with an IconPicker as the main content so the
	// component runtime scripts (go-daisy-popover.js, go-daisy-icon-picker.js)
	// are present in the document and the ordering can be asserted. IconPicker
	// emits both PopoverScript() and IconPickerScript().
	content := ui.IconPicker(ui.IconPickerProps{
		ID:      "test-icon",
		Name:    "icon",
		Options: []ui.IconPickerOption{{Name: "box", Label: "Box", Class: "lucide--box"}},
	})
	shell := renderHTML(t, appShell(
		"Inbox", nil, false, nil, "", nil, "", nil, nil, nil, nil, false,
		0,
		&currentUser{Name: "Ada", Sub: "sub-a"}, nil, content, nil,
		"", "", 0, 0, 0, "",
	))

	initAt := strings.Index(shell, "go-daisy-init.js")
	if initAt < 0 {
		t.Fatalf("appShell is missing the go-daisy init runtime: %q", "go-daisy-init.js")
	}
	for _, script := range []string{"go-daisy-popover.js", "go-daisy-icon-picker.js"} {
		at := strings.Index(shell, script)
		if at < 0 {
			t.Fatalf("appShell is missing %q (test fixture must render a component that emits it)", script)
		}
		if initAt >= at {
			t.Errorf("go-daisy-init.js (%d) must precede %s (%d) in document order", initAt, script, at)
		}
	}

	// The standalone branded error page and the dev gallery document each emit
	// their own <head> and therefore must wire the init runtime independently.
	errPage := renderHTML(t, errorPage(errorPageData{Status: 404, Heading: "Page not found", Message: "Gone"}))
	if !strings.Contains(errPage, "go-daisy-init.js") {
		t.Error("errorPage is missing the go-daisy init runtime")
	}

	gallery := renderHTML(t, ComponentGalleryDocument(templ.NopComponent, ""))
	if !strings.Contains(gallery, "go-daisy-init.js") {
		t.Error("ComponentGalleryDocument is missing the go-daisy init runtime")
	}
}
