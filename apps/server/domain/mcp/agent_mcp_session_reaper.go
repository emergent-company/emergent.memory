package mcp

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// agentMCPSessionReaperInterval is how often the reaper scans for expired
// sessions. It mirrors the agents domain's staleRunReaperInterval cadence.
const agentMCPSessionReaperInterval = 5 * time.Minute

// AgentMCPSessionReaper periodically marks idle/expired agent MCP sessions as
// expired so they are no longer reported as active and can no longer be
// continued. It mirrors StaleRunReaper (domain/agents/stale_run_reaper.go).
type AgentMCPSessionReaper struct {
	svc     *Service
	log     *slog.Logger
	stopCh  chan struct{}
	doneCh  chan struct{}
	mu      sync.Mutex
	running bool
}

// NewAgentMCPSessionReaper creates a new session reaper.
func NewAgentMCPSessionReaper(svc *Service, log *slog.Logger) *AgentMCPSessionReaper {
	return &AgentMCPSessionReaper{
		svc:    svc,
		log:    log.With("component", "agent-mcp-session-reaper"),
		stopCh: make(chan struct{}),
		doneCh: make(chan struct{}),
	}
}

// Start begins the periodic reaper goroutine.
func (r *AgentMCPSessionReaper) Start(ctx context.Context) {
	r.mu.Lock()
	if r.running {
		r.mu.Unlock()
		return
	}
	r.running = true
	r.mu.Unlock()

	go func() {
		defer close(r.doneCh)

		ticker := time.NewTicker(agentMCPSessionReaperInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				r.reap(ctx)
			case <-r.stopCh:
				return
			case <-ctx.Done():
				return
			}
		}
	}()
}

// Stop signals the reaper to stop and waits for it to finish.
func (r *AgentMCPSessionReaper) Stop() {
	r.mu.Lock()
	if !r.running {
		r.mu.Unlock()
		return
	}
	r.mu.Unlock()

	close(r.stopCh)
	<-r.doneCh
}

// reap marks every session past its expiry as expired.
func (r *AgentMCPSessionReaper) reap(ctx context.Context) {
	store := r.svc.agentSessionStore()
	if store == nil {
		return
	}
	n, err := store.MarkExpiredSessions(ctx, time.Now().UTC())
	if err != nil {
		r.log.Warn("failed to mark expired agent MCP sessions",
			slog.String("error", err.Error()),
		)
		return
	}
	if n > 0 {
		r.log.Info("marked agent MCP sessions as expired",
			slog.Int("count", n),
		)
	}
}
