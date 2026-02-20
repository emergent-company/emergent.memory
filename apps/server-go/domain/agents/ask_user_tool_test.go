package agents

import (
	"log/slog"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// BuildAskUserTool tests (task 10.6)
// =============================================================================

func TestBuildAskUserTool_Success(t *testing.T) {
	deps := AskUserToolDeps{
		Repo:       nil, // Not called during build
		Logger:     slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})),
		ProjectID:  "project-1",
		AgentID:    "agent-1",
		RunID:      "run-1",
		PauseState: &AskPauseState{},
		UserID:     "user-1",
	}

	tool, err := BuildAskUserTool(deps)
	require.NoError(t, err)
	require.NotNil(t, tool)

	assert.Equal(t, ToolNameAskUser, tool.Name())
	assert.Equal(t, "ask_user", tool.Name())
	assert.NotEmpty(t, tool.Description())
	assert.Contains(t, tool.Description(), "question")
}

func TestBuildAskUserTool_NameIsConstant(t *testing.T) {
	deps := AskUserToolDeps{
		Logger:     slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})),
		PauseState: &AskPauseState{},
	}

	tool, err := BuildAskUserTool(deps)
	require.NoError(t, err)

	// Verify it matches the constant used for tool registration
	assert.Equal(t, "ask_user", tool.Name())
	assert.Equal(t, ToolNameAskUser, tool.Name())
}

// =============================================================================
// AskPauseState tests
// =============================================================================

func TestAskPauseState_InitialState(t *testing.T) {
	state := &AskPauseState{}

	assert.False(t, state.ShouldPause())
	assert.Equal(t, "", state.QuestionID())
}

func TestAskPauseState_RequestPause(t *testing.T) {
	state := &AskPauseState{}

	state.RequestPause("q-123")

	assert.True(t, state.ShouldPause())
	assert.Equal(t, "q-123", state.QuestionID())
}

func TestAskPauseState_MultiplePauses(t *testing.T) {
	state := &AskPauseState{}

	state.RequestPause("q-1")
	assert.True(t, state.ShouldPause())
	assert.Equal(t, "q-1", state.QuestionID())

	// Second pause overwrites the question ID
	state.RequestPause("q-2")
	assert.True(t, state.ShouldPause())
	assert.Equal(t, "q-2", state.QuestionID())
}

// =============================================================================
// parseQuestionOptions tests
// =============================================================================

func TestParseQuestionOptions_NoOptionsKey(t *testing.T) {
	args := map[string]any{
		"question": "What do you think?",
	}
	result := parseQuestionOptions(args)
	assert.Nil(t, result)
}

func TestParseQuestionOptions_EmptyArray(t *testing.T) {
	args := map[string]any{
		"options": []any{},
	}
	result := parseQuestionOptions(args)
	assert.Nil(t, result)
}

func TestParseQuestionOptions_WrongType(t *testing.T) {
	args := map[string]any{
		"options": "not an array",
	}
	result := parseQuestionOptions(args)
	assert.Nil(t, result)
}

func TestParseQuestionOptions_ValidOptions(t *testing.T) {
	args := map[string]any{
		"options": []any{
			map[string]any{"label": "Red", "value": "red"},
			map[string]any{"label": "Blue", "value": "blue", "description": "The color blue"},
		},
	}
	result := parseQuestionOptions(args)
	require.Len(t, result, 2)

	assert.Equal(t, "Red", result[0].Label)
	assert.Equal(t, "red", result[0].Value)
	assert.Empty(t, result[0].Description)

	assert.Equal(t, "Blue", result[1].Label)
	assert.Equal(t, "blue", result[1].Value)
	assert.Equal(t, "The color blue", result[1].Description)
}

func TestParseQuestionOptions_SkipsInvalidItems(t *testing.T) {
	args := map[string]any{
		"options": []any{
			map[string]any{"label": "Good", "value": "good"},
			"not a map",                              // skipped
			map[string]any{"label": "", "value": ""}, // skipped: empty
			map[string]any{"label": "Only label"},    // skipped: no value
			map[string]any{"value": "only_value"},    // skipped: no label
			map[string]any{"label": "Also Good", "value": "also_good"},
		},
	}
	result := parseQuestionOptions(args)
	require.Len(t, result, 2)

	assert.Equal(t, "Good", result[0].Label)
	assert.Equal(t, "Also Good", result[1].Label)
}

func TestParseQuestionOptions_SingleOption(t *testing.T) {
	args := map[string]any{
		"options": []any{
			map[string]any{"label": "Yes", "value": "yes", "description": "Confirm"},
		},
	}
	result := parseQuestionOptions(args)
	require.Len(t, result, 1)
	assert.Equal(t, "Yes", result[0].Label)
	assert.Equal(t, "yes", result[0].Value)
	assert.Equal(t, "Confirm", result[0].Description)
}
