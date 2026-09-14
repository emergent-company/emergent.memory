package main

import (
	"strings"
	"unicode"

	ui "github.com/emergent-company/go-daisy/components/ui"
)

// supportedTypeIconClasses is the curated catalog of Lucide icons the gateway
// renders for schema-declared object-type icons. It is a closed set on purpose:
// Tailwind's @iconify/tailwind4 plugin only emits CSS for icon classes it finds
// as literal candidates while scanning sources (see the @source directives in
// webui/css/app.css), so a schema icon can only render if its class is listed
// here. Every entry is a literal "lucide--…" string so the CSS build picks it
// up; anything outside the catalog falls back to the generic box.
//
// The list mirrors the icon vocabulary the schema registry historically
// declares (bare Lucide names such as "FileText", "GitBranch", "Tag"), as used
// by the former React UI (emergent.memory.ui).
var supportedTypeIconClasses = []string{
	"lucide--box",
	"lucide--file-text",
	"lucide--file",
	"lucide--git-branch",
	"lucide--git-commit",
	"lucide--git-merge",
	"lucide--git-pull-request",
	"lucide--shield",
	"lucide--shield-check",
	"lucide--zap",
	"lucide--layers",
	"lucide--tag",
	"lucide--star",
	"lucide--heart",
	"lucide--alert-triangle",
	"lucide--bell",
	"lucide--book",
	"lucide--book-open",
	"lucide--briefcase",
	"lucide--calendar",
	"lucide--camera",
	"lucide--check",
	"lucide--check-circle",
	"lucide--circle",
	"lucide--clock",
	"lucide--cloud",
	"lucide--code",
	"lucide--cog",
	"lucide--settings",
	"lucide--database",
	"lucide--edit",
	"lucide--eye",
	"lucide--folder",
	"lucide--globe",
	"lucide--hash",
	"lucide--home",
	"lucide--image",
	"lucide--info",
	"lucide--key",
	"lucide--link",
	"lucide--list",
	"lucide--lock",
	"lucide--mail",
	"lucide--map",
	"lucide--map-pin",
	"lucide--message-circle",
	"lucide--monitor",
	"lucide--package",
	"lucide--paperclip",
	"lucide--pen",
	"lucide--phone",
	"lucide--play",
	"lucide--plus",
	"lucide--puzzle",
	"lucide--search",
	"lucide--send",
	"lucide--server",
	"lucide--share",
	"lucide--sparkles",
	"lucide--terminal",
	"lucide--trash",
	"lucide--user",
	"lucide--users",
	"lucide--wrench",
	"lucide--x",
}

// supportedTypeIcons is the set of bare Lucide kebab names derived from
// supportedTypeIconClasses ("lucide--file-text" → "file-text").
var supportedTypeIcons = buildSupportedTypeIcons()

func buildSupportedTypeIcons() map[string]struct{} {
	m := make(map[string]struct{}, len(supportedTypeIconClasses))
	for _, class := range supportedTypeIconClasses {
		m[strings.TrimPrefix(class, "lucide--")] = struct{}{}
	}
	return m
}

// supportedIconPickerOptions returns the icon picker's selectable options,
// derived from supportedTypeIconClasses so the UI and any future agent tooling
// share the one catalog that app.css actually compiles. "lucide--box" is
// omitted: the box glyph is reserved as the picker's "Default icon" affordance
// (the empty value), so offering it again would be a duplicate box tile.
//
// The result feeds ui.IconPicker: Name is the submitted value (and the key the
// component matches a stored value against), Class is the iconify glyph, and
// Label is the human name.
func supportedIconPickerOptions() []ui.IconPickerOption {
	opts := make([]ui.IconPickerOption, 0, len(supportedTypeIconClasses))
	for _, class := range supportedTypeIconClasses {
		name := strings.TrimPrefix(class, "lucide--")
		if name == "box" {
			continue
		}
		opts = append(opts, ui.IconPickerOption{
			Name:  name,
			Class: class,
			Label: iconOptionLabel(name),
		})
	}
	return opts
}

// iconOptionLabel title-cases a bare kebab icon name for display
// ("file-text" → "File Text").
func iconOptionLabel(name string) string {
	words := strings.Split(name, "-")
	for i, w := range words {
		if w == "" {
			continue
		}
		words[i] = strings.ToUpper(w[:1]) + w[1:]
	}
	return strings.Join(words, " ")
}

// typeIconClass resolves a schema-declared icon value to an iconify class that
// is compiled into app.css. Outcomes:
//   - undeclared ("") → the generic box;
//   - a known Lucide name, however spelled ("FileText", "file_text",
//     "lucide:FileText", "lucide--file-text") → its catalog class;
//   - an unknown name → the generic box (never literal text);
//   - a raw glyph (emoji/symbol, any non-ASCII rune) → "" so the caller renders
//     the value as text.
func typeIconClass(icon string) string {
	s := strings.TrimSpace(icon)
	if s == "" {
		return "lucide--box"
	}
	if hasNonASCII(s) {
		return ""
	}
	if name := normalizeIconName(s); name != "" {
		if _, ok := supportedTypeIcons[name]; ok {
			return "lucide--" + name
		}
	}
	return "lucide--box"
}

// normalizeIconName reduces an ASCII icon value to a bare Lucide kebab name
// ("FileText" → "file-text", "file_text" → "file-text", "lucide--file-text" →
// "file-text"). It returns "" when the value is not a plausible icon name
// (empty, or containing punctuation other than kebab/underscore/space
// separators).
func normalizeIconName(icon string) string {
	s := strings.TrimSpace(icon)
	s = strings.TrimPrefix(s, "lucide--")
	s = strings.TrimPrefix(s, "lucide:")
	var b strings.Builder
	prevAlnum := false
	for _, r := range s {
		switch {
		case r == '-' || r == '_' || r == ' ':
			if b.Len() > 0 && !strings.HasSuffix(b.String(), "-") {
				b.WriteByte('-')
			}
			prevAlnum = false
		case r >= 'A' && r <= 'Z':
			if prevAlnum {
				b.WriteByte('-')
			}
			b.WriteRune(unicode.ToLower(r))
			prevAlnum = true
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			prevAlnum = true
		default:
			return ""
		}
	}
	return strings.Trim(b.String(), "-")
}

// hasNonASCII reports whether s contains a rune outside ASCII, i.e. a glyph
// such as an emoji rather than an icon name.
func hasNonASCII(s string) bool {
	for _, r := range s {
		if r > unicode.MaxASCII {
			return true
		}
	}
	return false
}
