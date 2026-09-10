// Package acpslug provides the canonical ACP slug normalization shared by the
// agents domain and the MCP domain (which cannot import agents directly due to
// an import cycle).
package acpslug

import (
	"regexp"
	"strings"
)

// slugRegexp matches any non-alphanumeric, non-hyphen character.
var slugRegexp = regexp.MustCompile(`[^a-z0-9-]`)

// multiHyphenRegexp matches consecutive hyphens.
var multiHyphenRegexp = regexp.MustCompile(`-{2,}`)

// FromName normalizes a free-form agent name to an RFC 1123 DNS label:
// lowercase, replace non-alphanumeric with hyphens, collapse consecutive
// hyphens, trim leading/trailing hyphens, truncate to 63 characters.
func FromName(name string) string {
	s := strings.ToLower(name)
	s = slugRegexp.ReplaceAllString(s, "-")
	s = multiHyphenRegexp.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if len(s) > 63 {
		s = s[:63]
		s = strings.TrimRight(s, "-")
	}
	return s
}
