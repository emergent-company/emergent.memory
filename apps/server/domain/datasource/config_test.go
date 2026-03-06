package datasource

import (
	"os"
	"testing"
	"time"
)

func TestConfig_WorkerInterval(t *testing.T) {
	tests := []struct {
		name             string
		workerIntervalMs int
		expected         time.Duration
	}{
		{
			name:             "30 seconds",
			workerIntervalMs: 30000,
			expected:         30 * time.Second,
		},
		{
			name:             "1 second",
			workerIntervalMs: 1000,
			expected:         time.Second,
		},
		{
			name:             "100ms",
			workerIntervalMs: 100,
			expected:         100 * time.Millisecond,
		},
		{
			name:             "zero",
			workerIntervalMs: 0,
			expected:         0,
		},
		{
			name:             "1 minute",
			workerIntervalMs: 60000,
			expected:         time.Minute,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{WorkerIntervalMs: tt.workerIntervalMs}
			result := cfg.WorkerInterval()
			if result != tt.expected {
				t.Errorf("WorkerInterval() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestConfig_SyncTimeout(t *testing.T) {
	tests := []struct {
		name               string
		syncTimeoutMinutes int
		expected           time.Duration
	}{
		{
			name:               "30 minutes",
			syncTimeoutMinutes: 30,
			expected:           30 * time.Minute,
		},
		{
			name:               "1 minute",
			syncTimeoutMinutes: 1,
			expected:           time.Minute,
		},
		{
			name:               "1 hour",
			syncTimeoutMinutes: 60,
			expected:           time.Hour,
		},
		{
			name:               "zero",
			syncTimeoutMinutes: 0,
			expected:           0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{SyncTimeoutMinutes: tt.syncTimeoutMinutes}
			result := cfg.SyncTimeout()
			if result != tt.expected {
				t.Errorf("SyncTimeout() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestGetEnvBool(t *testing.T) {
	tests := []struct {
		name       string
		key        string
		envValue   string
		setEnv     bool
		defaultVal bool
		expected   bool
	}{
		{
			name:       "true value",
			key:        "TEST_BOOL_TRUE",
			envValue:   "true",
			setEnv:     true,
			defaultVal: false,
			expected:   true,
		},
		{
			name:       "false value",
			key:        "TEST_BOOL_FALSE",
			envValue:   "false",
			setEnv:     true,
			defaultVal: true,
			expected:   false,
		},
		{
			name:       "1 as true",
			key:        "TEST_BOOL_ONE",
			envValue:   "1",
			setEnv:     true,
			defaultVal: false,
			expected:   true,
		},
		{
			name:       "0 as false",
			key:        "TEST_BOOL_ZERO",
			envValue:   "0",
			setEnv:     true,
			defaultVal: true,
			expected:   false,
		},
		{
			name:       "invalid value uses default",
			key:        "TEST_BOOL_INVALID",
			envValue:   "not_a_bool",
			setEnv:     true,
			defaultVal: true,
			expected:   true,
		},
		{
			name:       "unset uses default true",
			key:        "TEST_BOOL_UNSET",
			setEnv:     false,
			defaultVal: true,
			expected:   true,
		},
		{
			name:       "unset uses default false",
			key:        "TEST_BOOL_UNSET2",
			setEnv:     false,
			defaultVal: false,
			expected:   false,
		},
		{
			name:       "empty string uses default",
			key:        "TEST_BOOL_EMPTY",
			envValue:   "",
			setEnv:     true,
			defaultVal: true,
			expected:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clean up after test
			defer os.Unsetenv(tt.key)

			if tt.setEnv {
				os.Setenv(tt.key, tt.envValue)
			} else {
				os.Unsetenv(tt.key)
			}

			result := getEnvBool(tt.key, tt.defaultVal)
			if result != tt.expected {
				t.Errorf("getEnvBool(%q, %v) = %v, want %v", tt.key, tt.defaultVal, result, tt.expected)
			}
		})
	}
}

func TestGetEnvInt(t *testing.T) {
	tests := []struct {
		name       string
		key        string
		envValue   string
		setEnv     bool
		defaultVal int
		expected   int
	}{
		{
			name:       "positive integer",
			key:        "TEST_INT_POS",
			envValue:   "42",
			setEnv:     true,
			defaultVal: 0,
			expected:   42,
		},
		{
			name:       "negative integer",
			key:        "TEST_INT_NEG",
			envValue:   "-10",
			setEnv:     true,
			defaultVal: 0,
			expected:   -10,
		},
		{
			name:       "zero",
			key:        "TEST_INT_ZERO",
			envValue:   "0",
			setEnv:     true,
			defaultVal: 100,
			expected:   0,
		},
		{
			name:       "large number",
			key:        "TEST_INT_LARGE",
			envValue:   "1000000",
			setEnv:     true,
			defaultVal: 0,
			expected:   1000000,
		},
		{
			name:       "invalid value uses default",
			key:        "TEST_INT_INVALID",
			envValue:   "not_a_number",
			setEnv:     true,
			defaultVal: 50,
			expected:   50,
		},
		{
			name:       "float uses default",
			key:        "TEST_INT_FLOAT",
			envValue:   "3.14",
			setEnv:     true,
			defaultVal: 10,
			expected:   10,
		},
		{
			name:       "unset uses default",
			key:        "TEST_INT_UNSET",
			setEnv:     false,
			defaultVal: 99,
			expected:   99,
		},
		{
			name:       "empty string uses default",
			key:        "TEST_INT_EMPTY",
			envValue:   "",
			setEnv:     true,
			defaultVal: 25,
			expected:   25,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clean up after test
			defer os.Unsetenv(tt.key)

			if tt.setEnv {
				os.Setenv(tt.key, tt.envValue)
			} else {
				os.Unsetenv(tt.key)
			}

			result := getEnvInt(tt.key, tt.defaultVal)
			if result != tt.expected {
				t.Errorf("getEnvInt(%q, %v) = %v, want %v", tt.key, tt.defaultVal, result, tt.expected)
			}
		})
	}
}

func TestNewConfig(t *testing.T) {
	// Save original env vars
	origEnabled := os.Getenv("DATASOURCE_SYNC_WORKER_ENABLED")
	origInterval := os.Getenv("DATASOURCE_SYNC_WORKER_INTERVAL_MS")
	origBatchSize := os.Getenv("DATASOURCE_SYNC_WORKER_BATCH_SIZE")
	origStaleJob := os.Getenv("DATASOURCE_SYNC_STALE_JOB_MINUTES")
	origTimeout := os.Getenv("DATASOURCE_SYNC_TIMEOUT_MINUTES")

	// Cleanup
	defer func() {
		if origEnabled != "" {
			os.Setenv("DATASOURCE_SYNC_WORKER_ENABLED", origEnabled)
		} else {
			os.Unsetenv("DATASOURCE_SYNC_WORKER_ENABLED")
		}
		if origInterval != "" {
			os.Setenv("DATASOURCE_SYNC_WORKER_INTERVAL_MS", origInterval)
		} else {
			os.Unsetenv("DATASOURCE_SYNC_WORKER_INTERVAL_MS")
		}
		if origBatchSize != "" {
			os.Setenv("DATASOURCE_SYNC_WORKER_BATCH_SIZE", origBatchSize)
		} else {
			os.Unsetenv("DATASOURCE_SYNC_WORKER_BATCH_SIZE")
		}
		if origStaleJob != "" {
			os.Setenv("DATASOURCE_SYNC_STALE_JOB_MINUTES", origStaleJob)
		} else {
			os.Unsetenv("DATASOURCE_SYNC_STALE_JOB_MINUTES")
		}
		if origTimeout != "" {
			os.Setenv("DATASOURCE_SYNC_TIMEOUT_MINUTES", origTimeout)
		} else {
			os.Unsetenv("DATASOURCE_SYNC_TIMEOUT_MINUTES")
		}
	}()

	t.Run("default values when env vars not set", func(t *testing.T) {
		os.Unsetenv("DATASOURCE_SYNC_WORKER_ENABLED")
		os.Unsetenv("DATASOURCE_SYNC_WORKER_INTERVAL_MS")
		os.Unsetenv("DATASOURCE_SYNC_WORKER_BATCH_SIZE")
		os.Unsetenv("DATASOURCE_SYNC_STALE_JOB_MINUTES")
		os.Unsetenv("DATASOURCE_SYNC_TIMEOUT_MINUTES")

		cfg := NewConfig()

		if !cfg.Enabled {
			t.Error("Enabled should default to true")
		}
		if cfg.WorkerIntervalMs != 30000 {
			t.Errorf("WorkerIntervalMs = %d, want 30000", cfg.WorkerIntervalMs)
		}
		if cfg.WorkerBatchSize != 5 {
			t.Errorf("WorkerBatchSize = %d, want 5", cfg.WorkerBatchSize)
		}
		if cfg.StaleJobMinutes != 10 {
			t.Errorf("StaleJobMinutes = %d, want 10", cfg.StaleJobMinutes)
		}
		if cfg.SyncTimeoutMinutes != 30 {
			t.Errorf("SyncTimeoutMinutes = %d, want 30", cfg.SyncTimeoutMinutes)
		}
	})

	t.Run("custom values from env vars", func(t *testing.T) {
		os.Setenv("DATASOURCE_SYNC_WORKER_ENABLED", "false")
		os.Setenv("DATASOURCE_SYNC_WORKER_INTERVAL_MS", "5000")
		os.Setenv("DATASOURCE_SYNC_WORKER_BATCH_SIZE", "10")
		os.Setenv("DATASOURCE_SYNC_STALE_JOB_MINUTES", "15")
		os.Setenv("DATASOURCE_SYNC_TIMEOUT_MINUTES", "60")

		cfg := NewConfig()

		if cfg.Enabled {
			t.Error("Enabled should be false")
		}
		if cfg.WorkerIntervalMs != 5000 {
			t.Errorf("WorkerIntervalMs = %d, want 5000", cfg.WorkerIntervalMs)
		}
		if cfg.WorkerBatchSize != 10 {
			t.Errorf("WorkerBatchSize = %d, want 10", cfg.WorkerBatchSize)
		}
		if cfg.StaleJobMinutes != 15 {
			t.Errorf("StaleJobMinutes = %d, want 15", cfg.StaleJobMinutes)
		}
		if cfg.SyncTimeoutMinutes != 60 {
			t.Errorf("SyncTimeoutMinutes = %d, want 60", cfg.SyncTimeoutMinutes)
		}
	})
}

func TestConfigStruct(t *testing.T) {
	t.Run("zero values", func(t *testing.T) {
		cfg := &Config{}
		if cfg.Enabled {
			t.Error("Enabled should be false by default")
		}
		if cfg.WorkerIntervalMs != 0 {
			t.Errorf("WorkerIntervalMs = %d, want 0", cfg.WorkerIntervalMs)
		}
		if cfg.WorkerBatchSize != 0 {
			t.Errorf("WorkerBatchSize = %d, want 0", cfg.WorkerBatchSize)
		}
	})

	t.Run("custom values", func(t *testing.T) {
		cfg := &Config{
			Enabled:            true,
			WorkerIntervalMs:   5000,
			WorkerBatchSize:    10,
			StaleJobMinutes:    15,
			SyncTimeoutMinutes: 60,
		}

		if !cfg.Enabled {
			t.Error("Enabled should be true")
		}
		if cfg.WorkerIntervalMs != 5000 {
			t.Errorf("WorkerIntervalMs = %d, want 5000", cfg.WorkerIntervalMs)
		}
		if cfg.WorkerBatchSize != 10 {
			t.Errorf("WorkerBatchSize = %d, want 10", cfg.WorkerBatchSize)
		}
		if cfg.StaleJobMinutes != 15 {
			t.Errorf("StaleJobMinutes = %d, want 15", cfg.StaleJobMinutes)
		}
		if cfg.SyncTimeoutMinutes != 60 {
			t.Errorf("SyncTimeoutMinutes = %d, want 60", cfg.SyncTimeoutMinutes)
		}
	})
}
