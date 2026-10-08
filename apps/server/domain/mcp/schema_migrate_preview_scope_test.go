package mcp

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestTypeScope_ExecuteSchemaMigratePreview_FailClosed pins the fail-closed
// guard on schema-migrate-preview: its plan embeds object/relationship type
// names that cannot be faithfully scrubbed, so a scoped agent must not receive
// it (#1695).
func TestTypeScope_ExecuteSchemaMigratePreview_FailClosed(t *testing.T) {
	svc := &Service{}
	args := map[string]any{"from_schema_id": "a", "to_schema_id": "b"}

	// Unrestricted: the guard passes and the nil-service path returns no error,
	// proving the guard is the only behaviour change.
	res, err := svc.executeSchemaMigratePreview(context.Background(), "proj", args)
	require.NoError(t, err)
	require.NotNil(t, res)

	// Any restriction fails closed before touching the schemas service.
	for _, tc := range []struct {
		name  string
		scope *TypeScope
	}{
		{"object", NewTypeScope([]string{"Law"}, nil)},
		{"relationship", NewTypeScope(nil, []string{"CITES"})},
	} {
		ctx := ContextWithTypeScope(context.Background(), tc.scope)
		_, err := svc.executeSchemaMigratePreview(ctx, "proj", args)
		require.Error(t, err, "%s scope must fail closed", tc.name)
	}
}
