package components

import "github.com/a-h/templ"

// alertModalDialogAttrs merges the alert modal's fixed ARIA wiring with
// caller-supplied dialog attributes (e.g. data-dialog-autoopen for a gallery
// preview). Caller attributes win on key collision.
func alertModalDialogAttrs(extra templ.Attributes) templ.Attributes {
	attrs := templ.Attributes{
		"aria-labelledby":  "memory-alert-title",
		"aria-describedby": "memory-alert-message",
	}
	for k, v := range extra {
		attrs[k] = v
	}
	return attrs
}
