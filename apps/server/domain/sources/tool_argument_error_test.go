package sources

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/internal/toolargscan"
)

// TestNoPlainToolArgumentValidationErrors extends issue #668's regression scan
// past the domain/mcp package boundary. This domain's github-pr-diff /
// github-review-submit tools and the github_app credential path feed the same
// tools/call channel: a plain fmt.Errorf validator here collapses to
// "An internal error occurred", so the agent never learns which argument to fix.
//
// The scan is shared with domain/mcp (internal/toolargscan) so both packages
// enforce one rule: an argument refusal is apperror.NewValidation(f).
func TestNoPlainToolArgumentValidationErrors(t *testing.T) {
	// The files that implement a tool or validate a caller-supplied source
	// configuration. Connector internals (a fetch cap, a body-size refusal) are
	// deliberately not in the set: they are not caller argument validation.
	files := []string{
		"review_tools.go",
		"github_app_provider.go",
		"gmail_tools.go",
		"source_admin_tools.go",
		"source_tools.go",
	}

	findings, err := toolargscan.Scan(files, toolargscan.Exemptions{
		Messages: map[string]string{
			// The connection-test answer for a trigger source: a platform fact
			// the caller cannot correct by changing an argument.
			"connection testing is not supported for trigger sources": "platform fact",
		},
	})
	require.NoError(t, err)

	offenders := make([]string, 0, len(findings))
	for _, finding := range findings {
		offenders = append(offenders, finding.String())
	}

	assert.Empty(t, offenders,
		"a tool argument refusal must return apperror.NewValidation(f) (issue #668) so the tools/call channel can relay it")
}
