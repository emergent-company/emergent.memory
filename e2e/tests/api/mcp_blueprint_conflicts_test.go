// Package api_test — mcp_blueprint_conflicts_test.go
//
// MCP coverage for blueprint object-type collisions: apply preview reports
// conflicts, apply without merge does not mutate, and apply with mergeTypes
// proceeds and registers the merged properties.
package api_test

import (
	"net/http"
	"reflect"
	"strings"
	"testing"
)

func TestMCPBlueprintConflicts_PreviewApplyAndMerge(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)
	projectID, _ := setupProjectLogged(t, rl)

	baseBP := createPrivateBlueprint(t, rl, projectID, uniqueName("mcp-base"), "1.0.0", baseConflictManifest())
	mustStatus(t, applyBlueprint(t, rl, projectID, baseBP, nil), http.StatusOK)

	conflictBP := createPrivateBlueprint(t, rl, projectID, uniqueName("mcp-conflict"), "1.0.0", conflictManifest("conflict-pack"))
	compiledBefore := compiledTypes(t, rl, projectID)

	sessionID := initLifecycleMCPSession(t, projectID)

	// 1. blueprint-apply-preview reports the collision.
	preview := parseLifecycleToolResult(t, callLifecycleMCPTool(t, projectID, sessionID, "blueprint-apply-preview", map[string]any{
		"id": conflictBP,
	}))
	if preview["requiresMerge"] != true {
		t.Fatalf("expected requiresMerge=true from MCP preview, got %+v", preview)
	}
	conflicts, _ := preview["conflicts"].([]any)
	if len(conflicts) == 0 {
		t.Fatalf("expected conflicts from MCP preview, got %+v", preview)
	}

	// 2. blueprint-apply without merge must refuse and mutate nothing.
	blocked := parseLifecycleToolResult(t, callLifecycleMCPTool(t, projectID, sessionID, "blueprint-apply", map[string]any{
		"id": conflictBP,
	}))
	errText, _ := blocked["error"].(string)
	if !strings.Contains(errText, "object type name conflicts require merge confirmation") {
		t.Fatalf("expected conflict error text, got %+v", blocked)
	}
	compiledAfterBlocked := compiledTypes(t, rl, projectID)
	if !reflect.DeepEqual(compiledBefore.ObjectTypes, compiledAfterBlocked.ObjectTypes) {
		t.Fatalf("blocked MCP apply mutated compiled types: before=%+v after=%+v", compiledBefore.ObjectTypes, compiledAfterBlocked.ObjectTypes)
	}
	thingBlocked := findCompiledType(t, compiledAfterBlocked, "Thing")
	if hasProperty(thingBlocked.Properties, "description") || hasProperty(thingBlocked.Properties, "owner") {
		t.Fatalf("blocked MCP apply registered merged properties: %v", thingBlocked.Properties)
	}

	// 3. blueprint-apply with mergeTypes=true succeeds and merges properties.
	merged := parseLifecycleToolResult(t, callLifecycleMCPTool(t, projectID, sessionID, "blueprint-apply", map[string]any{
		"id":         conflictBP,
		"mergeTypes": true,
	}))
	if merged["error"] != nil {
		t.Fatalf("merge-flagged MCP apply failed: %+v", merged)
	}
	if _, ok := merged["typeConflicts"]; !ok {
		t.Fatalf("expected typeConflicts in MCP apply result, got %+v", merged)
	}

	thing := findCompiledType(t, compiledTypes(t, rl, projectID), "Thing")
	if !hasProperty(thing.Properties, "description") || !hasProperty(thing.Properties, "owner") {
		t.Fatalf("expected merged Thing properties after MCP apply, got %v", thing.Properties)
	}
}
