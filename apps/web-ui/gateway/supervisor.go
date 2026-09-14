package main

import (
	"context"
	"log"
	"os"
	"os/exec"
	"sync"
	"time"
)

// Supervisor reconciles running bridge workers against memory's agent
// definitions: one child process per agent. It holds no agent config — the
// bridge reads everything from env + memory.
type Supervisor struct {
	memory   MemoryBackend
	bin      string
	args     []string
	workdir  string
	interval time.Duration

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
	agentID   string
	cmd       *exec.Cmd
	restarts  int
	startedAt time.Time
}

func NewSupervisor(memory MemoryBackend, bin string, args []string, workdir string, interval time.Duration, workerKey, bindingURL string) *Supervisor {
	return &Supervisor{
		memory:     memory,
		bin:        bin,
		args:       args,
		workdir:    workdir,
		interval:   interval,
		workerKey:  workerKey,
		bindingURL: bindingURL,
		workers:    map[string]*workerState{},
		done:       make(chan struct{}),
	}
}

// Reconcile converges running workers to the current agent list.
func (s *Supervisor) Reconcile(ctx context.Context) {
	agents, err := s.memory.ListAgentDefinitions(ctx)
	if err != nil {
		log.Printf("supervisor: list agents: %v", err)
		return
	}
	desired := map[string]bool{}
	for _, a := range agents {
		if a.Enabled {
			desired[a.Name] = true
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for name, ws := range s.workers {
		if !desired[name] {
			log.Printf("supervisor: stopping worker %s (agent removed or disabled)", name)
			_ = ws.cmd.Process.Kill()
			delete(s.workers, name)
		}
	}
	for name := range desired {
		if _, ok := s.workers[name]; !ok {
			s.spawnLocked(name)
		}
	}
}

func (s *Supervisor) spawnLocked(name string) {
	cmd := exec.Command(s.bin, s.args...)
	cmd.Env = append(os.Environ(),
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
	ws := &workerState{name: name, cmd: cmd, startedAt: time.Now()}
	s.workers[name] = ws
	log.Printf("supervisor: started worker %s (pid %d)", name, cmd.Process.Pid)
	s.wg.Go(func() { s.monitor(name, ws) })
}

// monitor waits for a worker to exit and removes it; the next reconcile tick
// respawns it (the interval is the natural crash-loop backoff).
func (s *Supervisor) monitor(name string, ws *workerState) {
	err := ws.cmd.Wait()
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, ok := s.workers[name]
	if !ok || cur != ws {
		return // already replaced or removed
	}
	log.Printf("supervisor: worker %s exited (%v) — respawn on next reconcile", name, err)
	ws.restarts++
	delete(s.workers, name)
}

// Status returns a snapshot of running workers (for observability).
func (s *Supervisor) Status() []map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]map[string]any, 0, len(s.workers))
	for name, ws := range s.workers {
		out = append(out, map[string]any{
			"name":      name,
			"agentId":   ws.agentID,
			"pid":       ws.cmd.Process.Pid,
			"restarts":  ws.restarts,
			"startedAt": ws.startedAt.Format(time.RFC3339),
		})
	}
	return out
}

// Run drives the reconcile loop until ctx is cancelled, then stops all workers.
func (s *Supervisor) Run(ctx context.Context) {
	defer close(s.done)
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	s.Reconcile(ctx)
	for {
		select {
		case <-ctx.Done():
			s.stopAll()
			s.wg.Wait()
			return
		case <-ticker.C:
			s.Reconcile(ctx)
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
