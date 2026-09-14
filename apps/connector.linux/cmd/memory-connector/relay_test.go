package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/emergent-company/memory.web-ui/connector/internal/appletools"
	"github.com/emergent-company/memory.web-ui/connector/internal/config"
	"github.com/emergent-company/memory.web-ui/connector/internal/toolreg"
	"github.com/emergent-company/memory.web-ui/connector/internal/tools"
)

// fakeRelayTool builds a deterministic no-op tool for provider injection.
func fakeRelayTool(name string) toolreg.Tool {
	return toolreg.Tool{
		Name:        name,
		Description: "fake " + name,
		InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
		Handler:     func(context.Context, map[string]any) (map[string]any, error) { return map[string]any{}, nil },
	}
}

// withRelayToolsProvider swaps the relay command's tool provider for the
// duration of a test.
func withRelayToolsProvider(t *testing.T, provider func(*config.Config) (*tools.Set, error)) {
	t.Helper()
	old := relayToolsProvider
	relayToolsProvider = provider
	t.Cleanup(func() { relayToolsProvider = old })
}

func registryNames(registry *toolreg.Registry) []string {
	tools := registry.List()
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		names = append(names, tool.Name)
	}
	return names
}

func payloadToolNames(t *testing.T, registry *toolreg.Registry) []string {
	t.Helper()
	raw, ok := registry.ToolsListPayload()["tools"]
	if !ok {
		t.Fatalf("ToolsListPayload() missing nested tools key")
	}
	items, ok := raw.([]map[string]any)
	if !ok {
		t.Fatalf("ToolsListPayload()[\"tools\"] type = %T, want []map[string]any", raw)
	}
	names := make([]string, 0, len(items))
	for _, item := range items {
		name, _ := item["name"].(string)
		names = append(names, name)
	}
	return names
}

func runRelayCapture(args []string) (code int, stdout, stderr string) {
	var out, errBuf bytes.Buffer
	code = runRelay(args, &out, &errBuf)
	return code, out.String(), errBuf.String()
}

func TestPrepareRelayRegistryRegistersProviderTools(t *testing.T) {
	withRelayToolsProvider(t, func(*config.Config) (*tools.Set, error) {
		return &tools.Set{Tools: []toolreg.Tool{fakeRelayTool("alpha"), fakeRelayTool("beta")}}, nil
	})

	registry, note, err := prepareRelayRegistry(&config.Config{})
	if err != nil {
		t.Fatalf("prepareRelayRegistry: %v", err)
	}
	if note != "" {
		t.Errorf("note = %q, want empty", note)
	}
	if got := registryNames(registry); !reflect.DeepEqual(got, []string{"alpha", "beta"}) {
		t.Errorf("enabled tools = %v, want [alpha beta]", got)
	}
	if got := payloadToolNames(t, registry); !reflect.DeepEqual(got, []string{"alpha", "beta"}) {
		t.Errorf("register payload tools = %v, want [alpha beta]", got)
	}
}

func TestPrepareRelayRegistryHonorsDisabledTools(t *testing.T) {
	withRelayToolsProvider(t, func(*config.Config) (*tools.Set, error) {
		return &tools.Set{Tools: []toolreg.Tool{fakeRelayTool("alpha"), fakeRelayTool("beta"), fakeRelayTool("gamma")}}, nil
	})

	registry, _, err := prepareRelayRegistry(&config.Config{DisabledTools: []string{"beta", "not_registered"}})
	if err != nil {
		t.Fatalf("prepareRelayRegistry: %v", err)
	}
	if got := registryNames(registry); !reflect.DeepEqual(got, []string{"alpha", "gamma"}) {
		t.Errorf("enabled tools = %v, want [alpha gamma]", got)
	}
	if got := payloadToolNames(t, registry); !reflect.DeepEqual(got, []string{"alpha", "gamma"}) {
		t.Errorf("register payload tools = %v, want [alpha gamma]", got)
	}

	// Disabled tools stay resolvable so dispatch rejects calls with a distinct
	// "disabled" error instead of "unknown tool".
	tool, ok := registry.Lookup("beta")
	if !ok {
		t.Fatal("disabled tool beta must remain resolvable")
	}
	_, err = tool.Handler(context.Background(), map[string]any{})
	if err == nil || !strings.Contains(err.Error(), "disabled") {
		t.Errorf("disabled handler error = %v, want a disabled error", err)
	}
	if _, ok := registry.Lookup("alpha"); !ok {
		t.Error("enabled tool alpha must remain resolvable")
	}
}

func TestPrepareRelayRegistryEmptyProviderPropagatesNote(t *testing.T) {
	withRelayToolsProvider(t, func(*config.Config) (*tools.Set, error) {
		return &tools.Set{Note: appletools.PlatformNote}, nil
	})

	registry, note, err := prepareRelayRegistry(&config.Config{})
	if err != nil {
		t.Fatalf("prepareRelayRegistry: %v", err)
	}
	if note != appletools.PlatformNote {
		t.Errorf("note = %q, want PlatformNote", note)
	}
	if got := registryNames(registry); len(got) != 0 {
		t.Errorf("tools = %v, want none", got)
	}
}

func TestPrepareRelayRegistrySurfacesRegisterError(t *testing.T) {
	withRelayToolsProvider(t, func(*config.Config) (*tools.Set, error) {
		return &tools.Set{Tools: []toolreg.Tool{
			fakeRelayTool("ok"),
			{Name: "broken"}, // nil handler is rejected by Registry.Register
		}}, nil
	})

	_, _, err := prepareRelayRegistry(&config.Config{})
	if err == nil {
		t.Fatal("prepareRelayRegistry: expected error for nil handler, got nil")
	}
	if !strings.Contains(err.Error(), `"broken"`) || !strings.Contains(err.Error(), "handler") {
		t.Errorf("error = %q, want it naming broken's nil handler", err)
	}
}

func TestRunRelayFlagAndArgErrors(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{name: "unknown flag", args: []string{"--bogus"}},
		{name: "positional arg", args: []string{"extra"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, _, _ := runRelayCapture(tc.args)
			if code != 2 {
				t.Errorf("runRelay code = %d, want 2", code)
			}
		})
	}
}

func TestRunRelayMissingConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing", "config.yml")
	code, _, stderr := runRelayCapture([]string{"--config", path})
	if code != 1 {
		t.Fatalf("runRelay code = %d, want 1 (stderr: %s)", code, stderr)
	}
	if !strings.Contains(stderr, "no config at") || !strings.Contains(stderr, "memory-connector init") {
		t.Errorf("stderr = %q, want a pointer to 'memory-connector init'", stderr)
	}
}

func TestRunRelayInvalidConfig(t *testing.T) {
	// Missing token: config.Load fails validation before any provider or
	// network work, so this is deterministic without a live hub.
	path := filepath.Join(t.TempDir(), "config.yml")
	if err := os.WriteFile(path, []byte("server_url: https://x.test\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	code, _, stderr := runRelayCapture([]string{"--config", path})
	if code != 1 {
		t.Fatalf("runRelay code = %d, want 1 (stderr: %s)", code, stderr)
	}
	if !strings.Contains(stderr, "memory-connector relay:") || !strings.Contains(stderr, "token") {
		t.Errorf("stderr = %q, want a token validation error", stderr)
	}
}
