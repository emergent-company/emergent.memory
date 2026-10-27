package mcp

import (
	"context"
	"strings"
	"testing"

	"github.com/emergent-company/emergent.memory/domain/branches"
	"github.com/emergent-company/emergent.memory/domain/graph"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// branchToolNames is the full branch surface exposed over MCP. Every name must
// have a definition, a required scope, and a dispatch path.
var branchToolNames = []string{
	"graph-branch-list",
	"graph-branch-get",
	"graph-branch-create",
	"graph-branch-update",
	"graph-branch-merge",
	"graph-branch-merge-readiness",
	"graph-branch-fork",
	"graph-branch-delete",
}

// TestBranchToolsRegistered proves each branch tool is listed in the runtime
// catalog with a non-empty description, an object input schema and a required
// scope. Without a scope entry the tool would be invisible to
// LookupToolScope and therefore to agent tool-group policy.
func TestBranchToolsRegistered(t *testing.T) {
	svc := &Service{}
	defs := svc.GetToolDefinitions()

	byName := make(map[string]ToolDefinition, len(defs))
	for _, d := range defs {
		byName[d.Name] = d
	}

	for _, name := range branchToolNames {
		def, ok := byName[name]
		require.True(t, ok, "%s must be in GetToolDefinitions", name)
		assert.NotEmpty(t, def.Description, "%s must have a description", name)
		assert.Equal(t, "object", def.InputSchema.Type, "%s input schema type", name)
		assert.NotEmpty(t, def.RequiredScope, "%s must have a required scope", name)

		scope, ok := LookupToolScope(name)
		assert.True(t, ok, "%s must be in the static scope table", name)
		assert.Equal(t, def.RequiredScope, scope, "%s scope table/definition mismatch", name)
	}
}

// TestBranchToolRequiredArgs pins the argument contract callers depend on.
func TestBranchToolRequiredArgs(t *testing.T) {
	svc := &Service{}
	defs := svc.GetToolDefinitions()

	wantRequired := map[string][]string{
		"graph-branch-get":             {"branch"},
		"graph-branch-create":          {"name"},
		"graph-branch-update":          {"branch"},
		"graph-branch-merge":           {"source_branch"},
		"graph-branch-merge-readiness": {"branch"},
		"graph-branch-fork":            {"name"},
		"graph-branch-delete":          {"branch"},
	}

	found := map[string]bool{}
	for _, d := range defs {
		want, ok := wantRequired[d.Name]
		if !ok {
			continue
		}
		found[d.Name] = true
		assert.Equal(t, want, d.InputSchema.Required, "%s required args", d.Name)
	}
	for name := range wantRequired {
		assert.True(t, found[name], "%s not found in GetToolDefinitions", name)
	}
}

// TestBranchMergeToolExposesPolicy proves dry-run compare semantics are
// reachable: policy=manual + execute=false is the REST compare endpoint.
func TestBranchMergeToolExposesPolicy(t *testing.T) {
	svc := &Service{}
	for _, d := range svc.GetToolDefinitions() {
		if d.Name != "graph-branch-merge" {
			continue
		}
		policy, ok := d.InputSchema.Properties["policy"]
		require.True(t, ok, "graph-branch-merge must expose policy")
		assert.Contains(t, policy.Description, "manual")
		assert.Contains(t, policy.Description, "compare")
		return
	}
	t.Fatal("graph-branch-merge not found in GetToolDefinitions")
}

// TestBranchReadToolsUseReadScope makes sure inspection tools do not require a
// write scope.
func TestBranchReadToolsUseReadScope(t *testing.T) {
	for _, name := range []string{"graph-branch-get", "graph-branch-merge-readiness"} {
		scope, ok := LookupToolScope(name)
		require.True(t, ok, "%s scope", name)
		assert.Equal(t, "branches:read", scope, "%s must be read-scoped", name)
	}
}

// TestBranchToolNilServiceGuards proves a Service without its branch/graph
// dependencies returns an error instead of panicking.
func TestBranchToolNilServiceGuards(t *testing.T) {
	svc := &Service{}
	cases := map[string]map[string]any{
		"graph-branch-get":             {"branch": "plan/x"},
		"graph-branch-update":          {"branch": "plan/x", "name": "plan/y"},
		"graph-branch-merge":           {"source_branch": "plan/x"},
		"graph-branch-merge-readiness": {"branch": "plan/x"},
		"graph-branch-fork":            {"name": "plan/y"},
	}
	// TransportEnforced mirrors what the HTTP transports set after their own
	// per-tool check; it keeps these tests on the dispatch path.
	ctx := ContextWithTransportEnforced(context.Background())
	for tool, args := range cases {
		_, err := svc.ExecuteTool(ctx, "proj-1", tool, args)
		require.Error(t, err, "%s must error without services", tool)
		assert.Contains(t, err.Error(), "not available", "%s error must name the missing service", tool)
	}
}

// TestBranchToolArgumentValidation covers the argument checks that run before
// any service or database call.
func TestBranchToolArgumentValidation(t *testing.T) {
	tests := []struct {
		tool    string
		svc     *Service
		args    map[string]any
		wantSub string
	}{
		{
			tool: "graph-branch-get", svc: &Service{branchSvc: new(branches.Service)},
			args: map[string]any{}, wantSub: "missing required parameter: branch",
		},
		{
			tool: "graph-branch-update", svc: &Service{branchSvc: new(branches.Service)},
			args: map[string]any{}, wantSub: "missing required parameter: branch",
		},
		{
			tool: "graph-branch-update", svc: &Service{branchSvc: new(branches.Service)},
			args:    map[string]any{"branch": "plan/x"},
			wantSub: "at least one of name or description is required",
		},
		{
			tool: "graph-branch-merge-readiness", svc: &Service{graphService: &graph.Service{}},
			args:    map[string]any{},
			wantSub: "missing required parameter: branch",
		},
		{
			tool: "graph-branch-fork", svc: &Service{graphService: &graph.Service{}},
			args:    map[string]any{},
			wantSub: "missing required parameter: name",
		},
	}

	for _, tc := range tests {
		t.Run(tc.tool+"/"+strings.ReplaceAll(tc.wantSub, " ", "_"), func(t *testing.T) {
			ctx := ContextWithTransportEnforced(context.Background())
			_, err := tc.svc.ExecuteTool(ctx, "6f1a1d3e-3a2b-4c5d-8e9f-0a1b2c3d4e5f", tc.tool, tc.args)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantSub)
		})
	}
}

// TestBranchUpdateRequestDescriptionHandling pins the null-vs-empty
// distinction: only a string selects the description, so an omitted or explicit
// null field cannot wipe stored metadata on a rename-only call, while an
// explicit "" still clears it.
func TestBranchUpdateRequestDescriptionHandling(t *testing.T) {
	t.Run("absent leaves description untouched", func(t *testing.T) {
		req, err := branchUpdateRequest(map[string]any{"branch": "plan/x", "name": "plan/y"})
		require.NoError(t, err)
		require.NotNil(t, req.Name)
		assert.Equal(t, "plan/y", *req.Name)
		assert.Nil(t, req.Description, "absent description must not be sent to the store")
	})

	t.Run("null leaves description untouched", func(t *testing.T) {
		req, err := branchUpdateRequest(map[string]any{"description": nil, "name": "plan/y"})
		require.NoError(t, err)
		assert.Nil(t, req.Description, "JSON null must not be treated as an empty description")
	})

	t.Run("empty string clears description", func(t *testing.T) {
		req, err := branchUpdateRequest(map[string]any{"description": ""})
		require.NoError(t, err)
		require.NotNil(t, req.Description)
		assert.Empty(t, *req.Description)
		assert.Nil(t, req.Name)
	})

	t.Run("name only", func(t *testing.T) {
		req, err := branchUpdateRequest(map[string]any{"name": "plan/y"})
		require.NoError(t, err)
		assert.Nil(t, req.Description)
	})

	t.Run("neither field is an error", func(t *testing.T) {
		_, err := branchUpdateRequest(map[string]any{"branch": "plan/x"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "at least one of name or description is required")
	})

	t.Run("non-string description is ignored", func(t *testing.T) {
		req, err := branchUpdateRequest(map[string]any{"name": "plan/y", "description": 42})
		require.NoError(t, err)
		assert.Nil(t, req.Description)
	})
}

// TestForkBranchRequest proves filter_types is honoured for both the decoded
// []any form and a Go-side []string, instead of silently forking the whole
// graph.
func TestForkBranchRequest(t *testing.T) {
	t.Run("name required", func(t *testing.T) {
		_, err := forkBranchRequest(map[string]any{})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "missing required parameter: name")
	})

	t.Run("decoded any slice", func(t *testing.T) {
		req, err := forkBranchRequest(map[string]any{
			"name":         "plan/x",
			"description":  "snapshot",
			"filter_types": []any{"Decision", "", 7, "Project"},
		})
		require.NoError(t, err)
		assert.Equal(t, "plan/x", req.Name)
		require.NotNil(t, req.Description)
		assert.Equal(t, "snapshot", *req.Description)
		assert.Equal(t, []string{"Decision", "Project"}, req.FilterTypes)
	})

	t.Run("typed string slice", func(t *testing.T) {
		req, err := forkBranchRequest(map[string]any{
			"name":         "plan/x",
			"filter_types": []string{"Decision", "Project"},
		})
		require.NoError(t, err)
		assert.Equal(t, []string{"Decision", "Project"}, req.FilterTypes)
	})

	t.Run("no filter copies everything", func(t *testing.T) {
		req, err := forkBranchRequest(map[string]any{"name": "plan/x"})
		require.NoError(t, err)
		assert.Empty(t, req.FilterTypes)
		assert.Nil(t, req.Description)
	})
}

// TestBranchMergeOptions pins the option pass-through, including the legacy
// conflict_strategy the service falls back to when policy is empty.
func TestBranchMergeOptions(t *testing.T) {
	execute, policy, conflict := branchMergeOptions(map[string]any{
		"execute":           true,
		"policy":            "manual",
		"conflict_strategy": "overwrite",
	})
	assert.True(t, execute)
	assert.Equal(t, "manual", policy)
	assert.Equal(t, "overwrite", conflict)

	execute, policy, conflict = branchMergeOptions(map[string]any{})
	assert.False(t, execute, "merge must default to dry-run")
	assert.Empty(t, policy)
	assert.Empty(t, conflict)

	_, policy, _ = branchMergeOptions(map[string]any{"policy": 42})
	assert.Empty(t, policy, "non-string policy must not be forwarded")
}
