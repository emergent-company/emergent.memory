package main

import (
	"os/exec"
	"testing"
	"time"
)

// --- workerRegistry: mint, authenticate, revoke ---

func TestWorkerRegistryIssueAuthenticateRevoke(t *testing.T) {
	r := newWorkerRegistry()
	cred := r.issue("agent-a")
	if cred == "" {
		t.Fatal("issue returned an empty credential")
	}
	if len(cred) != 64 {
		t.Fatalf("credential length = %d, want 64 hex chars (256 bits)", len(cred))
	}
	agent, ok := r.authenticate(cred)
	if !ok || agent != "agent-a" {
		t.Fatalf("authenticate = %q, ok=%v; want agent-a/true", agent, ok)
	}
	r.revoke(cred)
	if _, ok := r.authenticate(cred); ok {
		t.Fatal("authenticate succeeded after revoke")
	}
	if _, ok := r.authenticate("forged"); ok {
		t.Fatal("authenticate accepted an unknown credential")
	}
}

func TestWorkerRegistryIssueUniqueCredentials(t *testing.T) {
	r := newWorkerRegistry()
	seen := make(map[string]string)
	for range 100 {
		cred := r.issue("agent-a")
		if prev, dup := seen[cred]; dup {
			t.Fatalf("duplicate credential issued (first seen for %q)", prev)
		}
		seen[cred] = "agent-a"
	}
}

// --- supervisor: revoke on every worker-exit path ---

func supervisorWorkerCred(t *testing.T, s *Supervisor, name string) string {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	ws, ok := s.workers[name]
	if !ok {
		t.Fatalf("worker %s not spawned", name)
	}
	return ws.credential
}

func registryLen(r *workerRegistry) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.workers)
}

func TestSupervisorRevokesCredentialOnStopWorker(t *testing.T) {
	bin, err := exec.LookPath("sleep")
	if err != nil {
		t.Skip("sleep binary not available")
	}
	creds := newWorkerRegistry()
	s := NewSupervisor(bin, []string{"60"}, "", 0, 0, creds, "u")
	t.Cleanup(s.stopAll)

	s.EnsureWorker("agent-a")
	cred := supervisorWorkerCred(t, s, "agent-a")
	if agent, ok := creds.authenticate(cred); !ok || agent != "agent-a" {
		t.Fatalf("credential not registered for agent-a: agent=%q ok=%v", agent, ok)
	}

	s.StopWorker("agent-a")
	if _, ok := creds.authenticate(cred); ok {
		t.Fatal("credential still valid after StopWorker")
	}
}

func TestSupervisorRevokesCredentialOnMonitor(t *testing.T) {
	bin, err := exec.LookPath("sleep")
	if err != nil {
		t.Skip("sleep binary not available")
	}
	creds := newWorkerRegistry()
	// The worker exits on its own after 200ms, driving monitor's Wait() path.
	s := NewSupervisor(bin, []string{"0.2"}, "", 0, 0, creds, "u")
	t.Cleanup(s.stopAll)

	s.EnsureWorker("agent-a")
	cred := supervisorWorkerCred(t, s, "agent-a")
	if _, ok := creds.authenticate(cred); !ok {
		t.Fatal("credential not registered before worker exit")
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, ok := creds.authenticate(cred); !ok {
			return // revoked by monitor
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("credential not revoked after worker exit (monitor)")
}

func TestSupervisorRevokesCredentialOnReapIdle(t *testing.T) {
	bin, err := exec.LookPath("sleep")
	if err != nil {
		t.Skip("sleep binary not available")
	}
	creds := newWorkerRegistry()
	s := NewSupervisor(bin, []string{"60"}, "", time.Hour, 40*time.Millisecond, creds, "u")
	t.Cleanup(s.stopAll)

	s.EnsureWorker("agent-a")
	cred := supervisorWorkerCred(t, s, "agent-a")
	if _, ok := creds.authenticate(cred); !ok {
		t.Fatal("credential not registered before reap")
	}

	time.Sleep(80 * time.Millisecond) // exceed the idle TTL
	s.reapIdle()
	if _, ok := creds.authenticate(cred); ok {
		t.Fatal("credential still valid after reapIdle")
	}
}

func TestSupervisorRevokesCredentialOnStopAll(t *testing.T) {
	bin, err := exec.LookPath("sleep")
	if err != nil {
		t.Skip("sleep binary not available")
	}
	creds := newWorkerRegistry()
	s := NewSupervisor(bin, []string{"60"}, "", time.Hour, 0, creds, "u")

	s.EnsureWorker("agent-a")
	cred := supervisorWorkerCred(t, s, "agent-a")
	if _, ok := creds.authenticate(cred); !ok {
		t.Fatal("credential not registered before stopAll")
	}

	s.stopAll()
	if _, ok := creds.authenticate(cred); ok {
		t.Fatal("credential still valid after stopAll")
	}
}

func TestSupervisorRevokesCredentialOnStartFailure(t *testing.T) {
	creds := newWorkerRegistry()
	s := NewSupervisor("/nonexistent/binary", nil, "", 0, 0, creds, "u")

	s.EnsureWorker("agent-a")

	if n := registryLen(creds); n != 0 {
		t.Fatalf("credential registry has %d entries after a failed Start, want 0", n)
	}
	s.mu.Lock()
	_, ok := s.workers["agent-a"]
	s.mu.Unlock()
	if ok {
		t.Fatal("worker registered despite Start failure")
	}
}
