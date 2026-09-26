package mcp

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// TestExecuteToolSensitiveAdminGate proves the sensitive admin-scoped tools
// (token-*, provider-configure-project, project-create) are gated in-process on
// the superadmin_full authority — NOT trusted-internal. Their `admin` scope is
// token-only (no project role maps to it), so a non-admin member's TRUSTED
// session-UI run (TrustedInternal=true, no superadmin grant) must be refused —
// the residual #1018 closes here. A superadmin_full run reaches the tool's own
// dispatch (no over-correction).
func TestExecuteToolSensitiveAdminGate(t *testing.T) {
	dbc, memberID, superID := setupGateDB(t)

	svc := &Service{db: dbc}
	_ = svc.GetToolDefinitions() // populate the tool index GetToolByName reads

	// A trusted run carries TrustedInternal=true (session UI / scheduler) AND the
	// run's originating principal (the member user, no superadmin grant).
	memberCtx := ContextWithTrustedInternal(
		auth.ContextWithUser(context.Background(), &auth.AuthUser{ID: memberID}),
		true,
	)
	superCtx := ContextWithTrustedInternal(
		auth.ContextWithUser(context.Background(), &auth.AuthUser{ID: superID}),
		true,
	)
	projectID := uuid.New().String()

	for _, tool := range []string{
		"token-list", "token-create", "token-get", "token-revoke",
		"provider-configure-project",
		"project-create",
	} {
		t.Run("member trusted run refused on "+tool, func(t *testing.T) {
			_, err := svc.ExecuteTool(memberCtx, projectID, tool, map[string]any{})
			require.Error(t, err, "a non-admin member's trusted run must be refused on %s", tool)
			require.Contains(t, err.Error(), "superadmin", "refusal must come from the superadmin gate, got %v", err)
		})
	}

	t.Run("superadmin trusted run reaches token-create dispatch", func(t *testing.T) {
		_, err := svc.ExecuteTool(superCtx, projectID, "token-create", map[string]any{})
		require.Error(t, err, "token-create with empty args must error on its own validation")
		require.Contains(t, err.Error(), "name", "must reach the tool's own argument validation, got %v", err)
	})
}
