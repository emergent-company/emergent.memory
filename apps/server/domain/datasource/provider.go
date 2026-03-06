package datasource

import (
	"context"
	"sync"
)

// Provider is the interface that data source providers must implement.
// Each provider handles syncing data from a specific external service.
type Provider interface {
	// ProviderType returns the unique type identifier for this provider
	ProviderType() string

	// TestConnection tests if the connection to the data source is valid
	TestConnection(ctx context.Context, config ProviderConfig) error

	// Sync performs a full sync operation and returns sync results
	// The sync method should handle duplicate detection internally
	Sync(ctx context.Context, config ProviderConfig, options SyncOptions, progress ProgressCallback) (*SyncResult, error)
}

// ProviderConfig contains the decrypted configuration for a provider
type ProviderConfig struct {
	IntegrationID string
	ProjectID     string
	Config        map[string]interface{}
	Metadata      map[string]interface{}
}

// SyncOptions contains options for a sync operation
type SyncOptions struct {
	// Limit is the maximum number of items to sync (0 = no limit)
	Limit int

	// FullSync forces a full re-sync instead of incremental
	FullSync bool

	// ConfigurationID is the specific sync configuration to use
	ConfigurationID string

	// Custom options from the sync job
	Custom map[string]interface{}
}

// SyncResult contains the results of a sync operation
type SyncResult struct {
	TotalItems      int
	ProcessedItems  int
	SuccessfulItems int
	FailedItems     int
	SkippedItems    int
	DocumentIDs     []string
	Errors          []string
}

// ProgressCallback is called by providers to report sync progress
type ProgressCallback func(progress Progress)

// Progress represents the current progress of a sync operation
type Progress struct {
	Phase           string // 'discovering', 'importing', 'syncing'
	TotalItems      int
	ProcessedItems  int
	SuccessfulItems int
	FailedItems     int
	SkippedItems    int
	Message         string
}

// ProviderRegistry manages available data source providers
type ProviderRegistry struct {
	providers map[string]Provider
	mu        sync.RWMutex
}

// NewProviderRegistry creates a new provider registry
func NewProviderRegistry() *ProviderRegistry {
	return &ProviderRegistry{
		providers: make(map[string]Provider),
	}
}

// Register adds a provider to the registry
func (r *ProviderRegistry) Register(provider Provider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.providers[provider.ProviderType()] = provider
}

// Get retrieves a provider by type
func (r *ProviderRegistry) Get(providerType string) (Provider, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	provider, ok := r.providers[providerType]
	return provider, ok
}

// ListTypes returns all registered provider types
func (r *ProviderRegistry) ListTypes() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	types := make([]string, 0, len(r.providers))
	for t := range r.providers {
		types = append(types, t)
	}
	return types
}

// NoOpProvider is a placeholder provider for testing
type NoOpProvider struct {
	providerType string
}

// NewNoOpProvider creates a no-op provider
func NewNoOpProvider(providerType string) *NoOpProvider {
	return &NoOpProvider{providerType: providerType}
}

func (p *NoOpProvider) ProviderType() string {
	return p.providerType
}

func (p *NoOpProvider) TestConnection(ctx context.Context, config ProviderConfig) error {
	return nil
}

func (p *NoOpProvider) Sync(ctx context.Context, config ProviderConfig, options SyncOptions, progress ProgressCallback) (*SyncResult, error) {
	// Report progress
	if progress != nil {
		progress(Progress{
			Phase:   "syncing",
			Message: "No-op sync completed",
		})
	}

	return &SyncResult{
		TotalItems:      0,
		ProcessedItems:  0,
		SuccessfulItems: 0,
		FailedItems:     0,
		SkippedItems:    0,
	}, nil
}
