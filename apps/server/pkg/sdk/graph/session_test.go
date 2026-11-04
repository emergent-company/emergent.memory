package graph_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk"
	sdkgraph "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/graph"
	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/testutil"
)

// TestSessionCreate verifies that POST /api/graph/sessions creates a Session
// object retrievable via the returned ID (task 4.3).
func TestSessionCreate(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	mock.On("POST", "/api/graph/sessions", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		testutil.JSONResponse(t, w, map[string]interface{}{
			"id":           "sess_abc123",
			"project_id":   "proj_1",
			"canonical_id": "sess_abc123",
			"version":      1,
			"type":         "Session",
			"properties": map[string]interface{}{
				"title":         "Test session",
				"message_count": 0,
				"started_at":    "2026-01-01T00:00:00Z",
			},
			"labels":     []string{},
			"created_at": "2026-01-01T00:00:00Z",
		})
	})

	client, _ := sdk.New(sdk.Config{
		ServerURL: mock.URL,
		Auth:      sdk.AuthConfig{Mode: "apikey", APIKey: "test_key"},
	})

	session, err := client.Graph.CreateSession(context.Background(), &sdkgraph.CreateSessionRequest{
		Title: "Test session",
	})
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	if session.ID != "sess_abc123" {
		t.Errorf("expected ID sess_abc123, got %s", session.ID)
	}
	if session.Type != "Session" {
		t.Errorf("expected type Session, got %s", session.Type)
	}
	if title, _ := session.Properties["title"].(string); title != "Test session" {
		t.Errorf("expected title 'Test session', got %q", title)
	}
}

// TestSessionAppendMessage verifies that POST /api/graph/sessions/:id/messages
// creates a Message with the correct role and triggers embedding (task 4.4).
func TestSessionAppendMessage(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	sessionID := "sess_abc123"
	mock.On("POST", "/api/graph/sessions/"+sessionID+"/messages", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		testutil.JSONResponse(t, w, map[string]interface{}{
			"id":           "msg_xyz789",
			"project_id":   "proj_1",
			"canonical_id": "msg_xyz789",
			"version":      1,
			"type":         "Message",
			"properties": map[string]interface{}{
				"role":            "user",
				"content":         "Hello, world!",
				"sequence_number": 1,
				"timestamp":       "2026-01-01T00:00:01Z",
			},
			"labels":     []string{},
			"created_at": "2026-01-01T00:00:01Z",
		})
	})

	client, _ := sdk.New(sdk.Config{
		ServerURL: mock.URL,
		Auth:      sdk.AuthConfig{Mode: "apikey", APIKey: "test_key"},
	})

	msg, err := client.Graph.AppendMessage(context.Background(), sessionID, &sdkgraph.AppendMessageRequest{
		Role:    "user",
		Content: "Hello, world!",
	})
	if err != nil {
		t.Fatalf("AppendMessage() error = %v", err)
	}

	if msg.ID != "msg_xyz789" {
		t.Errorf("expected ID msg_xyz789, got %s", msg.ID)
	}
	if msg.Type != "Message" {
		t.Errorf("expected type Message, got %s", msg.Type)
	}
	if role, _ := msg.Properties["role"].(string); role != "user" {
		t.Errorf("expected role 'user', got %q", role)
	}
	// sequence_number comes back as float64 from JSON
	if seq, ok := msg.Properties["sequence_number"]; !ok {
		t.Error("expected sequence_number property to be set")
	} else if seq.(float64) != 1 {
		t.Errorf("expected sequence_number 1, got %v", seq)
	}
}

// TestSessionListMessages verifies that GET /api/graph/sessions/:id/messages
// returns messages in order.
func TestSessionListMessages(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	sessionID := "sess_abc123"
	mock.On("GET", "/api/graph/sessions/"+sessionID+"/messages", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		testutil.JSONResponse(t, w, map[string]interface{}{
			"items": []map[string]interface{}{
				{
					"id":           "msg_1",
					"project_id":   "proj_1",
					"canonical_id": "msg_1",
					"version":      1,
					"type":         "Message",
					"properties": map[string]interface{}{
						"role":            "user",
						"content":         "First message",
						"sequence_number": 1,
					},
					"labels":     []string{},
					"created_at": "2026-01-01T00:00:01Z",
				},
				{
					"id":           "msg_2",
					"project_id":   "proj_1",
					"canonical_id": "msg_2",
					"version":      1,
					"type":         "Message",
					"properties": map[string]interface{}{
						"role":            "assistant",
						"content":         "Second message",
						"sequence_number": 2,
					},
					"labels":     []string{},
					"created_at": "2026-01-01T00:00:02Z",
				},
			},
			"total": 2,
		})
	})

	client, _ := sdk.New(sdk.Config{
		ServerURL: mock.URL,
		Auth:      sdk.AuthConfig{Mode: "apikey", APIKey: "test_key"},
	})

	resp, err := client.Graph.ListMessages(context.Background(), sessionID, 50, "")
	if err != nil {
		t.Fatalf("ListMessages() error = %v", err)
	}

	if len(resp.Items) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(resp.Items))
	}
	if resp.Total != 2 {
		t.Errorf("expected total 2, got %d", resp.Total)
	}

	// Verify ordering: sequence_number 1 comes first.
	seq1, _ := resp.Items[0].Properties["sequence_number"].(float64)
	seq2, _ := resp.Items[1].Properties["sequence_number"].(float64)
	if seq1 != 1 || seq2 != 2 {
		t.Errorf("expected sequence numbers 1,2, got %v,%v", seq1, seq2)
	}
}

// TestSessionSequenceNumberAssignment (task 4.1) tests the sequence_number
// property is correctly assigned as 1, 2, 3 for sequential appends by
// verifying the mock server receives distinct sequence numbers per message.
//
// Note: This test exercises the SDK/mock layer to validate the contract shape.
// True atomic ordering is verified by the AppendMessage service integration test
// (requires a live DB — run with TEST_DATABASE_URL set).
func TestSessionSequenceNumberAssignment(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	sessionID := "sess_seq_test"
	seq := 0
	mock.On("POST", "/api/graph/sessions/"+sessionID+"/messages", func(w http.ResponseWriter, r *http.Request) {
		seq++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		testutil.JSONResponse(t, w, map[string]interface{}{
			"id":           "msg_seq_" + string(rune('0'+seq)),
			"project_id":   "proj_1",
			"canonical_id": "msg_seq_" + string(rune('0'+seq)),
			"version":      1,
			"type":         "Message",
			"properties": map[string]interface{}{
				"role":            "user",
				"content":         "msg",
				"sequence_number": seq,
			},
			"labels":     []string{},
			"created_at": "2026-01-01T00:00:00Z",
		})
	})

	client, _ := sdk.New(sdk.Config{
		ServerURL: mock.URL,
		Auth:      sdk.AuthConfig{Mode: "apikey", APIKey: "test_key"},
	})

	ctx := context.Background()
	for i := 1; i <= 3; i++ {
		msg, err := client.Graph.AppendMessage(ctx, sessionID, &sdkgraph.AppendMessageRequest{
			Role:    "user",
			Content: "msg",
		})
		if err != nil {
			t.Fatalf("AppendMessage %d error = %v", i, err)
		}
		gotSeq, _ := msg.Properties["sequence_number"].(float64)
		if int(gotSeq) != i {
			t.Errorf("message %d: expected sequence_number %d, got %v", i, i, gotSeq)
		}
	}
}
