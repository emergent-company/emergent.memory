package mcp

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/feedback"
	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

func TestSanitizeClientName(t *testing.T) {
	require.Equal(t, "claude-code", sanitizeClientName("  claude-code  "))
	require.Equal(t, "a b", sanitizeClientName("a\n\tb"))
	require.Empty(t, sanitizeClientName("   "))
	require.Empty(t, sanitizeClientName("\x00\x01"))
	long := strings.Repeat("x", maxClientNameRunes+20)
	require.Len(t, []rune(sanitizeClientName(long)), maxClientNameRunes)
}

func TestClientNameContextRoundTrip(t *testing.T) {
	ctx := withClientName(context.Background(), "cursor")
	require.Equal(t, "cursor", clientNameFromContext(ctx))
	require.Empty(t, clientNameFromContext(withClientName(context.Background(), "")))
}

func initializeRequest(t *testing.T, clientName string) *Request {
	t.Helper()
	raw, err := json.Marshal(InitializeParams{
		ProtocolVersion: LatestProtocolVersion,
		ClientInfo:      ClientInfo{Name: clientName, Version: "1.0"},
	})
	require.NoError(t, err)
	return &Request{JSONRPC: "2.0", ID: json.RawMessage(`1`), Method: "initialize", Params: raw}
}

func toolsCallRequest(t *testing.T, message string) *Request {
	t.Helper()
	raw, err := json.Marshal(ToolsCallParams{
		Name:      "feedback-submit",
		Arguments: map[string]any{"message": message},
	})
	require.NoError(t, err)
	return &Request{JSONRPC: "2.0", ID: json.RawMessage(`2`), Method: "tools/call", Params: raw}
}

func tokenContext(token string, user *auth.AuthUser) echo.Context {
	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/api/mcp", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	if user != nil {
		req = req.WithContext(auth.ContextWithUser(req.Context(), user))
	}
	return e.NewContext(req, httptest.NewRecorder())
}

type bodyRecorder struct {
	mu     sync.Mutex
	bodies []string
}

func (b *bodyRecorder) record(t *testing.T, r *http.Request) {
	t.Helper()
	var payload struct {
		Body string `json:"body"`
	}
	require.NoError(t, json.NewDecoder(r.Body).Decode(&payload))
	b.mu.Lock()
	b.bodies = append(b.bodies, payload.Body)
	b.mu.Unlock()
}

func (b *bodyRecorder) snapshot() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]string(nil), b.bodies...)
}

func newFeedbackMCPTestService(t *testing.T, githubURL string) *Service {
	t.Helper()
	db := connectTestDB(t)
	fb := feedback.NewService(&config.Config{
		Environment: "test",
		Feedback: config.FeedbackConfig{
			Enabled:          true,
			GitHubToken:      "test-token",
			Repo:             "o/r",
			APIBaseURL:       githubURL,
			Label:            "feedback",
			RateLimitPerHour: 10,
		},
	}, db, slog.New(slog.NewTextHandler(io.Discard, nil)))
	svc := &Service{feedbackSvc: fb}
	_ = svc.GetToolDefinitions()
	return svc
}

// TestLegacyToolsCallCarriesSessionClientName proves two initialized sessions
// keep their own client names: no package-level state, no cross-session bleed.
func TestLegacyToolsCallCarriesSessionClientName(t *testing.T) {
	recorder := &bodyRecorder{}
	github := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		recorder.record(t, r)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"number": 81, "html_url": "https://github.com/o/r/issues/81"}`))
	}))
	defer github.Close()

	svc := newFeedbackMCPTestService(t, github.URL)
	h := &Handler{
		svc:      svc,
		log:      slog.New(slog.NewTextHandler(io.Discard, nil)),
		sessions: make(map[string]*Session),
	}
	user := &auth.AuthUser{ID: "user-1", Email: "ada@example.com"}

	require.Nil(t, h.handleInitialize(tokenContext("token-a", user), initializeRequest(t, "claude-code"), user).Error)
	require.Nil(t, h.handleInitialize(tokenContext("token-b", user), initializeRequest(t, "cursor"), user).Error)
	require.Equal(t, "claude-code", h.getSession("token-a").ClientName)
	require.Equal(t, "cursor", h.getSession("token-b").ClientName)

	require.Nil(t, h.handleToolsCall(tokenContext("token-a", user), toolsCallRequest(t, "from claude-code"), user).Error)
	require.Nil(t, h.handleToolsCall(tokenContext("token-b", user), toolsCallRequest(t, "from cursor"), user).Error)

	bodies := recorder.snapshot()
	require.Len(t, bodies, 2)
	require.Contains(t, bodies[0], "| Client | claude-code |")
	require.NotContains(t, bodies[0], "cursor")
	require.Contains(t, bodies[1], "| Client | cursor |")
	require.NotContains(t, bodies[1], "claude-code")
}

// TestLegacyToolsCallFallsBackWhenSessionHasNoClientName covers a session that
// was not initialized with clientInfo (and, by extension, any transport with no
// session map): the Client row is the documented "MCP" constant.
func TestLegacyToolsCallFallsBackWhenSessionHasNoClientName(t *testing.T) {
	var body string
	github := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Body string `json:"body"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&payload))
		body = payload.Body
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"number": 82, "html_url": "https://github.com/o/r/issues/82"}`))
	}))
	defer github.Close()

	svc := newFeedbackMCPTestService(t, github.URL)
	h := &Handler{
		svc:      svc,
		log:      slog.New(slog.NewTextHandler(io.Discard, nil)),
		sessions: make(map[string]*Session),
	}
	h.sessions["token-c"] = &Session{Initialized: true}
	user := &auth.AuthUser{ID: "user-1", Email: "ada@example.com"}

	require.Nil(t, h.handleToolsCall(tokenContext("token-c", user), toolsCallRequest(t, "no client name"), user).Error)
	require.Contains(t, body, "| Client | MCP |")
}

// TestStreamableToolsCallCarriesSessionClientName proves the real /api/mcp
// transport (session-ID keyed) carries the initialized client name too.
func TestStreamableToolsCallCarriesSessionClientName(t *testing.T) {
	var body string
	github := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Body string `json:"body"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&payload))
		body = payload.Body
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"number": 83, "html_url": "https://github.com/o/r/issues/83"}`))
	}))
	defer github.Close()

	svc := newFeedbackMCPTestService(t, github.URL)
	h := &StreamableHTTPHandler{
		svc:      svc,
		log:      slog.New(slog.NewTextHandler(io.Discard, nil)),
		sessions: make(map[string]*MCPSession),
	}
	session := &MCPSession{
		ID:           uuid.NewString(),
		UserID:       "user-1",
		CreatedAt:    time.Now(),
		LastAccessAt: time.Now(),
	}
	user := &auth.AuthUser{ID: "user-1", Email: "ada@example.com"}
	ctx := tokenContext("streamable-token", user)

	require.Nil(t, h.handleInitialize(ctx, initializeRequest(t, "claude-code"), session, user).Error)
	require.Equal(t, "claude-code", h.getSession(session.ID).ClientName)
	require.Nil(t, h.handleToolsCall(ctx, toolsCallRequest(t, "streamable client name"), session, user).Error)

	require.Contains(t, body, "| Client | claude-code |")
}
