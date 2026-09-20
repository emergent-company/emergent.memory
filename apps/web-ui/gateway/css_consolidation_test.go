package main

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/a-h/templ"
)

// renderPageShell renders the full app shell (head included) with minimal
// data so tests can assert on the <head> output.
func renderPageShell(t *testing.T, content templ.Component) string {
	t.Helper()
	return renderHTML(t, appShell(
		"Agents", nil, false, nil, "", nil, "", nil, nil, nil, nil, false,
		nil, nil,
		content,
		"", "", 0, 0, 0,
	))
}

// TestPageHeadOmitsMonolithicGoDaisyCSS (change spec 4.1): the gateway no
// longer references go-daisy's pre-compiled /static/css/app.css; its own
// combined /assets/css/app.css is the only stylesheet.
func TestPageHeadOmitsMonolithicGoDaisyCSS(t *testing.T) {
	body := templ.ComponentFunc(func(_ context.Context, w io.Writer) error {
		_, err := w.Write([]byte("<p>hi</p>"))
		return err
	})
	html := renderPageShell(t, body)
	if strings.Contains(html, "/static/css/app.css") {
		t.Error("page head still references go-daisy monolithic /static/css/app.css")
	}
	if !strings.Contains(html, "/assets/css/app.css") {
		t.Error("page head missing the gateway's own /assets/css/app.css stylesheet")
	}
}

// compiledCSS returns the gateway's committed compiled stylesheet.
func compiledCSS(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("webui", "static", "css", "app.css"))
	if err != nil {
		t.Skipf("compiled css not present: %v", err)
	}
	return string(b)
}

// chatBubbleOverride returns the custom unlayered .chat-bubble rule from the
// compiled CSS, or "" when absent. It is the only .chat-bubble rule that sets a
// box-shadow — daisyUI's layered default has none — so the shadow uniquely
// identifies the rule that is meant to beat daisyUI's width:fit-content/90%.
func chatBubbleOverride(css string) string {
	for {
		start := strings.Index(css, ".chat-bubble{")
		if start < 0 {
			return ""
		}
		rest := css[start:]
		end := strings.IndexByte(rest, '}')
		if end < 0 {
			return ""
		}
		rule := rest[:end+1]
		if strings.Contains(rule, "box-shadow") {
			return rule
		}
		css = rest[len(".chat-bubble{"):]
	}
}

// TestCompiledCSSExcludesUnusedDaisyUIModules (change spec 4.2): daisyUI
// modules the gateway never renders (audited) must be absent from the compiled
// CSS, while used component styles + go-daisy custom CSS + icons remain.
func TestCompiledCSSExcludesUnusedDaisyUIModules(t *testing.T) {
	css := compiledCSS(t)
	for _, sel := range []string{".mockup", ".artboard", ".otp-"} {
		if strings.Contains(css, sel) {
			t.Errorf("compiled css still contains excluded daisyUI module %q", sel)
		}
	}
	// sanity: styles that must remain. `.status` is deliberately NOT excluded:
	// go-daisy's Badge renders its Dot prop as `status status-<intent>
	// status-xs`, so the module must stay compiled for badge dots to have any
	// geometry/colour at all.
	for _, want := range []string{"sidebar-menu-item", "data:image/svg+xml", ".status{", ".status-xs{"} {
		if !strings.Contains(css, want) {
			t.Errorf("compiled css missing expected rule/asset %q", want)
		}
	}
}

// TestCompiledCSSDeterministic (change spec 4.3): compiling webui/css/app.css
// twice yields byte-identical output.
func TestCompiledCSSDeterministic(t *testing.T) {
	tw := filepath.Join("node_modules", ".bin", "tailwindcss")
	if _, err := os.Stat(tw); err != nil {
		t.Skipf("tailwindcss not installed: %v", err)
	}
	compile := func() string {
		out := filepath.Join(t.TempDir(), "app.css")
		cmd := exec.Command(tw, "-i", filepath.Join("webui", "css", "app.css"), "-o", out, "--minify")
		if b, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("tailwind failed: %v\n%s", err, b)
		}
		b, _ := os.ReadFile(out)
		return string(b)
	}
	a, b := compile(), compile()
	if a != b {
		t.Error("compiled CSS is not deterministic across two identical builds")
	}
	if len(a) < 50000 {
		t.Errorf("compiled CSS suspiciously small (%d bytes)", len(a))
	}
}

// TestChatBubbleFillsColumn guards the chat bubble sizing fix: daisyUI sizes
// .chat-bubble to its content (width:fit-content; max-width:90%), so a short
// message rendered as a narrow pill. The custom unlayered override must still
// resolve width and max-width to 100% in the *compiled* output. A source-only
// change is not enough to guard this: webui/static/css/app.css is generated
// and gitignored, so a future Tailwind/daisyUI build could silently drop or
// override these declarations while the Go page-markup tests still pass.
func TestChatBubbleFillsColumn(t *testing.T) {
	css := compiledCSS(t)
	rule := chatBubbleOverride(css)
	if rule == "" {
		t.Fatal("compiled css missing the custom .chat-bubble override (no box-shadow rule)")
	}
	for _, decl := range []string{"width:100%", "max-width:100%"} {
		if !strings.Contains(rule, decl) {
			t.Errorf("chat-bubble override %q missing %q — a short message would render as a narrow pill", rule, decl)
		}
	}
}
