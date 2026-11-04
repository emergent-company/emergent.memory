// Bridge unit tests — verifiable on any platform via the exported Go helpers.
//
// CGO exports cannot be called from _test.go in the same package without
// building the c-archive first, so these tests exercise the pure-Go internals
// that the CGO exports delegate to.
package main

import (
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// parseEnvelope unmarshals the standard JSON envelope from bridge functions.
func parseEnvelope(t *testing.T, s string) (result json.RawMessage, errStr string) {
	t.Helper()
	var env struct {
		Result json.RawMessage `json:"result,omitempty"`
		Error  string          `json:"error,omitempty"`
	}
	if err := json.Unmarshal([]byte(s), &env); err != nil {
		t.Fatalf("parseEnvelope: malformed JSON: %v — raw: %s", err, s)
	}
	return env.Result, env.Error
}

// ---------------------------------------------------------------------------
// Task 3.1 — envelope helpers
// ---------------------------------------------------------------------------

func TestOKJSON(t *testing.T) {
	type payload struct {
		Name string `json:"name"`
	}
	raw := goOKJSON(payload{Name: "emergent"})
	result, errStr := parseEnvelope(t, raw)
	if errStr != "" {
		t.Fatalf("expected no error, got: %s", errStr)
	}
	var p payload
	if err := json.Unmarshal(result, &p); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if p.Name != "emergent" {
		t.Errorf("name = %q, want %q", p.Name, "emergent")
	}
}

func TestErrJSON(t *testing.T) {
	raw := goErrJSONStr("something went wrong")
	_, errStr := parseEnvelope(t, raw)
	if !strings.Contains(errStr, "something went wrong") {
		t.Errorf("expected error string, got: %s", errStr)
	}
}

// ---------------------------------------------------------------------------
// Task 3.1 — client handle registry
// ---------------------------------------------------------------------------

func TestClientRegistry(t *testing.T) {
	// Ensure handles start at a predictable point and increment.
	before := atomic.LoadUint32(&nextID)
	id1 := atomic.AddUint32(&nextID, 1) - 1
	id2 := atomic.AddUint32(&nextID, 1) - 1
	if id2 <= id1 {
		t.Errorf("handles should increment: id1=%d id2=%d", id1, id2)
	}
	_ = before
}

// ---------------------------------------------------------------------------
// Task 3.1 — CreateClient JSON validation (pure-Go path)
// ---------------------------------------------------------------------------

func TestCreateClientRequestValidation(t *testing.T) {
	tests := []struct {
		name    string
		payload string
		wantErr string
	}{
		{
			name:    "missing server_url",
			payload: `{"auth_mode":"apikey","api_key":"key123"}`,
			wantErr: "server_url is required",
		},
		{
			name:    "missing api_key",
			payload: `{"server_url":"http://localhost:9090","auth_mode":"apikey"}`,
			wantErr: "api_key is required",
		},
		{
			name:    "malformed json",
			payload: `{not valid}`,
			wantErr: "invalid config JSON",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := goCreateClient(tc.payload)
			_, errStr := parseEnvelope(t, result)
			if !strings.Contains(errStr, tc.wantErr) {
				t.Errorf("want error %q, got %q", tc.wantErr, errStr)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Task 3.1 — Ping validation (pure-Go path, no real server needed)
// ---------------------------------------------------------------------------

func TestPingUnknownHandle(t *testing.T) {
	result := goPing(99999, `{"message":"hello"}`)
	_, errStr := parseEnvelope(t, result)
	if !strings.Contains(errStr, "unknown client handle") {
		t.Errorf("expected unknown handle error, got %q", errStr)
	}
}

func TestPingMalformedJSON(t *testing.T) {
	// Register a fake client handle so the handle lookup succeeds.
	clientsMu.Lock()
	clients[777] = nil // nil client, but handle exists for this test
	clientsMu.Unlock()
	defer func() {
		clientsMu.Lock()
		delete(clients, 777)
		clientsMu.Unlock()
	}()

	result := goPing(777, `{bad json}`)
	_, errStr := parseEnvelope(t, result)
	if !strings.Contains(errStr, "invalid request JSON") {
		t.Errorf("expected JSON parse error, got %q", errStr)
	}
}

func TestPingEcho(t *testing.T) {
	// Register a fake client handle.
	clientsMu.Lock()
	clients[888] = nil
	clientsMu.Unlock()
	defer func() {
		clientsMu.Lock()
		delete(clients, 888)
		clientsMu.Unlock()
	}()

	msg := "hello from swift"
	result := goPing(888, `{"message":"`+msg+`"}`)
	raw, errStr := parseEnvelope(t, result)
	if errStr != "" {
		t.Fatalf("unexpected error: %s", errStr)
	}

	var resp PingResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatalf("unmarshal PingResponse: %v", err)
	}
	if resp.Echo != msg {
		t.Errorf("echo = %q, want %q", resp.Echo, msg)
	}
	if resp.Timestamp == "" {
		t.Error("timestamp should not be empty")
	}
	// Verify the timestamp parses as RFC3339
	if _, err := time.Parse(time.RFC3339, resp.Timestamp); err != nil {
		t.Errorf("timestamp %q not RFC3339: %v", resp.Timestamp, err)
	}
}

// ---------------------------------------------------------------------------
// Task 4.5 — Cancellation map
// ---------------------------------------------------------------------------

func TestCancelOperation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	opID := registerCancel(cancel)

	// Operation should still be running.
	select {
	case <-ctx.Done():
		t.Fatal("context should not be done yet")
	default:
	}

	// Cancel it.
	goCancel(opID)

	select {
	case <-ctx.Done():
		// expected
	case <-time.After(100 * time.Millisecond):
		t.Fatal("context should have been cancelled")
	}

	// Cancelling the same opID again is a no-op (should not panic).
	goCancel(opID)
}

func TestCancelNonexistentOperation(t *testing.T) {
	// Should not panic.
	goCancel(99999999)
}

func TestRegisterDeregisterCancel(t *testing.T) {
	_, cancel := context.WithCancel(context.Background())
	opID := registerCancel(cancel)

	fn := deregisterCancel(opID)
	if fn == nil {
		t.Fatal("expected cancel func, got nil")
	}
	// Deregistering again returns nil.
	fn2 := deregisterCancel(opID)
	if fn2 != nil {
		t.Fatal("expected nil on second deregister")
	}
}
