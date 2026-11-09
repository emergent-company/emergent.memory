package graph

import (
	"regexp"
	"strings"

	"github.com/google/uuid"
)

var keySlugRegexp = regexp.MustCompile(`[^a-z0-9]+`)

// resolveObjectName derives the first-class title for a write. An explicit
// request name wins; otherwise the established properties convention is used
// (name, then title). An empty/whitespace explicit name clears the title.
func resolveObjectName(explicit *string, props map[string]any) *string {
	if explicit != nil {
		return trimName(*explicit)
	}
	if props != nil {
		if v, ok := props["name"].(string); ok {
			if n := trimName(v); n != nil {
				return n
			}
		}
		if v, ok := props["title"].(string); ok {
			if n := trimName(v); n != nil {
				return n
			}
		}
	}
	return nil
}

func trimName(s string) *string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	return &s
}

// autoWorkKey mints the internal dedup key for a board-enabled object created
// without one. The claim lock keys on (project, type, key), so a work item must
// have a key; it is never shown to users.
func autoWorkKey(typeName string, name *string) string {
	base := ""
	if name != nil {
		base = keySlug(*name)
	}
	if base == "" {
		base = keySlug(typeName)
	}
	if base == "" {
		base = "item"
	}
	if len(base) > 32 {
		base = strings.Trim(base[:32], "-")
	}
	suffix := strings.ReplaceAll(uuid.New().String(), "-", "")[:8]
	return base + "-" + suffix
}

func keySlug(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = keySlugRegexp.ReplaceAllString(s, "-")
	return strings.Trim(s, "-")
}

// namesEqual compares two optional titles for change detection.
func namesEqual(a, b *string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}
