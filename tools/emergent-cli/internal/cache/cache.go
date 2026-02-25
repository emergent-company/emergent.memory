// Package cache provides completion caching functionality.
package cache

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// Entry represents a cached completion entry.
type Entry struct {
	Values    []string  `json:"values"`
	ExpiresAt time.Time `json:"expires_at"`
}

// Manager handles cache operations.
type Manager struct {
	cacheDir string
	ttl      time.Duration
}

// NewManager creates a new cache manager.
func NewManager(cacheDir string, ttl time.Duration) (*Manager, error) {
	if cacheDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		cacheDir = filepath.Join(home, ".emergent", "cache")
	}

	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return nil, err
	}

	return &Manager{
		cacheDir: cacheDir,
		ttl:      ttl,
	}, nil
}

// Get retrieves cached values if they exist and are not expired.
func (m *Manager) Get(key string) ([]string, bool) {
	path := filepath.Join(m.cacheDir, key+".json")

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}

	var entry Entry
	if err := json.Unmarshal(data, &entry); err != nil {
		return nil, false
	}

	if time.Now().After(entry.ExpiresAt) {
		// Cache expired
		return nil, false
	}

	return entry.Values, true
}

// Set stores values in cache with TTL.
func (m *Manager) Set(key string, values []string) error {
	entry := Entry{
		Values:    values,
		ExpiresAt: time.Now().Add(m.ttl),
	}

	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}

	path := filepath.Join(m.cacheDir, key+".json")
	return os.WriteFile(path, data, 0644)
}

// Clear removes a specific cache entry.
func (m *Manager) Clear(key string) error {
	path := filepath.Join(m.cacheDir, key+".json")
	return os.Remove(path)
}

// ClearAll removes all cache entries.
func (m *Manager) ClearAll() error {
	entries, err := os.ReadDir(m.cacheDir)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		if filepath.Ext(entry.Name()) == ".json" {
			if err := os.Remove(filepath.Join(m.cacheDir, entry.Name())); err != nil {
				return err
			}
		}
	}

	return nil
}
