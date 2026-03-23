package graphexplore

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// lucideToEmoji maps common Lucide icon names (from the schema registry
// ui_config) to single-character emoji equivalents for the sidebar.
var lucideToEmoji = map[string]string{
	"FileText":       "📄",
	"File":           "📄",
	"GitBranch":      "🔀",
	"GitCommit":      "⊙",
	"GitMerge":       "🔀",
	"GitPullRequest": "↗",
	"Shield":         "🛡",
	"ShieldCheck":    "🛡",
	"Zap":            "⚡",
	"Layers":         "◫",
	"Tag":            "🏷",
	"Star":           "⭐",
	"Heart":          "♥",
	"AlertTriangle":  "⚠",
	"Bell":           "🔔",
	"Book":           "📖",
	"BookOpen":       "📖",
	"Box":            "📦",
	"Briefcase":      "💼",
	"Calendar":       "📅",
	"Camera":         "📷",
	"Check":          "✓",
	"CheckCircle":    "✓",
	"Circle":         "●",
	"Clock":          "⏱",
	"Cloud":          "☁",
	"Code":           "⟨⟩",
	"Cog":            "⚙",
	"Settings":       "⚙",
	"Database":       "🗄",
	"Edit":           "✏",
	"Eye":            "👁",
	"Folder":         "📁",
	"Globe":          "🌐",
	"Hash":           "#",
	"Home":           "🏠",
	"Image":          "🖼",
	"Info":           "ℹ",
	"Key":            "🔑",
	"Link":           "🔗",
	"List":           "☰",
	"Lock":           "🔒",
	"Mail":           "✉",
	"Map":            "🗺",
	"MapPin":         "📍",
	"MessageCircle":  "💬",
	"Monitor":        "🖥",
	"Package":        "📦",
	"Paperclip":      "📎",
	"Pen":            "✏",
	"Phone":          "📞",
	"Play":           "▶",
	"Plus":           "+",
	"Puzzle":         "🧩",
	"Search":         "🔍",
	"Send":           "➤",
	"Server":         "🖥",
	"Share":          "↗",
	"Sparkles":       "✨",
	"Terminal":       "⌨",
	"Trash":          "🗑",
	"User":           "👤",
	"Users":          "👥",
	"Wrench":         "🔧",
	"X":              "✕",
}

// resolveIcon converts icon strings from the schema registry into a displayable
// character. Lucide icon names get mapped to emoji; single runes and emoji pass
// through; long unrecognised strings fall back to the first letter of typeName.
func resolveIcon(icon, typeName string) string {
	if icon == "" {
		return firstLetter(typeName)
	}

	// Check Lucide mapping (case-insensitive)
	for name, emoji := range lucideToEmoji {
		if strings.EqualFold(icon, name) {
			return emoji
		}
	}

	// If it's already a single rune (emoji or letter), use it directly
	if utf8.RuneCountInString(icon) == 1 {
		return icon
	}

	// If it looks like a Lucide name (PascalCase, no spaces, ASCII), use first letter of type
	if isLikelyIconName(icon) {
		return firstLetter(typeName)
	}

	// Otherwise it might be a short emoji sequence — use it
	if utf8.RuneCountInString(icon) <= 3 {
		return icon
	}

	return firstLetter(typeName)
}

// isLikelyIconName returns true if s looks like a PascalCase icon name
// (e.g. "FileText", "GitBranch") rather than an emoji or symbol.
func isLikelyIconName(s string) bool {
	for _, r := range s {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			return false
		}
	}
	return len(s) > 1
}

func firstLetter(s string) string {
	if s == "" {
		return "?"
	}
	return strings.ToUpper(string([]rune(s)[0:1]))
}

// iconFontSize returns the CSS font-size for a type icon.
// Emoji icons (multi-rune) get a slightly larger size, single-letter gets compact.
func iconFontSize(icon string) string {
	if utf8.RuneCountInString(icon) > 1 {
		return "13px"
	}
	return "11px"
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
