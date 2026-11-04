package main

import (
	"encoding/base64"
	"strings"
	"testing"
)

// --- provider brand-mark rendering (#1496 icon-in-select, #1499 theme-safe) ---

const (
	// testMonochromeSVGUsesCurrentColor mirrors openai/jina/longcat/mimo/
	// openrouter: the fill is currentColor, which resolves against the SVG's
	// OWN document once inlined via <img src="data:..."> — black on the dark
	// theme.
	testMonochromeSVGUsesCurrentColor = `<svg fill="currentColor" viewBox="0 0 24 24" xmlns="http://www.w3.org/2000/svg"><path d="M0 0h24v24H0z"/></svg>`
	// testColoredSVG mirrors litellm/gemini/azure_openai: an explicit brand
	// color that must survive untouched.
	testColoredSVG = `<svg viewBox="0 0 24 24" xmlns="http://www.w3.org/2000/svg"><rect width="24" height="24" rx="5" fill="#2B6CB0"/></svg>`
)

func svgDataURI(svg string) string {
	return svgDataURIPrefix + base64.StdEncoding.EncodeToString([]byte(svg))
}

// tagAround returns the full opening tag carrying marker (e.g. an id="..."
// attribute), attribute-order independent.
func tagAround(t *testing.T, html, marker string) string {
	t.Helper()
	i := strings.Index(html, marker)
	if i < 0 {
		t.Fatalf("marker %q missing from markup:\n%s", marker, html)
	}
	open := strings.LastIndex(html[:i], "<")
	if open < 0 {
		t.Fatalf("no opening tag before %q:\n%s", marker, html)
	}
	end := strings.Index(html[i:], ">")
	if end < 0 {
		t.Fatalf("tag carrying %q unterminated:\n%s", marker, html)
	}
	return html[open : i+end+1]
}

// providerIconRegion returns the markup between the provider-icon wrapper and
// the provider select (add form): the wrapper's rendered contents.
func providerIconRegion(t *testing.T, html string) string {
	t.Helper()
	i := strings.Index(html, `id="provider-icon"`)
	s := strings.Index(html, `id="provider-type"`)
	if i < 0 || s < 0 || i > s {
		t.Fatalf("provider icon region malformed (icon@%d, select@%d):\n%s", i, s, html)
	}
	return html[i:s]
}

// TestProviderIconResolution pins the monochrome classifier that decides
// mask-vs-img: currentColor SVGs are monochrome; colored, malformed, non-SVG,
// and script-bearing payloads are not.
func TestProviderIconResolution(t *testing.T) {
	scriptSVG := `<svg fill="currentColor" xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`
	handlerSVG := `<svg onload="alert(1)" fill="currentColor" xmlns="http://www.w3.org/2000/svg"></svg>`
	cases := []struct {
		name string
		uri  string
		mono bool
	}{
		{"currentColor svg is monochrome", svgDataURI(testMonochromeSVGUsesCurrentColor), true},
		{"explicit fill svg is colored", svgDataURI(testColoredSVG), false},
		{"empty uri", "", false},
		{"not an svg data uri", "data:image/png;base64,AAAA", false},
		{"undecodable payload", "data:image/svg+xml;base64,%%%not-base64%%%", false},
		{"embedded script is rejected", svgDataURI(scriptSVG), false},
		{"event handler is rejected", svgDataURI(handlerSVG), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolveProviderIcon(tc.uri); got.Monochrome != tc.mono {
				t.Errorf("resolveProviderIcon(%q).Monochrome = %v, want %v", tc.uri, got.Monochrome, tc.mono)
			}
		})
	}
}

// TestRenderProviderIconInsideSelect covers #1496: the brand mark sits inside
// the provider select control (a relative wrapper with an absolutely-positioned
// pointer-transparent icon), and the select carries the matching inline-start
// padding so the label does not overlap it.
func TestRenderProviderIconInsideSelect(t *testing.T) {
	vendors := []ProviderDefinition{
		{Type: "openai", DisplayName: "OpenAI", Order: 1, IconDataURI: svgDataURI(testMonochromeSVGUsesCurrentColor)},
	}
	html := renderHTML(t, providerConfigPage(providerConfigPageData{DraftProvider: "openai", Vendors: vendors}))

	iconIdx := strings.Index(html, `id="provider-icon"`)
	selectIdx := strings.Index(html, `id="provider-type"`)
	if iconIdx < 0 || selectIdx < 0 {
		t.Fatalf("icon preview and provider select must both render:\n%s", html)
	}
	if iconIdx > selectIdx {
		t.Errorf("icon must precede the select inside the control wrapper (icon@%d, select@%d)", iconIdx, selectIdx)
	}

	wrap := "relative"
	relIdx := strings.LastIndex(html[:iconIdx], wrap)
	if relIdx < 0 {
		t.Errorf("icon must live in a relative wrapper around the select:\n%s", html)
	}

	iconTag := tagAround(t, html, `id="provider-icon"`)
	if !strings.Contains(iconTag, "absolute") || !strings.Contains(iconTag, "pointer-events-none") {
		t.Errorf("icon must be absolutely positioned and click-through, got %q", iconTag)
	}

	selectTag := tagAround(t, html, `id="provider-type"`)
	if !strings.Contains(selectTag, "ps-10") {
		t.Errorf("select must carry the inline-start padding for the inset icon, got %q", selectTag)
	}
}

// TestRenderProviderIconThemeSafe covers #1499: a currentColor mark is painted
// with the theme text color via a CSS mask (never left as a black-only <img>),
// while a colored brand mark keeps its data-URI <img>. The edit form (no
// select) resolves the same way.
func TestRenderProviderIconThemeSafe(t *testing.T) {
	mono := svgDataURI(testMonochromeSVGUsesCurrentColor)
	colored := svgDataURI(testColoredSVG)
	vendors := []ProviderDefinition{
		{Type: "openai", DisplayName: "OpenAI", Order: 1, IconDataURI: mono},
		{Type: "litellm", DisplayName: "LiteLLM", Order: 2, IconDataURI: colored},
	}

	t.Run("monochrome selected uses mask", func(t *testing.T) {
		html := renderHTML(t, providerConfigPage(providerConfigPageData{DraftProvider: "openai", Vendors: vendors}))
		region := providerIconRegion(t, html)
		if !strings.Contains(region, "mask-image") || !strings.Contains(region, "currentColor") {
			t.Errorf("monochrome mark must render as a currentColor mask, got %q", region)
		}
		if strings.Contains(html, `src="`+mono+`"`) {
			t.Errorf("monochrome mark must not render as a black-only <img src>:\n%s", html)
		}
		// The option carries the sync hook so on-change picks the same path.
		if !strings.Contains(tagAround(t, html, `value="openai"`), `data-icon-mono="1"`) {
			t.Errorf("monochrome option must carry data-icon-mono for the sync script:\n%s", html)
		}
	})

	t.Run("colored selected keeps img", func(t *testing.T) {
		html := renderHTML(t, providerConfigPage(providerConfigPageData{DraftProvider: "litellm", Vendors: vendors}))
		region := providerIconRegion(t, html)
		if !strings.Contains(region, `src="`+colored+`"`) {
			t.Errorf("colored mark must keep its data-URI <img>, got %q", region)
		}
		if strings.Contains(region, "mask-image") {
			t.Errorf("colored mark must not be masked, got %q", region)
		}
		if strings.Contains(tagAround(t, html, `value="litellm"`), `data-icon-mono="1"`) {
			t.Errorf("colored option must not carry data-icon-mono:\n%s", html)
		}
	})

	t.Run("edit form resolves monochrome", func(t *testing.T) {
		data := providerConfigPageData{
			Provider: &ProjectProviderConfig{Provider: "openai"},
			Vendors:  vendors,
		}
		html := renderHTML(t, providerConfigPage(data))
		if !strings.Contains(html, "mask-image") || !strings.Contains(html, "currentColor") {
			t.Errorf("edit form must render the monochrome mark as a currentColor mask:\n%s", html)
		}
		if strings.Contains(html, `src="`+mono+`"`) {
			t.Errorf("edit form must not render the monochrome mark as <img src>:\n%s", html)
		}
	})

	t.Run("management row resolves monochrome", func(t *testing.T) {
		d := providerPanelData{
			Providers: []ProjectProviderConfig{{ID: "pc1", ProjectID: "proj", Provider: "openai"}},
			Vendors:   []ProviderDefinition{{Type: "openai", DisplayName: "OpenAI", IconDataURI: mono}},
		}
		html := renderHTML(t, providersPanel(d))
		if !strings.Contains(html, "mask-image") {
			t.Errorf("providers panel row must render the monochrome mark as a mask:\n%s", html)
		}
		if strings.Contains(html, `src="`+mono+`"`) {
			t.Errorf("providers panel row must not render the monochrome mark as <img src>:\n%s", html)
		}
	})
}

// TestRenderProviderEditAPIKeyUX covers #1498: on edit the stored key is never
// returned, so the field starts blank and the eye toggle is a dead control. The
// form must explain the stored key and gate the reveal on a value; the add form
// has no stored key to explain.
func TestRenderProviderEditAPIKeyUX(t *testing.T) {
	edit := renderHTML(t, providerConfigPage(providerConfigPageData{Provider: &ProjectProviderConfig{Provider: "openai"}}))
	for _, want := range []string{
		`data-testid="api-key-stored-hint"`,
		"already stored",
		"never displayed",
		"replace it",
		`id="api_key"`,
	} {
		if !strings.Contains(edit, want) {
			t.Errorf("edit form missing stored-key copy/hook %q, got:\n%s", want, edit)
		}
	}
	// The reveal toggle remains wired for typed values.
	if !strings.Contains(edit, `onclick="togglePasswordVisibility(this)"`) {
		t.Errorf("edit form API-key toggle button must be wired:\n%s", edit)
	}
	if !strings.Contains(edit, `window.togglePasswordVisibility = function`) {
		t.Errorf("edit form must emit the go-daisy password-toggle script:\n%s", edit)
	}

	add := renderHTML(t, providerConfigPage(providerConfigPageData{}))
	if strings.Contains(add, "api-key-stored-hint") {
		t.Errorf("add form must not claim a key is already stored:\n%s", add)
	}
}
