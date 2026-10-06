package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/emergent-company/emergent.memory/apps/connector.linux/internal/config"
	"github.com/emergent-company/emergent.memory/apps/connector.linux/internal/mcphost"
	"github.com/emergent-company/emergent.memory/apps/connector.linux/internal/relay"
	"github.com/emergent-company/emergent.memory/apps/connector.linux/internal/secretstore"
	"github.com/emergent-company/emergent.memory/apps/connector.linux/internal/toolreg"
)

// e2eHub is an in-process mcprelay stand-in for the end-to-end managed-config
// test. It enforces register-first, acks registrations, and lets the test drive
// config frames while surfacing the client's register/response frames.
type e2eHub struct {
	t      *testing.T
	server *httptest.Server

	mu      sync.Mutex
	writeMu sync.Mutex
	conn    *websocket.Conn

	regCh  chan relay.RegisterFrame
	respCh chan relay.ResponseFrame
}

func newE2eHub(t *testing.T) *e2eHub {
	t.Helper()
	h := &e2eHub{
		t:      t,
		regCh:  make(chan relay.RegisterFrame, 8),
		respCh: make(chan relay.ResponseFrame, 8),
	}
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		h.mu.Lock()
		h.conn = conn
		h.mu.Unlock()
		h.serve(conn)
	}))
	t.Cleanup(srv.Close)
	h.server = srv
	return h
}

func (h *e2eHub) write(conn *websocket.Conn, data []byte) error {
	h.writeMu.Lock()
	defer h.writeMu.Unlock()
	_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	return conn.WriteMessage(websocket.TextMessage, data)
}

// serve runs one hub-side connection: register first, ack it, then relay
// response frames (and re-ack any re-register) until the connection closes.
func (h *e2eHub) serve(conn *websocket.Conn) {
	_, data, err := conn.ReadMessage()
	if err != nil {
		return
	}
	frame, err := relay.Parse(data)
	if err != nil {
		return
	}
	reg, ok := frame.(*relay.RegisterFrame)
	if !ok {
		return
	}
	ack, _ := relay.Marshal(&relay.RegisteredFrame{Type: relay.FrameRegistered, InstanceID: reg.InstanceID})
	if err := h.write(conn, ack); err != nil {
		return
	}
	// Ack is on the wire before the register is surfaced, so the test can
	// immediately drive config frames without racing the hub's ack write.
	h.regCh <- *reg

	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
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
			ack, _ := relay.Marshal(&relay.RegisteredFrame{Type: relay.FrameRegistered, InstanceID: f.InstanceID})
			if err := h.write(conn, ack); err != nil {
				return
			}
			h.regCh <- *f
		}
	}
}

func (h *e2eHub) url() string { return h.server.URL }

func (h *e2eHub) sendConfig(frame relay.ConfigFrame) error {
	data, err := relay.Marshal(&frame)
	if err != nil {
		return err
	}
	h.mu.Lock()
	conn := h.conn
	h.mu.Unlock()
	if conn == nil {
		return nil
	}
	return h.write(conn, data)
}

func (h *e2eHub) waitRegister(timeout time.Duration) relay.RegisterFrame {
	select {
	case reg := <-h.regCh:
		return reg
	case <-time.After(timeout):
		h.t.Fatal("timed out waiting for register frame")
		return relay.RegisterFrame{}
	}
}

func (h *e2eHub) waitResponse(timeout time.Duration) relay.ResponseFrame {
	select {
	case resp := <-h.respCh:
		return resp
	case <-time.After(timeout):
		h.t.Fatal("timed out waiting for response frame")
		return relay.ResponseFrame{}
	}
}

// TestManagedConfigEndToEnd drives the REAL relay client against an in-process
// fake hub and the REAL managedConfigApplier (stub reload) to assert the full
// device-mcp-config flow: register advertises accepts_managed_config, a config
// frame is applied and acked ok, the managed set is persisted, a newer frame
// applies, and a stale (version <= last) frame acks "stale".
func TestManagedConfigEndToEnd(t *testing.T) {
	dir := t.TempDir()
	managedPath := filepath.Join(dir, "managed", "servers.yml")
	secrets := secretstore.New(filepath.Join(dir, "managed", "secrets"))

	cfg := &config.Config{ServerURL: "https://memory.example.test", Token: "emt_testtoken", InstanceID: "e2e-instance"}

	var reloaded []mcphost.ServerConfig
	reload := func(_ context.Context, c *config.Config) ([]mcphost.ServerStatus, error) {
		reloaded = append([]mcphost.ServerConfig(nil), c.MCPServers...)
		return []mcphost.ServerStatus{{Name: "github", Transport: mcphost.TransportStdio, Enabled: true, Connected: true, ToolCount: 3}}, nil
	}

	applier := newManagedConfigApplier(
		quietLog(), managedPath, secrets,
		func() *config.Config { return cfg },
		reload,
		func() int { return 3 },
		nil, 0,
	)

	hub := newE2eHub(t)

	client, err := relay.New(hub.url(), cfg.Token, "", cfg.InstanceID, "0.1.0", toolreg.New(),
		relay.WithAcceptsManagedConfig(true),
		relay.WithConfigApplier(applier.Apply),
		relay.WithAckTimeout(2*time.Second),
		relay.WithCallTimeout(2*time.Second),
		relay.WithConfigTimeout(2*time.Second),
		relay.WithReadWait(5*time.Second),
		relay.WithPingPeriod(time.Hour),
		relay.WithBackoff(15*time.Millisecond, 250*time.Millisecond, 2),
		relay.WithLogger(quietLog()),
	)
	if err != nil {
		t.Fatalf("relay.New: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = client.Run(ctx); close(done) }()
	defer func() {
		cancel()
		<-done
	}()

	// 1. Register advertises accepts_managed_config.
	reg := hub.waitRegister(3 * time.Second)
	if !reg.AcceptsManagedConfig {
		t.Errorf("register accepts_managed_config = false, want true")
	}

	// 2. Hub sends a config frame (version 1, one server).
	const fakeToken = "test-token-not-a-real-secret"
	if err := hub.sendConfig(relay.ConfigFrame{Type: relay.FrameConfig, ID: "cfg-1", Version: 1, Servers: []mcphost.ServerConfig{
		{Name: "github", Transport: mcphost.TransportStdio, Command: "github-mcp", Env: map[string]string{"GH_TOKEN": fakeToken}},
	}}); err != nil {
		t.Fatalf("sendConfig v1: %v", err)
	}

	// 3. Client acks {"ok":true,...}.
	resp := hub.waitResponse(3 * time.Second)
	if resp.Error != "" {
		t.Fatalf("v1 ack error = %q, want none", resp.Error)
	}
	if ok, _ := resp.Payload["ok"].(bool); !ok {
		t.Errorf("v1 ack ok = %v, want true", resp.Payload["ok"])
	}
	if applied, _ := resp.Payload["applied"].(float64); applied != 1 {
		t.Errorf("v1 ack applied = %v, want 1", resp.Payload["applied"])
	}

	// 4. The applier applied (reload saw the merged set) and persisted the
	// managed set to disk with the credential stripped from the file.
	if len(reloaded) != 1 || reloaded[0].Name != "github" {
		t.Fatalf("reload saw servers = %+v, want [github]", reloaded)
	}
	b, err := os.ReadFile(managedPath)
	if err != nil {
		t.Fatalf("read managed file: %v", err)
	}
	if !strings.Contains(string(b), "github") || !strings.Contains(string(b), "version: 1") {
		t.Errorf("managed file missing structure/version:\n%s", b)
	}
	if strings.Contains(string(b), fakeToken) {
		t.Error("managed file leaked a credential value")
	}
	if blob, err := secrets.Load("mcp.github"); err != nil || blob == nil {
		t.Errorf("credential not stored in secret store: %q (err=%v)", blob, err)
	}

	// 5. A newer frame applies.
	if err := hub.sendConfig(relay.ConfigFrame{Type: relay.FrameConfig, ID: "cfg-2", Version: 2, Servers: []mcphost.ServerConfig{
		{Name: "github", Transport: mcphost.TransportStdio, Command: "github-mcp-v2"},
	}}); err != nil {
		t.Fatalf("sendConfig v2: %v", err)
	}
	resp2 := hub.waitResponse(3 * time.Second)
	if resp2.Error != "" {
		t.Fatalf("v2 ack error = %q, want none", resp2.Error)
	}
	if applied, _ := resp2.Payload["applied"].(float64); applied != 1 {
		t.Errorf("v2 ack applied = %v, want 1", resp2.Payload["applied"])
	}

	// 6. A stale (version <= last) frame acks "stale".
	if err := hub.sendConfig(relay.ConfigFrame{Type: relay.FrameConfig, ID: "cfg-3", Version: 2, Servers: []mcphost.ServerConfig{
		{Name: "github", Transport: mcphost.TransportStdio, Command: "github-mcp"},
	}}); err != nil {
		t.Fatalf("sendConfig stale: %v", err)
	}
	resp3 := hub.waitResponse(3 * time.Second)
	if resp3.Error != "stale" {
		t.Fatalf("stale ack error = %q, want %q", resp3.Error, "stale")
	}
}
