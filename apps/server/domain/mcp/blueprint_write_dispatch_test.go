package mcp

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// stubBlueprintWriteHandler proves the seven write tools added for #18 reach the
// blueprint handler through ExecuteTool. A tool can be registered and
// discoverable while ExecuteTool routes it to the relay/unknown path — the exact
// defect the read-tool dispatch test already pins for the read surface. The
// embedded nil interface makes any other method panic, so a wrong route fails
// loudly rather than silently.
type stubBlueprintWriteHandler struct {
	BlueprintToolHandler
	called string
}

func (s *stubBlueprintWriteHandler) ExecuteBlueprintDelete(context.Context, string, map[string]any) (*ToolResult, error) {
	s.called = "blueprint-delete"
	return &ToolResult{}, nil
}

func (s *stubBlueprintWriteHandler) ExecuteBlueprintDeprecate(context.Context, string, map[string]any) (*ToolResult, error) {
	s.called = "blueprint-deprecate"
	return &ToolResult{}, nil
}

func (s *stubBlueprintWriteHandler) ExecuteBlueprintImport(context.Context, string, map[string]any) (*ToolResult, error) {
	s.called = "blueprint-import"
	return &ToolResult{}, nil
}

func (s *stubBlueprintWriteHandler) ExecuteBlueprintUpdatesCheck(context.Context, string, map[string]any) (*ToolResult, error) {
	s.called = "blueprint-updates-check"
	return &ToolResult{}, nil
}

func (s *stubBlueprintWriteHandler) ExecuteBlueprintUpdatesApply(context.Context, string, map[string]any) (*ToolResult, error) {
	s.called = "blueprint-updates-apply"
	return &ToolResult{}, nil
}

func (s *stubBlueprintWriteHandler) ExecuteBlueprintModelOverridesPut(context.Context, string, map[string]any) (*ToolResult, error) {
	s.called = "blueprint-model-overrides-put"
	return &ToolResult{}, nil
}

func (s *stubBlueprintWriteHandler) ExecuteBlueprintGithubCredentialsPut(context.Context, string, map[string]any) (*ToolResult, error) {
	s.called = "blueprint-github-credentials-put"
	return &ToolResult{}, nil
}

func TestBlueprintWriteToolsReachTheHandler(t *testing.T) {
	stub := &stubBlueprintWriteHandler{}
	svc := &Service{blueprintToolHandler: stub}
	_ = svc.GetToolDefinitions() // populate the tool index the authority gate reads

	ctx := ContextWithTrustedInternal(context.Background(), true)
	projectID := "6f1a1d3e-3a2b-4c5d-8e9f-0a1b2c3d4e5f"

	for _, name := range []string{
		"blueprint-delete",
		"blueprint-deprecate",
		"blueprint-import",
		"blueprint-updates-check",
		"blueprint-updates-apply",
		"blueprint-model-overrides-put",
		"blueprint-github-credentials-put",
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
