package workspace

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// Orchestrator manages provider registration, selection, fallback, and health monitoring.
type Orchestrator struct {
	mu        sync.RWMutex
	providers map[ProviderType]Provider
	health    map[ProviderType]*HealthStatus
	log       *slog.Logger
	stopCh    chan struct{}
}

// NewOrchestrator creates a new workspace orchestrator.
func NewOrchestrator(log *slog.Logger) *Orchestrator {
	return &Orchestrator{
		providers: make(map[ProviderType]Provider),
		health:    make(map[ProviderType]*HealthStatus),
		log:       log.With("component", "workspace-orchestrator"),
		stopCh:    make(chan struct{}),
	}
}

// RegisterProvider adds a provider to the orchestrator.
func (o *Orchestrator) RegisterProvider(providerType ProviderType, provider Provider) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.providers[providerType] = provider
	o.health[providerType] = &HealthStatus{Healthy: true, Message: "registered"}
	o.log.Info("provider registered", "type", providerType, "name", provider.Capabilities().Name)
}

// DeregisterProvider removes a provider from the orchestrator.
func (o *Orchestrator) DeregisterProvider(providerType ProviderType) {
	o.mu.Lock()
	defer o.mu.Unlock()
	delete(o.providers, providerType)
	delete(o.health, providerType)
	o.log.Info("provider deregistered", "type", providerType)
}

// ListProviders returns all registered providers with their health status.
func (o *Orchestrator) ListProviders() []ProviderStatusResponse {
	o.mu.RLock()
	defer o.mu.RUnlock()

	result := make([]ProviderStatusResponse, 0, len(o.providers))
	for pt, p := range o.providers {
		status := ProviderStatusResponse{
			Name:         p.Capabilities().Name,
			Type:         pt,
			Capabilities: p.Capabilities(),
		}
		if h, ok := o.health[pt]; ok {
			status.Healthy = h.Healthy
			status.Message = h.Message
			status.ActiveCount = h.ActiveCount
		}
		result = append(result, status)
	}
	return result
}

// SelectProvider chooses the best provider based on container type, deployment mode, and availability.
func (o *Orchestrator) SelectProvider(containerType ContainerType, deploymentMode DeploymentMode, requested ProviderType) (Provider, ProviderType, error) {
	o.mu.RLock()
	defer o.mu.RUnlock()

	// Explicit provider request — no fallback
	if requested != "" && requested != "auto" {
		p, ok := o.providers[requested]
		if !ok {
			return nil, "", fmt.Errorf("requested provider %q is not registered", requested)
		}
		if h, ok := o.health[requested]; ok && !h.Healthy {
			return nil, "", fmt.Errorf("requested provider %q is unhealthy: %s", requested, h.Message)
		}
		return p, requested, nil
	}

	// Automatic selection with container-type-aware routing
	chain := o.buildSelectionChain(containerType, deploymentMode)

	for _, pt := range chain {
		p, ok := o.providers[pt]
		if !ok {
			continue
		}
		if h, ok := o.health[pt]; ok && !h.Healthy {
			o.log.Debug("skipping unhealthy provider", "type", pt, "reason", h.Message)
			continue
		}
		o.log.Debug("selected provider", "type", pt, "container_type", containerType)
		return p, pt, nil
	}

	return nil, "", fmt.Errorf("no healthy providers available")
}

// SelectProviderWithFallback tries the primary provider and falls back on failure.
func (o *Orchestrator) SelectProviderWithFallback(containerType ContainerType, deploymentMode DeploymentMode, requested ProviderType) (Provider, ProviderType, error) {
	// Try explicit selection first
	p, pt, err := o.SelectProvider(containerType, deploymentMode, requested)
	if err == nil {
		return p, pt, nil
	}

	// If explicit provider was requested and failed, don't fallback
	if requested != "" && requested != "auto" {
		return nil, "", err
	}

	// Fallback: try any healthy provider
	o.mu.RLock()
	defer o.mu.RUnlock()

	for pt, p := range o.providers {
		if h, ok := o.health[pt]; ok && h.Healthy {
			o.log.Warn("falling back to alternative provider", "type", pt, "original_error", err)
			return p, pt, nil
		}
	}

	return nil, "", fmt.Errorf("no healthy providers available (fallback exhausted)")
}

// GetProvider returns a specific provider by type.
func (o *Orchestrator) GetProvider(providerType ProviderType) (Provider, error) {
	o.mu.RLock()
	defer o.mu.RUnlock()

	p, ok := o.providers[providerType]
	if !ok {
		return nil, fmt.Errorf("provider %q not registered", providerType)
	}
	return p, nil
}

// StartHealthMonitoring begins a background goroutine that checks provider health every 30 seconds.
func (o *Orchestrator) StartHealthMonitoring(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()

		// Initial health check
		o.checkAllHealth(ctx)

		for {
			select {
			case <-ticker.C:
				o.checkAllHealth(ctx)
			case <-o.stopCh:
				return
			case <-ctx.Done():
				return
			}
		}
	}()
	o.log.Info("provider health monitoring started (30s interval)")
}

// StopHealthMonitoring stops the background health check goroutine.
func (o *Orchestrator) StopHealthMonitoring() {
	close(o.stopCh)
}

// checkAllHealth runs health checks on all registered providers.
func (o *Orchestrator) checkAllHealth(ctx context.Context) {
	o.mu.RLock()
	providers := make(map[ProviderType]Provider, len(o.providers))
	for k, v := range o.providers {
		providers[k] = v
	}
	o.mu.RUnlock()

	for pt, p := range providers {
		checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		status, err := p.Health(checkCtx)
		cancel()

		o.mu.Lock()
		if err != nil {
			o.health[pt] = &HealthStatus{
				Healthy: false,
				Message: fmt.Sprintf("health check failed: %v", err),
			}
			o.log.Warn("provider health check failed", "type", pt, "error", err)
		} else {
			o.health[pt] = status
			if !status.Healthy {
				o.log.Warn("provider unhealthy", "type", pt, "message", status.Message)
			}
		}
		o.mu.Unlock()
	}
}

// UpdateHealth manually updates the health status of a provider.
// This is useful for marking a provider unhealthy after a failed operation
// to prevent it from being selected again immediately.
func (o *Orchestrator) UpdateHealth(providerType ProviderType, healthy bool, message string) {
	o.mu.Lock()
	defer o.mu.Unlock()

	if _, exists := o.providers[providerType]; !exists {
		return // Provider not registered
	}

	o.health[providerType] = &HealthStatus{
		Healthy: healthy,
		Message: message,
	}

	if !healthy {
		o.log.Warn("provider manually marked unhealthy", "type", providerType, "message", message)
	}
}

// buildSelectionChain returns the priority order of providers for a given container type and deployment mode.
func (o *Orchestrator) buildSelectionChain(containerType ContainerType, deploymentMode DeploymentMode) []ProviderType {
	if deploymentMode == DeploymentManaged {
		// Managed mode: prefer E2B
		if containerType == ContainerTypeMCPServer {
			return []ProviderType{ProviderGVisor, ProviderE2B, ProviderFirecracker}
		}
		return []ProviderType{ProviderE2B, ProviderFirecracker, ProviderGVisor}
	}

	// Self-hosted mode
	if containerType == ContainerTypeMCPServer {
		// MCP servers prefer gVisor (lighter for long-running)
		return []ProviderType{ProviderGVisor, ProviderFirecracker, ProviderE2B}
	}

	// Agent workspaces prefer Firecracker (better isolation)
	return []ProviderType{ProviderFirecracker, ProviderGVisor, ProviderE2B}
}
