package agentcompat

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/agents"
)

// TestClassifyErrorResumeClaim pins how a resume that lost the pause claim is
// reported on the OpenAI-compatible surface (issues #887, #912). Both reasons a
// claim can match no rows are state conflicts (409 / invalid_state), but only the
// benign one may be rendered as "the turn is running under another claim": a
// pause that went terminal (cancelled/failed) must report its real status.
func TestClassifyErrorResumeClaim(t *testing.T) {
	alreadyResumed := fmt.Errorf("%w: run run-1", agents.ErrRunAlreadyResumed)
	notResumable := fmt.Errorf("%w: run run-1 left input-required with status cancelled",
		agents.ErrRunPauseNotResumable)

	t.Run("already_resumed_is_benign_conflict", func(t *testing.T) {
		assert.Equal(t, http.StatusConflict, classifyError(alreadyResumed))
		assert.Equal(t, "invalid_state", classifyErrorType(alreadyResumed))
	})

	t.Run("terminal_pause_is_conflict_with_real_status", func(t *testing.T) {
		assert.Equal(t, http.StatusConflict, classifyError(notResumable))
		assert.Equal(t, "invalid_state", classifyErrorType(notResumable))
	})

	t.Run("unrelated_error_keeps_existing_classification", func(t *testing.T) {
		assert.Equal(t, http.StatusBadRequest, classifyError(errors.New("tool result missing")))
	})
}

// TestResumeErrorMessage pins the in-stream error text (issue #912): the
// "running under that claim" wording is only for a pause another caller actually
// claimed. A terminal pause keeps its own message, which names the real status.
func TestResumeErrorMessage(t *testing.T) {
	alreadyResumed := fmt.Errorf("%w: run run-1", agents.ErrRunAlreadyResumed)
	msg := resumeErrorMessage(alreadyResumed)
	assert.Contains(t, msg, "already resumed by another caller")
	assert.Contains(t, msg, "running under that claim")

	notResumable := fmt.Errorf("%w: run run-1 left input-required with status cancelled",
		agents.ErrRunPauseNotResumable)
	msg = resumeErrorMessage(notResumable)
	assert.NotContains(t, msg, "running under that claim",
		"a cancelled pause must not be described as a running turn")
	assert.NotContains(t, msg, "already resumed by another caller")
	assert.Contains(t, msg, "cancelled")

	require.NotEqual(t, "", resumeErrorMessage(errors.New("boom")))
}
