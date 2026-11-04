package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
)

// convErrMemory overrides only GetConversation so the handler's error mapping
// can be exercised without touching the shared fakeMemory.
type convErrMemory struct {
	*fakeMemory
	err error
}

func (m convErrMemory) GetConversation(ctx context.Context, id string) (*ConversationDetail, error) {
	return nil, m.err
}

// TestGetConversationErrorMapping pins the status codes the assistant drawer's
// session loader depends on. A conversation id that is not in the current
// project (or was deleted) is a 404 — the drawer's scoped localStorage can
// still hold an id after the tenancy changed, and answering 502 "memory service
// unavailable" made a stale id look like an outage.
func TestGetConversationErrorMapping(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want int
	}{
		{"missing conversation is a 404", errors.New("memory: GET /api/chat/abc: 404 not_found"), http.StatusNotFound},
		{"malformed id is a 404", errors.New("memory: GET /api/chat/abc: 400 bad_request: invalid conversation id"), http.StatusNotFound},
		{"upstream failure is a 502", errors.New("dial tcp: connection refused"), http.StatusBadGateway},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: convErrMemory{fakeMemory: &fakeMemory{}, err: tc.err}}
			e := echo.New()
			e.GET("/api/conversations/:id", s.getConversation)

			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/conversations/abc", nil))
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d (body %s)", rec.Code, tc.want, rec.Body.String())
			}
		})
	}
}
