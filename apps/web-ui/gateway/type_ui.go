package main

import "strings"

// typeUI carries one compiled object type's schema-declared ui accent: the
// per-type icon (iconify name like "lucide--note", or a plain glyph such as an
// emoji "📝") and color (an arbitrary CSS color such as "#4F46E5"). Both are
// optional; empty means "not declared, use the generic box".
type typeUI struct {
	Icon  string
	Color string
}

// objectTypeUIMap indexes compiled object types by name, keeping only those
// that declare a ui icon and/or color. Missing names (or a nil input) yield a
// nil map, so lookups fall back to the generic box accent.
func objectTypeUIMap(objectTypes []CompiledType) map[string]typeUI {
	var m map[string]typeUI
	for _, t := range objectTypes {
		ui := typeUI{Icon: compiledTypeIcon(t), Color: compiledTypeColor(t)}
		if ui.Icon == "" && ui.Color == "" {
			continue
		}
		if m == nil {
			m = map[string]typeUI{}
		}
		m[t.Name] = ui
	}
	return m
}

// typeUIOf returns the declared ui accent for name, or the zero typeUI when the
// type is absent from the map (callers then render the generic box icon).
func typeUIOf(m map[string]typeUI, name string) typeUI {
	if m == nil {
		return typeUI{}
	}
	return m[name]
}

// typeColorStyle returns the inline style declarations that tint a chip or tile
// with a schema-declared color: the color's text/border plus a translucent
// background, matching the house "tone/10 bg + tone/15 border" tile look.
// Colors are arbitrary CSS values (hex in practice), so they are applied via a
// style attribute — never a class. Returns "" when no color is declared (the
// caller then keeps its default tone classes). An invalid color value is
// dropped by the browser declaration by declaration, leaving the default look.
func typeColorStyle(color string) string {
	c := strings.TrimSpace(color)
	if c == "" {
		return ""
	}
	return "color:" + c +
		";background-color:color-mix(in oklch," + c + " 10%,transparent)" +
		";border-color:color-mix(in oklch," + c + " 15%,transparent)"
}
