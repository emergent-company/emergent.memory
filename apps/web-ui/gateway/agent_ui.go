package main

import (
	"encoding/json"
	"strings"
)

// --- agent-level ui accent (icon + color) ---
//
// Agent definitions carry a user-picked appearance in an optional `uiConfig`
// JSON blob on both the detail payload (AgentDefinition) and the list payload
// (AgentDefinitionSummary):
//
//	{"icon":"<bare-kebab-lucide-name>","color":"<CSS color>"}
//
// Both keys are optional; an absent, null, or {} blob means "no appearance"
// and every surface falls back to the default bot tile. These helpers parse the
// blob exactly the way compiledTypeIcon/compiledTypeColor/compiledTypeUIValue
// parse an object type's ui block, and the agent_ui.templ primitives render it
// through the object-type tile/chip/glyph partials so there is only one tile
// implementation.

// agentUI carries one agent definition's declared appearance: the icon (a bare
// Lucide kebab name like "bot" or "sparkles") and color (an arbitrary CSS
// color such as "#4F46E5"). Both optional; empty means "not declared".
type agentUI struct {
	Icon  string
	Color string
}

// agentUIOf parses an agent definition's raw uiConfig JSON. It is tolerant of
// absent (nil/empty), null, and {} payloads (all yield the zero agentUI) and
// ignores non-string values.
func agentUIOf(raw json.RawMessage) agentUI {
	if len(raw) == 0 {
		return agentUI{}
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return agentUI{}
	}
	return agentUI{Icon: uiStringValue(m, "icon"), Color: uiStringValue(m, "color")}
}

// uiStringValue reads one string key from a decoded ui object, or "" when the
// key is absent or not a string.
func uiStringValue(m map[string]any, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

// agentIcon returns the agent's declared icon, or "" when none is set.
func agentIcon(raw json.RawMessage) string { return agentUIOf(raw).Icon }

// agentColor returns the agent's declared color, or "" when none is set.
func agentColor(raw json.RawMessage) string { return agentUIOf(raw).Color }

// agentInlineGlyphStyle is the inline style for agentInlineGlyph: it tints the
// glyph's text color only, unlike typeColorStyle (which also paints the tile
// background and border for the framed surfaces). Empty when no color is
// declared, trimmed like typeColorStyle so a whitespace-only value counts as
// undeclared.
func agentInlineGlyphStyle(color string) string {
	if c := strings.TrimSpace(color); c != "" {
		return "color:" + c
	}
	return ""
}

// agentUIConfig builds the uiConfig blob from an editor's icon/color values.
// Both empty yields "{}" (an explicit no-appearance declaration) rather than
// nil/omitted, so clearing the pickers on a PATCH actually removes a
// previously-set appearance. It is the agent counterpart of editUIBlock.
func agentUIConfig(icon, color string) json.RawMessage {
	m := map[string]string{}
	if s := strings.TrimSpace(icon); s != "" {
		m["icon"] = s
	}
	if s := strings.TrimSpace(color); s != "" {
		m["color"] = s
	}
	b, err := json.Marshal(m)
	if err != nil {
		return json.RawMessage("{}")
	}
	return b
}

// agentIconName maps an agent's declared icon to the value typeGlyph should
// render. An undeclared (or unresolvable ASCII) name falls back to the agent
// default "bot"; raw glyphs (emoji) and catalogued names pass through so
// typeGlyph's own box/emoji rules stay authoritative.
func agentIconName(icon string) string {
	s := strings.TrimSpace(icon)
	if s == "" {
		return "bot"
	}
	if class := typeIconClass(s); class == "lucide--box" && normalizeIconName(s) != "box" {
		return "bot"
	}
	return s
}

// agentIconifyClass resolves an agent's declared icon to a compiled iconify
// class, falling back to "lucide--bot" for the default and for raw glyphs
// (which cannot be represented as a class).
func agentIconifyClass(icon string) string {
	if class := typeIconClass(agentIconName(icon)); class != "" {
		return class
	}
	return "lucide--bot"
}

// agentUIForList returns the declared appearance for the agent with id in a
// summary list, or the zero agentUI when the agent is absent.
func agentUIForList(agents []AgentDefinitionSummary, id string) agentUI {
	for _, a := range agents {
		if a.ID == id {
			return agentUIOf(a.UIConfig)
		}
	}
	return agentUI{}
}
