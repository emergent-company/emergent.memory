package agents

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildSubmitTool_Name(t *testing.T) {
	tool, err := buildSubmitTool(&WorkTerminatorState{})
	require.NoError(t, err)
	assert.Equal(t, "submit", tool.Name())
	assert.Contains(t, tool.Description(), "end the run")
}

func TestFinalizeSubmit_RecordsSummary(t *testing.T) {
	terminator := &WorkTerminatorState{}
	finalizeSubmit(terminator, "implemented and verified")

	require.True(t, terminator.ShouldFinalize())
	assert.Equal(t, WorkTerminatorSubmitted, terminator.Kind())
	summary := terminator.Summary()
	require.NotNil(t, summary)
	assert.Equal(t, WorkTerminatorSubmitted, summary["work_terminator"])
	assert.Equal(t, "implemented and verified", summary["submission"])
}

func TestFinalizeSubmit_FirstCallWins(t *testing.T) {
	terminator := &WorkTerminatorState{}
	finalizeSubmit(terminator, "first")
	finalizeSubmit(terminator, "second")

	assert.Equal(t, "first", terminator.Summary()["submission"])
}
