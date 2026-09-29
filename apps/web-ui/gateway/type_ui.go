package main

import (
	"encoding/json"
	"strings"

	"github.com/a-h/templ"
)

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

// objectTypeUI is one compiled type's client-facing entry in the embedded type
// map: its human display label plus the optional declared ui icon/color. The
// client uses Label to render object "sources" without the raw machine name,
// and Icon/Color to keep the type's accent (mirroring typeNameChip server-side).
// Field names are the JSON keys the chat client reads.
type objectTypeUI struct {
	Label string `json:"label"`
	Icon  string `json:"icon,omitempty"`
	Color string `json:"color,omitempty"`
}

// objectTypeMapFor builds the name → {label,icon,color} map embedded in the web
// shell so the client can render human type labels synchronously. Both object
// and relationship types are included (citations can be either). An object type
// wins when a name appears in both; empty names are skipped. A nil/empty input
// yields an empty (non-nil) map so the serializer always emits valid JSON.
func objectTypeMapFor(objectTypes, relationshipTypes []CompiledType) map[string]objectTypeUI {
	m := make(map[string]objectTypeUI, len(objectTypes)+len(relationshipTypes))
	add := func(types []CompiledType) {
		for _, t := range types {
			// Shadowed types are inactive (an earlier pack superseded by a later
			// one) and must not appear — the winning declaration for a name wins.
			if t.Name == "" || t.Shadowed {
				continue
			}
			if _, ok := m[t.Name]; ok {
				continue
			}
			m[t.Name] = objectTypeUI{
				Label: compiledTypeLabel(t),
				Icon:  compiledTypeIcon(t),
				Color: compiledTypeColor(t),
			}
		}
	}
	add(objectTypes)
	add(relationshipTypes)
	return m
}

// objectTypeMapJSON serializes the embedded type map for a JSON <script>
// element. encoding/json HTML-escapes '<', '>' and '&' by default, so the
// result cannot terminate the surrounding <script> or inject markup. On the
// (impossible for this shape) marshal error it degrades to "{}".
func objectTypeMapJSON(m map[string]objectTypeUI) string {
	if len(m) == 0 {
		return "{}"
	}
	b, err := json.Marshal(m)
	if err != nil {
		return "{}"
	}
	return string(b)
}

// objectTypeMapScript renders the embedded type map as a JSON <script> element
// for the shell. templ treats a <script> body as raw text and does not evaluate
// expressions inside it, so the whole element is emitted as a raw component;
// objectTypeMapJSON's HTML escaping makes that safe.
func objectTypeMapScript(m map[string]objectTypeUI) templ.Component {
	return templ.Raw(`<script type="application/json" id="memory-object-types">` + objectTypeMapJSON(m) + `</script>`)
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
