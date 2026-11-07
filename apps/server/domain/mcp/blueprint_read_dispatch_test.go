package mcp

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// stubBlueprintReadHandler proves the three read tools added for #18 reach the
// blueprint handler through ExecuteTool. A tool can be registered and
// discoverable while ExecuteTool routes it to the relay/unknown path — which is
// exactly what a missing case in ExecuteTool's blueprint switch does, and what
// an earlier revision of this change shipped. The embedded nil interface makes
// any other method panic, so a wrong route fails loudly rather than silently.
type stubBlueprintReadHandler struct {
	BlueprintToolHandler
	called string
}

func (s *stubBlueprintReadHandler) ExecuteBlueprintUpdatesStatus(context.Context, string, map[string]any) (*ToolResult, error) {
	s.called = "blueprint-updates-status"
	return &ToolResult{}, nil
}

func (s *stubBlueprintReadHandler) ExecuteBlueprintImportPreview(context.Context, string, map[string]any) (*ToolResult, error) {
	s.called = "blueprint-import-preview"
	return &ToolResult{}, nil
}

func (s *stubBlueprintReadHandler) ExecuteBlueprintModelOverridesGet(context.Context, string, map[string]any) (*ToolResult, error) {
	s.called = "blueprint-model-overrides-get"
	return &ToolResult{}, nil
}

func TestBlueprintReadToolsReachTheHandler(t *testing.T) {
	stub := &stubBlueprintReadHandler{}
	svc := &Service{blueprintToolHandler: stub}
	_ = svc.GetToolDefinitions() // populate the tool index the authority gate reads

	ctx := ContextWithTrustedInternal(context.Background(), true)
	projectID := "6f1a1d3e-3a2b-4c5d-8e9f-0a1b2c3d4e5f"

	for _, name := range []string{
		"blueprint-updates-status",
		"blueprint-import-preview",
		"blueprint-model-overrides-get",
	} {
		t.Run(name, func(t *testing.T) {
			stub.called = ""
			_, err := svc.ExecuteTool(ctx, projectID, name, map[string]any{})
			require.NoError(t, err)
			require.Equal(t, name, stub.called,
				"%s is discoverable but ExecuteTool did not route it to the blueprint handler", name)
		})
	}
}
