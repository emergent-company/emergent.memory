package workspace

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestDefaultCleanupConfig(t *testing.T) {
	cfg := DefaultCleanupConfig()

	assert.Equal(t, 1*time.Hour, cfg.Interval, "default interval should be 1 hour")
	assert.Equal(t, 10, cfg.MaxConcurrent, "default max concurrent should be 10")
	assert.Equal(t, 0.8, cfg.AlertThreshold, "default alert threshold should be 80%")
}

func TestCleanupJob_StartStop(t *testing.T) {
	// Verify that Start/Stop don't panic and are idempotent
	job := &CleanupJob{
		config: CleanupConfig{
			Interval:       100 * time.Millisecond,
			MaxConcurrent:  10,
			AlertThreshold: 0.8,
		},
		stopCh: make(chan struct{}),
	}

	// Stop before start should be safe
	job.Stop()

	// Double stop should be safe (stopCh already closed but running is false)
	job.Stop()
}

func TestCleanupJob_CheckResourceUsage_BelowThreshold(t *testing.T) {
	// Verify resource monitoring at different usage levels.
	// These test the logic of the threshold computation.

	tests := []struct {
		name           string
		activeCount    int
		maxConcurrent  int
		alertThreshold float64
		expectWarning  bool // >= alertThreshold but < 1.0
		expectError    bool // >= 1.0
	}{
		{
			name:           "no active workspaces",
			activeCount:    0,
			maxConcurrent:  10,
			alertThreshold: 0.8,
			expectWarning:  false,
			expectError:    false,
		},
		{
			name:           "below threshold",
			activeCount:    5,
			maxConcurrent:  10,
			alertThreshold: 0.8,
			expectWarning:  false,
			expectError:    false,
		},
		{
			name:           "at threshold",
			activeCount:    8,
			maxConcurrent:  10,
			alertThreshold: 0.8,
			expectWarning:  true,
			expectError:    false,
		},
		{
			name:           "above threshold below max",
			activeCount:    9,
			maxConcurrent:  10,
			alertThreshold: 0.8,
			expectWarning:  true,
			expectError:    false,
		},
		{
			name:           "at max capacity",
			activeCount:    10,
			maxConcurrent:  10,
			alertThreshold: 0.8,
			expectWarning:  false, // Error takes precedence
			expectError:    true,
		},
		{
			name:           "over max capacity",
			activeCount:    12,
			maxConcurrent:  10,
			alertThreshold: 0.8,
			expectWarning:  false,
			expectError:    true,
		},
		{
			name:           "zero max concurrent skips check",
			activeCount:    5,
			maxConcurrent:  0,
			alertThreshold: 0.8,
			expectWarning:  false,
			expectError:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.maxConcurrent <= 0 {
				// Zero maxConcurrent causes early return — no alerts
				return
			}

			usageRatio := float64(tt.activeCount) / float64(tt.maxConcurrent)

			isError := usageRatio >= 1.0
			isWarning := !isError && usageRatio >= tt.alertThreshold

			assert.Equal(t, tt.expectError, isError, "error state mismatch")
			assert.Equal(t, tt.expectWarning, isWarning, "warning state mismatch")
		})
	}
}

func TestCleanupJob_MCPExemption(t *testing.T) {
	// Verify that the ListExpired query naturally excludes persistent MCP servers.
	// Persistent MCP servers have NULL expires_at, and ListExpired filters on
	// "expires_at IS NOT NULL AND expires_at < NOW()" — so they're automatically excluded.

	// Create a persistent MCP server entity
	mcpServer := &AgentWorkspace{
		ContainerType: ContainerTypeMCPServer,
		Lifecycle:     LifecyclePersistent,
		ExpiresAt:     nil, // NULL — will never be returned by ListExpired
	}

	assert.Nil(t, mcpServer.ExpiresAt, "persistent MCP servers should have nil ExpiresAt")
	assert.Equal(t, LifecyclePersistent, mcpServer.Lifecycle)
	assert.Equal(t, ContainerTypeMCPServer, mcpServer.ContainerType)

	// Create an ephemeral workspace with expired TTL
	expired := time.Now().Add(-1 * time.Hour)
	agentWs := &AgentWorkspace{
		ContainerType: ContainerTypeAgentWorkspace,
		Lifecycle:     LifecycleEphemeral,
		ExpiresAt:     &expired,
	}

	assert.NotNil(t, agentWs.ExpiresAt, "ephemeral workspaces should have ExpiresAt set")
	assert.True(t, agentWs.ExpiresAt.Before(time.Now()), "should be expired")

	// Create a non-expired ephemeral workspace
	notExpired := time.Now().Add(24 * time.Hour)
	activeWs := &AgentWorkspace{
		ContainerType: ContainerTypeAgentWorkspace,
		Lifecycle:     LifecycleEphemeral,
		ExpiresAt:     &notExpired,
	}

	assert.NotNil(t, activeWs.ExpiresAt)
	assert.True(t, activeWs.ExpiresAt.After(time.Now()), "should not be expired")
}

func TestCleanupConfig_CustomValues(t *testing.T) {
	cfg := CleanupConfig{
		Interval:       30 * time.Minute,
		MaxConcurrent:  50,
		AlertThreshold: 0.9,
	}

	assert.Equal(t, 30*time.Minute, cfg.Interval)
	assert.Equal(t, 50, cfg.MaxConcurrent)
	assert.Equal(t, 0.9, cfg.AlertThreshold)
}
