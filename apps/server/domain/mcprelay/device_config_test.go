package mcprelay

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/mcpregistry"
)

// startWSPeer returns a server-side websocket.Conn (to back a Session) and a
// client-side websocket.Conn (to act as the remote device). The client conn is
// closed by the test cleanup.
func startWSPeer(t *testing.T) (*websocket.Conn, *websocket.Conn) {
	t.Helper()
	serverCh := make(chan *websocket.Conn, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		serverCh <- c
	}))
	t.Cleanup(srv.Close)

	url := "ws" + strings.TrimPrefix(srv.URL, "http")
	client, _, err := websocket.DefaultDialer.Dial(url, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })

	server := <-serverCh
	t.Cleanup(func() { _ = server.Close() })
	return server, client
}

// newTestSession builds an opted-in Session on the given server conn.
func newTestSession(server *websocket.Conn, projectID, instanceID, owner string) *Session {
	s := newSession(projectID, instanceID, "1.0.0", map[string]any{}, server)
	s.AcceptsManagedConfig = true
	s.OwnerSubject = owner
	return s
}

func TestConfigFrameRoundTrip(t *testing.T) {
	frame := ConfigFrame{
		Type:    FrameConfig,
		ID:      "id-1",
		Version: 7,
		Servers: []mcpregistry.DeviceServerConfig{
			{Name: "s1", Transport: "stdio", Enabled: true, Command: "cmd", Args: []string{"a"}, Env: map[string]any{"K": "v"}, Headers: map[string]any{}},
		},
	}
	data, err := json.Marshal(frame)
	require.NoError(t, err)

	var back ConfigFrame
	require.NoError(t, json.Unmarshal(data, &back))
	require.Equal(t, FrameConfig, back.Type)
	require.Equal(t, "id-1", back.ID)
	require.Equal(t, int64(7), back.Version)
	require.Len(t, back.Servers, 1)
	require.Equal(t, "stdio", back.Servers[0].Transport)

	// The config frame must be distinct from request/response types.
	require.NotEqual(t, FrameRequest, FrameConfig)
	require.NotEqual(t, FrameResponse, FrameConfig)
}

func TestRegisterFrameAcceptsManagedConfigDefault(t *testing.T) {
	var reg RegisterFrame
	require.NoError(t, json.Unmarshal([]byte(`{"type":"register","instance_id":"i"}`), &reg))
	require.False(t, reg.AcceptsManagedConfig, "accepts_managed_config must default to false")

	require.NoError(t, json.Unmarshal([]byte(`{"type":"register","instance_id":"i","accepts_managed_config":true}`), &reg))
	require.True(t, reg.AcceptsManagedConfig)
}

// runPeer reads frames from the client conn until done, invoking onFrame for
// each text message.
func runPeer(t *testing.T, client *websocket.Conn, onFrame func(t *testing.T, frame ConfigFrame)) {
	t.Helper()
	for {
		_, data, err := client.ReadMessage()
		if err != nil {
			return
		}
		var f ConfigFrame
		if json.Unmarshal(data, &f) != nil {
			continue
		}
		if onFrame != nil {
			onFrame(t, f)
		}
	}
}

// serveResponses runs the server-side read loop for a session: it reads frames
// from the server conn and routes ResponseFrames to the session's pending calls
// (mirroring handler.Connect's read loop).
func serveResponses(server *websocket.Conn, sess *Session) {
	go func() {
		for {
			_, data, err := server.ReadMessage()
			if err != nil {
				return
			}
			var resp ResponseFrame
			if json.Unmarshal(data, &resp) == nil {
				sess.deliverResponse(resp)
			}
		}
	}()
}

func TestSendConfigOK(t *testing.T) {
	server, client := startWSPeer(t)
	sess := newTestSession(server, "p", "i", "owner")
	serveResponses(server, sess)

	go runPeer(t, client, func(t *testing.T, f ConfigFrame) {
		resp, _ := json.Marshal(ResponseFrame{Type: FrameResponse, ID: json.RawMessage(`"` + f.ID + `"`), Payload: map[string]any{"ok": true}})
		_ = client.WriteMessage(websocket.TextMessage, resp)
	})

	ack, err := sess.SendConfig(context.Background(), ConfigFrame{Type: FrameConfig, ID: "c1", Version: 1, Servers: nil})
	require.NoError(t, err)
	require.Equal(t, true, ack["ok"])
}

func TestSendConfigError(t *testing.T) {
	server, client := startWSPeer(t)
	sess := newTestSession(server, "p", "i", "owner")
	serveResponses(server, sess)

	go runPeer(t, client, func(t *testing.T, f ConfigFrame) {
		resp, _ := json.Marshal(ResponseFrame{Type: FrameResponse, ID: json.RawMessage(`"` + f.ID + `"`), Error: "stale"})
		_ = client.WriteMessage(websocket.TextMessage, resp)
	})

	_, err := sess.SendConfig(context.Background(), ConfigFrame{Type: FrameConfig, ID: "c2", Version: 1, Servers: nil})
	require.Error(t, err)
	require.Contains(t, err.Error(), "stale")
}

func TestSendConfigTimeout(t *testing.T) {
	server, client := startWSPeer(t)
	sess := newTestSession(server, "p", "i", "owner")
	serveResponses(server, sess)

	// Peer never responds.
	go runPeer(t, client, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := sess.SendConfig(ctx, ConfigFrame{Type: FrameConfig, ID: "c3", Version: 1, Servers: nil})
	require.Error(t, err)
	require.Equal(t, context.DeadlineExceeded, err)
}

func TestPushConfigToInstanceSkipsNonOptedIn(t *testing.T) {
	server, client := startWSPeer(t)
	sess := newTestSession(server, "p", "i", "owner")
	sess.AcceptsManagedConfig = false // not opted in

	svc := NewService(slog.Default())
	svc.Register(sess)

	received := make(chan struct{}, 1)
	go runPeer(t, client, func(t *testing.T, f ConfigFrame) { received <- struct{}{} })

	svc.PushConfigToInstance(context.Background(), "p", "i", nil)
	// Give any (wrong) push a moment to arrive.
	select {
	case <-received:
		t.Fatal("non-opted-in device must not be pushed to")
	case <-time.After(100 * time.Millisecond):
	}
}

func TestPushConfigToInstanceSkipsUnregistered(t *testing.T) {
	svc := NewService(slog.Default())
	// No session registered: must not panic and must not push.
	svc.PushConfigToInstance(context.Background(), "p", "missing", nil)
}

func TestPushConfigToInstanceNonBlockingAndVersioned(t *testing.T) {
	server, client := startWSPeer(t)
	sess := newTestSession(server, "p", "i", "owner")
	svc := NewService(slog.Default())
	svc.Register(sess)

	var mu sync.Mutex
	var versions []int64
	done := make(chan struct{}, 2)
	go runPeer(t, client, func(t *testing.T, f ConfigFrame) {
		mu.Lock()
		versions = append(versions, f.Version)
		mu.Unlock()
		done <- struct{}{}
	})

	start := time.Now()
	svc.PushConfigToInstance(context.Background(), "p", "i", nil)
	svc.PushConfigToInstance(context.Background(), "p", "i", nil)
	require.Less(t, time.Since(start), 500*time.Millisecond, "push must be non-blocking")

	for i := 0; i < 2; i++ {
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for config frame")
		}
	}
	mu.Lock()
	defer mu.Unlock()
	require.Len(t, versions, 2)
	// Versions are strictly increasing; delivery order is intentionally not
	// guaranteed (each push is its own goroutine) — the device resolves
	// reordering via the version guard (stale ack).
	sort.Slice(versions, func(i, j int) bool { return versions[i] < versions[j] })
	require.Greater(t, versions[0], int64(0))
	require.Greater(t, versions[1], versions[0], "config version must be monotonic per instance")
}

// TestConfigVersionSurvivesServerRestart proves the version is wall-clock based
// rather than a process counter: a brand-new Service (as after a server restart)
// must issue a version greater than one already issued by a previous Service.
// Otherwise the connector, which persists its last-applied version, would reject
// every post-restart push as stale and never apply another config update.
func TestConfigVersionSurvivesServerRestart(t *testing.T) {
	before := NewService(slog.Default())
	v1 := before.nextConfigVersion("p", "i")

	afterRestart := NewService(slog.Default())
	v2 := afterRestart.nextConfigVersion("p", "i")

	require.Greater(t, v2, v1, "a restarted server must issue a strictly newer version")
}

func TestPushConfigToProjectOwnershipFilter(t *testing.T) {
	// Owner "alice" and "bob" both opted in. A non-admin broadcast from alice must
	// reach only alice's device.
	svc := NewService(slog.Default())

	sa, ca := startWSPeer(t)
	svc.Register(newTestSession(sa, "p", "alice-dev", "alice"))
	sb, cb := startWSPeer(t)
	svc.Register(newTestSession(sb, "p", "bob-dev", "bob"))

	var aliceGot, bobGot int
	var mu sync.Mutex
	done := make(chan struct{}, 2)
	go runPeer(t, ca, func(t *testing.T, f ConfigFrame) { mu.Lock(); aliceGot++; mu.Unlock(); done <- struct{}{} })
	go runPeer(t, cb, func(t *testing.T, f ConfigFrame) { mu.Lock(); bobGot++; mu.Unlock(); done <- struct{}{} })

	resolve := func(ctx context.Context, instanceID string) ([]mcpregistry.DeviceServerConfig, error) {
		return []mcpregistry.DeviceServerConfig{{Name: "s", Transport: "stdio", Enabled: true}}, nil
	}

	svc.PushConfigToProject(context.Background(), "p", "alice", false, resolve)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for alice's config")
	}
	time.Sleep(100 * time.Millisecond) // allow any (wrong) bob push to land
	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, 1, aliceGot, "alice must be pushed to")
	require.Equal(t, 0, bobGot, "non-owner bob must not be pushed to")
}

func TestNotifyRegisteredAfterAck(t *testing.T) {
	svc := NewService(slog.Default())

	// OnChange fires during Register (before the ack); OnRegistered fires only on
	// NotifyRegistered (after the ack). This separation is the correctness guard:
	// hydration must never ride OnChange.
	var changeCalled, registeredCalled bool
	svc.OnChange(func(projectID, instanceID string, isRegister bool) { changeCalled = true })
	svc.OnRegistered(func(projectID, instanceID string) { registeredCalled = true })

	svc.Register(newTestSession(nil, "p", "i", "owner"))

	require.True(t, changeCalled, "OnChange fires on Register")
	require.False(t, registeredCalled, "hydration must not fire before NotifyRegistered")

	svc.NotifyRegistered("p", "i")
	// NotifyRegistered runs callbacks on their own goroutine.
	require.Eventually(t, func() bool { return registeredCalled }, time.Second, 10*time.Millisecond)
}
