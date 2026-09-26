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
