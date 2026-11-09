package sources

import (
	"strings"
	"testing"
)

func TestSourceScopeHashIsStable(t *testing.T) {
	a := SourceScopeInput{
		Filter:               map[string]any{"categories": []any{"Primary"}, "hasAttachment": true},
		ObjectTypes:          []string{"EmailMessage", "EmailThread"},
		MCPWriteToolsEnabled: false,
	}
	b := SourceScopeInput{
		Filter:               map[string]any{"hasAttachment": true, "categories": []any{"Primary"}},
		ObjectTypes:          []string{"EmailMessage", "EmailThread"},
		MCPWriteToolsEnabled: false,
	}
	if sourceScopeHash("gmail", a) != sourceScopeHash("gmail", b) {
		t.Fatal("hash must not depend on map key order")
	}
	b.MCPWriteToolsEnabled = true
	if sourceScopeHash("gmail", a) == sourceScopeHash("gmail", b) {
		t.Fatal("hash must change when write-tool state changes")
	}
}

func TestMergeSourceScopePartial(t *testing.T) {
	current := SourceScopeInput{
		Filter:               map[string]any{"categories": []any{"Primary"}},
		ObjectTypes:          []string{"EmailMessage"},
		MCPWriteToolsEnabled: true,
	}
	got := mergeSourceScope(current, &SourceScopeRequest{ObjectTypes: []string{"EmailThread"}})
	if len(got.Filter) != 1 {
		t.Fatalf("filter = %v, want preserved", got.Filter)
	}
	if len(got.ObjectTypes) != 1 || got.ObjectTypes[0] != "EmailThread" {
		t.Fatalf("object types = %v", got.ObjectTypes)
	}
	if !got.MCPWriteToolsEnabled {
		t.Fatal("write-tool state must be preserved when omitted")
	}
	off := false
	got = mergeSourceScope(current, &SourceScopeRequest{MCPWriteToolsEnabled: &off})
	if got.MCPWriteToolsEnabled {
		t.Fatal("explicit false must disable write tools")
	}
}

func TestRejectRawQuery(t *testing.T) {
	if err := rejectRawQuery(map[string]any{"query": "in:anywhere"}); err == nil {
		t.Fatal("raw query must be rejected for MCP")
	}
	if err := rejectRawQuery(map[string]any{"categories": []any{"Primary"}}); err != nil {
		t.Fatalf("structured filter rejected: %v", err)
	}
}

func TestSourceScopeFromConfigDefaults(t *testing.T) {
	scope := sourceScopeFromConfig(map[string]any{})
	if len(scope.ObjectTypes) != 2 || scope.ObjectTypes[0] != "EmailMessage" || scope.ObjectTypes[1] != "EmailThread" {
		t.Fatalf("default object types = %v", scope.ObjectTypes)
	}
	if scope.MCPWriteToolsEnabled {
		t.Fatal("write tools must default off")
	}
}

func TestScopeRequestFromArgsStructured(t *testing.T) {
	req, err := scopeRequestFromArgs(map[string]any{
		"filter": map[string]any{"categories": []any{"Primary"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if req.Filter == nil || req.Filter["categories"] == nil {
		t.Fatal("filter not parsed")
	}
	if _, err := scopeRequestFromArgs(map[string]any{"filter": map[string]any{"query": "in:anywhere"}}); err == nil || !strings.Contains(err.Error(), "unsupported filter key") {
		t.Fatalf("query rejection = %v", err)
	}
	if _, err := scopeRequestFromArgs(map[string]any{"query": "in:anywhere"}); err == nil || !strings.Contains(err.Error(), "raw Gmail query") {
		t.Fatalf("top-level query rejection = %v", err)
	}
}
