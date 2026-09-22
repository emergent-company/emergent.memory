package a2a

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestServer builds an httptest server dispatching on method+path. Handler
// returns (statusCode, body) where body is a value to JSON-marshal, or a
// raw string.
func newTestServer(t *testing.T, handler func(w http.ResponseWriter, r *http.Request) (int, any)) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		status, body := handler(w, r)
		w.Header().Set("Content-Type", A2AContentType)
		w.WriteHeader(status)
		if body == nil {
			return
		}
		switch v := body.(type) {
		case string:
			_, _ = w.Write([]byte(v))
		default:
			if err := json.NewEncoder(w).Encode(v); err != nil {
				t.Fatalf("failed to encode response: %v", err)
			}
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func sampleTask() Task {
	return Task{
		ID:        "task-1",
		ContextID: "ctx-1",
		Status:    TaskStatus{State: TaskStateCompleted},
		Artifacts: []Artifact{{ArtifactID: "a-1", Name: "result", Parts: []Part{TextPart("hello")}}},
		History:   []Message{{MessageID: "m-1", Role: RoleUser, Parts: []Part{TextPart("hi")}}},
	}
}

func TestExtendedAgentCard(t *testing.T) {
	card := AgentCard{
		Name:        "Memory",
		Description: "agent platform",
		Version:     "1.0.0",
		Capabilities: AgentCapabilities{
			Streaming:         true,
			ExtendedAgentCard: true,
		},
		Skills: []AgentSkill{
			{ID: "my-agent", Name: "My Agent", Description: "desc", Tags: []string{}},
		},
	}
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) (int, any) {
		assert.Equal(t, "GET", r.Method)
		assert.Equal(t, "/extendedAgentCard", r.URL.Path)
		assert.Equal(t, "Bearer emt_token", r.Header.Get("Authorization"))
		assert.Equal(t, A2AProtocolVersion, r.Header.Get(A2AVersionHeader))
		return http.StatusOK, card
	})

	client := NewClient(srv.URL, "emt_token")
	got, err := client.ExtendedAgentCard(context.Background())
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "Memory", got.Name)
	require.Len(t, got.Skills, 1)
	assert.Equal(t, "my-agent", got.Skills[0].ID)
}

// TestExtendedAgentCardSendsProjectHeader is the #761 regression guard: the
// authenticated extended card is project-scoped and the server rejects a
// request with no project selector, so the client must send X-Project-ID.
func TestExtendedAgentCardSendsProjectHeader(t *testing.T) {
	card := AgentCard{Name: "Memory", Version: "1.0.0"}
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) (int, any) {
		assert.Equal(t, "GET", r.Method)
		assert.Equal(t, "/extendedAgentCard", r.URL.Path)
		assert.Equal(t, "proj-123", r.Header.Get(ProjectIDHeader))
		return http.StatusOK, card
	})

	client := NewClientWithProject(srv.URL, "emt_token", "proj-123")
	got, err := client.ExtendedAgentCard(context.Background())
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "Memory", got.Name)
}

// TestProjectHeaderOmittedWhenUnset pins the pre-existing behaviour: a client
// with no configured project sends no selector (the unauthenticated global card
// path must keep working).
func TestProjectHeaderOmittedWhenUnset(t *testing.T) {
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) (int, any) {
		assert.Empty(t, r.Header.Get(ProjectIDHeader))
		return http.StatusOK, AgentCard{Name: "Memory"}
	})

	_, err := NewClient(srv.URL, "emt_token").ExtendedAgentCard(context.Background())
	require.NoError(t, err)
}

// TestExtendedAgentCardProjectRequiredError covers the project-scoped failure
// path: the server now answers a missing selector with a specific 400
// INVALID_ARGUMENT / PROJECT_REQUIRED envelope, not a 500
// INVALID_AGENT_RESPONSE. The client must surface that reason distinctly.
func TestExtendedAgentCardProjectRequiredError(t *testing.T) {
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) (int, any) {
		return http.StatusBadRequest, `{"error":{"code":-32012,"status":"INVALID_ARGUMENT","message":"project context is required: send the X-Project-ID header, or use a project-scoped API token","details":[{"@type":"type.googleapis.com/lf.a2a.v1.Error","reason":"PROJECT_REQUIRED","domain":"a2a-protocol.org"}]}}`
	})

	_, err := NewClient(srv.URL, "emt_token").ExtendedAgentCard(context.Background())
	require.Error(t, err)
	assert.Equal(t, "PROJECT_REQUIRED", Reason(err))
	assert.False(t, IsNotFound(err))

	var apiErr *Error
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, -32012, apiErr.Code)
	assert.Equal(t, http.StatusBadRequest, apiErr.StatusCode)
	assert.Equal(t, "INVALID_ARGUMENT", apiErr.Status)
	assert.Equal(t, A2AErrorDomain, apiErr.Domain)
}

func TestSendMessage(t *testing.T) {
	task := sampleTask()
	var gotReq SendMessageRequest
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) (int, any) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/message:send", r.URL.Path)
		assert.Equal(t, A2AContentType, r.Header.Get("Content-Type"))
		assert.Equal(t, "proj-123", r.Header.Get(ProjectIDHeader))
		require.NoError(t, json.NewDecoder(r.Body).Decode(&gotReq))
		return http.StatusOK, SendMessageResponse{Task: &task}
	})

	client := NewClientWithProject(srv.URL, "emt_token", "proj-123")
	resp, err := client.SendMessage(context.Background(), SendMessageRequest{
		Message: Message{
			MessageID: "m-1",
			Role:      RoleUser,
			Parts:     []Part{TextPart("hello")},
			Metadata:  map[string]any{SkillIDMetadataKey: "my-agent"},
		},
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.NotNil(t, resp.Task)
	assert.Equal(t, "task-1", resp.Task.ID)
	assert.Equal(t, TaskStateCompleted, resp.Task.Status.State)

	// Request round-tripped correctly.
	assert.Equal(t, "hello", *gotReq.Message.Parts[0].Text)
	assert.Equal(t, "my-agent", gotReq.Message.Metadata[SkillIDMetadataKey])
}

func TestSendMessageAsync(t *testing.T) {
	task := Task{ID: "task-2", ContextID: "ctx-1", Status: TaskStatus{State: TaskStateSubmitted}}
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) (int, any) {
		var req SendMessageRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		require.NotNil(t, req.Configuration)
		assert.True(t, req.Configuration.ReturnImmediately)
		return http.StatusOK, SendMessageResponse{Task: &task}
	})

	client := NewClient(srv.URL, "emt_token")
	resp, err := client.SendMessage(context.Background(), SendMessageRequest{
		Message:       Message{MessageID: "m-2", Role: RoleUser, Parts: []Part{TextPart("go")}},
		Configuration: &SendMessageConfiguration{ReturnImmediately: true},
	})
	require.NoError(t, err)
	require.NotNil(t, resp.Task)
	assert.Equal(t, TaskStateSubmitted, resp.Task.Status.State)
}

func TestStreamMessage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/message:stream", r.URL.Path)
		assert.Equal(t, "proj-123", r.Header.Get(ProjectIDHeader))
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("expected http.Flusher")
		}
		_, _ = fmt.Fprint(w, "data: {\"statusUpdate\":{\"taskId\":\"t1\",\"contextId\":\"c1\",\"status\":{\"state\":\"TASK_STATE_WORKING\"}}}\n\n")
		flusher.Flush()
		_, _ = fmt.Fprint(w, "data: {\"artifactUpdate\":{\"taskId\":\"t1\",\"contextId\":\"c1\",\"artifact\":{\"artifactId\":\"a1\",\"parts\":[{\"text\":\"hel\"}]}}}\n\n")
		flusher.Flush()
		_, _ = fmt.Fprint(w, "data: {\"task\":{\"id\":\"t1\",\"contextId\":\"c1\",\"status\":{\"state\":\"TASK_STATE_COMPLETED\"}}}\n\n")
		flusher.Flush()
	}))
	defer srv.Close()

	client := NewClientWithProject(srv.URL, "emt_token", "proj-123")
	stream, err := client.StreamMessage(context.Background(), SendMessageRequest{
		Message: Message{MessageID: "m-1", Role: RoleUser, Parts: []Part{TextPart("hi")}},
	})
	require.NoError(t, err)
	defer stream.Close()

	// Event 1: status update
	ev, err := stream.Next()
	require.NoError(t, err)
	require.NotNil(t, ev.StatusUpdate)
	assert.Equal(t, TaskStateWorking, ev.StatusUpdate.Status.State)

	// Event 2: artifact update
	ev, err = stream.Next()
	require.NoError(t, err)
	require.NotNil(t, ev.ArtifactUpdate)
	require.Len(t, ev.ArtifactUpdate.Artifact.Parts, 1)
	assert.Equal(t, "hel", *ev.ArtifactUpdate.Artifact.Parts[0].Text)

	// Event 3: terminal task
	ev, err = stream.Next()
	require.NoError(t, err)
	require.NotNil(t, ev.Task)
	assert.Equal(t, TaskStateCompleted, ev.Task.Status.State)
}

// newSSEStream builds an SSEStream over an in-memory body so the parser can be
// exercised without an HTTP server.
func newSSEStream(body string) *SSEStream {
	return &SSEStream{
		resp:    &http.Response{Body: io.NopCloser(strings.NewReader(body))},
		scanner: bufio.NewScanner(strings.NewReader(body)),
	}
}

// collectEvents drains a stream, returning every dispatched event. An io.EOF
// ends iteration; any other error fails the test.
func collectEvents(t *testing.T, s *SSEStream) []StreamResponse {
	t.Helper()
	var events []StreamResponse
	for {
		ev, err := s.Next()
		if errors.Is(err, io.EOF) {
			return events
		}
		require.NoError(t, err)
		events = append(events, *ev)
	}
}

// statusUpdateJSON is a single-line StreamResponse payload used across cases.
const statusUpdateJSON = `{"statusUpdate":{"taskId":"t1","contextId":"c1","status":{"state":"TASK_STATE_WORKING"}}}`

func TestSSEStreamNextParsing(t *testing.T) {
	wantStatus := []StreamResponse{{StatusUpdate: &TaskStatusUpdateEvent{
		TaskID:    "t1",
		ContextID: "c1",
		Status:    TaskStatus{State: TaskStateWorking},
	}}}
	wantArtifact := []StreamResponse{{ArtifactUpdate: &TaskArtifactUpdateEvent{
		TaskID:    "t1",
		ContextID: "c1",
		Artifact:  Artifact{ArtifactID: "a1", Parts: []Part{TextPart("hel")}},
	}}}

	tests := []struct {
		name string
		body string
		want []StreamResponse
	}{
		{
			name: "no space after colon",
			body: "data:" + statusUpdateJSON + "\n\n",
			want: wantStatus,
		},
		{
			name: "single space after colon",
			body: "data: " + statusUpdateJSON + "\n\n",
			want: wantStatus,
		},
		{
			name: "multiple spaces after colon",
			body: "data:    " + statusUpdateJSON + "\n\n",
			want: wantStatus,
		},
		{
			name: "tab after colon",
			body: "data:\t" + statusUpdateJSON + "\n\n",
			want: wantStatus,
		},
		{
			name: "multi-line data joined with newline",
			body: "data: {\"statusUpdate\":{\"taskId\":\"t1\",\"contextId\":\"c1\",\n" +
				"data: \"status\":{\"state\":\"TASK_STATE_WORKING\"}}}\n\n",
			want: wantStatus,
		},
		{
			name: "multiple events in one chunk",
			body: "data: " + statusUpdateJSON + "\n\n" +
				"data: {\"artifactUpdate\":{\"taskId\":\"t1\",\"contextId\":\"c1\",\"artifact\":{\"artifactId\":\"a1\",\"parts\":[{\"text\":\"hel\"}]}}}\n\n",
			want: append(append([]StreamResponse{}, wantStatus...), wantArtifact...),
		},
		{
			name: "bare data empty payload is skipped",
			body: "data:\n\ndata: " + statusUpdateJSON + "\n\n",
			want: wantStatus,
		},
		{
			name: "only bare data empty payload emits nothing",
			body: "data:\n\n",
			want: nil,
		},
		{
			name: "comment and metadata fields are ignored",
			body: ": keep-alive\nevent: message\nid: 42\nretry: 1000\ndata: " + statusUpdateJSON + "\n\n",
			want: wantStatus,
		},
		{
			name: "unknown field is ignored",
			body: "foo: bar\ndata: " + statusUpdateJSON + "\n\n",
			want: wantStatus,
		},
		{
			name: "unterminated event is discarded at EOF",
			body: "data: " + statusUpdateJSON,
			want: nil,
		},
		{
			name: "leading blank lines are skipped",
			body: "\n\n\ndata: " + statusUpdateJSON + "\n\n",
			want: wantStatus,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := collectEvents(t, newSSEStream(tc.body))
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestSSEStreamDoneSentinel(t *testing.T) {
	// [DONE] terminates the stream; any payload after it is not dispatched.
	s := newSSEStream("data: [DONE]\n\ndata: " + statusUpdateJSON + "\n\n")
	_, err := s.Next()
	require.ErrorIs(t, err, io.EOF)
}

func TestGetTask(t *testing.T) {
	task := sampleTask()
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) (int, any) {
		assert.Equal(t, "GET", r.Method)
		assert.Equal(t, "/tasks/task-1", r.URL.Path)
		assert.Equal(t, "5", r.URL.Query().Get("historyLength"))
		return http.StatusOK, task
	})

	client := NewClient(srv.URL, "emt_token")
	got, err := client.GetTask(context.Background(), "task-1", 5)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "task-1", got.ID)
	assert.Equal(t, "ctx-1", got.ContextID)
}

func TestListTasks(t *testing.T) {
	tasks := []Task{sampleTask()}
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) (int, any) {
		assert.Equal(t, "GET", r.Method)
		assert.Equal(t, "/tasks", r.URL.Path)
		assert.Equal(t, "ctx-1", r.URL.Query().Get("contextId"))
		assert.Equal(t, "TASK_STATE_COMPLETED", r.URL.Query().Get("status"))
		assert.Equal(t, "10", r.URL.Query().Get("pageSize"))
		return http.StatusOK, ListTasksResponse{Tasks: tasks, TotalSize: 1, PageSize: 10}
	})

	client := NewClient(srv.URL, "emt_token")
	got, err := client.ListTasks(context.Background(), ListTasksRequest{
		ContextID: "ctx-1",
		Status:    "TASK_STATE_COMPLETED",
		PageSize:  10,
	})
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Len(t, got.Tasks, 1)
	assert.Equal(t, "task-1", got.Tasks[0].ID)
	assert.Equal(t, 1, got.TotalSize)
}

func TestCancelTask(t *testing.T) {
	task := sampleTask()
	task.Status = TaskStatus{State: TaskStateCanceled}
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) (int, any) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/tasks/task-1:cancel", r.URL.Path)
		return http.StatusOK, task
	})

	client := NewClient(srv.URL, "emt_token")
	got, err := client.CancelTask(context.Background(), "task-1")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, TaskStateCanceled, got.Status.State)
}

func TestErrorEnvelopeNotFound(t *testing.T) {
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) (int, any) {
		return http.StatusNotFound, `{"error":{"code":-32001,"status":"NOT_FOUND","message":"task not found","details":[{"@type":"type.googleapis.com/lf.a2a.v1.TaskNotFoundError","reason":"TASK_NOT_FOUND","domain":"a2a-protocol.org"}]}}`
	})

	client := NewClient(srv.URL, "emt_token")
	_, err := client.GetTask(context.Background(), "missing", 0)
	require.Error(t, err)
	assert.True(t, IsNotFound(err), "expected IsNotFound to be true")
	assert.Equal(t, "TASK_NOT_FOUND", Reason(err))

	var apiErr *Error
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, -32001, apiErr.Code)
	assert.Equal(t, "NOT_FOUND", apiErr.Status)
	assert.Equal(t, "a2a-protocol.org", apiErr.Domain)
}

func TestErrorEnvelopeVersionRejection(t *testing.T) {
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) (int, any) {
		return http.StatusBadRequest, `{"error":{"code":-32009,"status":"INVALID_ARGUMENT","message":"unsupported A2A protocol version 9.9","details":[{"@type":"type.googleapis.com/lf.a2a.v1.VersionNotSupportedError","reason":"VERSION_NOT_SUPPORTED","domain":"a2a-protocol.org"}]}}`
	})

	client := NewClient(srv.URL, "emt_token")
	_, err := client.ExtendedAgentCard(context.Background())
	require.Error(t, err)
	assert.False(t, IsNotFound(err))
	assert.Equal(t, "VERSION_NOT_SUPPORTED", Reason(err))
}
