package relay

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

// sendConfigFrame writes a config frame to the connected client.
func (h *fakeHub) sendConfigFrame(id string, version int64, servers string) error {
	raw := fmt.Sprintf(`{"type":"config","id":%q,"version":%d,"servers":%s}`, id, version, servers)
	return h.writeText([]byte(raw))
}

// tryResponse polls respCh for a response frame within timeout without failing
// the test; used for "no ack expected" assertions.
func (h *fakeHub) tryResponse(timeout time.Duration) (ResponseFrame, bool) {
	select {
	case resp := <-h.respCh:
		return resp, true
	case <-time.After(timeout):
		return ResponseFrame{}, false
	}
}

func TestConfigFrameAckSuccess(t *testing.T) {
	hub := newFakeHub(t)
	seen := make(chan ConfigFrame, 1)
	applier := func(_ context.Context, f ConfigFrame) (ConfigResult, error) {
		seen <- f
		return ConfigResult{Applied: 1, Tools: 5}, nil
	}
	_, cancel, _ := startClient(t, testClientOpts{serverURL: hub.url(), configApplier: applier})
	defer cancel()
	hub.waitRegisters(1, 3*time.Second)

	if err := hub.sendConfigFrame("cfg-1", 1, `[{"name":"github","transport":"stdio","command":"github-mcp"}]`); err != nil {
		t.Fatalf("sendConfigFrame: %v", err)
	}

	select {
	case f := <-seen:
		if f.ID != "cfg-1" || f.Version != 1 || len(f.Servers) != 1 {
			t.Errorf("applier saw frame %+v", f)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("applier was not invoked")
	}

	resp := hub.waitResponse(3 * time.Second)
	if got := string(resp.ID); got != `"cfg-1"` {
		t.Errorf("response id = %s, want cfg-1", got)
	}
	if resp.Error != "" {
		t.Errorf("response error = %q, want none", resp.Error)
	}
	if resp.Payload["ok"] != true {
		t.Errorf("payload ok = %v, want true", resp.Payload["ok"])
	}
	if resp.Payload["applied"] != float64(1) {
		t.Errorf("payload applied = %v, want 1", resp.Payload["applied"])
	}
	if resp.Payload["tools"] != float64(5) {
		t.Errorf("payload tools = %v, want 5", resp.Payload["tools"])
	}
}

func TestConfigFrameAckError(t *testing.T) {
	hub := newFakeHub(t)
	applier := func(_ context.Context, f ConfigFrame) (ConfigResult, error) {
		return ConfigResult{}, errors.New("boom from applier")
	}
	_, cancel, _ := startClient(t, testClientOpts{serverURL: hub.url(), configApplier: applier})
	defer cancel()
	hub.waitRegisters(1, 3*time.Second)

	if err := hub.sendConfigFrame("cfg-2", 2, `[]`); err != nil {
		t.Fatalf("sendConfigFrame: %v", err)
	}
	resp := hub.waitResponse(3 * time.Second)
	if got := string(resp.ID); got != `"cfg-2"` {
		t.Errorf("response id = %s, want cfg-2", got)
	}
	if !strings.Contains(resp.Error, "boom from applier") {
		t.Errorf("response error = %q, want applier error", resp.Error)
	}
	if resp.Payload != nil {
		t.Errorf("payload = %v, want nil on error", resp.Payload)
	}
}

func TestConfigFrameStaleAck(t *testing.T) {
	hub := newFakeHub(t)
	applier := func(_ context.Context, f ConfigFrame) (ConfigResult, error) {
		return ConfigResult{}, errors.New("stale")
	}
	_, cancel, _ := startClient(t, testClientOpts{serverURL: hub.url(), configApplier: applier})
	defer cancel()
	hub.waitRegisters(1, 3*time.Second)

	if err := hub.sendConfigFrame("cfg-stale", 1, `[]`); err != nil {
		t.Fatalf("sendConfigFrame: %v", err)
	}
	resp := hub.waitResponse(3 * time.Second)
	if resp.Error != "stale" {
		t.Errorf("response error = %q, want exact %q", resp.Error, "stale")
	}
}

func TestConfigFrameApplierPanicRecovered(t *testing.T) {
	hub := newFakeHub(t)
	applier := func(_ context.Context, f ConfigFrame) (ConfigResult, error) {
		panic("applier exploded")
	}
	client, cancel, _ := startClient(t, testClientOpts{serverURL: hub.url(), configApplier: applier})
	defer cancel()
	hub.waitRegisters(1, 3*time.Second)

	if err := hub.sendConfigFrame("cfg-panic", 1, `[]`); err != nil {
		t.Fatalf("sendConfigFrame: %v", err)
	}
	resp := hub.waitResponse(3 * time.Second)
	if !strings.Contains(resp.Error, "panicked") {
		t.Errorf("response error = %q, want panic message", resp.Error)
	}

	// Connection stays healthy after a panicking apply.
	if err := hub.sendRequest("after-panic", "echo", map[string]any{"value": "alive"}); err != nil {
		t.Fatalf("sendRequest: %v", err)
	}
	resp2 := hub.waitResponse(3 * time.Second)
	if resp2.Error != "" || resp2.Payload["echoed"] != "alive" {
		t.Errorf("connection unhealthy after panicking apply: %+v", resp2)
	}
	if st := client.State(); !st.Connected {
		t.Error("client disconnected after panicking apply")
	}
}

func TestConfigFrameApplyDoesNotBlockReadLoop(t *testing.T) {
	hub := newFakeHub(t)
	release := make(chan struct{})
	applier := func(_ context.Context, f ConfigFrame) (ConfigResult, error) {
		<-release // block until the test releases it
		return ConfigResult{Applied: 1, Tools: 0}, nil
	}
	_, cancel, _ := startClient(t, testClientOpts{serverURL: hub.url(), configApplier: applier})
	defer cancel()
	hub.waitRegisters(1, 3*time.Second)

	if err := hub.sendConfigFrame("cfg-slow", 1, `[]`); err != nil {
		t.Fatalf("sendConfigFrame: %v", err)
	}

	// While the applier is blocked, a normal tool call must still round-trip,
	// proving the apply runs off the read loop.
	if err := hub.sendRequest("req-while-blocked", "echo", map[string]any{"value": "pong"}); err != nil {
		t.Fatalf("sendRequest: %v", err)
	}
	resp := hub.waitResponse(3 * time.Second)
	if resp.Error != "" || resp.Payload["echoed"] != "pong" {
		t.Fatalf("read loop blocked during apply: %+v", resp)
	}

	close(release)
	ack := hub.waitResponse(3 * time.Second)
	if string(ack.ID) != `"cfg-slow"` {
		t.Errorf("config ack id = %s, want cfg-slow", ack.ID)
	}
}

func TestConfigFrameOptedOutIgnored(t *testing.T) {
	hub := newFakeHub(t)
	// No configApplier and no accepts flag: a push must be ignored (no ack).
	client, cancel, _ := startClient(t, testClientOpts{serverURL: hub.url()})
	defer cancel()
	regs := hub.waitRegisters(1, 3*time.Second)
	if regs[0].AcceptsManagedConfig {
		t.Error("register must not advertise accepts_managed_config when opted out")
	}

	if err := hub.sendConfigFrame("cfg-ignored", 1, `[]`); err != nil {
		t.Fatalf("sendConfigFrame: %v", err)
	}
	if resp, ok := hub.tryResponse(300 * time.Millisecond); ok {
		t.Errorf("opted-out client acked a config frame: %+v", resp)
	}

	// Connection still healthy.
	if err := hub.sendRequest("req-1", "echo", map[string]any{"value": "ok"}); err != nil {
		t.Fatalf("sendRequest: %v", err)
	}
	if resp := hub.waitResponse(3 * time.Second); resp.Error != "" {
		t.Errorf("connection unhealthy after ignored config frame: %+v", resp)
	}
	if st := client.State(); !st.Connected {
		t.Error("client disconnected after ignored config frame")
	}
}

func TestRegisterAdvertisesAcceptsManagedConfig(t *testing.T) {
	hub := newFakeHub(t)
	applier := func(_ context.Context, f ConfigFrame) (ConfigResult, error) {
		return ConfigResult{Applied: 1, Tools: 0}, nil
	}
	_, cancel, _ := startClient(t, testClientOpts{serverURL: hub.url(), configApplier: applier, acceptsManagedConfig: true})
	defer cancel()
	regs := hub.waitRegisters(1, 3*time.Second)
	if !regs[0].AcceptsManagedConfig {
		t.Error("register must advertise accepts_managed_config=true when opted in")
	}
}

func TestConfigFrameAckContainsNoSecretValues(t *testing.T) {
	hub := newFakeHub(t)
	const secret = "super-secret-credential-value"
	applier := func(_ context.Context, f ConfigFrame) (ConfigResult, error) {
		// Verify the frame carried the secret (so the test is meaningful), then
		// return only counts.
		if len(f.Servers) != 1 || f.Servers[0].Env["TOKEN"] != secret {
			t.Errorf("applier frame missing secret: %+v", f)
		}
		return ConfigResult{Applied: 1, Tools: 3}, nil
	}
	_, cancel, _ := startClient(t, testClientOpts{serverURL: hub.url(), configApplier: applier})
	defer cancel()
	hub.waitRegisters(1, 3*time.Second)

	servers := fmt.Sprintf(`[{"name":"svc","transport":"stdio","command":"run","env":{"TOKEN":%q}}]`, secret)
	if err := hub.sendConfigFrame("cfg-secret", 1, servers); err != nil {
		t.Fatalf("sendConfigFrame: %v", err)
	}
	resp := hub.waitResponse(3 * time.Second)
	raw, _ := json.Marshal(resp)
	if strings.Contains(string(raw), secret) {
		t.Errorf("ack leaked secret value: %s", raw)
	}
	if strings.Contains(string(raw), "TOKEN") {
		t.Errorf("ack leaked credential key: %s", raw)
	}
}

func TestConfigFrameOversizedAcksErrorNotDisconnect(t *testing.T) {
	hub := newFakeHub(t)
	applier := func(_ context.Context, f ConfigFrame) (ConfigResult, error) {
		return ConfigResult{Applied: 1, Tools: 0}, nil
	}
	client, cancel, _ := startClient(t, testClientOpts{
		serverURL:     hub.url(),
		configApplier: applier,
		maxFrameSize:  300,
	})
	defer cancel()
	hub.waitRegisters(1, 3*time.Second)

	// A config frame well over the 300-byte limit.
	bigEnv := strings.Repeat("x", 2000)
	servers := fmt.Sprintf(`[{"name":"big","transport":"stdio","command":"run","env":{"K":%q}}]`, bigEnv)
	if err := hub.sendConfigFrame("cfg-big", 1, servers); err != nil {
		t.Fatalf("sendConfigFrame: %v", err)
	}

	resp := hub.waitResponse(3 * time.Second)
	if got := string(resp.ID); got != `"cfg-big"` {
		t.Errorf("response id = %s, want cfg-big", got)
	}
	if !strings.Contains(resp.Error, "exceeds read limit") {
		t.Errorf("response error = %q, want read-limit error", resp.Error)
	}

	// The applier must NOT have been invoked for an oversized frame.
	// Connection must remain healthy: a subsequent call round-trips.
	if err := hub.sendRequest("after-big", "echo", map[string]any{"value": "alive"}); err != nil {
		t.Fatalf("sendRequest: %v", err)
	}
	resp2 := hub.waitResponse(3 * time.Second)
	if resp2.Error != "" || resp2.Payload["echoed"] != "alive" {
		t.Errorf("connection unhealthy after oversized config frame: %+v", resp2)
	}
	if st := client.State(); !st.Connected {
		t.Error("client disconnected after oversized config frame")
	}
}

func TestOversizedNonConfigFrameDroppedConnectionHealthy(t *testing.T) {
	hub := newFakeHub(t)
	client, cancel, _ := startClient(t, testClientOpts{serverURL: hub.url(), maxFrameSize: 300})
	defer cancel()
	hub.waitRegisters(1, 3*time.Second)

	// A large non-config frame (a request) is dropped, not acked, and the
	// connection stays up.
	if err := hub.sendRaw(fmt.Sprintf(`{"type":"request","id":"big-req","payload":{"method":"tools/call","params":{"name":"echo","arguments":{"value":%q}}}}`, strings.Repeat("x", 2000))); err != nil {
		t.Fatalf("sendRaw: %v", err)
	}
	if err := hub.sendRequest("after-big-req", "echo", map[string]any{"value": "alive"}); err != nil {
		t.Fatalf("sendRequest: %v", err)
	}
	resp := hub.waitResponse(3 * time.Second)
	if resp.Error != "" || resp.Payload["echoed"] != "alive" {
		t.Errorf("connection unhealthy after oversized non-config frame: %+v", resp)
	}
	if st := client.State(); !st.Connected {
		t.Error("client disconnected after oversized non-config frame")
	}
}
