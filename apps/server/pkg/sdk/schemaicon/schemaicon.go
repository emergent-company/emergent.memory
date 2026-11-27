// Package schemaicon is the shared catalog of Lucide icon names the UI can
// render for schema object types. The canonical list mirrors the gateway's
// supportedTypeIconClasses (apps/web-ui/gateway/type_icons.go): Tailwind's
// iconify plugin only emits CSS for the literal classes listed there, so an
// icon outside this set silently falls back to the generic box. Schema authors,
// agents, and the CLI validator all consult this one catalog instead of keeping
// private copies that drift.
package schemaicon

import "strings"

// catalog is the closed set of bare Lucide kebab-case names the UI renders.
// Keep in sync with apps/web-ui/gateway/type_icons.go supportedTypeIconClasses.
var catalog = []string{
	"box", "file-text", "file", "git-branch", "git-commit", "git-merge",
	"git-pull-request", "shield", "shield-check", "zap", "layers", "tag",
	"star", "heart", "alert-triangle", "bell", "book", "book-open", "bot",
	"briefcase", "calendar", "camera", "check", "check-circle", "circle",
	"clock", "cloud", "code", "cog", "settings", "database", "edit", "eye",
	"folder", "globe", "hash", "home", "image", "info", "key", "link",
	"list", "lock", "mail", "map", "map-pin", "message-circle", "monitor",
	"package", "paperclip", "pen", "phone", "play", "plus", "puzzle",
	"search", "send", "server", "share", "sparkles", "terminal", "trash",
	"user", "users", "wrench", "x",
}

// tablerCatalog is the curated set of bare Tabler kebab-case names the UI
// renders. Tabler (MIT) is the breadth family for schema.org-derived type
// icons; the list is deliberately curated rather than the full ~6.5k set,
// because the gateway compiles one CSS class per literal name and the iOS
// client needs an SF Symbol per name. Keep in sync with
// apps/web-ui/gateway/type_icons.go supportedTablerIconClasses.
var tablerCatalog = []string{
	"box", "user", "building", "map-pin", "calendar-event", "calendar",
	"file-text", "article", "book", "movie", "device-tv", "video", "photo",
	"browser", "world-www", "globe", "package", "tag", "trademark",
	"message-star", "star-half", "chef-hat", "building-store", "app-window",
	"code", "briefcase", "id-badge", "school", "list-check", "category",
	"stethoscope", "pill", "map",
	"building-bank", "building-hospital", "plane", "user-heart", "map-2", "camera", "bed", "car", "music", "users", "album", "microphone", "palette", "database", "device-mobile", "device-gamepad", "shopping-cart", "receipt", "tools", "tools-kitchen-2", "first-aid-kit", "shield",
}

// Family identifies the icon family that owns a name. A schema author writes
// either an explicit family prefix ("tabler--movie", "lucide--user") or a bare
// name, which defaults to Lucide for backward compatibility.
type Family string

const (
	FamilyLucide Family = "lucide"
	FamilyTabler Family = "tabler"
)

// set and tablerSet are built once and never mutated.
var (
	set       = buildSet(catalog)
	tablerSet = buildSet(tablerCatalog)
)

func buildSet(names []string) map[string]struct{} {
	m := make(map[string]struct{}, len(names))
	for _, name := range names {
		m[name] = struct{}{}
	}
	return m
}

// Names returns the canonical bare Lucide kebab-case names in catalog order.
// The returned slice is a copy, so callers cannot mutate the catalog.
func Names() []string {
	out := make([]string, len(catalog))
	copy(out, catalog)
	return out
}

// TablerNames returns the canonical bare Tabler kebab-case names. Author a
// Tabler icon with the explicit family prefix ("tabler--movie"); a bare name
// resolves to Lucide for backward compatibility.
func TablerNames() []string {
	out := make([]string, len(tablerCatalog))
	copy(out, tablerCatalog)
	return out
}

// Set returns a copy of the lookup set of canonical Lucide names.
func Set() map[string]struct{} {
	out := make(map[string]struct{}, len(set))
	for name := range set {
		out[name] = struct{}{}
	}
	return out
}

// TablerSet returns a copy of the lookup set of canonical Tabler names.
func TablerSet() map[string]struct{} {
	out := make(map[string]struct{}, len(tablerSet))
	for name := range tablerSet {
		out[name] = struct{}{}
	}
	return out
}

// parseFamily splits an icon value into its family and bare name. Bare names
// default to Lucide (the pre-Tabler behaviour); explicit "tabler--"/"tabler:"
// prefixes select Tabler.
func parseFamily(s string) (Family, string) {
	trimmed := strings.TrimSpace(s)
	lower := strings.ToLower(trimmed)
	for _, prefix := range []string{"tabler--", "tabler:", "tabler_"} {
		if strings.HasPrefix(lower, prefix) {
			return FamilyTabler, trimmed[len(prefix):]
		}
	}
	for _, prefix := range []string{"lucide--", "lucide:", "lucide_"} {
		if strings.HasPrefix(lower, prefix) {
			return FamilyLucide, trimmed[len(prefix):]
		}
	}
	return FamilyLucide, trimmed
}

// Normalize returns the canonical bare kebab-case name for s. It accepts the
// forms schema authors and agents actually write:
//
//   - bare kebab: "file-text"
//   - iconify class: "lucide--file-text"
//   - colon form: "lucide:file-text"
//   - PascalCase: "FileText"
//   - underscore/space separators: "file_text", "file text"
//
// Unrecognized input is still lowercased and kebab-cased; use IsRenderable to
// decide whether the result is in the catalog.
func Normalize(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	lower := strings.ToLower(s)
	for _, prefix := range []string{
		"lucide--", "lucide:", "lucide_",
		"tabler--", "tabler:", "tabler_",
	} {
		if strings.HasPrefix(lower, prefix) {
			s = s[len(prefix):]
			break
		}
	}
	parts := strings.FieldsFunc(s, func(r rune) bool {
		return r == '_' || r == ' ' || r == '\t' || r == '\n'
	})
	kebab := make([]string, 0, len(parts))
	for _, p := range parts {
		if v := camelToKebab(p); v != "" {
			kebab = append(kebab, v)
		}
	}
	return collapseHyphens(strings.Join(kebab, "-"))
}

func camelToKebab(s string) string {
	runes := []rune(s)
	var b strings.Builder
	b.Grow(len(runes) + 4)
	for i, r := range runes {
		switch {
		case r >= 'A' && r <= 'Z':
			if i > 0 {
				prev := runes[i-1]
				nextLower := i+1 < len(runes) && runes[i+1] >= 'a' && runes[i+1] <= 'z'
				prevLowerOrDigit := (prev >= 'a' && prev <= 'z') || (prev >= '0' && prev <= '9')
				prevUpper := prev >= 'A' && prev <= 'Z'
				if prevLowerOrDigit || (prevUpper && nextLower) {
					b.WriteByte('-')
				}
			}
			b.WriteRune(r + ('a' - 'A'))
		default:
			b.WriteRune(r)
		}
	}
	return collapseHyphens(b.String())
}

func collapseHyphens(s string) string {
	for strings.Contains(s, "--") {
		s = strings.ReplaceAll(s, "--", "-")
	}
	return strings.Trim(s, "-")
}

// isNonASCII reports whether s contains a rune outside ASCII. Such a string is
// treated as a text glyph (emoji or other symbol) rather than an icon name.
func isNonASCII(s string) bool {
	for _, r := range s {
		if r > 127 {
			return true
		}
	}
	return false
}

// IsRenderable reports whether s is something the UI can render: a non-ASCII
// glyph (emoji/symbol, rendered as text) or a catalog icon name after
// normalization. Empty input is not renderable.
func IsRenderable(s string) bool {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return false
	}
	if isNonASCII(trimmed) {
		return true
	}
	family, bare := parseFamily(trimmed)
	name := Normalize(bare)
	if family == FamilyTabler {
		_, ok := tablerSet[name]
		return ok
	}
	_, ok := set[name]
	return ok
}
