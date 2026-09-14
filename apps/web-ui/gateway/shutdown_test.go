package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
)

// TestSupervisorRunClosesDone asserts Run returns (and closes Done) promptly
// after its context is cancelled.
func TestSupervisorRunClosesDone(t *testing.T) {
	s := NewSupervisor(&fakeMemory{}, "true", nil, "", time.Hour, "k", "u")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	go s.Run(ctx)
	select {
	case <-s.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after context cancel")
	}
}

// TestConversationEventsStopsOnShutdown asserts the SSE handler returns once
// servers begin shutdown.
func TestConversationEventsStopsOnShutdown(t *testing.T) {
	s := &Server{cfg: Config{AuthMode: "dev"}, shutdownCh: make(chan struct{})}
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/conversations/c1/events", nil)
	c := e.NewContext(req, httptest.NewRecorder())
	c.SetParamNames("id")
	c.SetParamValues("c1")

	done := make(chan error, 1)
	go func() { done <- s.conversationEvents(c) }()

	time.Sleep(50 * time.Millisecond)
	s.beginShutdown()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("conversationEvents returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("conversationEvents did not return after shutdown")
	}
}
