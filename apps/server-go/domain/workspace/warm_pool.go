package workspace

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

const (
	// defaultWarmPoolSize is the default number of pre-booted containers (0 = disabled).
	defaultWarmPoolSize = 0

	// poolResizeTimeout is the maximum time to wait for pool resize operations.
	poolResizeTimeout = 60 * time.Second

	// replenishTimeout is the timeout for creating a single replenishment container.
	replenishTimeout = 30 * time.Second
)

// WarmPoolConfig configures the warm container pool.
type WarmPoolConfig struct {
	// Size is the target number of pre-booted containers to maintain.
	Size int `json:"size"`
}

// DefaultWarmPoolConfig returns the default warm pool configuration.
func DefaultWarmPoolConfig() WarmPoolConfig {
	return WarmPoolConfig{
		Size: defaultWarmPoolSize,
	}
}

// warmContainer represents a pre-booted container ready for assignment.
type warmContainer struct {
	providerID   string       // Provider-specific container ID
	providerType ProviderType // Which provider created it
	createdAt    time.Time    // When the container was pre-booted
}

// WarmPoolMetrics tracks warm pool usage statistics.
type WarmPoolMetrics struct {
	Hits       int64 `json:"hits"`        // Successful assignments from pool
	Misses     int64 `json:"misses"`      // Requests that required cold start
	PoolSize   int   `json:"pool_size"`   // Current number of warm containers
	TargetSize int   `json:"target_size"` // Configured target pool size
}

// WarmPool manages a pool of pre-booted workspace containers for fast assignment.
// Containers are created using the default provider and matched to incoming requests.
type WarmPool struct {
	mu           sync.Mutex
	config       WarmPoolConfig
	orchestrator *Orchestrator
	log          *slog.Logger

	// Pool of ready containers
	containers []*warmContainer

	// Metrics
	hits   atomic.Int64
	misses atomic.Int64

	// Lifecycle
	stopCh  chan struct{}
	stopped atomic.Bool
}

// NewWarmPool creates a new warm container pool.
func NewWarmPool(orchestrator *Orchestrator, log *slog.Logger, config WarmPoolConfig) *WarmPool {
	cap := config.Size
	if cap < 0 {
		cap = 0
	}
	return &WarmPool{
		config:       config,
		orchestrator: orchestrator,
		log:          log.With("component", "warm-pool"),
		containers:   make([]*warmContainer, 0, cap),
		stopCh:       make(chan struct{}),
	}
}

// Start initializes the warm pool by pre-creating containers.
// This should be called during server startup after providers are registered.
func (wp *WarmPool) Start(ctx context.Context) error {
	if wp.config.Size <= 0 {
		wp.log.Info("warm pool disabled (size=0)")
		return nil
	}

	wp.log.Info("initializing warm pool", "target_size", wp.config.Size)

	// Create containers in parallel
	var wg sync.WaitGroup
	created := make(chan *warmContainer, wp.config.Size)
	errors := make(chan error, wp.config.Size)

	for i := 0; i < wp.config.Size; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			wc, err := wp.createWarmContainer(ctx)
			if err != nil {
				errors <- err
				return
			}
			created <- wc
		}()
	}

	// Wait for all creation goroutines
	wg.Wait()
	close(created)
	close(errors)

	// Collect results
	for wc := range created {
		wp.containers = append(wp.containers, wc)
	}

	// Log any errors
	var errCount int
	for err := range errors {
		errCount++
		wp.log.Warn("failed to create warm container", "error", err)
	}

	wp.log.Info("warm pool initialized",
		"ready", len(wp.containers),
		"failed", errCount,
		"target", wp.config.Size,
	)

	return nil
}

// Stop shuts down the warm pool and destroys all pre-booted containers.
func (wp *WarmPool) Stop(ctx context.Context) error {
	if !wp.stopped.CompareAndSwap(false, true) {
		return nil
	}
	close(wp.stopCh)

	wp.mu.Lock()
	toDestroy := make([]*warmContainer, len(wp.containers))
	copy(toDestroy, wp.containers)
	wp.containers = wp.containers[:0]
	wp.mu.Unlock()

	if len(toDestroy) == 0 {
		return nil
	}

	wp.log.Info("stopping warm pool", "containers_to_destroy", len(toDestroy))

	var wg sync.WaitGroup
	for _, wc := range toDestroy {
		wg.Add(1)
		go func() {
			defer wg.Done()
			wp.destroyContainer(ctx, wc)
		}()
	}
	wg.Wait()

	wp.log.Info("warm pool stopped")
	return nil
}

// Acquire attempts to get a pre-booted container from the pool that matches
// the given provider type. Returns nil if no matching container is available (pool miss).
// On a hit, a replenishment goroutine is started automatically.
func (wp *WarmPool) Acquire(providerType ProviderType) *warmContainer {
	if wp.config.Size <= 0 || wp.stopped.Load() {
		wp.misses.Add(1)
		return nil
	}

	wp.mu.Lock()
	defer wp.mu.Unlock()

	// Find a matching container
	for i, wc := range wp.containers {
		if wc.providerType == providerType {
			// Remove from pool
			wp.containers = append(wp.containers[:i], wp.containers[i+1:]...)
			wp.hits.Add(1)

			wp.log.Info("warm pool hit",
				"provider_id", wc.providerID,
				"provider_type", wc.providerType,
				"age", time.Since(wc.createdAt).Round(time.Millisecond),
				"remaining", len(wp.containers),
			)

			// Trigger async replenishment
			go wp.replenish()

			return wc
		}
	}

	// No matching container
	wp.misses.Add(1)
	wp.log.Debug("warm pool miss",
		"requested_provider", providerType,
		"pool_size", len(wp.containers),
	)

	return nil
}

// Metrics returns current warm pool usage statistics.
func (wp *WarmPool) Metrics() WarmPoolMetrics {
	wp.mu.Lock()
	poolSize := len(wp.containers)
	wp.mu.Unlock()

	return WarmPoolMetrics{
		Hits:       wp.hits.Load(),
		Misses:     wp.misses.Load(),
		PoolSize:   poolSize,
		TargetSize: wp.config.Size,
	}
}

// Resize adjusts the pool to a new target size. Creates or destroys containers
// to match the new size within poolResizeTimeout.
func (wp *WarmPool) Resize(ctx context.Context, newSize int) error {
	if newSize < 0 {
		return fmt.Errorf("pool size cannot be negative")
	}

	wp.mu.Lock()
	oldSize := wp.config.Size
	wp.config.Size = newSize
	currentCount := len(wp.containers)
	wp.mu.Unlock()

	wp.log.Info("resizing warm pool", "old_size", oldSize, "new_size", newSize, "current_count", currentCount)

	if newSize == 0 {
		// Drain all warm containers
		return wp.drainExcess(ctx, 0)
	}

	if currentCount > newSize {
		// Too many — destroy excess
		return wp.drainExcess(ctx, newSize)
	}

	if currentCount < newSize {
		// Too few — create more
		return wp.fillToTarget(ctx, newSize)
	}

	return nil
}

// replenish creates a single new container to maintain pool size.
// Called asynchronously after a container is acquired.
func (wp *WarmPool) replenish() {
	if wp.stopped.Load() {
		return
	}

	wp.mu.Lock()
	currentCount := len(wp.containers)
	targetSize := wp.config.Size
	wp.mu.Unlock()

	if currentCount >= targetSize {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), replenishTimeout)
	defer cancel()

	wc, err := wp.createWarmContainer(ctx)
	if err != nil {
		wp.log.Warn("failed to replenish warm container", "error", err)
		return
	}

	wp.mu.Lock()
	defer wp.mu.Unlock()

	// Re-check — pool may have been resized or stopped
	if wp.stopped.Load() || len(wp.containers) >= wp.config.Size {
		// Discard — pool is full or stopped
		go wp.destroyContainer(context.Background(), wc)
		return
	}

	wp.containers = append(wp.containers, wc)
	wp.log.Debug("warm container replenished",
		"provider_id", wc.providerID,
		"pool_size", len(wp.containers),
	)
}

// fillToTarget creates containers until the pool reaches the target size.
func (wp *WarmPool) fillToTarget(ctx context.Context, target int) error {
	resizeCtx, cancel := context.WithTimeout(ctx, poolResizeTimeout)
	defer cancel()

	wp.mu.Lock()
	needed := target - len(wp.containers)
	wp.mu.Unlock()

	if needed <= 0 {
		return nil
	}

	var wg sync.WaitGroup
	created := make(chan *warmContainer, needed)
	errors := make(chan error, needed)

	for i := 0; i < needed; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			wc, err := wp.createWarmContainer(resizeCtx)
			if err != nil {
				errors <- err
				return
			}
			created <- wc
		}()
	}

	wg.Wait()
	close(created)
	close(errors)

	wp.mu.Lock()
	for wc := range created {
		if len(wp.containers) < target {
			wp.containers = append(wp.containers, wc)
		} else {
			go wp.destroyContainer(context.Background(), wc)
		}
	}
	wp.mu.Unlock()

	var errCount int
	for err := range errors {
		errCount++
		wp.log.Warn("failed to create container during resize", "error", err)
	}

	if errCount > 0 {
		return fmt.Errorf("failed to create %d/%d containers during resize", errCount, needed)
	}

	return nil
}

// drainExcess removes containers until the pool matches the target size.
func (wp *WarmPool) drainExcess(ctx context.Context, target int) error {
	wp.mu.Lock()
	if len(wp.containers) <= target {
		wp.mu.Unlock()
		return nil
	}

	excess := wp.containers[target:]
	wp.containers = wp.containers[:target]
	wp.mu.Unlock()

	var wg sync.WaitGroup
	for _, wc := range excess {
		wg.Add(1)
		go func() {
			defer wg.Done()
			wp.destroyContainer(ctx, wc)
		}()
	}
	wg.Wait()

	return nil
}

// createWarmContainer provisions a new pre-booted container using the default provider.
func (wp *WarmPool) createWarmContainer(ctx context.Context) (*warmContainer, error) {
	// Select the default provider for agent workspaces
	provider, providerType, err := wp.orchestrator.SelectProvider(
		ContainerTypeAgentWorkspace,
		DeploymentSelfHosted,
		"auto",
	)
	if err != nil {
		return nil, fmt.Errorf("no provider available for warm pool: %w", err)
	}

	result, err := provider.Create(ctx, &CreateContainerRequest{
		ContainerType: ContainerTypeAgentWorkspace,
		Labels: map[string]string{
			"emergent.warm-pool": "true",
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create warm container via %s: %w", providerType, err)
	}

	return &warmContainer{
		providerID:   result.ProviderID,
		providerType: providerType,
		createdAt:    time.Now(),
	}, nil
}

// destroyContainer destroys a warm container via its provider.
func (wp *WarmPool) destroyContainer(ctx context.Context, wc *warmContainer) {
	provider, err := wp.orchestrator.GetProvider(wc.providerType)
	if err != nil {
		wp.log.Warn("cannot destroy warm container: provider not found",
			"provider_id", wc.providerID,
			"provider_type", wc.providerType,
		)
		return
	}

	if err := provider.Destroy(ctx, wc.providerID); err != nil {
		wp.log.Warn("failed to destroy warm container",
			"provider_id", wc.providerID,
			"error", err,
		)
	}
}

// IsEnabled returns whether the warm pool is active (size > 0).
func (wp *WarmPool) IsEnabled() bool {
	return wp.config.Size > 0
}

// ProviderIDFromWarm extracts the provider ID from a warm container.
// This is used by the service to assign the container to a workspace.
func (wc *warmContainer) ProviderID() string {
	return wc.providerID
}

// ProviderType returns the provider type of the warm container.
func (wc *warmContainer) Provider() ProviderType {
	return wc.providerType
}
