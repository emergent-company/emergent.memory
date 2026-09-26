package agents

import (
	"context"
	"log/slog"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"google.golang.org/adk/tool"

	"github.com/emergent-company/emergent.memory/domain/mcp"
	"github.com/emergent-company/emergent.memory/internal/testdb"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// fakeTool is a minimal tool.Tool for exercising StripOperatorTools by name.
type fakeTool struct{ name string }

func (f fakeTool) Name() string        { return f.name }
func (f fakeTool) Description() string { return "" }
func (f fakeTool) IsLongRunning() bool { return false }

// TestStripOperatorTools proves the resolution-level filter strips
// superadmin-only operator tools from a non-superadmin principal's resolved
// tool set (and keeps them for a superadmin_full principal). It is the
// toolset-side complement to mcp.Service.ExecuteTool's dispatch-time gate
// (issue #948).
func TestStripOperatorTools(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	ctx := context.Background()
	tdb := testdb.SetupTestDBOrFail(t, ctx, "agents_strip_operator")
	t.Cleanup(tdb.Close)
	dbc := tdb.DB

	memberID := uuid.NewString()
	_, err := dbc.NewRaw(`INSERT INTO core.user_profiles (id, zitadel_user_id) VALUES (?, ?)`,
		memberID, "strip-member-user").Exec(ctx)
	require.NoError(t, err)

	superID := uuid.NewString()
	_, err = dbc.NewRaw(`INSERT INTO core.user_profiles (id, zitadel_user_id) VALUES (?, ?)`,
		superID, "strip-super-user").Exec(ctx)
	require.NoError(t, err)
	_, err = dbc.NewRaw(`INSERT INTO core.superadmins (user_id, role) VALUES (?, 'superadmin_full')`, superID).Exec(ctx)
	require.NoError(t, err)

	mcpSvc := mcp.NewService(mcp.ServiceParams{DB: dbc, Cfg: tdb.Config, Log: slog.Default()})
	tp := NewToolPool(ToolPoolConfig{MCPService: mcpSvc})

	tools := []tool.Tool{
		fakeTool{name: "entity-query"},
		fakeTool{name: "embedding-pause"},
		fakeTool{name: "provider-configure-org"},
	}

	names := func(ts []tool.Tool) map[string]bool {
		m := make(map[string]bool, len(ts))
		for _, tt := range ts {
			m[tt.Name()] = true
		}
		return m
	}

	memberCtx := auth.ContextWithUser(ctx, &auth.AuthUser{ID: memberID, Scopes: []string{"admin"}})
	superCtx := auth.ContextWithUser(ctx, &auth.AuthUser{ID: superID, Scopes: []string{"admin"}})

	got := names(tp.StripOperatorTools(memberCtx, tools))
	require.True(t, got["entity-query"], "ordinary tool must remain for a project member")
	require.False(t, got["embedding-pause"], "operator tool must be stripped for a project member")
	require.False(t, got["provider-configure-org"], "operator tool must be stripped for a project member")

	got = names(tp.StripOperatorTools(superCtx, tools))
	require.True(t, got["entity-query"], "ordinary tool must remain for superadmin_full")
	require.True(t, got["embedding-pause"], "operator tool must remain for superadmin_full")
	require.True(t, got["provider-configure-org"], "operator tool must remain for superadmin_full")

	// Principal-less context fails closed: operator tools are stripped.
	got = names(tp.StripOperatorTools(ctx, tools))
	require.False(t, got["embedding-pause"], "principal-less context must strip operator tools (fail closed)")
	require.False(t, got["provider-configure-org"], "principal-less context must strip operator tools (fail closed)")
}
