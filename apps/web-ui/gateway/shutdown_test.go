package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
)

// TestSupervisorEnsureWorkerIdempotentAndReap covers the on-demand spawn model:
// EnsureWorker starts one process per agent regardless of how often it is
// called, and reapIdle stops a worker idle past its TTL.
func TestSupervisorEnsureWorkerIdempotentAndReap(t *testing.T) {
	bin, err := exec.LookPath("sleep")
	if err != nil {
		t.Skip("sleep binary not available")
	}
	s := NewSupervisor(bin, []string{"60"}, "", time.Hour, 40*time.Millisecond, "k", "u")
	t.Cleanup(func() {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		go s.Run(ctx)
		<-s.Done()
	})

	s.EnsureWorker("agent-a")
	s.mu.Lock()
	pid1 := s.workers["agent-a"].cmd.Process.Pid
	s.mu.Unlock()

	// A second call for a live worker must reuse it, not spawn a duplicate.
	s.EnsureWorker("agent-a")
	s.mu.Lock()
	pid2, count := s.workers["agent-a"].cmd.Process.Pid, len(s.workers)
	s.mu.Unlock()
	if pid1 != pid2 || count != 1 {
		t.Fatalf("EnsureWorker not idempotent: pids %d/%d, workers %d", pid1, pid2, count)
	}

	time.Sleep(80 * time.Millisecond) // exceed the idle TTL
	s.reapIdle()
	s.mu.Lock()
	_, ok := s.workers["agent-a"]
	s.mu.Unlock()
	if ok {
		t.Fatal("idle worker was not reaped")
	}
}

// TestSupervisorRunClosesDone asserts Run returns (and closes Done) promptly
// after its context is cancelled.
func TestSupervisorRunClosesDone(t *testing.T) {
	s := NewSupervisor("true", nil, "", time.Hour, time.Minute, "k", "u")
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
