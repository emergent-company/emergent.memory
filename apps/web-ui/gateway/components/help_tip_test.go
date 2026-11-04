package components

import (
	"strings"
	"testing"
)

// TestHelpTipWithIDExposesReferencedDescription pins the #2393 described mode:
// a control's aria-describedby points at a real, visually-hidden copy of the
// text, while data-tip keeps the pointer tooltip.
func TestHelpTipWithIDExposesReferencedDescription(t *testing.T) {
	const tip = "Recurse into subfolders when true."
	html := renderHTML(t, HelpTip(tip, HelpTipProps{ID: "ds-tip-recursive"}))

	assertContains(t, html,
		`data-tip="`+tip+`"`,
		`<span id="ds-tip-recursive" class="sr-only">`+tip+`</span>`,
	)
	if strings.Contains(html, "tabindex") {
		t.Error("the described trigger must not add a tab stop; the control carries the description")
	}
}

// TestHelpTipWithoutIDIsAFocusableNote covers contexts with no control to
// describe (a label/key note): the icon itself becomes keyboard reachable and
// carries the text as its accessible name.
func TestHelpTipWithoutIDIsAFocusableNote(t *testing.T) {
	const tip = "Label: Recursive · Key: recursive"
	html := renderHTML(t, HelpTip(tip, HelpTipProps{Position: "bottom"}))

	assertContains(t, html,
		`class="tooltip tooltip-bottom"`,
		`data-tip="`+tip+`"`,
		`tabindex="0"`,
		`role="note"`,
		`aria-label="`+tip+`"`,
	)
	if strings.Contains(html, "sr-only") {
		t.Error("the focusable note must not also emit a hidden copy")
	}
}
