package mcpregistry

import (
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSlugifyServerName pins the canonical server-name slug used for external
// pool keys AND call routing. The algorithm is byte-for-byte the original
// agents.slugifyFunctionPart so already-synced pool keys never change.
func TestSlugifyServerName(t *testing.T) {
	cases := []struct{ in, want string }{
		{"E2E MCP 123", "e2e_mcp_123"},
		{"E2E  MCP   123", "e2e_mcp_123"}, // runs of separators collapse to a single _
		{"MyCool-Server!!v2", "mycool_server_v2"},
		{"MixedCASE123", "mixedcase123"},
		{"already_clean", "already_clean"},
		{"_leading", "leading"},
		{"trailing_", "trailing"},
		{"_both_", "both"},
		{"", "server"}, // empty input
		{"   ", "server"},
		{"!!!", "server"}, // nothing left after collapsing
	}
	for _, c := range cases {
		assert.Equal(t, c.want, SlugifyServerName(c.in), "SlugifyServerName(%q)", c.in)
	}
}

// TestSlugifyServerName_AlwaysValidFunctionName asserts the slug always obeys
// the LLM function-name CHARSET — the reason slugging exists at all. (Length is
// bounded separately: externalToolKey truncates the slug to fit the 64-char key
// cap, so the slug itself may exceed 64 for pathological names.)
func TestSlugifyServerName_AlwaysValidFunctionName(t *testing.T) {
	names := []string{
		"E2E MCP 123",
		"MyCool-Server!!v2",
		"server/with/slashes & dots.v1",
		"!!!",
		"",
		"normal",
		"my_server", // underscores preserved as single separators
		strings.Repeat("s", 120),
	}
	for _, name := range names {
		slug := SlugifyServerName(name)
		require.Regexp(t, regexp.MustCompile(`^[a-z0-9_]+$`), slug,
			"SlugifyServerName(%q) = %q must be lowercase-alnum/underscore only", name, slug)
	}
}
