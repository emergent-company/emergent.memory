package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/livekit/protocol/auth"
)

// newTokenEcho registers POST /api/token against a fake backend.
func newTokenEcho(f MemoryBackend, cfg Config) (*Server, *echo.Echo) {
	s := &Server{cfg: cfg, memory: f}
	e := echo.New()
	e.POST("/api/token", s.mintToken)
	return s, e
}

func tokenTestConfig() Config {
	return Config{
		LiveKitAPIKey:    "lkapikey",
		LiveKitAPISecret: "lksecret",
		LiveKitURL:       "ws://internal:7880",
		LiveKitPublicURL: "wss://lk.example.com",
		DefaultAgent:     "memory",
	}
}

// tokenAgentBackend lists "memory" so any memory-prefixed room passes the
// allow-list (roomAllowed falls back to the agent-name prefix rule).
func tokenAgentBackend() *fakeMemory {
	return &fakeMemory{agents: []AgentDefinitionSummary{{ID: "a1", Name: "memory"}}}
}

// decodeRoomGrant parses the minted JWT and returns the video-grant room.
func decodeRoomGrant(t *testing.T, raw, secret string) string {
	t.Helper()
	v, err := auth.ParseAPIToken(raw)
	if err != nil {
		t.Fatalf("ParseAPIToken: %v", err)
	}
	_, claims, err := v.Verify(secret)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if claims.Video == nil {
		t.Fatal("token has no video grant")
	}
	return claims.Video.Room
}

// TestMintTokenDerivesRoom verifies an omitted room yields a fresh
// <agent>-<client>-<8hex> room baked into the JWT grant, keyed off the
// request's client field (defaulting to "web" when omitted).
func TestMintTokenDerivesRoom(t *testing.T) {
	cases := []struct {
		name   string
		body   string
		prefix string
	}{
		{name: "client omitted defaults to web", body: `{"identity":"iphone-1"}`, prefix: "memory-web-"},
		{name: "client ios", body: `{"identity":"iphone-1","client":"ios"}`, prefix: "memory-ios-"},
		{name: "client web", body: `{"identity":"web-1","client":"web"}`, prefix: "memory-web-"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, e := newTokenEcho(tokenAgentBackend(), tokenTestConfig())
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/api/token",
				strings.NewReader(tc.body))
			req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
			e.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
			}
			var got struct {
				ServerURL        string `json:"server_url"`
				ParticipantToken string `json:"participant_token"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			if got.ServerURL != "wss://lk.example.com" {
				t.Errorf("server_url = %q", got.ServerURL)
			}
			if got.ParticipantToken == "" {
				t.Fatal("participant_token empty")
			}
			room := decodeRoomGrant(t, got.ParticipantToken, "lksecret")
			re := regexp.MustCompile(`^` + regexp.QuoteMeta(tc.prefix) + `[0-9a-f]{8}$`)
			if !re.MatchString(room) {
				t.Errorf("derived room = %q, want ^%s[0-9a-f]{8}$", room, tc.prefix)
			}
		})
	}
}

// TestMintTokenExplicitRoom verifies an explicit room is honored verbatim.
func TestMintTokenExplicitRoom(t *testing.T) {
	_, e := newTokenEcho(tokenAgentBackend(), tokenTestConfig())
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/token",
		strings.NewReader(`{"identity":"iphone-2","room":"memory-explicit"}`))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var got struct {
		ParticipantToken string `json:"participant_token"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if room := decodeRoomGrant(t, got.ParticipantToken, "lksecret"); room != "memory-explicit" {
		t.Errorf("room grant = %q, want memory-explicit", room)
	}
}

// TestMintTokenNotConfigured verifies fail-closed when LiveKit creds are unset.
func TestMintTokenNotConfigured(t *testing.T) {
	_, e := newTokenEcho(&fakeMemory{}, Config{})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/token",
		strings.NewReader(`{"identity":"x"}`))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "token service not configured") {
		t.Errorf("body = %s", rec.Body.String())
	}
}

// TestMintTokenIdentityRequired verifies identity is still mandatory.
func TestMintTokenIdentityRequired(t *testing.T) {
	_, e := newTokenEcho(tokenAgentBackend(), tokenTestConfig())
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/token",
		strings.NewReader(`{}`))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "identity required") {
		t.Errorf("body = %s", rec.Body.String())
	}
}

// TestRandomHex verifies the shape of the per-token room suffix helper.
func TestRandomHex(t *testing.T) {
	re := regexp.MustCompile(`^[0-9a-f]{8}$`)
	for range 20 {
		h := randomHex(4)
		if !re.MatchString(h) {
			t.Fatalf("randomHex(4) = %q, want 8 lowercase hex chars", h)
		}
	}
}
