package mcp

import "testing"

// toolByName returns the ToolDefinition with the given name from the package
// catalog, or nil if absent.
func toolByName(t *testing.T, name string) *ToolDefinition {
	t.Helper()
	for _, tool := range (&Service{}).GetToolDefinitions() {
		if tool.Name == name {
			return &tool
		}
	}
	t.Fatalf("tool %q not found in catalog", name)
	return nil
}

// TestSuperadminOnlyToolClassification pins which deployment-wide / org-level
// operator tools are gated on the superadmin_full authority (issue #948) and
// which tools remain scope-gated because they are genuinely project-scoped or
// read-only.
func TestSuperadminOnlyToolClassification(t *testing.T) {
	superadminOnly := []string{
		"embedding-status",
		"embedding-pause",
		"embedding-resume",
		"embedding-config-update",
		"provider-list-org",
		"provider-configure-org",
		"provider-test",
		"provider-usage-get",
	}
	for _, name := range superadminOnly {
		tool := toolByName(t, name)
		if !tool.SuperadminOnly {
			t.Errorf("tool %q must be SuperadminOnly, got false", name)
		}
		if tool.RequiredScope != "" {
			t.Errorf("tool %q must not be scope-gated, got RequiredScope %q", name, tool.RequiredScope)
		}
	}

	// provider-configure-project is project-scoped (the underlying
	// UpsertProjectConfig enforces org membership via assertCallerOwnsProject);
	// provider-models-list is a read-only catalog with no credentials. Both stay
	// scope-gated rather than superadmin-gated.
	scopeGated := []struct {
		name  string
		scope string
	}{
		{"provider-configure-project", "admin"},
		{"provider-models-list", "admin"},
	}
	for _, tc := range scopeGated {
		tool := toolByName(t, tc.name)
		if tool.SuperadminOnly {
			t.Errorf("tool %q must NOT be SuperadminOnly", tc.name)
		}
		if tool.RequiredScope != tc.scope {
			t.Errorf("tool %q RequiredScope = %q, want %q", tc.name, tool.RequiredScope, tc.scope)
		}
	}
}

// TestFilterToolsForSuperadmin proves operator tools are hidden from
// non-superadmins and preserved for superadmins.
func TestFilterToolsForSuperadmin(t *testing.T) {
	tools := []ToolDefinition{
		{Name: "project-get"},
		{Name: "embedding-pause", SuperadminOnly: true},
		{Name: "entity-query"},
		{Name: "provider-configure-org", SuperadminOnly: true},
	}

	names := func(defs []ToolDefinition) map[string]bool {
		m := make(map[string]bool, len(defs))
		for _, d := range defs {
			m[d.Name] = true
		}
		return m
	}

	// Non-superadmin: operator tools removed.
	got := names(FilterToolsForSuperadmin(tools, false))
	if got["embedding-pause"] || got["provider-configure-org"] {
		t.Errorf("non-superadmin must not see SuperadminOnly tools: %v", got)
	}
	if !got["project-get"] || !got["entity-query"] {
		t.Errorf("non-superadmin must still see ordinary tools: %v", got)
	}

	// Superadmin: full catalog preserved.
	got = names(FilterToolsForSuperadmin(tools, true))
	if len(got) != 4 {
		t.Errorf("superadmin must see all 4 tools, got %v", got)
	}
}
