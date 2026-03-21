package graphexplore

import "fmt"

// iconFontSize returns the CSS font-size for a type icon.
// Emoji icons (multi-rune) get larger size, single-letter gets smaller.
func iconFontSize(icon string) string {
	if len([]rune(icon)) > 1 {
		return "13px"
	}
	return "9px"
}

// displayType returns the type name for display, or "(none)" if empty.
func displayType(name string) string {
	if name == "" {
		return "(none)"
	}
	return name
}

// countLabel formats the in-graph/total count display.
func countLabel(inGraph, total int) string {
	if inGraph > 0 {
		return fmt.Sprintf("%d/%d", inGraph, total)
	}
	return fmt.Sprintf("%d", total)
}

// visTitle returns the tooltip for the visibility toggle button.
func visTitle(hidden bool, typeName string) string {
	if hidden {
		return "Show " + typeName + " in graph"
	}
	return "Hide " + typeName + " in graph"
}

// rowBg returns alternating row background style.
func rowBg(index int) string {
	if index%2 == 0 {
		return "background:#0d1117"
	}
	return "background:#161b22"
}
