package main

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/emergent-company/emergent.memory/apps/connector.linux/internal/config"
	"github.com/emergent-company/emergent.memory/apps/connector.linux/internal/mcphost"
	"github.com/emergent-company/emergent.memory/apps/connector.linux/internal/relay"
	"github.com/emergent-company/emergent.memory/apps/connector.linux/internal/toolreg"
)

// buildRegistry assembles a fresh registry for cfg: the built-in platform/Apple
// tools (via prepareRelayRegistry) plus a started mcphost Manager for the
// configured local MCP servers, with the global disabled_tools applied last
// (hosted tools are only resolvable after the manager registers them). The
// returned note explains an empty built-in tool set (for example on hosts
// without osascript).
//
// The caller owns the returned Manager and must Close it.
func buildRegistry(ctx context.Context, cfg *config.Config, log *slog.Logger) (*toolreg.Registry, *mcphost.Manager, string, error) {
	reg, note, err := prepareRelayRegistry(cfg)
	if err != nil {
		return nil, nil, note, err
	}
	mgr := mcphost.NewManager(cfg.MCPServers)
	if err := mgr.Start(ctx, reg, log); err != nil {
		mgr.Close()
		return nil, nil, note, err
	}
	// prepareRelayRegistry already disabled the built-in names; re-applying is
	// idempotent and now also catches namespaced hosted tools.
	for _, toolName := range cfg.DisabledTools {
		reg.MarkDisabled(toolName)
	}
	return reg, mgr, note, nil
}

// registryManager owns the connector's live tool registry and the mcphost
// Manager behind its hosted tools. Reload builds a brand-new registry+manager
// from a freshly persisted config, atomically swaps it into the reloadable
// registry, closes the old manager, and asks the relay client to re-register
// so the hub sees the new tool list immediately.
type registryManager struct {
	registry *toolreg.Reloadable
	log      *slog.Logger

	mu      sync.Mutex
	manager *mcphost.Manager
	client  *relay.Client
}

// reload implements the management API's Reloader. The new manager is built
// (and its servers connected) before the old one is closed, so a failed reload
// leaves the running connector untouched; only a successful build is swapped
// in and closed-over.
func (rm *registryManager) reload(ctx context.Context, newCfg *config.Config) ([]mcphost.ServerStatus, error) {
	if rm.registry == nil {
		return nil, fmt.Errorf("registry not initialised")
	}
	newReg, newMgr, _, err := buildRegistry(ctx, newCfg, rm.log)
	if err != nil {
		return nil, err
	}

	rm.mu.Lock()
	old := rm.manager
	rm.manager = newMgr
	client := rm.client
	rm.mu.Unlock()

	rm.registry.Swap(newReg)
	if old != nil {
		old.Close()
	}
	if client != nil {
		if err := client.RefreshRegistration(); err != nil {
			rm.log.Warn("memory-connector relay: refresh registration after reload failed", "err", err)
		}
	}
	return newMgr.Status(), nil
}

// setClient wires the relay client used to re-register after a reload. Called
// once, before the management API starts serving.
func (rm *registryManager) setClient(c *relay.Client) {
	rm.mu.Lock()
	rm.client = c
	rm.mu.Unlock()
}

// Close closes the current hosted-server manager. Safe to call more than once.
func (rm *registryManager) Close() {
	rm.mu.Lock()
	m := rm.manager
	rm.manager = nil
	rm.mu.Unlock()
	if m != nil {
		m.Close()
	}
}
