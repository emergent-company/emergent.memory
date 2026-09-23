package main

import (
	"context"
	"log"
	"os"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"time"
)

// gatewayOnlyEnvVars are credentials the gateway holds for its own surfaces
// that must never reach bridge children. The bridge legitimately needs most of
// the parent environment (LIVEKIT_*, DEEPGRAM_*, CARTESIA_*, MEMORY_URL, …), so
// these are explicitly stripped instead of allowlisting the whole child env.
var gatewayOnlyEnvVars = []string{
	"AGENT_TRIGGER_TOKEN",   // webhook-only static memory credential
	"SESSION_SECRET",        // browser session cookie HMAC key
	"SHARE_COOKIE_SECRET",   // public-share cookie HMAC key
	"SHARE_REF_SECRET",      // public-share ref secret
	"TOKEN_API_KEY",         // admin X-API-Key
	"GITHUB_WEBHOOK_SECRET", // GitHub webhook HMAC secret
}

// workerEnv returns the parent environment with gateway-only credentials
// removed, so a bridge child cannot read secrets it has no use for.
func workerEnv() []string {
	env := os.Environ()
	out := make([]string, 0, len(env))
	for _, kv := range env {
		if name, _, ok := strings.Cut(kv, "="); ok && slices.Contains(gatewayOnlyEnvVars, name) {
			continue
		}
		out = append(out, kv)
	}
	return out
}

// Supervisor owns the on-demand bridge worker pool. A worker is started by
// EnsureWorker when an agent is first requested (a voice token is minted) and
// stopped once it has been idle for idleTTL, so the gateway needs no standing
// process-global memory credential to poll agent definitions in the background.
type Supervisor struct {
	bin      string
	args     []string
	workdir  string
	interval time.Duration
	idleTTL  time.Duration

	// workerKey + bindingURL are injected into each worker so it can fetch its
	// per-room voice binding from the gateway's internal endpoint.
	workerKey  string
	bindingURL string

	mu      sync.Mutex
	workers map[string]*workerState

	wg   sync.WaitGroup
	done chan struct{}
}

type workerState struct {
	name      string
	cmd       *exec.Cmd
	restarts  int
	startedAt time.Time
	lastUsed  time.Time
}

func NewSupervisor(bin string, args []string, workdir string, interval, idleTTL time.Duration, workerKey, bindingURL string) *Supervisor {
	return &Supervisor{
		bin:        bin,
		args:       args,
		workdir:    workdir,
		interval:   interval,
		idleTTL:    idleTTL,
		workerKey:  workerKey,
		bindingURL: bindingURL,
		workers:    map[string]*workerState{},
		done:       make(chan struct{}),
	}
}

// EnsureWorker starts a bridge worker for name when none is running, and marks
// the existing worker as recently used otherwise. It is idempotent: concurrent
// callers race on the mutex and at most one process is spawned per agent.
func (s *Supervisor) EnsureWorker(name string) {
	if name == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if ws, ok := s.workers[name]; ok {
		ws.lastUsed = time.Now()
		return
	}
	s.spawnLocked(name)
}

// StopWorker stops and removes the worker for name, if one is running. It is
// used to enforce the enabled gate on an agent that was disabled after its
// worker had already been spawned.
func (s *Supervisor) StopWorker(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ws, ok := s.workers[name]
	if !ok {
		return
	}
	log.Printf("supervisor: stopping worker %s (agent disabled)", name)
	_ = ws.cmd.Process.Kill()
	delete(s.workers, name)
}

func (s *Supervisor) spawnLocked(name string) {
	cmd := exec.Command(s.bin, s.args...)
	cmd.Env = append(workerEnv(),
		"AGENT_NAME="+name,
		"WORKER_INTERNAL_KEY="+s.workerKey,
		"VOICE_BINDING_URL="+s.bindingURL,
	)
	if s.workdir != "" {
		cmd.Dir = s.workdir
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		log.Printf("supervisor: start worker %s: %v", name, err)
		return
	}
	now := time.Now()
	ws := &workerState{name: name, cmd: cmd, startedAt: now, lastUsed: now}
	s.workers[name] = ws
	log.Printf("supervisor: started worker %s (pid %d)", name, cmd.Process.Pid)
	s.wg.Go(func() { s.monitor(name, ws) })
}

// monitor waits for a worker to exit and removes it. Respawn is on demand: the
// next EnsureWorker for this agent starts a fresh process, so a crash is only
// recovered when the agent is used again.
func (s *Supervisor) monitor(name string, ws *workerState) {
	err := ws.cmd.Wait()
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, ok := s.workers[name]
	if !ok || cur != ws {
		return // already replaced or reaped
	}
	log.Printf("supervisor: worker %s exited (%v)", name, err)
	ws.restarts++
	delete(s.workers, name)
}

// reapIdle stops workers whose last use is older than idleTTL.
func (s *Supervisor) reapIdle() {
	if s.idleTTL <= 0 {
		return
	}
	cutoff := time.Now().Add(-s.idleTTL)
	s.mu.Lock()
	defer s.mu.Unlock()
	for name, ws := range s.workers {
		if ws.lastUsed.Before(cutoff) {
			log.Printf("supervisor: reaping idle worker %s (idle > %s)", name, s.idleTTL)
			_ = ws.cmd.Process.Kill()
			delete(s.workers, name)
		}
	}
}

// Status returns a snapshot of running workers (for observability).
func (s *Supervisor) Status() []map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]map[string]any, 0, len(s.workers))
	for name, ws := range s.workers {
		out = append(out, map[string]any{
			"name":      name,
			"pid":       ws.cmd.Process.Pid,
			"restarts":  ws.restarts,
			"startedAt": ws.startedAt.Format(time.RFC3339),
			"lastUsed":  ws.lastUsed.Format(time.RFC3339),
		})
	}
	return out
}

// Run drives the idle-reap loop until ctx is cancelled, then stops all workers.
func (s *Supervisor) Run(ctx context.Context) {
	defer close(s.done)
	if s.interval <= 0 {
		<-ctx.Done()
		s.stopAll()
		s.wg.Wait()
		return
	}
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			s.stopAll()
			s.wg.Wait()
			return
		case <-ticker.C:
			s.reapIdle()
		}
	}
}

// Done is closed once Run has stopped all workers and reaped their processes.
func (s *Supervisor) Done() <-chan struct{} { return s.done }

func (s *Supervisor) stopAll() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for name, ws := range s.workers {
		log.Printf("supervisor: stopping worker %s", name)
		_ = ws.cmd.Process.Kill()
	}
	s.workers = map[string]*workerState{}
}
