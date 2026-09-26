package blueprints

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/domain/mcp"
	"github.com/emergent-company/emergent.memory/domain/superadmin"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// newMCPAuthzHandler builds an MCPBlueprintToolHandler wired to a real
// superadmin repository over a fresh hermetic DB, returning the DB handle for
// seeding. create/publish only touch s.repo and the shared authority helper, so
// the nil cross-domain deps are safe.
func newMCPAuthzHandler(t *testing.T) (*MCPBlueprintToolHandler, bun.IDB) {
	t.Helper()
	db := connectTestDB(t)
	repo := NewRepository(db, testLogger())
	sa := superadmin.NewRepository(db)
	svc := NewService(ServiceParams{Repo: repo, Superadmin: sa, Log: testLogger()})
	return NewMCPBlueprintToolHandler(svc), db
}

// mcpResultText returns the single text block of a successful (nil go error)
// MCP ToolResult.
func mcpResultText(t *testing.T, res *mcp.ToolResult, err error) string {
	t.Helper()
	require.NoError(t, err)
	require.NotNil(t, res)
	require.Len(t, res.Content, 1)
	return res.Content[0].Text
}

// TestMCPBlueprintTools_GlobalWrite_RequireSuperadmin is the fail-first for
// issue #1041's MCP leg: a plain project member invoking blueprint-create with
// no project (global) or blueprint-publish targeting a global blueprint must be
// refused, while an active superadmin_full succeeds. A member's project-scoped
// create/publish stays allowed (no over-fix — the #1036 gateway seed flow).
func TestMCPBlueprintTools_GlobalWrite_RequireSuperadmin(t *testing.T) {
	h, db := newMCPAuthzHandler(t)
	ctx := context.Background()

	member := uuid.NewString()
	super := uuid.NewString()
	seedSuperadmin(t, ctx, db, super)

	memberCtx := auth.ContextWithUser(ctx, &auth.AuthUser{ID: member})
	superCtx := auth.ContextWithUser(ctx, &auth.AuthUser{ID: super})

	t.Run("global create refused for member", func(t *testing.T) {
		res, err := h.ExecuteBlueprintCreate(memberCtx, "", map[string]any{
			"name":     uniqueName("mcp-g-create"),
			"version":  "1.0.0",
			"manifest": map[string]any{"kind": "test"},
		})
		require.Contains(t, mcpResultText(t, res, err), "forbidden")
	})

	t.Run("global create allowed for superadmin", func(t *testing.T) {
		res, err := h.ExecuteBlueprintCreate(superCtx, "", map[string]any{
			"name":     uniqueName("mcp-g-create-sa"),
			"version":  "1.0.0",
			"manifest": map[string]any{"kind": "test"},
		})
		require.NotContains(t, mcpResultText(t, res, err), "forbidden")
	})

	t.Run("project-scoped create allowed for member", func(t *testing.T) {
		proj := uuid.NewString()
		res, err := h.ExecuteBlueprintCreate(memberCtx, proj, map[string]any{
			"name":     uniqueName("mcp-p-create"),
			"version":  "1.0.0",
			"manifest": map[string]any{"kind": "test"},
		})
		require.NotContains(t, mcpResultText(t, res, err), "forbidden")
	})

	// Seed a global draft directly through the repo (the gateway/superadmin seed
	// path), then prove the MCP publish tool denies/permits writes to it.
	seedGlobal := func(t *testing.T) *Blueprint {
		bp := &Blueprint{
			Name:      uniqueName("mcp-g-life"),
			Version:   "1.0.0",
			Status:    StatusDraft,
			Manifest:  []byte(`{"kind":"life"}`),
			ProjectID: nil, // global
		}
		require.NoError(t, h.svc.repo.Create(ctx, bp))
		return bp
	}

	t.Run("global publish refused for member", func(t *testing.T) {
		bp := seedGlobal(t)
		res, err := h.ExecuteBlueprintPublish(memberCtx, "", map[string]any{"id": bp.ID})
		require.Contains(t, mcpResultText(t, res, err), "forbidden")
	})

	t.Run("global publish allowed for superadmin", func(t *testing.T) {
		bp := seedGlobal(t)
		res, err := h.ExecuteBlueprintPublish(superCtx, "", map[string]any{"id": bp.ID})
		require.NotContains(t, mcpResultText(t, res, err), "forbidden")
	})

	t.Run("project-scoped publish allowed for member", func(t *testing.T) {
		proj := uuid.NewString()
		res, err := h.ExecuteBlueprintCreate(memberCtx, proj, map[string]any{
			"name":     uniqueName("mcp-p-pub"),
			"version":  "1.0.0",
			"manifest": map[string]any{"kind": "test"},
		})
		createText := mcpResultText(t, res, err)

		var created struct {
			ID string `json:"id"`
		}
		require.NoError(t, json.Unmarshal([]byte(createText), &created))
		require.NotEmpty(t, created.ID)

		pubRes, pubErr := h.ExecuteBlueprintPublish(memberCtx, proj, map[string]any{"id": created.ID})
		require.NotContains(t, mcpResultText(t, pubRes, pubErr), "forbidden")
	})
}

// TestMCPBlueprintTools_GlobalNewVersion_RequireSuperadmin is the repair
// fail-first for blueprint-new-version: forking with no project context lands a
// NEW global version (project_id IS NULL), the same global-catalogue write the
// REST NewVersion handler gates with superadmin_full. A member must be refused,
// a superadmin_full allowed, and a member's project-scoped fork (global →
// private) must stay allowed (no over-restriction).
func TestMCPBlueprintTools_GlobalNewVersion_RequireSuperadmin(t *testing.T) {
	h, db := newMCPAuthzHandler(t)
	ctx := context.Background()

	member := uuid.NewString()
	super := uuid.NewString()
	seedSuperadmin(t, ctx, db, super)

	memberCtx := auth.ContextWithUser(ctx, &auth.AuthUser{ID: member})
	superCtx := auth.ContextWithUser(ctx, &auth.AuthUser{ID: super})

	seedGlobal := func(t *testing.T) *Blueprint {
		bp := &Blueprint{
			Name:      uniqueName("mcp-g-fork"),
			Version:   "1.0.0",
			Status:    StatusDraft,
			Manifest:  []byte(`{"kind":"fork"}`),
			ProjectID: nil, // global
		}
		require.NoError(t, h.svc.repo.Create(ctx, bp))
		return bp
	}

	t.Run("global fork (no project) refused for member", func(t *testing.T) {
		bp := seedGlobal(t)
		res, err := h.ExecuteBlueprintNewVersion(memberCtx, "", map[string]any{
			"id":      bp.ID,
			"version": "2.0.0",
		})
		require.Contains(t, mcpResultText(t, res, err), "forbidden")
	})

	t.Run("global fork (no project) allowed for superadmin", func(t *testing.T) {
		bp := seedGlobal(t)
		res, err := h.ExecuteBlueprintNewVersion(superCtx, "", map[string]any{
			"id":      bp.ID,
			"version": "3.0.0",
		})
		require.NotContains(t, mcpResultText(t, res, err), "forbidden")
	})

	t.Run("project-scoped fork allowed for member", func(t *testing.T) {
		bp := seedGlobal(t)
		proj := uuid.NewString()
		res, err := h.ExecuteBlueprintNewVersion(memberCtx, proj, map[string]any{
			"id":      bp.ID,
			"version": "4.0.0",
		})
		require.NotContains(t, mcpResultText(t, res, err), "forbidden")
	})
}
