package graph

import (
	"regexp"
	"strings"

	"github.com/google/uuid"
)

var keySlugRegexp = regexp.MustCompile(`[^a-z0-9]+`)

// resolveObjectName derives the first-class title for a write. The explicit
// request name is the only source: properties["name"] and properties["title"]
// are ordinary properties and are never consulted as a title (#824). An
// empty/whitespace explicit name clears the title, and an absent one leaves it
// unset — a nameless write is accepted, not rejected, because a provider record
// or an extraction result legitimately has no title yet.
func resolveObjectName(explicit *string) *string {
	if explicit == nil {
		return nil
	}
	return trimName(*explicit)
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
// have a key; it is never shown to users. The trailing 12 hex characters match
// the machine-key shape (`<type>-<12 hex>`) that the display layer and the
// migration 00280 backfill recognise as a non-title, so a minted key never
// becomes the object's label.
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
	suffix := strings.ReplaceAll(uuid.New().String(), "-", "")[:12]
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
