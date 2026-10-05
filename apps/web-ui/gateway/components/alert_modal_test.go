package components

import "testing"

// TestAlertModalContract locks the ids app.js depends on to fill and open the
// modal (alertModal reads #memory-alert-{banner,icon,title,message}), the a11y
// role and accessible name/description (the dialog is labelled by its live title
// and described by its live message), and the no-auto-open contract (the modal
// is opened by the event, never on render).
func TestAlertModalContract(t *testing.T) {
	html := renderHTML(t, AlertModal())
	assertContains(t, html,
		`id="memory-alert-modal"`,
		`id="memory-alert-banner"`,
		`id="memory-alert-icon"`,
		`id="memory-alert-title"`,
		`id="memory-alert-message"`,
		`data-testid="memory-alert-banner"`,
		`role="alert"`,
		`aria-labelledby="memory-alert-title"`,
		`aria-describedby="memory-alert-message"`,
	)
	assertNotContains(t, html, "data-dialog-autoopen")
}
