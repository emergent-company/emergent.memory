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
// It scans the whole package rather than a hand-picked file list. A list is a
// promise that someone will remember to add the next file that grows a
// caller-facing validator, and review round 2 found twelve keyword sites outside
// the original five (store_source_apps.go among them). Deliberate non-argument
// errors are named one at a time in Exemptions, with the reason.
//
// The scan is shared with domain/mcp (internal/toolargscan) so both packages
// enforce one rule: an argument refusal is apperror.NewValidation(f).
func TestNoPlainToolArgumentValidationErrors(t *testing.T) {
	findings, err := toolargscan.ScanDir(".", toolargscan.Exemptions{
		Messages: map[string]string{
			// The connection-test answer for a trigger source: a platform fact
			// the caller cannot correct by changing an argument.
			"connection testing is not supported for trigger sources": "platform fact",

			// Inbound webhook verification sentinels (source_events_dispatch.go).
			// The handler matches them with errors.Is to pick a status and a
			// retry policy, so they must keep their identity; none of them is
			// rendered into a tool result.
			"sources: source app not found":      "webhook sentinel",
			"sources: invalid webhook signature": "webhook sentinel",
			"missing delivery id":                "webhook sentinel",

			// The board's trigger-invocation resolver: a miss leaves the item
			// without an invocation, and the caller ignores the error
			// (domain/agents/work_invocation.go). The keys are the full
			// messages so they cannot silence a tool-surface validator that
			// happens to share the phrase.
			"sources: invalid project id":        "board resolver (error ignored)",
			"sources: invalid source trigger id": "board resolver (error ignored)",

			// Store guards: the value is server-derived, so a gap is a bug in
			// the caller's plumbing, not something a tool caller can correct.
			"project context is required for source trigger/event rows": "store guard",

			// Connector and sync-worker internals. These run inside a sync job
			// or a connector contract check; their text is recorded on the job
			// row or skipped item, never returned through tools/call.
			"trale: MCP server is missing required tools": "connector contract check",
			"web: page %q not found in the crawl result":  "sync selector miss",
			"web: skipping unsupported content type":      "per-item sync skip",
			"unknown provider %q":                         "job-row failure",
		},
	})
	require.NoError(t, err)

	offenders := make([]string, 0, len(findings))
	for _, finding := range findings {
		offenders = append(offenders, finding.String())
	}

	assert.Empty(t, offenders,
		"a caller-facing refusal must be an app error (issue #668) so the tools/call channel can relay it")
}
