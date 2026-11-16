package components

import (
	"strings"

	"github.com/a-h/templ"
	ui "github.com/emergent-company/go-daisy/components/ui"
	"github.com/emergent-company/go-daisy/shared"
)

// badge.go holds the class and style helpers for the BadgeWithClass wrapper in
// badge.templ. They mirror ui.Badge's own unexported helpers (its class list,
// dotClass, accentStyle); the parity test in badge_alert_class_test.go renders
// the wrapper and the upstream primitive side by side, so a change to ui.Badge
// that is not carried here fails rather than drifting silently.

// badgeClassAndAttrs returns the element's full class list and the Attrs to
// spread. It follows the shape ui.DropdownMenu uses upstream for an
// arbitrary-class hook: a "class" left in Attrs is read, deleted, and merged
// with Class — so neither path can render a second class attribute (#855).
func badgeClassAndAttrs(props BadgeWithClassProps) (string, templ.Attributes) {
	attrs, extra := takeClassAttr(props.Attrs)

	classes := []string{"badge gap-1"}
	if props.Variant != "" && props.Color == "" {
		classes = append(classes, string(props.Variant))
	}
	if props.Style != "" {
		classes = append(classes, string(props.Style))
	}
	if props.Size != "" {
		classes = append(classes, string(props.Size))
	}
	if props.MaxWidth != "" {
		classes = append(classes, "overflow-hidden", props.MaxWidth)
	}
	if props.Position != "" {
		classes = append(classes, props.Position)
	}
	if extra != "" {
		classes = append(classes, extra)
	}
	if props.Class != "" {
		classes = append(classes, props.Class)
	}
	return shared.TwMerge(classes...), attrs
}

// takeClassAttr returns attrs without its "class" entry, plus that entry's
// value. The map is copied, so the caller's templ.Attributes is not mutated.
func takeClassAttr(attrs templ.Attributes) (templ.Attributes, string) {
	if len(attrs) == 0 {
		return nil, ""
	}
	class, ok := attrs["class"].(string)
	if !ok {
		return attrs, ""
	}
	out := make(templ.Attributes, len(attrs)-1)
	for k, v := range attrs {
		if k != "class" {
			out[k] = v
		}
	}
	return out, class
}

// dotClass maps a BadgeIntent to its DaisyUI status-dot class (ui.Badge's
// dotClass, unchanged).
func dotClass(intent ui.BadgeIntent) string {
	switch intent {
	case ui.BadgeSuccess:
		return "status status-success status-xs"
	case ui.BadgeError:
		return "status status-error status-xs"
	case ui.BadgeWarning:
		return "status status-warning status-xs"
	case ui.BadgeInfo:
		return "status status-info status-xs"
	case ui.BadgePrimary:
		return "status status-primary status-xs"
	case ui.BadgeSecondary:
		return "status status-secondary status-xs"
	case ui.BadgeAccent:
		return "status status-accent status-xs"
	default:
		return "status status-neutral status-xs"
	}
}

// accentStyle is ui.Badge's inline colour style for an arbitrary CSS colour
// (ui.Badge's accentStyle, unchanged): the text is the colour, the background
// and border are translucent mixes of it.
func accentStyle(color string) string {
	c := strings.TrimSpace(color)
	if c == "" {
		return ""
	}
	return "color:" + c +
		";background-color:color-mix(in oklch," + c + " 10%,transparent)" +
		";border-color:color-mix(in oklch," + c + " 15%,transparent)"
}
