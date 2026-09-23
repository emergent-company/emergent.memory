package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"syscall"
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

// TestWorkerEnvStripsGatewaySecrets guards the child-env boundary: bridge
// workers inherit the environment they need but must never see the gateway's
// own secrets.
func TestWorkerEnvStripsGatewaySecrets(t *testing.T) {
	t.Setenv("AGENT_TRIGGER_TOKEN", "emt_webhook_secret")
	t.Setenv("SESSION_SECRET", "session-secret")
	t.Setenv("GITHUB_WEBHOOK_SECRET", "gh-secret")
	t.Setenv("LIVEKIT_API_SECRET", "livekit-secret") // the bridge legitimately needs this

	env := workerEnv()
	find := func(k string) (string, bool) {
		for _, kv := range env {
			if name, v, ok := strings.Cut(kv, "="); ok && name == k {
				return v, true
			}
		}
		return "", false
	}
	for _, k := range []string{"AGENT_TRIGGER_TOKEN", "SESSION_SECRET", "GITHUB_WEBHOOK_SECRET"} {
		if _, ok := find(k); ok {
			t.Fatalf("%s leaked into the bridge worker env", k)
		}
	}
	if v, ok := find("LIVEKIT_API_SECRET"); !ok || v != "livekit-secret" {
		t.Fatalf("LIVEKIT_API_SECRET = %q, ok=%v; bridge needs it", v, ok)
	}
}

// TestSupervisorStopWorker asserts StopWorker terminates and forgets the worker
// (used when an agent is disabled after its worker was spawned).
func TestSupervisorStopWorker(t *testing.T) {
	bin, err := exec.LookPath("sleep")
	if err != nil {
		t.Skip("sleep binary not available")
	}
	s := NewSupervisor(bin, []string{"60"}, "", 0, 0, "k", "u")
	t.Cleanup(s.stopAll)

	s.EnsureWorker("agent-b")
	s.mu.Lock()
	ws, ok := s.workers["agent-b"]
	s.mu.Unlock()
	if !ok {
		t.Fatal("worker not spawned")
	}

	s.StopWorker("agent-b")
	s.mu.Lock()
	_, still := s.workers["agent-b"]
	s.mu.Unlock()
	if still {
		t.Fatal("StopWorker did not remove the worker")
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if err := ws.cmd.Process.Signal(syscall.Signal(0)); err != nil {
			return // process gone
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("StopWorker did not kill the process")
}

// TestSupervisorZeroTTLDisablesReaping documents the default posture: with
// WorkerIdleTTL=0 a worker is never reaped, because the gateway has no
// per-room liveness signal and a long call would otherwise look idle.
func TestSupervisorZeroTTLDisablesReaping(t *testing.T) {
	bin, err := exec.LookPath("sleep")
	if err != nil {
		t.Skip("sleep binary not available")
	}
	s := NewSupervisor(bin, []string{"60"}, "", time.Hour, 0, "k", "u")
	t.Cleanup(s.stopAll)

	s.EnsureWorker("agent-c")
	time.Sleep(10 * time.Millisecond)
	s.reapIdle()
	s.mu.Lock()
	_, ok := s.workers["agent-c"]
	s.mu.Unlock()
	if !ok {
		t.Fatal("worker reaped with TTL=0; reaping must be disabled")
	}
}
