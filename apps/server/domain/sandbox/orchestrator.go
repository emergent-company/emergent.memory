package sandbox

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"
)

// knownProviderTypes is the stable, deterministic order in which providers are
// reported by ListProviders and enumerated in selection errors.
var knownProviderTypes = []ProviderType{ProviderGVisor, ProviderFirecracker, ProviderE2B}

// Orchestrator manages provider registration, selection, fallback, and health monitoring.
type Orchestrator struct {
	mu           sync.RWMutex
	providers    map[ProviderType]Provider
	health       map[ProviderType]*HealthStatus
	displayNames map[ProviderType]string
	log          *slog.Logger
	stopCh       chan struct{}
	stopOnce     sync.Once
}

// NewOrchestrator creates a new workspace orchestrator.
func NewOrchestrator(log *slog.Logger) *Orchestrator {
	return &Orchestrator{
		providers:    make(map[ProviderType]Provider),
		health:       make(map[ProviderType]*HealthStatus),
		displayNames: make(map[ProviderType]string),
		log:          log.With("component", "workspace-orchestrator"),
		stopCh:       make(chan struct{}),
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
	delete(o.displayNames, providerType)
	o.log.Info("provider deregistered", "type", providerType)
}

// MarkUnavailable records a known-but-unavailable provider without registering it.
// The provider's display name and reason are stored in the health map only; it is
// never added to the providers map, so selection behaviour is unchanged.
func (o *Orchestrator) MarkUnavailable(pt ProviderType, displayName, reason string) {
	o.mu.Lock()
	defer o.mu.Unlock()

	// Never clobber an already-registered provider.
	if _, exists := o.providers[pt]; exists {
		return
	}

	o.displayNames[pt] = displayName
	o.health[pt] = &HealthStatus{Healthy: false, Message: reason}
	o.log.Info("provider marked unavailable", "type", pt, "reason", reason)
}

// orderedProviderTypes returns the stable reporting order: the known types first
// (in knownProviderTypes order), then any additionally registered types in
// deterministic (sorted) order. Must be called with o.mu held (read or write).
func (o *Orchestrator) orderedProviderTypes() []ProviderType {
	order := make([]ProviderType, 0, len(o.providers)+len(o.displayNames))
	seen := make(map[ProviderType]bool, len(o.providers)+len(o.displayNames))

	for _, pt := range knownProviderTypes {
		if _, ok := o.providers[pt]; ok {
			order = append(order, pt)
			seen[pt] = true
		} else if _, ok := o.displayNames[pt]; ok {
			order = append(order, pt)
			seen[pt] = true
		}
	}

	var leftovers []ProviderType
	for pt := range o.providers {
		if !seen[pt] {
			leftovers = append(leftovers, pt)
		}
	}
	sort.Slice(leftovers, func(i, j int) bool { return leftovers[i] < leftovers[j] })
	order = append(order, leftovers...)

	return order
}

// providerStatusLocked builds a ProviderStatusResponse for a provider type.
// Must be called with o.mu held (read or write).
func (o *Orchestrator) providerStatusLocked(pt ProviderType) ProviderStatusResponse {
	status := ProviderStatusResponse{Type: pt}

	if p, registered := o.providers[pt]; registered {
		status.Registered = true
		status.Name = p.Capabilities().Name
		status.Capabilities = p.Capabilities()
	} else {
		status.Name = o.displayNames[pt]
	}

	if h, ok := o.health[pt]; ok {
		status.Healthy = h.Healthy
		status.Message = h.Message
		status.ActiveCount = h.ActiveCount
	}
	return status
}

// ListProviders returns every known provider (registered or marked unavailable)
// plus any additional registered providers, in a stable order with availability.
func (o *Orchestrator) ListProviders() []ProviderStatusResponse {
	o.mu.RLock()
	defer o.mu.RUnlock()

	order := o.orderedProviderTypes()
	result := make([]ProviderStatusResponse, 0, len(order))
	for _, pt := range order {
		result = append(result, o.providerStatusLocked(pt))
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

	return nil, "", fmt.Errorf("no healthy providers available: %s", o.rejectionReasons())
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

	return nil, "", fmt.Errorf("no healthy providers available (fallback exhausted): %s", o.rejectionReasons())
}

// rejectionReasons builds a deterministic, one-line summary of why each candidate
// provider was rejected during selection, in the same order as ListProviders.
// Must be called with o.mu held (read or write).
func (o *Orchestrator) rejectionReasons() string {
	var parts []string
	for _, pt := range o.orderedProviderTypes() {
		if _, registered := o.providers[pt]; registered {
			if h, ok := o.health[pt]; ok && !h.Healthy {
				parts = append(parts, fmt.Sprintf("%s: unhealthy: %s", pt, h.Message))
			}
		} else {
			msg := ""
			if h, ok := o.health[pt]; ok {
				msg = h.Message
			}
			parts = append(parts, fmt.Sprintf("%s: not registered: %s", pt, msg))
		}
	}
	if len(parts) == 0 {
		return "no providers registered"
	}
	return strings.Join(parts, "; ")
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
// The monitoring goroutine lives for the whole process lifetime and is independent of the
// caller's context; it is only stopped by StopHealthMonitoring.
func (o *Orchestrator) StartHealthMonitoring(ctx context.Context) {
	o.startHealthMonitoring(ctx, 30*time.Second)
}

// startHealthMonitoring starts the health monitoring goroutine with the given interval.
// It detaches from the caller's context so the monitor keeps running for the process
// lifetime regardless of when the caller's context is canceled. Shutdown is driven
// exclusively by stopCh (closed in StopHealthMonitoring).
func (o *Orchestrator) startHealthMonitoring(ctx context.Context, interval time.Duration) {
	// Detach from the caller's context (e.g. the fx OnStart hook ctx, which is
	// canceled ~15s after startup) so health checks keep running forever.
	monitorCtx := context.WithoutCancel(ctx)

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		// Initial health check
		o.checkAllHealth(monitorCtx)

		for {
			select {
			case <-ticker.C:
				o.checkAllHealth(monitorCtx)
			case <-o.stopCh:
				return
			}
		}
	}()
	o.log.Info("provider health monitoring started", "interval", interval)
}

// StopHealthMonitoring stops the background health check goroutine.
// It is idempotent and safe to call multiple times.
func (o *Orchestrator) StopHealthMonitoring() {
	o.stopOnce.Do(func() {
		close(o.stopCh)
	})
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
