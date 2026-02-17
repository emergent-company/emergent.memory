package workspace

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/emergent/emergent-core/pkg/apperror"
)

const (
	// Default resource limits for MCP servers (lighter than workspace defaults).
	defaultMCPCPU    = "0.5"
	defaultMCPMemory = "512M"
	defaultMCPDisk   = "1G"

	// Crash loop detection and backoff.
	crashWindowDuration  = 60 * time.Second // Window for counting crashes
	crashLoopThreshold   = 3                // Crashes within window to trigger backoff
	initialBackoff       = 5 * time.Second
	maxBackoff           = 5 * time.Minute
	backoffMultiplier    = 3.0
	autoRestartDelay     = 5 * time.Second // Delay before auto-restart (non-backoff)
	gracefulStopTimeout  = 30 * time.Second
	manualRestartTimeout = 10 * time.Second
)

// mcpServerState tracks the runtime state of a hosted MCP server.
type mcpServerState struct {
	mu           sync.Mutex
	workspaceID  string
	providerID   string
	bridge       *StdioBridge
	attachment   *ContainerAttachment
	provider     *GVisorProvider
	stopCh       chan struct{} // Signals the monitor goroutine to stop
	stopped      bool
	restartCount int
	lastCrash    *time.Time
	crashTimes   []time.Time // Recent crash timestamps for backoff calculation
	backoff      time.Duration
	startedAt    time.Time
}

// MCPHostingService manages the lifecycle of MCP server containers.
// It handles registration, stdio bridging, crash recovery with exponential backoff,
// graceful shutdown, and auto-start on boot.
type MCPHostingService struct {
	mu           sync.RWMutex
	servers      map[string]*mcpServerState // workspaceID -> state
	store        *Store
	service      *Service
	orchestrator *Orchestrator
	log          *slog.Logger
	shutdownCh   chan struct{}
	shutdownOnce sync.Once
}

// NewMCPHostingService creates a new MCP hosting service.
func NewMCPHostingService(store *Store, service *Service, orchestrator *Orchestrator, log *slog.Logger) *MCPHostingService {
	return &MCPHostingService{
		servers:      make(map[string]*mcpServerState),
		store:        store,
		service:      service,
		orchestrator: orchestrator,
		log:          log.With("component", "mcp-hosting"),
		shutdownCh:   make(chan struct{}),
	}
}

// Register creates a new persistent MCP server container and starts it.
func (m *MCPHostingService) Register(ctx context.Context, req *RegisterMCPServerRequest) (*MCPServerStatus, error) {
	// Validate
	if req.Name == "" {
		return nil, apperror.NewBadRequest("name is required")
	}
	if req.Image == "" {
		return nil, apperror.NewBadRequest("image is required")
	}

	// Apply default resource limits for MCP servers
	limits := req.ResourceLimits
	if limits == nil {
		limits = &ResourceLimits{
			CPU:    defaultMCPCPU,
			Memory: defaultMCPMemory,
			Disk:   defaultMCPDisk,
		}
	}

	// Build MCP config
	mcpCfg := &MCPConfig{
		Name:          req.Name,
		Image:         req.Image,
		StdioBridge:   req.StdioBridge,
		RestartPolicy: req.RestartPolicy,
		Environment:   req.Environment,
		Volumes:       req.Volumes,
	}
	if mcpCfg.RestartPolicy == "" {
		mcpCfg.RestartPolicy = "always"
	}

	// Create workspace record
	wsReq := &CreateWorkspaceRequest{
		ContainerType:  ContainerTypeMCPServer,
		Provider:       string(ProviderGVisor), // MCP servers prefer gVisor
		ResourceLimits: limits,
		MCPConfig:      mcpCfg,
	}
	wsResp, err := m.service.Create(ctx, wsReq)
	if err != nil {
		return nil, fmt.Errorf("failed to create workspace record: %w", err)
	}

	// Select provider and create actual container
	provider, providerType, err := m.orchestrator.SelectProvider(ContainerTypeMCPServer, DeploymentSelfHosted, ProviderGVisor)
	if err != nil {
		// Clean up workspace record on failure
		_ = m.service.Delete(ctx, wsResp.ID)
		return nil, fmt.Errorf("failed to select provider: %w", err)
	}

	gvisorProvider, ok := provider.(*GVisorProvider)
	if !ok {
		_ = m.service.Delete(ctx, wsResp.ID)
		return nil, fmt.Errorf("MCP hosting requires gVisor provider, got %s", providerType)
	}

	containerReq := &CreateContainerRequest{
		ContainerType:  ContainerTypeMCPServer,
		ResourceLimits: limits,
		BaseImage:      req.Image,
		Labels: map[string]string{
			"mcp.server.name": req.Name,
			"mcp.workspace":   wsResp.ID,
		},
		Env:          req.Environment,
		ExtraVolumes: req.Volumes,
		AttachStdin:  req.StdioBridge,
	}

	// Set command if provided, otherwise let image default handle it
	if len(req.Cmd) > 0 {
		containerReq.Cmd = req.Cmd
	}

	result, err := gvisorProvider.Create(ctx, containerReq)
	if err != nil {
		_ = m.service.Delete(ctx, wsResp.ID)
		return nil, fmt.Errorf("failed to create container: %w", err)
	}

	// Update workspace record with provider ID and status
	ws, err := m.store.GetByID(ctx, wsResp.ID)
	if err != nil || ws == nil {
		_ = gvisorProvider.Destroy(ctx, result.ProviderID)
		return nil, fmt.Errorf("failed to get workspace after creation: %w", err)
	}
	ws.ProviderWorkspaceID = result.ProviderID
	ws.Provider = providerType
	ws.Status = StatusReady
	if _, err := m.store.Update(ctx, ws, "provider_workspace_id", "provider", "status"); err != nil {
		_ = gvisorProvider.Destroy(ctx, result.ProviderID)
		return nil, fmt.Errorf("failed to update workspace: %w", err)
	}

	// Start the server (establish stdio bridge + monitor)
	state := &mcpServerState{
		workspaceID: wsResp.ID,
		providerID:  result.ProviderID,
		provider:    gvisorProvider,
		stopCh:      make(chan struct{}),
		startedAt:   time.Now(),
	}

	if req.StdioBridge {
		if err := m.establishBridge(ctx, state); err != nil {
			m.log.Warn("failed to establish stdio bridge on registration, will retry on crash monitor",
				"workspace_id", wsResp.ID, "error", err)
		}
	}

	m.mu.Lock()
	m.servers[wsResp.ID] = state
	m.mu.Unlock()

	// Start crash monitor
	go m.monitorContainer(state)

	m.log.Info("MCP server registered and started",
		"workspace_id", wsResp.ID,
		"name", req.Name,
		"image", req.Image,
		"stdio_bridge", req.StdioBridge,
		"provider_id", result.ProviderID[:min(12, len(result.ProviderID))],
	)

	return m.buildStatus(ctx, wsResp.ID)
}

// Call routes a JSON-RPC method call to a hosted MCP server via its stdio bridge.
func (m *MCPHostingService) Call(ctx context.Context, workspaceID string, method string, params any, timeout time.Duration) (*JSONRPCResponse, error) {
	m.mu.RLock()
	state, ok := m.servers[workspaceID]
	m.mu.RUnlock()
	if !ok {
		return nil, apperror.NewNotFound("MCP server", workspaceID)
	}

	state.mu.Lock()
	bridge := state.bridge
	state.mu.Unlock()

	if bridge == nil || bridge.IsClosed() {
		return nil, apperror.NewBadRequest("MCP server stdio bridge is not connected")
	}

	// Touch last_used_at
	_ = m.store.TouchLastUsed(ctx, workspaceID, nil)

	return bridge.Call(method, params, timeout)
}

// GetStatus returns the current status of a hosted MCP server.
func (m *MCPHostingService) GetStatus(ctx context.Context, workspaceID string) (*MCPServerStatus, error) {
	return m.buildStatus(ctx, workspaceID)
}

// ListAll returns all hosted MCP servers with their status.
func (m *MCPHostingService) ListAll(ctx context.Context) ([]*MCPServerStatus, error) {
	workspaces, err := m.store.ListPersistentMCPServers(ctx)
	if err != nil {
		return nil, apperror.ErrDatabase.WithInternal(err)
	}

	statuses := make([]*MCPServerStatus, 0, len(workspaces))
	for _, ws := range workspaces {
		status := m.buildStatusFromWorkspace(ws)
		statuses = append(statuses, status)
	}
	return statuses, nil
}

// Remove stops and removes an MCP server container and its workspace record.
func (m *MCPHostingService) Remove(ctx context.Context, workspaceID string) error {
	// Stop the monitor and bridge
	m.stopServer(workspaceID)

	// Get workspace to find provider ID
	ws, err := m.store.GetByID(ctx, workspaceID)
	if err != nil {
		return apperror.ErrDatabase.WithInternal(err)
	}
	if ws == nil {
		return apperror.NewNotFound("MCP server", workspaceID)
	}

	// Destroy the container
	if ws.ProviderWorkspaceID != "" {
		provider, err := m.orchestrator.GetProvider(ws.Provider)
		if err == nil {
			if err := provider.Destroy(ctx, ws.ProviderWorkspaceID); err != nil {
				m.log.Warn("failed to destroy MCP container", "workspace_id", workspaceID, "error", err)
			}
		}
	}

	// Delete workspace record
	if err := m.service.Delete(ctx, workspaceID); err != nil {
		return err
	}

	m.log.Info("MCP server removed", "workspace_id", workspaceID)
	return nil
}

// Restart gracefully restarts an MCP server.
func (m *MCPHostingService) Restart(ctx context.Context, workspaceID string) (*MCPServerStatus, error) {
	m.mu.RLock()
	state, ok := m.servers[workspaceID]
	m.mu.RUnlock()
	if !ok {
		return nil, apperror.NewNotFound("MCP server", workspaceID)
	}

	ws, err := m.store.GetByID(ctx, workspaceID)
	if err != nil || ws == nil {
		return nil, apperror.NewNotFound("MCP server", workspaceID)
	}

	state.mu.Lock()

	// Close existing bridge
	if state.bridge != nil {
		_ = state.bridge.Close()
		state.bridge = nil
	}
	if state.attachment != nil {
		_ = state.attachment.Close()
		state.attachment = nil
	}

	provider := state.provider
	providerID := state.providerID
	state.mu.Unlock()

	// Graceful stop with shorter timeout for manual restart
	m.log.Info("restarting MCP server", "workspace_id", workspaceID)

	stopCtx, cancel := context.WithTimeout(ctx, manualRestartTimeout)
	defer cancel()
	if err := provider.Stop(stopCtx, providerID); err != nil {
		m.log.Warn("failed to gracefully stop MCP container, force destroying", "error", err)
	}

	// Resume (start) the container
	if err := provider.Resume(ctx, providerID); err != nil {
		// If resume fails, the container may be in a bad state
		m.log.Error("failed to resume MCP container after restart", "error", err)
		_, _ = m.service.UpdateStatus(ctx, workspaceID, StatusError)
		return nil, fmt.Errorf("failed to restart container: %w", err)
	}

	// Re-establish bridge
	state.mu.Lock()
	state.restartCount++
	state.crashTimes = nil // Reset crash loop on manual restart
	state.backoff = 0
	state.startedAt = time.Now()
	state.mu.Unlock()

	if ws.MCPConfig != nil && ws.MCPConfig.StdioBridge {
		if err := m.establishBridge(ctx, state); err != nil {
			m.log.Warn("failed to re-establish stdio bridge after restart", "error", err)
		}
	}

	_, _ = m.service.UpdateStatus(ctx, workspaceID, StatusReady)

	m.log.Info("MCP server restarted", "workspace_id", workspaceID)
	return m.buildStatus(ctx, workspaceID)
}

// StartAll starts all persistent MCP servers from the database (for boot-time auto-start).
func (m *MCPHostingService) StartAll(ctx context.Context) error {
	workspaces, err := m.store.ListPersistentMCPServers(ctx)
	if err != nil {
		return fmt.Errorf("failed to list persistent MCP servers: %w", err)
	}

	if len(workspaces) == 0 {
		m.log.Info("no persistent MCP servers to start")
		return nil
	}

	m.log.Info("auto-starting persistent MCP servers", "count", len(workspaces))

	var wg sync.WaitGroup
	for _, ws := range workspaces {
		wg.Add(1)
		go func(ws *AgentWorkspace) {
			defer wg.Done()
			if err := m.startExistingServer(ctx, ws); err != nil {
				m.log.Error("failed to auto-start MCP server",
					"workspace_id", ws.ID,
					"name", ws.MCPConfig.Name,
					"error", err,
				)
			}
		}(ws)
	}
	wg.Wait()

	m.log.Info("MCP server auto-start complete")
	return nil
}

// Shutdown gracefully shuts down all hosted MCP servers (SIGTERM → 30s → SIGKILL).
func (m *MCPHostingService) Shutdown(ctx context.Context) error {
	m.shutdownOnce.Do(func() {
		close(m.shutdownCh)
	})

	m.mu.RLock()
	serverIDs := make([]string, 0, len(m.servers))
	for id := range m.servers {
		serverIDs = append(serverIDs, id)
	}
	m.mu.RUnlock()

	if len(serverIDs) == 0 {
		return nil
	}

	m.log.Info("shutting down MCP servers", "count", len(serverIDs))

	// Stop all servers in parallel with graceful timeout
	shutdownCtx, cancel := context.WithTimeout(ctx, gracefulStopTimeout)
	defer cancel()

	var wg sync.WaitGroup
	for _, id := range serverIDs {
		wg.Add(1)
		go func(workspaceID string) {
			defer wg.Done()
			m.stopServerGracefully(shutdownCtx, workspaceID)
		}(id)
	}
	wg.Wait()

	m.log.Info("all MCP servers shut down")
	return nil
}

// --- Internal Methods ---

// establishBridge attaches to the container and creates a stdio bridge.
func (m *MCPHostingService) establishBridge(ctx context.Context, state *mcpServerState) error {
	state.mu.Lock()
	defer state.mu.Unlock()

	// Close existing bridge if any
	if state.bridge != nil {
		_ = state.bridge.Close()
	}
	if state.attachment != nil {
		_ = state.attachment.Close()
	}

	attachment, err := state.provider.AttachToContainer(ctx, state.providerID)
	if err != nil {
		return fmt.Errorf("failed to attach to container: %w", err)
	}

	state.attachment = attachment
	state.bridge = NewStdioBridge(attachment.Conn, attachment.Reader, m.log)

	m.log.Debug("stdio bridge established", "workspace_id", state.workspaceID)
	return nil
}

// startExistingServer starts a previously registered MCP server (used at boot time).
func (m *MCPHostingService) startExistingServer(ctx context.Context, ws *AgentWorkspace) error {
	if ws.ProviderWorkspaceID == "" {
		return fmt.Errorf("workspace %s has no provider ID", ws.ID)
	}

	providerIface, err := m.orchestrator.GetProvider(ws.Provider)
	if err != nil {
		return fmt.Errorf("provider %s not available: %w", ws.Provider, err)
	}

	gvisorProvider, ok := providerIface.(*GVisorProvider)
	if !ok {
		return fmt.Errorf("MCP hosting requires gVisor provider")
	}

	// Check if container is already running
	inspection, err := gvisorProvider.InspectContainer(ctx, ws.ProviderWorkspaceID)
	if err != nil {
		m.log.Warn("MCP container not found, may need re-creation", "workspace_id", ws.ID, "error", err)
		_, _ = m.service.UpdateStatus(ctx, ws.ID, StatusError)
		return err
	}

	if !inspection.Running {
		// Resume the container
		if err := gvisorProvider.Resume(ctx, ws.ProviderWorkspaceID); err != nil {
			m.log.Warn("failed to resume MCP container", "workspace_id", ws.ID, "error", err)
			_, _ = m.service.UpdateStatus(ctx, ws.ID, StatusError)
			return err
		}
	}

	state := &mcpServerState{
		workspaceID: ws.ID,
		providerID:  ws.ProviderWorkspaceID,
		provider:    gvisorProvider,
		stopCh:      make(chan struct{}),
		startedAt:   time.Now(),
	}

	// Establish bridge if configured
	if ws.MCPConfig != nil && ws.MCPConfig.StdioBridge {
		if err := m.establishBridge(ctx, state); err != nil {
			m.log.Warn("failed to establish stdio bridge on auto-start", "workspace_id", ws.ID, "error", err)
		}
	}

	m.mu.Lock()
	m.servers[ws.ID] = state
	m.mu.Unlock()

	// Start crash monitor
	go m.monitorContainer(state)

	_, _ = m.service.UpdateStatus(ctx, ws.ID, StatusReady)

	m.log.Info("MCP server auto-started",
		"workspace_id", ws.ID,
		"name", ws.MCPConfig.Name,
	)
	return nil
}

// monitorContainer watches a container and auto-restarts on crash.
func (m *MCPHostingService) monitorContainer(state *mcpServerState) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-state.stopCh:
			return
		case <-m.shutdownCh:
			return
		case <-ticker.C:
			m.checkContainerHealth(state)
		}
	}
}

// checkContainerHealth checks if the container is still running and handles restarts.
func (m *MCPHostingService) checkContainerHealth(state *mcpServerState) {
	state.mu.Lock()
	if state.stopped {
		state.mu.Unlock()
		return
	}
	provider := state.provider
	providerID := state.providerID
	workspaceID := state.workspaceID
	state.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	inspection, err := provider.InspectContainer(ctx, providerID)
	if err != nil {
		m.log.Warn("failed to inspect MCP container", "workspace_id", workspaceID, "error", err)
		return
	}

	if inspection.Running {
		return // Container is healthy
	}

	// Container has stopped — check restart policy
	ws, err := m.store.GetByID(ctx, workspaceID)
	if err != nil || ws == nil {
		m.log.Error("failed to get workspace for restart decision", "workspace_id", workspaceID, "error", err)
		return
	}

	restartPolicy := "always"
	if ws.MCPConfig != nil && ws.MCPConfig.RestartPolicy != "" {
		restartPolicy = ws.MCPConfig.RestartPolicy
	}

	if restartPolicy == "never" {
		m.log.Info("MCP server exited, restart policy is 'never'",
			"workspace_id", workspaceID,
			"exit_code", inspection.ExitCode,
		)
		_, _ = m.service.UpdateStatus(ctx, workspaceID, StatusStopped)
		return
	}

	if restartPolicy == "on-failure" && inspection.ExitCode == 0 {
		m.log.Info("MCP server exited cleanly, restart policy is 'on-failure'",
			"workspace_id", workspaceID,
		)
		_, _ = m.service.UpdateStatus(ctx, workspaceID, StatusStopped)
		return
	}

	// Record crash
	now := time.Now()
	state.mu.Lock()
	state.lastCrash = &now
	state.crashTimes = append(state.crashTimes, now)
	state.restartCount++

	// Prune old crashes outside the window
	cutoff := now.Add(-crashWindowDuration)
	pruned := state.crashTimes[:0]
	for _, t := range state.crashTimes {
		if t.After(cutoff) {
			pruned = append(pruned, t)
		}
	}
	state.crashTimes = pruned

	// Calculate backoff
	recentCrashes := len(state.crashTimes)
	delay := autoRestartDelay
	if recentCrashes >= crashLoopThreshold {
		// Crash loop detected — apply exponential backoff
		if state.backoff == 0 {
			state.backoff = initialBackoff
		} else {
			state.backoff = time.Duration(float64(state.backoff) * backoffMultiplier)
			if state.backoff > maxBackoff {
				state.backoff = maxBackoff
			}
		}
		delay = state.backoff
		m.log.Warn("MCP server in crash loop, applying backoff",
			"workspace_id", workspaceID,
			"recent_crashes", recentCrashes,
			"backoff", delay,
		)
	} else {
		// Reset backoff if not in crash loop
		state.backoff = 0
	}

	exitCode := inspection.ExitCode
	restartNum := state.restartCount
	state.mu.Unlock()

	m.log.Warn("MCP server crashed, scheduling restart",
		"workspace_id", workspaceID,
		"exit_code", exitCode,
		"restart_count", restartNum,
		"delay", delay,
	)

	_, _ = m.service.UpdateStatus(ctx, workspaceID, StatusCreating)

	// Schedule restart after delay
	go func() {
		select {
		case <-time.After(delay):
			m.restartContainer(workspaceID)
		case <-state.stopCh:
			return
		case <-m.shutdownCh:
			return
		}
	}()
}

// restartContainer restarts a crashed MCP server container.
func (m *MCPHostingService) restartContainer(workspaceID string) {
	m.mu.RLock()
	state, ok := m.servers[workspaceID]
	m.mu.RUnlock()
	if !ok {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	state.mu.Lock()
	if state.stopped {
		state.mu.Unlock()
		return
	}

	provider := state.provider
	providerID := state.providerID

	// Close existing bridge
	if state.bridge != nil {
		_ = state.bridge.Close()
		state.bridge = nil
	}
	if state.attachment != nil {
		_ = state.attachment.Close()
		state.attachment = nil
	}
	state.mu.Unlock()

	// Resume (restart) the container
	if err := provider.Resume(ctx, providerID); err != nil {
		m.log.Error("failed to restart MCP container",
			"workspace_id", workspaceID,
			"error", err,
		)
		_, _ = m.service.UpdateStatus(ctx, workspaceID, StatusError)
		return
	}

	// Re-establish bridge
	ws, err := m.store.GetByID(ctx, workspaceID)
	if err == nil && ws != nil && ws.MCPConfig != nil && ws.MCPConfig.StdioBridge {
		if err := m.establishBridge(ctx, state); err != nil {
			m.log.Warn("failed to re-establish stdio bridge after restart", "workspace_id", workspaceID, "error", err)
		}
	}

	state.mu.Lock()
	state.startedAt = time.Now()
	state.mu.Unlock()

	_, _ = m.service.UpdateStatus(ctx, workspaceID, StatusReady)

	m.log.Info("MCP server restarted after crash", "workspace_id", workspaceID)
}

// stopServer stops a server's monitor and bridge (used for removal).
func (m *MCPHostingService) stopServer(workspaceID string) {
	m.mu.Lock()
	state, ok := m.servers[workspaceID]
	if ok {
		delete(m.servers, workspaceID)
	}
	m.mu.Unlock()

	if !ok {
		return
	}

	state.mu.Lock()
	state.stopped = true
	close(state.stopCh)
	if state.bridge != nil {
		_ = state.bridge.Close()
	}
	if state.attachment != nil {
		_ = state.attachment.Close()
	}
	state.mu.Unlock()
}

// stopServerGracefully stops a server with graceful shutdown (SIGTERM → wait → SIGKILL via provider.Stop).
func (m *MCPHostingService) stopServerGracefully(ctx context.Context, workspaceID string) {
	m.mu.Lock()
	state, ok := m.servers[workspaceID]
	if ok {
		delete(m.servers, workspaceID)
	}
	m.mu.Unlock()

	if !ok {
		return
	}

	state.mu.Lock()
	state.stopped = true
	close(state.stopCh)
	if state.bridge != nil {
		_ = state.bridge.Close()
	}
	if state.attachment != nil {
		_ = state.attachment.Close()
	}
	provider := state.provider
	providerID := state.providerID
	state.mu.Unlock()

	// Graceful stop via provider (sends SIGTERM, waits timeout, then SIGKILL)
	if err := provider.Stop(ctx, providerID); err != nil {
		m.log.Warn("failed to gracefully stop MCP server", "workspace_id", workspaceID, "error", err)
	}

	_, _ = m.service.UpdateStatus(ctx, workspaceID, StatusStopped)
	m.log.Info("MCP server stopped gracefully", "workspace_id", workspaceID)
}

// buildStatus builds a status response for an MCP server.
func (m *MCPHostingService) buildStatus(ctx context.Context, workspaceID string) (*MCPServerStatus, error) {
	ws, err := m.store.GetByID(ctx, workspaceID)
	if err != nil {
		return nil, apperror.ErrDatabase.WithInternal(err)
	}
	if ws == nil {
		return nil, apperror.NewNotFound("MCP server", workspaceID)
	}

	return m.buildStatusFromWorkspace(ws), nil
}

// buildStatusFromWorkspace builds a status response from a workspace entity.
func (m *MCPHostingService) buildStatusFromWorkspace(ws *AgentWorkspace) *MCPServerStatus {
	status := &MCPServerStatus{
		WorkspaceID: ws.ID,
		Status:      ws.Status,
		Provider:    ws.Provider,
		CreatedAt:   ws.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		LastUsedAt:  ws.LastUsedAt.Format("2006-01-02T15:04:05Z07:00"),
	}

	if ws.MCPConfig != nil {
		status.Name = ws.MCPConfig.Name
		status.Image = ws.MCPConfig.Image
		status.StdioBridge = ws.MCPConfig.StdioBridge
		status.RestartPolicy = ws.MCPConfig.RestartPolicy
		status.Volumes = ws.MCPConfig.Volumes
	}

	if ws.ResourceLimits != nil {
		status.ResourceLimits = ws.ResourceLimits
	}

	// Merge runtime state
	m.mu.RLock()
	state, ok := m.servers[ws.ID]
	m.mu.RUnlock()

	if ok {
		state.mu.Lock()
		status.RestartCount = state.restartCount
		if state.lastCrash != nil {
			t := state.lastCrash.Format("2006-01-02T15:04:05Z07:00")
			status.LastCrash = &t
		}
		status.Uptime = time.Since(state.startedAt).String()
		status.BridgeConnected = state.bridge != nil && !state.bridge.IsClosed()
		state.mu.Unlock()
	}

	return status
}
