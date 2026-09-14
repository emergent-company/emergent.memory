package relay

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/emergent-company/emergent.memory/apps/connector.linux/internal/toolreg"
)

// ---------------------------------------------------------------------------
// Test tools + registry helpers
// ---------------------------------------------------------------------------

// testRegistry builds a toolreg.Registry mirroring the hub's expectations:
// nested tools/list payload shape, three tools.
func testRegistry(t *testing.T) *toolreg.Registry {
	t.Helper()
	r := toolreg.New()
	echo := toolreg.Tool{
		Name:        "echo",
		Description: "echo a value",
		InputSchema: map[string]any{"type": "object", "properties": map[string]any{"value": map[string]any{"type": "string"}}},
		Handler: func(_ context.Context, args map[string]any) (map[string]any, error) {
			return map[string]any{"echoed": args["value"]}, nil
		},
	}
	fail := toolreg.Tool{
		Name:        "fail",
		Description: "always fails",
		InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
		Handler: func(context.Context, map[string]any) (map[string]any, error) {
			return nil, errors.New("kaboom from handler")
		},
	}
	panicTool := toolreg.Tool{
		Name:        "panic_tool",
		Description: "panics",
		InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
		Handler: func(context.Context, map[string]any) (map[string]any, error) {
			panic("handler panicked")
		},
	}
	for _, tool := range []toolreg.Tool{echo, fail, panicTool} {
		if err := r.Register(tool); err != nil {
			t.Fatalf("register test tool: %v", err)
		}
	}
	return r
}

// quietLogger returns a logger that drops everything (race-safe in tests),
// or streams to stderr when RELAY_DEBUG is set.
func quietLogger() *slog.Logger {
	if os.Getenv("RELAY_DEBUG") != "" {
		return slog.New(slog.NewTextHandler(os.Stderr, nil))
	}
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// ---------------------------------------------------------------------------
// Fake hub — implements the mcprelay handler semantics for tests
// ---------------------------------------------------------------------------

var testUpgrader = websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}

// fakeHub is an in-memory mcprelay stand-in: it enforces register-first,
// acks registrations, counts protocol pings, forwards request frames, and
// receives response frames.
type fakeHub struct {
	t       *testing.T
	server  *httptest.Server
	auth    string
	rawPath string
	query   url.Values

	mu           sync.Mutex
	writeMu      sync.Mutex
	conn         *websocket.Conn
	pingCount    int
	controlPongN int

	regCh  chan RegisterFrame
	respCh chan ResponseFrame
	pongCh chan PongFrame
}

func newFakeHub(t *testing.T) *fakeHub {
	t.Helper()
	h := &fakeHub{
		t:      t,
		regCh:  make(chan RegisterFrame, 64),
		respCh: make(chan ResponseFrame, 64),
		pongCh: make(chan PongFrame, 16),
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.mu.Lock()
		h.rawPath = r.URL.Path
		h.auth = r.Header.Get("Authorization")
		h.query = r.URL.Query()
		h.mu.Unlock()

		conn, err := testUpgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		_ = conn.SetReadDeadline(time.Now().Add(30 * time.Second))
		conn.SetPingHandler(func(string) error {
			h.mu.Lock()
			h.pingCount++
			h.mu.Unlock()
			return h.writeFrame(conn, websocket.PongMessage, nil)
		})
		conn.SetPongHandler(func(string) error {
			h.mu.Lock()
			h.controlPongN++
			h.mu.Unlock()
			return conn.SetReadDeadline(time.Now().Add(30 * time.Second))
		})

		h.mu.Lock()
		h.conn = conn
		h.mu.Unlock()
		h.serve(conn)
	}))
	t.Cleanup(srv.Close)
	h.server = srv
	return h
}

// writeFrame serializes all hub-side writes so the ack, control pong replies,
// and test-driven request frames never race.
func (h *fakeHub) writeFrame(conn *websocket.Conn, msgType int, data []byte) error {
	h.writeMu.Lock()
	defer h.writeMu.Unlock()
	_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	return conn.WriteMessage(msgType, data)
}

// serve runs one hub-side connection: first frame must be register, then it
// acks and relays response frames.
func (h *fakeHub) serve(conn *websocket.Conn) {
	_, data, err := conn.ReadMessage()
	if err != nil {
		return
	}
	frame, err := Parse(data)
	if err != nil {
		h.t.Errorf("fake hub: first frame not parseable: %v", err)
		return
	}
	reg, ok := frame.(*RegisterFrame)
	if !ok {
		// Mirror mcprelay: reject non-register first frames.
		_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"error","message":"first frame must be register"}`))
		h.t.Errorf("fake hub: first frame type = %T, want register", frame)
		return
	}
	ack, _ := json.Marshal(map[string]string{"type": "registered", "instance_id": reg.InstanceID})
	if err := h.writeFrame(conn, websocket.TextMessage, ack); err != nil {
		return
	}
	// The ack is on the wire before the register is reported observed, so a
	// test that sees the event can safely drive the connection without
	// racing the hub's own ack write.
	h.regCh <- *reg

	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			_ = conn.Close()
			return
		}
		parsed, err := Parse(data)
		if err != nil {
			continue
		}
		switch f := parsed.(type) {
		case *ResponseFrame:
			h.respCh <- *f
		case *PongFrame:
			h.pongCh <- *f
		case *RegisterFrame:
			// A re-register on a live connection (RefreshRegistration): ack it
			// and record it so tests can assert the updated tool list.
			ack, _ := json.Marshal(map[string]string{"type": "registered", "instance_id": f.InstanceID})
			if err := h.writeFrame(conn, websocket.TextMessage, ack); err != nil {
				return
			}
			h.regCh <- *f
		}
	}
}

// waitRegisters blocks until at least n register frames arrived (or times out).
func (h *fakeHub) waitRegisters(n int, timeout time.Duration) []RegisterFrame {
	var out []RegisterFrame
	deadline := time.After(timeout)
	for len(out) < n {
		select {
		case reg := <-h.regCh:
			out = append(out, reg)
		case <-deadline:
			h.t.Errorf("fake hub: timed out waiting for %d registers, got %d", n, len(out))
			return out
		}
	}
	return out
}

func (h *fakeHub) pingCountNow() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.pingCount
}

// sendRequest writes a tools/call request frame to the current connection.
func (h *fakeHub) sendRequest(id, name string, args map[string]any) error {
	payload := map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      name,
			"arguments": args,
		},
	}
	frame := &RequestFrame{Type: FrameRequest, ID: id, Payload: payload}
	data, err := Marshal(frame)
	if err != nil {
		return err
	}
	return h.writeText(data)
}

// sendRaw writes a raw text frame to the current connection.
func (h *fakeHub) sendRaw(raw string) error {
	return h.writeText([]byte(raw))
}

func (h *fakeHub) writeText(data []byte) error {
	h.mu.Lock()
	conn := h.conn
	h.mu.Unlock()
	if conn == nil {
		return errors.New("fake hub: no connection")
	}
	return h.writeFrame(conn, websocket.TextMessage, data)
}

// drop closes the current connection from the hub side.
func (h *fakeHub) drop() {
	h.mu.Lock()
	conn := h.conn
	h.mu.Unlock()
	if conn != nil {
		_ = conn.Close()
	}
}

func (h *fakeHub) url() string { return h.server.URL }

// waitResponse blocks until a response frame arrives or times out.
func (h *fakeHub) waitResponse(timeout time.Duration) ResponseFrame {
	select {
	case resp := <-h.respCh:
		return resp
	case <-time.After(timeout):
		h.t.Errorf("fake hub: timed out waiting for response frame")
		return ResponseFrame{}
	}
}

// ---------------------------------------------------------------------------
// Client runner helpers
// ---------------------------------------------------------------------------

type testClientOpts struct {
	pingPeriod time.Duration
	serverURL  string
	token      string
	projectID  string
	instanceID string
	registry   ToolRegistry
	// disabled marks tools disabled on the test registry before Run starts,
	// mirroring the relay wiring's treatment of config disabled_tools.
	disabled []string

	// Optional reconnect-backoff overrides; zero values keep the fast test
	// defaults.
	backoffBase       time.Duration
	backoffMax        time.Duration
	backoffFactor     float64
	backoffResetAfter time.Duration
}

// runHandle lets multiple goroutines (test body and cleanup) wait on the
// same Run completion.
type runHandle struct {
	done chan struct{}
	err  error
}

// wait blocks until Run returns (or timeout) and returns its error.
func (h *runHandle) wait(timeout time.Duration) (error, bool) {
	select {
	case <-h.done:
		return h.err, true
	case <-time.After(timeout):
		return nil, false
	}
}

// waitFor polls cond until it holds or the timeout expires.
func waitFor(t *testing.T, timeout time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// startClient builds a client with tiny timers, starts Run in the background,
// and registers a cleanup that cancels and waits for Run to return.
func startClient(t *testing.T, opts testClientOpts) (*Client, context.CancelFunc, *runHandle) {
	t.Helper()
	if opts.serverURL == "" {
		opts.serverURL = "http://hub.invalid"
	}
	if opts.token == "" {
		opts.token = "emt_testtoken"
	}
	if opts.instanceID == "" {
		opts.instanceID = "test-host-connector"
	}
	if opts.registry == nil {
		opts.registry = testRegistry(t)
	}
	if len(opts.disabled) > 0 {
		reg, ok := opts.registry.(*toolreg.Registry)
		if !ok {
			t.Fatalf("disabled list requires a *toolreg.Registry, got %T", opts.registry)
		}
		for _, name := range opts.disabled {
			reg.MarkDisabled(name)
		}
	}
	backoffBase := opts.backoffBase
	if backoffBase == 0 {
		backoffBase = 15 * time.Millisecond
	}
	backoffMax := opts.backoffMax
	if backoffMax == 0 {
		backoffMax = 250 * time.Millisecond
	}
	backoffFactor := opts.backoffFactor
	if backoffFactor == 0 {
		backoffFactor = 2
	}
	clientOpts := []Option{
		WithPingPeriod(opts.pingPeriod),
		WithAckTimeout(2 * time.Second),
		WithCallTimeout(2 * time.Second),
		WithBackoff(backoffBase, backoffMax, backoffFactor),
		WithReadWait(5 * time.Second),
		WithLogger(quietLogger()),
	}
	if opts.backoffResetAfter != 0 {
		clientOpts = append(clientOpts, WithBackoffResetAfter(opts.backoffResetAfter))
	}
	client, err := New(opts.serverURL, opts.token, opts.projectID, opts.instanceID, "0.1.0", opts.registry, clientOpts...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	handle := &runHandle{done: make(chan struct{})}
	go func() {
		handle.err = client.Run(ctx)
		close(handle.done)
	}()
	t.Cleanup(func() {
		cancel()
		if _, ok := handle.wait(3 * time.Second); !ok {
			t.Error("client Run did not return after cancel")
		}
	})
	return client, cancel, handle
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestConnectURL(t *testing.T) {
	cases := []struct {
		name      string
		serverURL string
		projectID string
		want      string
		wantErr   bool
	}{
		{name: "https to wss", serverURL: "https://memory.example.test", want: "wss://memory.example.test/api/mcp-relay/connect"},
		{name: "http to ws", serverURL: "http://localhost:8095", want: "ws://localhost:8095/api/mcp-relay/connect"},
		{name: "trailing slash stripped", serverURL: "https://memory.example.test/", want: "wss://memory.example.test/api/mcp-relay/connect"},
		{name: "path preserved", serverURL: "https://host.test/base/", want: "wss://host.test/base/api/mcp-relay/connect"},
		{name: "project id query", serverURL: "https://host.test", projectID: "proj-1", want: "wss://host.test/api/mcp-relay/connect?projectId=proj-1"},
		{name: "bad scheme", serverURL: "ftp://host.test", wantErr: true},
		{name: "unparseable", serverURL: "://bad", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ConnectURL(tc.serverURL, tc.projectID)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("ConnectURL(%q) = %q, want error", tc.serverURL, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ConnectURL(%q): %v", tc.serverURL, err)
			}
			if got != tc.want {
				t.Errorf("ConnectURL(%q) = %q, want %q", tc.serverURL, got, tc.want)
			}
		})
	}
}

func TestRetryDelay(t *testing.T) {
	c, err := New("https://h", "t", "", "i", "v", testRegistry(t))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	cases := []struct {
		attempt int
		want    time.Duration
	}{
		{attempt: 0, want: 30 * time.Second},
		{attempt: 1, want: 60 * time.Second},
		{attempt: 2, want: 2 * time.Minute},
		{attempt: 3, want: 4 * time.Minute},
		{attempt: 4, want: 5 * time.Minute}, // capped
		{attempt: 9, want: 5 * time.Minute}, // stays capped
	}
	for _, tc := range cases {
		if got := c.retryDelay(tc.attempt); got != tc.want {
			t.Errorf("retryDelay(%d) = %v, want %v", tc.attempt, got, tc.want)
		}
	}
}

func TestClientConnectRegistersFirst(t *testing.T) {
	hub := newFakeHub(t)
	client, cancel, _ := startClient(t, testClientOpts{serverURL: hub.url(), instanceID: "mbp-1"})

	regs := hub.waitRegisters(1, 3*time.Second)
	reg := regs[0]
	if reg.InstanceID != "mbp-1" {
		t.Errorf("registered instance_id = %q, want mbp-1", reg.InstanceID)
	}
	if reg.Version != "0.1.0" {
		t.Errorf("registered version = %q, want 0.1.0", reg.Version)
	}
	items, ok := reg.Tools["tools"].([]any)
	if !ok {
		t.Fatalf("register tools not nested []any: %T", reg.Tools["tools"])
	}
	if len(items) != 3 {
		t.Errorf("registered tool count = %d, want 3", len(items))
	}

	hub.mu.Lock()
	auth, rawPath, query := hub.auth, hub.rawPath, hub.query
	hub.mu.Unlock()
	if auth != "Bearer emt_testtoken" {
		t.Errorf("Authorization = %q, want Bearer emt_testtoken", auth)
	}
	if rawPath != "/api/mcp-relay/connect" {
		t.Errorf("dial path = %q, want /api/mcp-relay/connect", rawPath)
	}
	if _, has := query["projectId"]; has {
		t.Errorf("projectId must be omitted when not configured, got %v", query)
	}

	// Ack handling is asynchronous on the client; wait until it lands.
	waitFor(t, 3*time.Second, "client registration to be acked", func() bool {
		return client.State().RegisterCount >= 1
	})
	if st := client.State(); !st.Connected {
		t.Error("State not connected after register")
	}

	cancel()
	waitFor(t, 3*time.Second, "client to disconnect after cancel", func() bool {
		return !client.State().Connected
	})
}

func TestClientConnectSendsProjectIDQuery(t *testing.T) {
	hub := newFakeHub(t)
	_, cancel, _ := startClient(t, testClientOpts{serverURL: hub.url(), projectID: "proj-42"})
	defer cancel()

	hub.waitRegisters(1, 3*time.Second)
	hub.mu.Lock()
	query := hub.query
	hub.mu.Unlock()
	if got := query.Get("projectId"); got != "proj-42" {
		t.Errorf("projectId query = %q, want proj-42", got)
	}
}

func TestReRegisterAfterDrop(t *testing.T) {
	hub := newFakeHub(t)
	client, cancel, _ := startClient(t, testClientOpts{serverURL: hub.url()})
	defer cancel()

	hub.waitRegisters(1, 3*time.Second)

	hub.drop()
	// The reconnect registers exactly once more; expect that new register.
	regs := hub.waitRegisters(1, 5*time.Second)
	reg := regs[0]
	if reg.InstanceID != "test-host-connector" {
		t.Errorf("re-registered instance_id = %q", reg.InstanceID)
	}
	if items := reg.Tools["tools"].([]any); len(items) != 3 {
		t.Errorf("re-registered tool count = %d, want 3", len(items))
	}
	waitFor(t, 3*time.Second, "re-registration to be acked", func() bool {
		return client.State().RegisterCount >= 2
	})
}

func TestDispatchSuccessRoundTrip(t *testing.T) {
	hub := newFakeHub(t)
	_, cancel, _ := startClient(t, testClientOpts{serverURL: hub.url()})
	defer cancel()
	hub.waitRegisters(1, 3*time.Second)

	if err := hub.sendRequest("req-100", "echo", map[string]any{"value": "hello"}); err != nil {
		t.Fatalf("sendRequest: %v", err)
	}
	resp := hub.waitResponse(3 * time.Second)
	if got := string(resp.ID); got != `"req-100"` {
		t.Errorf("response id = %s, want echo of request id", got)
	}
	if resp.Error != "" {
		t.Errorf("response error = %q, want none", resp.Error)
	}
	if resp.Payload["echoed"] != "hello" {
		t.Errorf("payload = %v, want echoed=hello", resp.Payload)
	}
}

func TestDispatchUnknownTool(t *testing.T) {
	hub := newFakeHub(t)
	_, cancel, _ := startClient(t, testClientOpts{serverURL: hub.url()})
	defer cancel()
	hub.waitRegisters(1, 3*time.Second)

	if err := hub.sendRequest("req-2", "does_not_exist", nil); err != nil {
		t.Fatalf("sendRequest: %v", err)
	}
	resp := hub.waitResponse(3 * time.Second)
	if got := string(resp.ID); got != `"req-2"` {
		t.Errorf("response id = %s, want req-2", got)
	}
	if !strings.Contains(resp.Error, `unknown tool: "does_not_exist"`) {
		t.Errorf("response error = %q, want unknown tool message", resp.Error)
	}
	if resp.Payload != nil {
		t.Errorf("payload = %v, want nil on error", resp.Payload)
	}
}

func TestDispatchHandlerError(t *testing.T) {
	hub := newFakeHub(t)
	_, cancel, _ := startClient(t, testClientOpts{serverURL: hub.url()})
	defer cancel()
	hub.waitRegisters(1, 3*time.Second)

	if err := hub.sendRequest("req-3", "fail", nil); err != nil {
		t.Fatalf("sendRequest: %v", err)
	}
	resp := hub.waitResponse(3 * time.Second)
	if got := string(resp.ID); got != `"req-3"` {
		t.Errorf("response id = %s, want req-3", got)
	}
	if !strings.Contains(resp.Error, "kaboom from handler") {
		t.Errorf("response error = %q, want handler message", resp.Error)
	}
}

func TestDispatchPanicRecovered(t *testing.T) {
	hub := newFakeHub(t)
	_, cancel, _ := startClient(t, testClientOpts{serverURL: hub.url()})
	defer cancel()
	hub.waitRegisters(1, 3*time.Second)

	if err := hub.sendRequest("req-4", "panic_tool", nil); err != nil {
		t.Fatalf("sendRequest: %v", err)
	}
	resp := hub.waitResponse(3 * time.Second)
	if !strings.Contains(resp.Error, "panicked") {
		t.Errorf("response error = %q, want panic message", resp.Error)
	}
}

func TestDispatchMalformedRequests(t *testing.T) {
	hub := newFakeHub(t)
	_, cancel, _ := startClient(t, testClientOpts{serverURL: hub.url()})
	defer cancel()
	hub.waitRegisters(1, 3*time.Second)

	cases := []struct {
		name string
		raw  string
		id   string
		want string
	}{
		{name: "wrong method", raw: `{"type":"request","id":"r5","payload":{"jsonrpc":"2.0","method":"tools/other","params":{}}}`, id: "r5", want: "unsupported method"},
		{name: "missing name", raw: `{"type":"request","id":"r6","payload":{"jsonrpc":"2.0","method":"tools/call","params":{"arguments":{}}}}`, id: "r6", want: "missing tool name"},
		{name: "missing params", raw: `{"type":"request","id":"r7","payload":{"jsonrpc":"2.0","method":"tools/call"}}`, id: "r7", want: "missing params"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := hub.sendRaw(tc.raw); err != nil {
				t.Fatalf("sendRaw: %v", err)
			}
			resp := hub.waitResponse(3 * time.Second)
			if got := string(resp.ID); got != `"`+tc.id+`"` {
				t.Errorf("response id = %s, want %q", got, tc.id)
			}
			if !strings.Contains(resp.Error, tc.want) {
				t.Errorf("response error = %q, want containing %q", resp.Error, tc.want)
			}
		})
	}
}

func TestUnknownAndMalformedFramesNotFatal(t *testing.T) {
	hub := newFakeHub(t)
	client, cancel, _ := startClient(t, testClientOpts{serverURL: hub.url()})
	defer cancel()
	hub.waitRegisters(1, 3*time.Second)

	// Unknown type and malformed JSON must be logged and ignored.
	for _, raw := range []string{`{"type":"teleport","to":"mars"}`, `this is not json`} {
		if err := hub.sendRaw(raw); err != nil {
			t.Fatalf("sendRaw: %v", err)
		}
	}

	// App-level ping gets an app-level pong (hub tolerates the reply).
	if err := hub.sendRaw(`{"type":"ping"}`); err != nil {
		t.Fatalf("sendRaw: %v", err)
	}

	// Connection still healthy afterwards.
	if err := hub.sendRequest("req-8", "echo", map[string]any{"value": "still-alive"}); err != nil {
		t.Fatalf("sendRequest: %v", err)
	}
	resp := hub.waitResponse(3 * time.Second)
	if resp.Error != "" || resp.Payload["echoed"] != "still-alive" {
		t.Errorf("connection unhealthy after noise frames: %+v", resp)
	}
	if st := client.State(); !st.Connected {
		t.Error("client disconnected after noise frames")
	}
}

func TestPingCadence(t *testing.T) {
	hub := newFakeHub(t)
	_, cancel, _ := startClient(t, testClientOpts{serverURL: hub.url(), pingPeriod: 20 * time.Millisecond})
	defer cancel()
	hub.waitRegisters(1, 3*time.Second)

	deadline := time.Now().Add(2 * time.Second)
	for hub.pingCountNow() < 3 {
		if time.Now().After(deadline) {
			t.Fatalf("hub received %d pings, want >= 3 within 2s", hub.pingCountNow())
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestCleanShutdown(t *testing.T) {
	hub := newFakeHub(t)
	_, cancel, handle := startClient(t, testClientOpts{serverURL: hub.url()})
	hub.waitRegisters(1, 3*time.Second)

	cancel()
	err, ok := handle.wait(3 * time.Second)
	if !ok {
		t.Fatal("Run did not return after context cancel")
	}
	if err != nil {
		t.Errorf("Run returned error on cancel: %v", err)
	}
}

func TestNoGoroutineLeakAcrossCycles(t *testing.T) {
	runCycle := func() {
		hub := newFakeHub(t)
		_, cancel, handle := startClient(t, testClientOpts{serverURL: hub.url()})
		hub.waitRegisters(1, 3*time.Second)
		cancel()
		if _, ok := handle.wait(3 * time.Second); !ok {
			t.Fatal("Run did not return")
		}
	}

	runCycle() // warm-up

	time.Sleep(50 * time.Millisecond)
	base := runtime.NumGoroutine()
	for i := 0; i < 3; i++ {
		runCycle()
	}
	time.Sleep(100 * time.Millisecond)
	after := runtime.NumGoroutine()
	if after > base+3 {
		t.Errorf("goroutines grew from %d to %d across connect/cancel cycles", base, after)
	}
}

func TestDisabledToolsExcludedFromRegister(t *testing.T) {
	hub := newFakeHub(t)
	_, cancel, _ := startClient(t, testClientOpts{serverURL: hub.url(), disabled: []string{"echo"}})
	defer cancel()

	regs := hub.waitRegisters(1, 3*time.Second)
	items, ok := regs[0].Tools["tools"].([]any)
	if !ok {
		t.Fatalf("register tools not nested []any: %T", regs[0].Tools["tools"])
	}
	var names []string
	for _, item := range items {
		m := item.(map[string]any)
		names = append(names, m["name"].(string))
	}
	if len(names) != 2 {
		t.Errorf("registered tool count = %d, want 2 (%v)", len(names), names)
	}
	for _, name := range names {
		if name == "echo" {
			t.Errorf("disabled tool %q must not appear in the register frame: %v", name, names)
		}
	}
	// The remaining tools must still be present.
	for _, want := range []string{"fail", "panic_tool"} {
		found := false
		for _, name := range names {
			if name == want {
				found = true
			}
		}
		if !found {
			t.Errorf("enabled tool %q missing from register frame: %v", want, names)
		}
	}
}

func TestDispatchDisabledToolRejected(t *testing.T) {
	hub := newFakeHub(t)
	_, cancel, _ := startClient(t, testClientOpts{serverURL: hub.url(), disabled: []string{"echo"}})
	defer cancel()
	hub.waitRegisters(1, 3*time.Second)

	if err := hub.sendRequest("req-disabled-1", "echo", map[string]any{"value": "x"}); err != nil {
		t.Fatalf("sendRequest: %v", err)
	}
	resp := hub.waitResponse(3 * time.Second)
	if got := string(resp.ID); got != `"req-disabled-1"` {
		t.Errorf("response id = %s, want req-disabled-1", got)
	}
	if !strings.Contains(resp.Error, "disabled") || !strings.Contains(resp.Error, `"echo"`) {
		t.Errorf("response error = %q, want it naming the tool as disabled", resp.Error)
	}
	if resp.Payload != nil {
		t.Errorf("payload = %v, want nil on disabled error", resp.Payload)
	}
}
