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
