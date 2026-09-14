// Package e2e contains a hermetic end-to-end test for the memory-connector
// daemon. It builds the real `memory-connector` binary once and runs it as a
// subprocess against an in-process fake relay hub, exercising the whole path:
// config -> daemon start -> register -> tool-call round-trip -> `status --json`
// -> single-instance lock -> reconnect -> graceful stop.
//
// Everything is local (127.0.0.1, no external network, no secrets) and
// bounded by timeouts, so it is safe to run in CI. The whole package is
// skipped under `go test -short`.
package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/emergent-company/memory.web-ui/connector/internal/relay"
)

// binaryPath is the built memory-connector binary, populated once by TestMain.
var binaryPath string

// TestMain builds the real binary once for the whole package. Under -short the
// build is skipped entirely (every test skips itself first).
func TestMain(m *testing.M) {
	// testing.Init has registered the test flags by the time TestMain runs;
	// parse them so testing.Short() is safe to call.
	flag.Parse()
	if testing.Short() {
		os.Exit(m.Run())
	}

	dir, err := os.MkdirTemp("", "memory-connector-e2e-bin-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "e2e: create build dir: %v\n", err)
		os.Exit(1)
	}
	binaryPath = filepath.Join(dir, "memory-connector")
	if err := buildConnectorBinary(binaryPath); err != nil {
		fmt.Fprintf(os.Stderr, "e2e: build connector: %v\n", err)
		_ = os.RemoveAll(dir)
		os.Exit(1)
	}

	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

// buildConnectorBinary runs `go build` from the module root (the parent of
// this package's directory), independent of the test's working directory.
func buildConnectorBinary(out string) error {
	moduleRoot, err := findModuleRoot()
	if err != nil {
		return err
	}
	cmd := exec.Command("go", "build", "-o", out, "./cmd/memory-connector")
	cmd.Dir = moduleRoot
	var buf bytes.Buffer
	cmd.Stdout, cmd.Stderr = &buf, &buf
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(buf.String()))
	}
	return nil
}

// findModuleRoot walks up from the current working directory until it finds
// the go.mod that owns the connector module.
func findModuleRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("go.mod not found above test directory")
		}
		dir = parent
	}
}

// requireRunnable skips the test under -short or on non-Linux hosts (the test
// asserts the Linux built-in tool set).
func requireRunnable(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping connector e2e in -short mode")
	}
	if runtime.GOOS != "linux" {
		t.Skipf("connector e2e asserts the Linux tool set (host is %s)", runtime.GOOS)
	}
}

// ---------------------------------------------------------------------------
// Fake relay hub (mirrors connector/internal/relay/client_test.go's fakeHub,
// reusing the real frame types so the wire shapes can never drift)
// ---------------------------------------------------------------------------

var e2eUpgrader = websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}

// fakeHub is an in-process stand-in for the mcprelay hub. It enforces
// register-first, acks registrations, answers app-level pings, records
// response frames, and serves GET /api/mcp-relay/sessions so `status --json`
// can report the instance as connected.
type fakeHub struct {
	t      *testing.T
	server *httptest.Server

	mu       sync.Mutex
	writeMu  sync.Mutex
	conn     *websocket.Conn
	auth     string
	sessions map[string]int // instance_id -> registered tool count

	regCh  chan relay.RegisterFrame
	respCh chan relay.ResponseFrame
}

func newFakeHub(t *testing.T) *fakeHub {
	t.Helper()
	h := &fakeHub{
		t:        t,
		sessions: map[string]int{},
		regCh:    make(chan relay.RegisterFrame, 64),
		respCh:   make(chan relay.ResponseFrame, 64),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/mcp-relay/connect", h.handleConnect)
	mux.HandleFunc("/api/mcp-relay/sessions", h.handleSessions)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	h.server = srv
	return h
}

func (h *fakeHub) url() string { return h.server.URL }

func (h *fakeHub) handleConnect(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	h.auth = r.Header.Get("Authorization")
	h.mu.Unlock()

	conn, err := e2eUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	h.mu.Lock()
	h.conn = conn
	h.mu.Unlock()

	// Generous freshness window: the client pings every 25s, refreshing this.
	_ = conn.SetReadDeadline(time.Now().Add(120 * time.Second))
	conn.SetPingHandler(func(string) error {
		return h.writeFrame(conn, websocket.PongMessage, nil)
	})
	conn.SetPongHandler(func(string) error {
		_ = conn.SetReadDeadline(time.Now().Add(120 * time.Second))
		return nil
	})

	h.serve(conn)
}

// serve runs one hub-side connection: first frame must be register, then it
// acks, records, and relays all subsequent frames.
func (h *fakeHub) serve(conn *websocket.Conn) {
	_, data, err := conn.ReadMessage()
	if err != nil {
		return
	}
	frame, err := relay.Parse(data)
	if err != nil {
		h.t.Errorf("fake hub: first frame not parseable: %v", err)
		_ = conn.Close()
		return
	}
	reg, ok := frame.(*relay.RegisterFrame)
	if !ok {
		_ = h.writeFrame(conn, websocket.TextMessage, []byte(`{"type":"error","message":"first frame must be register"}`))
		h.t.Errorf("fake hub: first frame type = %T, want register", frame)
		_ = conn.Close()
		return
	}
	if err := h.ack(conn, *reg); err != nil {
		return
	}
	h.regCh <- *reg

	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			h.mu.Lock()
			delete(h.sessions, reg.InstanceID)
			h.mu.Unlock()
			_ = conn.Close()
			return
		}
		parsed, err := relay.Parse(data)
		if err != nil {
			continue
		}
		switch f := parsed.(type) {
		case *relay.ResponseFrame:
			h.respCh <- *f
		case *relay.RegisterFrame:
			if err := h.ack(conn, *f); err != nil {
				return
			}
			h.regCh <- *f
		case *relay.PingFrame:
			if pong, err := relay.Marshal(&relay.PongFrame{Type: relay.FramePong}); err == nil {
				_ = h.writeText(conn, pong)
			}
		}
	}
}

// ack writes the registered acknowledgment and records the session before the
// register is reported observed, so a test that sees the event can drive the
// connection without racing the ack.
func (h *fakeHub) ack(conn *websocket.Conn, reg relay.RegisterFrame) error {
	ack, err := json.Marshal(map[string]any{"type": "registered", "instance_id": reg.InstanceID})
	if err != nil {
		return err
	}
	if err := h.writeFrame(conn, websocket.TextMessage, ack); err != nil {
		return err
	}
	h.mu.Lock()
	h.sessions[reg.InstanceID] = toolCount(reg)
	h.mu.Unlock()
	return nil
}

// handleSessions serves the sessions listing `status` probes.
func (h *fakeHub) handleSessions(w http.ResponseWriter, _ *http.Request) {
	type session struct {
		InstanceID  string    `json:"instance_id"`
		Version     string    `json:"version,omitempty"`
		ToolCount   int       `json:"tool_count"`
		ConnectedAt time.Time `json:"connected_at"`
	}
	h.mu.Lock()
	out := make([]session, 0, len(h.sessions))
	for id, count := range h.sessions {
		out = append(out, session{InstanceID: id, Version: "0.1.0", ToolCount: count, ConnectedAt: time.Now().UTC()})
	}
	h.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"sessions": out})
}

// writeFrame serializes all hub-side writes so the ack, control pong replies,
// and test-driven frames never race.
func (h *fakeHub) writeFrame(conn *websocket.Conn, msgType int, data []byte) error {
	h.writeMu.Lock()
	defer h.writeMu.Unlock()
	_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	return conn.WriteMessage(msgType, data)
}

func (h *fakeHub) writeText(conn *websocket.Conn, data []byte) error {
	return h.writeFrame(conn, websocket.TextMessage, data)
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
	data, err := relay.Marshal(&relay.RequestFrame{Type: relay.FrameRequest, ID: id, Payload: payload})
	if err != nil {
		return err
	}
	h.mu.Lock()
	conn := h.conn
	h.mu.Unlock()
	if conn == nil {
		return errors.New("fake hub: no connection")
	}
	return h.writeText(conn, data)
}

// drop closes the current connection from the hub side, forcing the client
// into its reconnect/backoff loop.
func (h *fakeHub) drop() {
	h.mu.Lock()
	conn := h.conn
	h.mu.Unlock()
	if conn != nil {
		_ = conn.Close()
	}
}

// waitRegisters blocks until at least n register frames arrive (or fails).
func (h *fakeHub) waitRegisters(n int, timeout time.Duration) []relay.RegisterFrame {
	h.t.Helper()
	var out []relay.RegisterFrame
	deadline := time.After(timeout)
	for len(out) < n {
		select {
		case reg := <-h.regCh:
			out = append(out, reg)
		case <-deadline:
			h.t.Fatalf("fake hub: timed out waiting for %d register frame(s), got %d", n, len(out))
		}
	}
	return out
}

// waitResponse blocks until a response frame arrives (or fails).
func (h *fakeHub) waitResponse(timeout time.Duration) relay.ResponseFrame {
	h.t.Helper()
	select {
	case resp := <-h.respCh:
		return resp
	case <-time.After(timeout):
		h.t.Fatal("fake hub: timed out waiting for response frame")
		return relay.ResponseFrame{}
	}
}

func (h *fakeHub) authHeader() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.auth
}

func toolCount(reg relay.RegisterFrame) int {
	items, _ := reg.Tools["tools"].([]any)
	return len(items)
}

func toolNames(reg relay.RegisterFrame) []string {
	items, _ := reg.Tools["tools"].([]any)
	names := make([]string, 0, len(items))
	for _, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if name, ok := m["name"].(string); ok {
			names = append(names, name)
		}
	}
	return names
}

func containsString(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Daemon subprocess helpers
// ---------------------------------------------------------------------------

// syncBuffer is a concurrency-safe bytes.Buffer for capturing subprocess output
// while the process is still writing.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// proc wraps a running subprocess with a single Wait owned by a goroutine.
type proc struct {
	cmd     *exec.Cmd
	stdout  *syncBuffer
	stderr  *syncBuffer
	done    chan struct{}
	waitErr error
}

func startDaemon(t *testing.T, cfgPath string) *proc {
	t.Helper()
	p := &proc{stdout: &syncBuffer{}, stderr: &syncBuffer{}}
	cmd := exec.Command(binaryPath, "daemon", "--config", cfgPath)
	cmd.Stdout = p.stdout
	cmd.Stderr = p.stderr
	p.cmd = cmd
	if err := cmd.Start(); err != nil {
		t.Fatalf("start daemon: %v", err)
	}
	p.done = make(chan struct{})
	go func() {
		p.waitErr = cmd.Wait()
		close(p.done)
	}()
	t.Cleanup(func() {
		select {
		case <-p.done:
			return
		default:
		}
		_ = cmd.Process.Signal(syscall.SIGTERM)
		select {
		case <-p.done:
		case <-time.After(5 * time.Second):
			_ = cmd.Process.Kill()
			<-p.done
		}
	})
	return p
}

func (p *proc) signal(sig os.Signal) error { return p.cmd.Process.Signal(sig) }

// wait blocks until the process exits or the timeout elapses.
func (p *proc) wait(timeout time.Duration) (error, bool) {
	select {
	case <-p.done:
		return p.waitErr, true
	case <-time.After(timeout):
		return nil, false
	}
}

func (p *proc) exitCode() int {
	if p.waitErr == nil {
		return 0
	}
	var ee *exec.ExitError
	if errors.As(p.waitErr, &ee) {
		return ee.ExitCode()
	}
	return -1
}

// ---------------------------------------------------------------------------
// Config + status helpers
// ---------------------------------------------------------------------------

// writeConfig writes a mode-0600 config YAML pointing at serverURL.
func writeConfig(t *testing.T, serverURL, instanceID, token string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "memory-connector.yml")
	body := fmt.Sprintf("server_url: %s\ntoken: %s\ninstance_id: %s\n", serverURL, token, instanceID)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatalf("chmod config: %v", err)
	}
	return path
}

// statusDoc is the subset of `status --json` the e2e test asserts.
type statusDoc struct {
	SchemaVersion int      `json:"schema_version"`
	InstanceID    string   `json:"instance_id"`
	Tools         []string `json:"tools"`
	HubState      string   `json:"hub_state"`
}

// runStatusJSON runs `status --config <cfg> --json` and parses the document.
func runStatusJSON(t *testing.T, cfgPath string) statusDoc {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binaryPath, "status", "--config", cfgPath, "--json")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("status --json: %v; stderr=%s", err, stderr.String())
	}
	var doc statusDoc
	if err := json.Unmarshal(stdout.Bytes(), &doc); err != nil {
		t.Fatalf("parse status json: %v; out=%s", err, stdout.String())
	}
	return doc
}

// uniqueInstanceID returns a per-test instance id.
func uniqueInstanceID(prefix string) string {
	return fmt.Sprintf("%s-%d-%d", prefix, os.Getpid(), time.Now().UnixNano())
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// TestDaemonLifecycle covers register, tool-call round-trip, status --json,
// the single-instance lock, and graceful SIGTERM shutdown in one daemon run.
func TestDaemonLifecycle(t *testing.T) {
	requireRunnable(t)

	hub := newFakeHub(t)
	instanceID := uniqueInstanceID("e2e-lifecycle")
	cfgPath := writeConfig(t, hub.url(), instanceID, "e2e-token")

	p := startDaemon(t, cfgPath)

	// 1. Register: first frame is register with our instance id and tool list.
	regs := hub.waitRegisters(1, 15*time.Second)
	reg := regs[0]
	if reg.InstanceID != instanceID {
		t.Fatalf("register instance_id = %q, want %q", reg.InstanceID, instanceID)
	}
	if reg.Version != "0.1.0" {
		t.Errorf("register version = %q, want 0.1.0", reg.Version)
	}
	if names := toolNames(reg); !containsString(names, "linux-host-info") {
		t.Errorf("register tools = %v, want it to contain linux-host-info", names)
	}
	if got := hub.authHeader(); got != "Bearer e2e-token" {
		t.Errorf("handshake Authorization = %q, want Bearer e2e-token", got)
	}

	// 2. Tool call: hub invokes linux-host-info; daemon returns host data.
	const callID = "req-e2e-lifecycle-1"
	if err := hub.sendRequest(callID, "linux-host-info", nil); err != nil {
		t.Fatalf("sendRequest: %v", err)
	}
	resp := hub.waitResponse(15 * time.Second)
	if got := string(resp.ID); got != `"`+callID+`"` {
		t.Errorf("response id = %s, want echo of %q", got, callID)
	}
	if resp.Error != "" {
		t.Fatalf("tool call returned error: %q", resp.Error)
	}
	if hostname, _ := resp.Payload["hostname"].(string); hostname == "" {
		t.Errorf("tool payload hostname empty: %v", resp.Payload)
	}
	if osname, _ := resp.Payload["os"].(string); osname == "" {
		t.Errorf("tool payload os empty: %v", resp.Payload)
	}

	// 3. Status: while the daemon runs, `status --json` reports connected.
	doc := runStatusJSON(t, cfgPath)
	if doc.SchemaVersion != 1 {
		t.Errorf("status schema_version = %d, want 1", doc.SchemaVersion)
	}
	if doc.InstanceID != instanceID {
		t.Errorf("status instance_id = %q, want %q", doc.InstanceID, instanceID)
	}
	if !containsString(doc.Tools, "linux-host-info") {
		t.Errorf("status tools = %v, want it to contain linux-host-info", doc.Tools)
	}
	if doc.HubState != "connected" {
		t.Errorf("status hub_state = %q, want connected", doc.HubState)
	}

	// 4. Single-instance lock: a second daemon on the same config exits
	//    non-zero without starting.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, binaryPath, "daemon", "--config", cfgPath).CombinedOutput()
	if err == nil {
		t.Fatalf("second daemon exited 0, want non-zero; output=%s", out)
	}
	var ee *exec.ExitError
	if !errors.As(err, &ee) {
		t.Fatalf("second daemon: %v (output=%s)", err, out)
	}
	if ee.ExitCode() == 0 {
		t.Fatalf("second daemon exit code = 0, want non-zero")
	}
	if !strings.Contains(string(out), "already running") {
		t.Errorf("second daemon output = %q, want it to mention already running", out)
	}

	// 5. Graceful stop: SIGTERM -> clean exit 0.
	if err := p.signal(syscall.SIGTERM); err != nil {
		t.Fatalf("signal daemon: %v", err)
	}
	if _, ok := p.wait(10 * time.Second); !ok {
		t.Fatalf("daemon did not exit after SIGTERM; stdout=%s stderr=%s", p.stdout.String(), p.stderr.String())
	}
	if code := p.exitCode(); code != 0 {
		t.Fatalf("daemon exit code = %d, want 0; stderr=%s", code, p.stderr.String())
	}
}

// TestDaemonReconnectsAfterHubDrop drops the hub connection and asserts the
// daemon reconnects and re-registers. It uses a bound that covers the client's
// default reconnect backoff (DefaultBackoffBase = 30s), since the binary
// exposes no backoff override.
func TestDaemonReconnectsAfterHubDrop(t *testing.T) {
	requireRunnable(t)

	hub := newFakeHub(t)
	instanceID := uniqueInstanceID("e2e-reconnect")
	cfgPath := writeConfig(t, hub.url(), instanceID, "e2e-token")

	p := startDaemon(t, cfgPath)

	if regs := hub.waitRegisters(1, 15*time.Second); regs[0].InstanceID != instanceID {
		t.Fatalf("initial register instance_id = %q, want %q", regs[0].InstanceID, instanceID)
	}

	hub.drop()

	// The client's first reconnect waits its backoff base (30s); 45s is a safe
	// upper bound that keeps the test bounded without being flaky.
	regs := hub.waitRegisters(1, 45*time.Second)
	if regs[0].InstanceID != instanceID {
		t.Errorf("re-register instance_id = %q, want %q", regs[0].InstanceID, instanceID)
	}
	if names := toolNames(regs[0]); !containsString(names, "linux-host-info") {
		t.Errorf("re-register tools = %v, want it to contain linux-host-info", names)
	}

	if err := p.signal(syscall.SIGTERM); err != nil {
		t.Fatalf("signal daemon: %v", err)
	}
	if _, ok := p.wait(10 * time.Second); !ok {
		t.Fatalf("daemon did not exit after SIGTERM")
	}
	if code := p.exitCode(); code != 0 {
		t.Fatalf("daemon exit code = %d, want 0; stderr=%s", code, p.stderr.String())
	}
}
