package main

import (
	"strings"
	"testing"
)

// TestPageTerminalFormShape guards the pane-level form contract the sticky
// action bar depends on (components.StickyFormActions). A page-terminal form is
// a DIRECT child of main#main-content, laid out as:
//
//	<div class={pageFormChromeClass}> … header/toasts … </div>
//	<form …>
//	  <div class={pageFormFieldsClass …}> … fields … </div>
//	  <div class="sticky-form-actions …"> … buttons … </div>
//	</form>
//
// …so the bar spans the full content pane, rests flush on the pane's bottom
// edge at max scroll, and is the form's LAST in-flow child. The render is
// order-checked by string index (matching the other gateway render tests), not
// parsed.
//
// If a page needs a legitimate trailing node inside the form (e.g. a hidden
// input after the bar), that is a real invariant break — fix the page, do not
// loosen this test. A page's <script> belongs AFTER </form>, never between the
// bar and </form>.
func TestPageTerminalFormShape(t *testing.T) {
	pages := []struct {
		name string
		html string
	}{
		{
			"MCPServerNewPage",
			renderHTML(t, MCPServerNewPage(mcpServerFormData{Type: "stdio", Enabled: true})),
		},
		{
			"MCPShareNewPage",
			renderHTML(t, MCPShareNewPage(mcpShareFormData{})),
		},
		{
			"BlueprintImportModelMapPage",
			renderHTML(t, BlueprintImportModelMapPage(BlueprintModelMapData{
				URL: "https://github.com/org/repo",
				Rows: []BlueprintModelMapRow{
					{AgentName: "writer", SourceModel: "gemini-2.5-flash", Issue: "needs a provider mapping"},
				},
			})),
		},
		{
			"dsCredentialsConnectPage",
			renderHTML(t, dsCredentialsConnectPage(DSProvider{Type: "drive", Name: "Google Drive"}, nil, nil, dsSavedCredentialPicker{}, nil, nil)),
		},
	}

	for _, p := range pages {
		t.Run(p.name, func(t *testing.T) {
			assertPageTerminalForm(t, p.html)
		})
	}
}

// assertPageTerminalForm asserts the three pane-level shape invariants:
//  1. the page root starts with the chrome wrapper,
//  2. DOM order is <form → fields wrapper → sticky bar → </form>,
//  3. the bar is the form's last in-flow child (only whitespace between the
//     bar's closing </div> and </form>).
func assertPageTerminalForm(t *testing.T, html string) {
	t.Helper()

	// 1. The page's outermost element is the chrome wrapper.
	if got := firstClass(html); got != pageFormChromeClass {
		t.Errorf("page root class = %q, want %q (chrome wrapper must be the page's first element)", got, pageFormChromeClass)
	}

	// 2. DOM order.
	chromeIdx := strings.Index(html, pageFormChromeClass)
	formIdx := strings.Index(html, "<form")
	fieldsIdx := strings.Index(html, pageFormFieldsClass)
	barIdx := strings.Index(html, "sticky-form-actions")
	formCloseIdx := strings.Index(html, "</form>")

	markers := []struct {
		name string
		idx  int
	}{
		{"chrome", chromeIdx},
		{"<form", formIdx},
		{"fields", fieldsIdx},
		{"sticky-form-actions", barIdx},
		{"</form>", formCloseIdx},
	}
	for _, m := range markers {
		if m.idx < 0 {
			t.Fatalf("missing required marker %q", m.name)
		}
	}
	orderOK := chromeIdx < formIdx && formIdx < fieldsIdx && fieldsIdx < barIdx && barIdx < formCloseIdx
	if !orderOK {
		t.Errorf(
			"page-terminal form order wrong: chrome(%d) < form(%d) < fields(%d) < bar(%d) < </form>(%d)",
			chromeIdx, formIdx, fieldsIdx, barIdx, formCloseIdx,
		)
	}

	// 3. The bar is the form's LAST in-flow child. The bar root is a div whose
	// close is the last </div> in the span from the bar open to </form>; after
	// it there must be nothing but whitespace before </form>.
	tail := html[barIdx:formCloseIdx]
	lastDiv := strings.LastIndex(tail, "</div>")
	if lastDiv < 0 {
		t.Fatal("sticky bar has no closing </div> before </form>")
	}
	if after := strings.TrimSpace(tail[lastDiv+len("</div>"):]); after != "" {
		t.Errorf("content between the sticky bar and </form> (bar must be the last in-flow child): %q", after)
	}
}
