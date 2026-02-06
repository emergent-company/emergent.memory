package datasource

import (
	"context"
	"testing"
)

func TestNewNoOpProvider(t *testing.T) {
	tests := []struct {
		name         string
		providerType string
	}{
		{
			name:         "google_drive provider",
			providerType: "google_drive",
		},
		{
			name:         "notion provider",
			providerType: "notion",
		},
		{
			name:         "empty provider type",
			providerType: "",
		},
		{
			name:         "custom provider",
			providerType: "custom_provider",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := NewNoOpProvider(tt.providerType)
			if provider == nil {
				t.Fatal("NewNoOpProvider() returned nil")
			}
			if provider.providerType != tt.providerType {
				t.Errorf("providerType = %q, want %q", provider.providerType, tt.providerType)
			}
		})
	}
}

func TestNoOpProvider_ProviderType(t *testing.T) {
	tests := []struct {
		name         string
		providerType string
	}{
		{"google_drive", "google_drive"},
		{"notion", "notion"},
		{"empty", ""},
		{"with_underscore", "my_custom_provider"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := NewNoOpProvider(tt.providerType)
			result := provider.ProviderType()
			if result != tt.providerType {
				t.Errorf("ProviderType() = %q, want %q", result, tt.providerType)
			}
		})
	}
}

func TestNoOpProvider_TestConnection(t *testing.T) {
	provider := NewNoOpProvider("test")
	ctx := context.Background()
	config := ProviderConfig{
		IntegrationID: "int-123",
		ProjectID:     "proj-456",
		Config:        map[string]interface{}{"key": "value"},
	}

	err := provider.TestConnection(ctx, config)
	if err != nil {
		t.Errorf("TestConnection() error = %v, want nil", err)
	}
}

func TestNoOpProvider_Sync(t *testing.T) {
	t.Run("without progress callback", func(t *testing.T) {
		provider := NewNoOpProvider("test")
		ctx := context.Background()
		config := ProviderConfig{IntegrationID: "int-123", ProjectID: "proj-456"}
		options := SyncOptions{Limit: 10, FullSync: true}

		result, err := provider.Sync(ctx, config, options, nil)
		if err != nil {
			t.Errorf("Sync() error = %v, want nil", err)
		}
		if result == nil {
			t.Fatal("Sync() returned nil result")
		}
		if result.TotalItems != 0 {
			t.Errorf("TotalItems = %d, want 0", result.TotalItems)
		}
		if result.ProcessedItems != 0 {
			t.Errorf("ProcessedItems = %d, want 0", result.ProcessedItems)
		}
	})

	t.Run("with progress callback", func(t *testing.T) {
		provider := NewNoOpProvider("test")
		ctx := context.Background()
		config := ProviderConfig{}
		options := SyncOptions{}

		var progressCalled bool
		var receivedProgress Progress

		callback := func(p Progress) {
			progressCalled = true
			receivedProgress = p
		}

		result, err := provider.Sync(ctx, config, options, callback)
		if err != nil {
			t.Errorf("Sync() error = %v, want nil", err)
		}
		if result == nil {
			t.Fatal("Sync() returned nil result")
		}
		if !progressCalled {
			t.Error("Progress callback was not called")
		}
		if receivedProgress.Phase != "syncing" {
			t.Errorf("Progress.Phase = %q, want %q", receivedProgress.Phase, "syncing")
		}
		if receivedProgress.Message != "No-op sync completed" {
			t.Errorf("Progress.Message = %q, want %q", receivedProgress.Message, "No-op sync completed")
		}
	})
}

func TestProviderRegistry(t *testing.T) {
	t.Run("new registry is empty", func(t *testing.T) {
		registry := NewProviderRegistry()
		if registry == nil {
			t.Fatal("NewProviderRegistry() returned nil")
		}
		types := registry.ListTypes()
		if len(types) != 0 {
			t.Errorf("ListTypes() len = %d, want 0", len(types))
		}
	})

	t.Run("register and get provider", func(t *testing.T) {
		registry := NewProviderRegistry()
		provider := NewNoOpProvider("google_drive")

		registry.Register(provider)

		got, ok := registry.Get("google_drive")
		if !ok {
			t.Error("Get() did not find registered provider")
		}
		if got != provider {
			t.Error("Get() returned different provider")
		}
	})

	t.Run("get unregistered provider", func(t *testing.T) {
		registry := NewProviderRegistry()

		_, ok := registry.Get("nonexistent")
		if ok {
			t.Error("Get() should return false for unregistered provider")
		}
	})

	t.Run("register multiple providers", func(t *testing.T) {
		registry := NewProviderRegistry()
		p1 := NewNoOpProvider("google_drive")
		p2 := NewNoOpProvider("notion")
		p3 := NewNoOpProvider("confluence")

		registry.Register(p1)
		registry.Register(p2)
		registry.Register(p3)

		types := registry.ListTypes()
		if len(types) != 3 {
			t.Errorf("ListTypes() len = %d, want 3", len(types))
		}

		// Check all providers are retrievable
		for _, pt := range []string{"google_drive", "notion", "confluence"} {
			_, ok := registry.Get(pt)
			if !ok {
				t.Errorf("Get(%q) failed", pt)
			}
		}
	})

	t.Run("overwrite provider", func(t *testing.T) {
		registry := NewProviderRegistry()
		p1 := NewNoOpProvider("test")
		p2 := NewNoOpProvider("test")

		registry.Register(p1)
		registry.Register(p2)

		got, ok := registry.Get("test")
		if !ok {
			t.Error("Get() should find provider")
		}
		if got != p2 {
			t.Error("Get() should return the last registered provider")
		}

		types := registry.ListTypes()
		if len(types) != 1 {
			t.Errorf("ListTypes() len = %d, want 1", len(types))
		}
	})
}

func TestProviderConfig(t *testing.T) {
	t.Run("zero values", func(t *testing.T) {
		config := ProviderConfig{}
		if config.IntegrationID != "" {
			t.Errorf("IntegrationID = %q, want empty", config.IntegrationID)
		}
		if config.ProjectID != "" {
			t.Errorf("ProjectID = %q, want empty", config.ProjectID)
		}
		if config.Config != nil {
			t.Error("Config should be nil")
		}
		if config.Metadata != nil {
			t.Error("Metadata should be nil")
		}
	})

	t.Run("with values", func(t *testing.T) {
		config := ProviderConfig{
			IntegrationID: "int-123",
			ProjectID:     "proj-456",
			Config: map[string]interface{}{
				"api_key": "secret",
				"folder":  "/documents",
			},
			Metadata: map[string]interface{}{
				"last_sync": "2024-01-01",
			},
		}

		if config.IntegrationID != "int-123" {
			t.Errorf("IntegrationID = %q, want %q", config.IntegrationID, "int-123")
		}
		if config.ProjectID != "proj-456" {
			t.Errorf("ProjectID = %q, want %q", config.ProjectID, "proj-456")
		}
		if config.Config["api_key"] != "secret" {
			t.Error("Config[api_key] mismatch")
		}
		if config.Metadata["last_sync"] != "2024-01-01" {
			t.Error("Metadata[last_sync] mismatch")
		}
	})
}

func TestSyncOptions(t *testing.T) {
	t.Run("zero values", func(t *testing.T) {
		options := SyncOptions{}
		if options.Limit != 0 {
			t.Errorf("Limit = %d, want 0", options.Limit)
		}
		if options.FullSync {
			t.Error("FullSync should be false")
		}
		if options.ConfigurationID != "" {
			t.Errorf("ConfigurationID = %q, want empty", options.ConfigurationID)
		}
	})

	t.Run("with values", func(t *testing.T) {
		options := SyncOptions{
			Limit:           100,
			FullSync:        true,
			ConfigurationID: "config-123",
			Custom: map[string]interface{}{
				"include_deleted": true,
			},
		}

		if options.Limit != 100 {
			t.Errorf("Limit = %d, want 100", options.Limit)
		}
		if !options.FullSync {
			t.Error("FullSync should be true")
		}
		if options.ConfigurationID != "config-123" {
			t.Errorf("ConfigurationID = %q, want %q", options.ConfigurationID, "config-123")
		}
		if options.Custom["include_deleted"] != true {
			t.Error("Custom[include_deleted] mismatch")
		}
	})
}

func TestSyncResult(t *testing.T) {
	t.Run("zero values", func(t *testing.T) {
		result := SyncResult{}
		if result.TotalItems != 0 {
			t.Errorf("TotalItems = %d, want 0", result.TotalItems)
		}
		if result.ProcessedItems != 0 {
			t.Errorf("ProcessedItems = %d, want 0", result.ProcessedItems)
		}
		if result.DocumentIDs != nil {
			t.Error("DocumentIDs should be nil")
		}
		if result.Errors != nil {
			t.Error("Errors should be nil")
		}
	})

	t.Run("with values", func(t *testing.T) {
		result := SyncResult{
			TotalItems:      100,
			ProcessedItems:  95,
			SuccessfulItems: 90,
			FailedItems:     5,
			SkippedItems:    5,
			DocumentIDs:     []string{"doc-1", "doc-2", "doc-3"},
			Errors:          []string{"error1", "error2"},
		}

		if result.TotalItems != 100 {
			t.Errorf("TotalItems = %d, want 100", result.TotalItems)
		}
		if result.ProcessedItems != 95 {
			t.Errorf("ProcessedItems = %d, want 95", result.ProcessedItems)
		}
		if result.SuccessfulItems != 90 {
			t.Errorf("SuccessfulItems = %d, want 90", result.SuccessfulItems)
		}
		if result.FailedItems != 5 {
			t.Errorf("FailedItems = %d, want 5", result.FailedItems)
		}
		if result.SkippedItems != 5 {
			t.Errorf("SkippedItems = %d, want 5", result.SkippedItems)
		}
		if len(result.DocumentIDs) != 3 {
			t.Errorf("DocumentIDs len = %d, want 3", len(result.DocumentIDs))
		}
		if len(result.Errors) != 2 {
			t.Errorf("Errors len = %d, want 2", len(result.Errors))
		}
	})
}

func TestProgress(t *testing.T) {
	t.Run("zero values", func(t *testing.T) {
		progress := Progress{}
		if progress.Phase != "" {
			t.Errorf("Phase = %q, want empty", progress.Phase)
		}
		if progress.TotalItems != 0 {
			t.Errorf("TotalItems = %d, want 0", progress.TotalItems)
		}
		if progress.Message != "" {
			t.Errorf("Message = %q, want empty", progress.Message)
		}
	})

	t.Run("discovering phase", func(t *testing.T) {
		progress := Progress{
			Phase:      "discovering",
			TotalItems: 50,
			Message:    "Finding items...",
		}

		if progress.Phase != "discovering" {
			t.Errorf("Phase = %q, want %q", progress.Phase, "discovering")
		}
	})

	t.Run("importing phase", func(t *testing.T) {
		progress := Progress{
			Phase:           "importing",
			TotalItems:      100,
			ProcessedItems:  50,
			SuccessfulItems: 48,
			FailedItems:     2,
			Message:         "Importing documents...",
		}

		if progress.Phase != "importing" {
			t.Errorf("Phase = %q, want %q", progress.Phase, "importing")
		}
		if progress.ProcessedItems != 50 {
			t.Errorf("ProcessedItems = %d, want 50", progress.ProcessedItems)
		}
	})

	t.Run("syncing phase", func(t *testing.T) {
		progress := Progress{
			Phase:           "syncing",
			TotalItems:      100,
			ProcessedItems:  100,
			SuccessfulItems: 95,
			FailedItems:     3,
			SkippedItems:    2,
			Message:         "Sync complete",
		}

		if progress.Phase != "syncing" {
			t.Errorf("Phase = %q, want %q", progress.Phase, "syncing")
		}
		if progress.SkippedItems != 2 {
			t.Errorf("SkippedItems = %d, want 2", progress.SkippedItems)
		}
	})
}
