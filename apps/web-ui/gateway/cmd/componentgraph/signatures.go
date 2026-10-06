package main

// componentSignature describes the static markup markers a shared component
// owns: the class tokens on its wrapper element and the element tag(s) it emits.
//
// Only high-confidence markers are listed. A component owns its WRAPPER, never
// its slot — so caller-supplied content (a raw <table> inside TableCard, a raw
// <select> inside SelectOptionGroups) is deliberately excluded, and generic
// utilities (nav, dl, dt, dd, label, input, select, option, optgroup) are left
// out entirely. The adoption scanner inverts this map to flag authored templates
// that re-declare the same wrapper markup inline instead of rendering the
// component.
type componentSignature struct {
	Classes []string
	Tags    []string
}

// componentSignatures maps each shared component id (the Go identifier as
// written) to the markup markers it owns. When two components share a marker —
// Button and SaveChangesButton both own "btn", Badge and StatusBadge both own
// "badge", CardRaw/PanelCard/TableCard all own "card"/"card-border" — the
// scanner reports every owner so an adoption gap surfaces all the components
// that could replace the ad-hoc markup.
func componentSignatures() map[string]componentSignature {
	return map[string]componentSignature{
		"Button":            {Classes: []string{"btn"}},
		"SaveChangesButton": {Classes: []string{"btn"}},
		"Badge":             {Classes: []string{"badge"}},
		"StatusBadge":       {Classes: []string{"badge"}},
		"Alert":             {Classes: []string{"alert"}},
		"CardRaw":           {Classes: []string{"card", "card-border"}},
		"PanelCard":         {Classes: []string{"card", "card-border"}},
		"TableCard":         {Classes: []string{"card", "card-border"}},
		"Dialog": {
			Classes: []string{"modal", "modal-box", "modal-backdrop"},
			Tags:    []string{"dialog"},
		},
		"SecretRevealModal": {
			// modal-action is deliberately not a marker: the component's own
			// action row lives in the components package (never scanned), and
			// authored action rows sit inside an already-adopted dialog's
			// children. A raw dialog is still caught by the modal/dialog markers.
			Classes: []string{"modal", "modal-box", "modal-backdrop"},
			Tags:    []string{"dialog"},
		},
		"ToggleField": {Classes: []string{"toggle"}},
		"ToggleInput": {Classes: []string{"toggle"}},
	}
}
