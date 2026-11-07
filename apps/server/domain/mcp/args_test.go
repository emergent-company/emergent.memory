package mcp

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// TestExecuteToolRejectsNonBooleanArgs pins the catalogue-wide boolean guard
// (#24): every handler used to read args[name].(bool), so a caller that sent
// "true" as a string silently got the false branch. The refusal happens before
// dispatch, so it applies to every tool that declares a boolean property, not
// only the ones with a hand-written strict read.
func TestExecuteToolRejectsNonBooleanArgs(t *testing.T) {
	svc := &Service{}
	svc.GetToolDefinitions() // populates the package-level toolIndex
	ctx := ContextWithTrustedInternal(context.Background(), true)

	cases := []struct {
		tool string
		arg  string
	}{
		{"schema-list", "include_deprecated"},
		{"schema-assign", "force"},
		{"schema-assignment-update", "active"},
		{"schema-migrate-execute", "force"},
		{"blueprint-apply", "mergeTypes"},
	}
	for _, tc := range cases {
		t.Run(tc.tool, func(t *testing.T) {
			_, err := svc.ExecuteTool(ctx, uuid.NewString(), tc.tool, map[string]any{
				tc.arg: "true",
			})
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.arg)
			require.Contains(t, err.Error(), "boolean")
		})
	}
}

// TestValidateBooleanArgsUnits covers the helper's edge semantics: absent and
// null keep their documented default, real booleans pass, and non-boolean
// properties are not inspected.
func TestValidateBooleanArgsUnits(t *testing.T) {
	def := &ToolDefinition{
		Name: "test-tool",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]PropertySchema{
				"flag":   {Type: "boolean"},
				"name":   {Type: "string"},
				"nested": {Type: "object"},
			},
		},
	}

	require.NoError(t, validateBooleanArgs(def, nil))
	require.NoError(t, validateBooleanArgs(def, map[string]any{}))
	require.NoError(t, validateBooleanArgs(def, map[string]any{"flag": nil}))
	require.NoError(t, validateBooleanArgs(def, map[string]any{"flag": true}))
	require.NoError(t, validateBooleanArgs(def, map[string]any{"flag": false}))
	// A string property holding "true" is not a boolean declaration and is left
	// to the handler.
	require.NoError(t, validateBooleanArgs(def, map[string]any{"name": "true"}))

	err := validateBooleanArgs(def, map[string]any{"flag": "true"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "flag")
}
