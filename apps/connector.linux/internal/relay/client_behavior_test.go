package relay

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// ---------------------------------------------------------------------------
// fakeHub extensions used by the behavior tests
// ---------------------------------------------------------------------------

// controlPongCount returns how many WS control pongs the hub has received.
func (h *fakeHub) controlPongCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.controlPongN
}

// sendControlPing sends a WS control ping to the connected client.
func (h *fakeHub) sendControlPing() error {
	h.mu.Lock()
	conn := h.conn
	h.mu.Unlock()
	if conn == nil {
		return errors.New("fake hub: no connection")
	}
	return h.writeFrame(conn, websocket.PingMessage, nil)
}

// waitAppPong blocks until the client sends an app-level pong frame.
func (h *fakeHub) waitAppPong(timeout time.Duration) (PongFrame, bool) {
	select {
	case pong := <-h.pongCh:
		return pong, true
	case <-time.After(timeout):
		return PongFrame{}, false
	}
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// TestConcurrentDispatchCorrelatesResponses drives several simultaneous calls
// through the client's goroutine-per-request dispatch and asserts each
// response is framed as a response and carries back its own request id and
// matching payload — i.e. correlation survives out-of-order completion.
func TestConcurrentDispatchCorrelatesResponses(t *testing.T) {
	hub := newFakeHub(t)
	_, cancel, _ := startClient(t, testClientOpts{serverURL: hub.url()})
	defer cancel()
	hub.waitRegisters(1, 3*time.Second)

	const n = 8
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("corr-%d", i)
		if err := hub.sendRequest(id, "echo", map[string]any{"value": id}); err != nil {
			t.Fatalf("sendRequest(%s): %v", id, err)
		}
	}

	got := make(map[string]string, n)
	for i := 0; i < n; i++ {
		resp := hub.waitResponse(3 * time.Second)
		if resp.Type != FrameResponse {
			t.Errorf("response type = %q, want %q", resp.Type, FrameResponse)
		}
		if resp.Error != "" {
			t.Fatalf("response error = %q, want none", resp.Error)
		}
		var id string
		if err := json.Unmarshal(resp.ID, &id); err != nil {
			t.Fatalf("unmarshal response id %s: %v", resp.ID, err)
		}
		got[id], _ = resp.Payload["echoed"].(string)
	}
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("corr-%d", i)
		if got[id] != id {
			t.Errorf("id %q echoed %q, want %q (correlation broken)", id, got[id], id)
		}
	}
}

// TestClientAnswersAppLevelPingWithPong asserts the client replies to an
// application-level ping frame with an application-level pong frame.
func TestClientAnswersAppLevelPingWithPong(t *testing.T) {
	hub := newFakeHub(t)
	_, cancel, _ := startClient(t, testClientOpts{serverURL: hub.url()})
	defer cancel()
	hub.waitRegisters(1, 3*time.Second)

	if err := hub.sendRaw(`{"type":"ping"}`); err != nil {
		t.Fatalf("sendRaw: %v", err)
	}
	pong, ok := hub.waitAppPong(3 * time.Second)
	if !ok {
		t.Fatal("client did not answer an app-level ping with a pong")
	}
	if pong.Type != FramePong {
		t.Errorf("pong frame type = %q, want %q", pong.Type, FramePong)
	}
}

// TestClientAnswersControlPingWithPong asserts the client's WS control ping
// handler replies with a control pong (hub-side PongHandler fires).
func TestClientAnswersControlPingWithPong(t *testing.T) {
	hub := newFakeHub(t)
	_, cancel, _ := startClient(t, testClientOpts{serverURL: hub.url()})
	defer cancel()
	hub.waitRegisters(1, 3*time.Second)

	before := hub.controlPongCount()
	if err := hub.sendControlPing(); err != nil {
		t.Fatalf("sendControlPing: %v", err)
	}
	waitFor(t, 3*time.Second, "client control pong", func() bool {
		return hub.controlPongCount() > before
	})
}

// TestMalformedKnownFramesIgnored feeds frames whose type is known but whose
// fields cannot be decoded, then verifies the connection stays healthy and a
// subsequent call still round-trips.
func TestMalformedKnownFramesIgnored(t *testing.T) {
	hub := newFakeHub(t)
	client, cancel, _ := startClient(t, testClientOpts{serverURL: hub.url()})
	defer cancel()
	hub.waitRegisters(1, 3*time.Second)

	malformed := []string{
		`{"type":"request","id":{},"payload":{}}`, // id must decode into a string
		`{"type":"register","tools":123}`,         // tools must decode into an object
		`{"type":"response"`,                      // truncated JSON
	}
	for _, raw := range malformed {
		if err := hub.sendRaw(raw); err != nil {
			t.Fatalf("sendRaw(%s): %v", raw, err)
		}
	}

	if err := hub.sendRequest("after-malformed", "echo", map[string]any{"value": "alive"}); err != nil {
		t.Fatalf("sendRequest: %v", err)
	}
	resp := hub.waitResponse(3 * time.Second)
	if got := string(resp.ID); got != `"after-malformed"` {
		t.Errorf("response id = %s, want after-malformed", got)
	}
	if resp.Payload["echoed"] != "alive" {
		t.Errorf("payload = %v, want echoed=alive", resp.Payload)
	}
	if st := client.State(); !st.Connected {
		t.Error("client disconnected after malformed frames")
	}
}

// TestBackoffResetsAfterSustainedConnection verifies the Run reconnect loop
// resets its attempt counter once a connection has stayed up for
// backoffResetAfter. Two sustained connections are dropped; if the counter
// were not reset, the second reconnect would wait base*factor (3.2s) instead
// of base (400ms).
func TestBackoffResetsAfterSustainedConnection(t *testing.T) {
	const (
		base       = 400 * time.Millisecond
		factor     = 8.0
		resetAfter = 20 * time.Millisecond
		hold       = 2 * resetAfter
		upper      = 2 * base
		lower      = base / 2
	)
	hub := newFakeHub(t)
	_, cancel, _ := startClient(t, testClientOpts{
		serverURL:         hub.url(),
		backoffBase:       base,
		backoffMax:        10 * time.Second,
		backoffFactor:     factor,
		backoffResetAfter: resetAfter,
	})
	defer cancel()

	hub.waitRegisters(1, 3*time.Second)

	time.Sleep(hold)
	droppedAt := time.Now()
	hub.drop()
	hub.waitRegisters(1, 3*time.Second)
	firstGap := time.Since(droppedAt)

	// Second sustained cycle: without the reset this delay would grow to
	// base*factor.
	time.Sleep(hold)
	droppedAt = time.Now()
	hub.drop()
	hub.waitRegisters(1, 3*time.Second)
	secondGap := time.Since(droppedAt)

	if firstGap > upper {
		t.Errorf("first reconnect gap = %v, want <= %v (base backoff)", firstGap, upper)
	}
	if secondGap > upper {
		t.Errorf("second reconnect gap = %v, want <= %v (counter must reset after sustained connection)", secondGap, upper)
	}
	if secondGap < lower {
		t.Errorf("second reconnect gap = %v, want >= %v (backoff must still be applied)", secondGap, lower)
	}
}
