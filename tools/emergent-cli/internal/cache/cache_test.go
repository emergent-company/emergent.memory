package cache

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCacheManager(t *testing.T) {
	// Create temporary directory for cache
	tmpDir := t.TempDir()

	// Create cache manager
	manager, err := NewManager(tmpDir, 1*time.Second)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	// Test Set and Get
	t.Run("SetAndGet", func(t *testing.T) {
		key := "test-key"
		values := []string{"value1", "value2", "value3"}

		err := manager.Set(key, values)
		if err != nil {
			t.Fatalf("Failed to set cache: %v", err)
		}

		retrieved, ok := manager.Get(key)
		if !ok {
			t.Fatal("Expected cache hit, got miss")
		}

		if len(retrieved) != len(values) {
			t.Fatalf("Expected %d values, got %d", len(values), len(retrieved))
		}

		for i, v := range values {
			if retrieved[i] != v {
				t.Errorf("Expected value %s at index %d, got %s", v, i, retrieved[i])
			}
		}
	})

	// Test cache expiration
	t.Run("Expiration", func(t *testing.T) {
		key := "expiring-key"
		values := []string{"value1"}

		err := manager.Set(key, values)
		if err != nil {
			t.Fatalf("Failed to set cache: %v", err)
		}

		// Wait for expiration
		time.Sleep(1500 * time.Millisecond)

		_, ok := manager.Get(key)
		if ok {
			t.Fatal("Expected cache miss after expiration, got hit")
		}
	})

	// Test Clear
	t.Run("Clear", func(t *testing.T) {
		key := "clear-key"
		values := []string{"value1"}

		err := manager.Set(key, values)
		if err != nil {
			t.Fatalf("Failed to set cache: %v", err)
		}

		err = manager.Clear(key)
		if err != nil {
			t.Fatalf("Failed to clear cache: %v", err)
		}

		_, ok := manager.Get(key)
		if ok {
			t.Fatal("Expected cache miss after clear, got hit")
		}
	})

	// Test ClearAll
	t.Run("ClearAll", func(t *testing.T) {
		keys := []string{"key1", "key2", "key3"}
		values := []string{"value1"}

		for _, key := range keys {
			err := manager.Set(key, values)
			if err != nil {
				t.Fatalf("Failed to set cache for key %s: %v", key, err)
			}
		}

		err := manager.ClearAll()
		if err != nil {
			t.Fatalf("Failed to clear all: %v", err)
		}

		for _, key := range keys {
			_, ok := manager.Get(key)
			if ok {
				t.Fatalf("Expected cache miss for key %s after ClearAll, got hit", key)
			}
		}
	})
}

func TestNewManager_DefaultCacheDir(t *testing.T) {
	manager, err := NewManager("", 1*time.Minute)
	if err != nil {
		t.Fatalf("Failed to create manager with default dir: %v", err)
	}

	home, _ := os.UserHomeDir()
	expectedDir := filepath.Join(home, ".emergent", "cache")

	if manager.cacheDir != expectedDir {
		t.Errorf("Expected cache dir %s, got %s", expectedDir, manager.cacheDir)
	}

	// Cleanup
	_ = os.RemoveAll(expectedDir)
}
