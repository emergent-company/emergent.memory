package components

import (
	"github.com/a-h/templ"
	"github.com/emergent-company/go-daisy/shared"
)

// alertClassAndAttrs returns the element's full class list and the Attrs to
// spread: ui.Alert's own classes (the alert base, its type, its style) with
// Class merged in last so it wins a Tailwind conflict (twmerge, last-wins), and
// a "class" left in Attrs folded into the same merge rather than rendered as a
// second class attribute (#855). Empty members are dropped; templ.KV did the
// same upstream.
func alertClassAndAttrs(props AlertWithClassProps) (string, templ.Attributes) {
	attrs, extra := takeClassAttr(props.Attrs)

	classes := []string{"alert"}
	if props.Type != "" {
		classes = append(classes, string(props.Type))
	}
	if props.Style != "" {
		classes = append(classes, string(props.Style))
	}
	if extra != "" {
		classes = append(classes, extra)
	}
	if props.Class != "" {
		classes = append(classes, props.Class)
	}
	return shared.TwMerge(classes...), attrs
}
