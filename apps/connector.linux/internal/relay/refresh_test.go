package relay

import (
	"context"
	"testing"
	"time"

	"github.com/emergent-company/memory.web-ui/connector/internal/toolreg"
)

// TestRefreshRegistrationNoopWhenDisconnected verifies RefreshRegistration is
// a safe no-op before any connection is established (the next connect
// registers the current tool list anyway).
func TestRefreshRegistrationNoopWhenDisconnected(t *testing.T) {
	client, err := New("https://hub.example.test", "emt_token", "", "inst-1", "0.1.0", testRegistry(t))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := client.RefreshRegistration(); err != nil {
		t.Fatalf("RefreshRegistration on disconnected client = %v, want nil", err)
	}
}

// TestRefreshRegistrationResendsUpdatedTools verifies that after the reloadable
// registry is swapped, RefreshRegistration re-sends a register frame carrying
// the new tool list on the live connection.
func TestRefreshRegistrationResendsUpdatedTools(t *testing.T) {
	hub := newFakeHub(t)
	rr := toolreg.NewReloadable(testRegistry(t))
	client, cancel, _ := startClient(t, testClientOpts{serverURL: hub.url(), registry: rr})
	defer cancel()
	hub.waitRegisters(1, 3*time.Second)

	next := toolreg.New()
	if err := next.Register(toolreg.Tool{
		Name:        "solo",
		Description: "only tool",
		InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
		Handler:     func(context.Context, map[string]any) (map[string]any, error) { return map[string]any{}, nil },
	}); err != nil {
		t.Fatalf("register solo: %v", err)
	}
	rr.Swap(next)

	if err := client.RefreshRegistration(); err != nil {
		t.Fatalf("RefreshRegistration: %v", err)
	}

	regs := hub.waitRegisters(1, 3*time.Second)
	if len(regs) != 1 {
		t.Fatalf("re-register frames = %d, want 1", len(regs))
	}
	tools, ok := regs[0].Tools["tools"].([]any)
	if !ok {
		t.Fatalf("re-register tools payload = %#v", regs[0].Tools)
	}
	if len(tools) != 1 {
		t.Fatalf("re-registered tool count = %d, want 1", len(tools))
	}
	entry, ok := tools[0].(map[string]any)
	if !ok || entry["name"] != "solo" {
		t.Fatalf("re-registered tool = %#v, want name solo", tools[0])
	}
}
