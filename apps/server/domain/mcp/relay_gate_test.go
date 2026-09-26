package mcp

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// fakeRelayProvider is a hermetic RelayToolProvider for the ExecuteTool relay
// fallback: it exposes one instance with one tool and records CallTool so the
// test can prove the relay trust gate fires before the relay call.
type fakeRelayProvider struct {
	sessions []*RelaySession
	called   int
}

func (f *fakeRelayProvider) ListByProject(projectID string) []*RelaySession { return f.sessions }
func (f *fakeRelayProvider) CallTool(ctx context.Context, projectID, instanceID, toolName string, args map[string]any) (map[string]any, error) {
	f.called++
	return map[string]any{"ok": true}, nil
}

// TestExecuteToolRelayGate proves the ExecuteTool relay fallback — which routes
// "{instanceID}_{tool}" names to a connected device — is gated on the trust
// marker: an untrusted run is refused, a trusted run reaches the relay (issue #994).
func TestExecuteToolRelayGate(t *testing.T) {
	relay := &fakeRelayProvider{sessions: []*RelaySession{{InstanceID: "inst1"}}}
	svc := &Service{relaySvc: relay}

	projectID := "00000000-0000-0000-0000-000000000000"
	untrustedCtx := context.Background()
	trustedCtx := ContextWithTrustedInternal(context.Background(), true)

	t.Run("untrusted run is refused on a relay tool", func(t *testing.T) {
		_, err := svc.ExecuteTool(untrustedCtx, projectID, "inst1_reminders_list", map[string]any{})
		require.Error(t, err, "untrusted run must be refused on relay tools")
		require.Contains(t, err.Error(), "untrusted surface", "refusal must name the trust boundary")
		require.Zero(t, relay.called, "relay must not be reached for an untrusted run")
	})

	t.Run("trusted run reaches the relay (no over-correction)", func(t *testing.T) {
		result, err := svc.ExecuteTool(trustedCtx, projectID, "inst1_reminders_list", map[string]any{})
		require.NoError(t, err, "trusted run must reach the relay, not be refused")
		require.NotNil(t, result)
		require.Equal(t, 1, relay.called, "relay must be reached for a trusted run")
	})
}

// TestExecuteToolRelayHTTPTransportRefused proves the relay fallback refuses a
// call that an HTTP transport already authorized. Relay tools are the agent-only
// class and must be reachable ONLY from a genuinely internal agent run. An HTTP
// transport marks its dispatch transport-enforced (NOT trusted-internal) after
// its own per-tool check, so an authenticated HTTP caller can never satisfy the
// trusted-internal gate and is refused before reaching the relay (issue #1017).
func TestExecuteToolRelayHTTPTransportRefused(t *testing.T) {
	relay := &fakeRelayProvider{sessions: []*RelaySession{{InstanceID: "inst1"}}}
	svc := &Service{relaySvc: relay}

	projectID := "00000000-0000-0000-0000-000000000000"
	// What an HTTP transport does: enforce per-tool checks, then mark the call
	// transport-enforced before dispatch.
	httpCtx := ContextWithTransportEnforced(context.Background())

	_, err := svc.ExecuteTool(httpCtx, projectID, "inst1_reminders_list", map[string]any{})
	require.Error(t, err, "an HTTP transport must be refused on a relay tool")
	require.Contains(t, err.Error(), "untrusted surface", "refusal must name the trust boundary")
	require.Zero(t, relay.called, "relay must not be reached from an HTTP transport")
}
