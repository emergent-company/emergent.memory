package jobs

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestTruncateError(t *testing.T) {
	tests := []struct {
		name string
		msg  string
		want string
	}{
		{
			name: "short message",
			msg:  "short error",
			want: "short error",
		},
		{
			name: "exactly 500 characters",
			msg:  strings.Repeat("a", 500),
			want: strings.Repeat("a", 500),
		},
		{
			name: "501 characters truncated to 500",
			msg:  strings.Repeat("a", 501),
			want: strings.Repeat("a", 500),
		},
		{
			name: "long message truncated",
			msg:  strings.Repeat("b", 1000),
			want: strings.Repeat("b", 500),
		},
		{
			name: "empty string",
			msg:  "",
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateError(tt.msg)
			assert.Equal(t, tt.want, got)
			assert.LessOrEqual(t, len(got), 500)
		})
	}
}

func TestDefaultQueueConfig(t *testing.T) {
	config := DefaultQueueConfig("kb.test_jobs", "object_id")

	assert.Equal(t, "kb.test_jobs", config.TableName)
	assert.Equal(t, "object_id", config.EntityIDColumn)
	assert.Equal(t, 0, config.MaxAttempts) // unlimited by default
	assert.Equal(t, 60, config.BaseRetryDelaySec)
	assert.Equal(t, 3600, config.MaxRetryDelaySec)
	assert.Equal(t, 10, config.BatchSize)
}

func TestJobStatusConstants(t *testing.T) {
	// Test that status constants have expected values
	assert.Equal(t, JobStatus("pending"), StatusPending)
	assert.Equal(t, JobStatus("processing"), StatusProcessing)
	assert.Equal(t, JobStatus("completed"), StatusCompleted)
	assert.Equal(t, JobStatus("failed"), StatusFailed)
	assert.Equal(t, JobStatus("sent"), StatusSent)
}

func TestQueueConfig(t *testing.T) {
	t.Run("custom config values", func(t *testing.T) {
		config := QueueConfig{
			TableName:         "custom.jobs",
			EntityIDColumn:    "custom_id",
			MaxAttempts:       5,
			BaseRetryDelaySec: 30,
			MaxRetryDelaySec:  1800,
			BatchSize:         20,
		}

		assert.Equal(t, "custom.jobs", config.TableName)
		assert.Equal(t, "custom_id", config.EntityIDColumn)
		assert.Equal(t, 5, config.MaxAttempts)
		assert.Equal(t, 30, config.BaseRetryDelaySec)
		assert.Equal(t, 1800, config.MaxRetryDelaySec)
		assert.Equal(t, 20, config.BatchSize)
	})

	t.Run("zero values for optional fields", func(t *testing.T) {
		config := QueueConfig{
			TableName:      "kb.jobs",
			EntityIDColumn: "id",
			// Leave other fields at zero values
		}

		assert.Equal(t, "kb.jobs", config.TableName)
		assert.Equal(t, "id", config.EntityIDColumn)
		assert.Equal(t, 0, config.MaxAttempts)
		assert.Equal(t, 0, config.BaseRetryDelaySec)
		assert.Equal(t, 0, config.MaxRetryDelaySec)
		assert.Equal(t, 0, config.BatchSize)
	})
}

func TestStatsStruct(t *testing.T) {
	stats := Stats{
		Pending:    10,
		Processing: 5,
		Completed:  100,
		Failed:     2,
	}

	assert.Equal(t, int64(10), stats.Pending)
	assert.Equal(t, int64(5), stats.Processing)
	assert.Equal(t, int64(100), stats.Completed)
	assert.Equal(t, int64(2), stats.Failed)
}

func TestDequeueResult(t *testing.T) {
	result := DequeueResult{
		IDs: []string{"id1", "id2", "id3"},
	}

	assert.Len(t, result.IDs, 3)
	assert.Equal(t, "id1", result.IDs[0])
	assert.Equal(t, "id2", result.IDs[1])
	assert.Equal(t, "id3", result.IDs[2])
}

func TestDequeueResult_Empty(t *testing.T) {
	result := DequeueResult{
		IDs: []string{},
	}

	assert.Empty(t, result.IDs)
}

func TestDefaultWorkerConfig(t *testing.T) {
	config := DefaultWorkerConfig("test-worker")

	assert.Equal(t, "test-worker", config.Name)
	assert.Equal(t, 5*time.Second, config.PollInterval)
	assert.Equal(t, 10, config.BatchSize)
	assert.Equal(t, 10, config.StaleThresholdMinutes)
	assert.True(t, config.RecoverStaleOnStart)
}

func TestDefaultWorkerConfig_DifferentNames(t *testing.T) {
	tests := []struct {
		name string
	}{
		{"email-worker"},
		{"chunk-embedding-worker"},
		{"graph-embedding-worker"},
		{""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := DefaultWorkerConfig(tt.name)
			assert.Equal(t, tt.name, config.Name)
			// Other defaults should be the same
			assert.Equal(t, 5*time.Second, config.PollInterval)
			assert.Equal(t, 10, config.BatchSize)
		})
	}
}

func TestWorkerConfig_CustomValues(t *testing.T) {
	config := WorkerConfig{
		Name:                  "custom-worker",
		PollInterval:          10 * time.Second,
		BatchSize:             20,
		StaleThresholdMinutes: 30,
		RecoverStaleOnStart:   false,
	}

	assert.Equal(t, "custom-worker", config.Name)
	assert.Equal(t, 10*time.Second, config.PollInterval)
	assert.Equal(t, 20, config.BatchSize)
	assert.Equal(t, 30, config.StaleThresholdMinutes)
	assert.False(t, config.RecoverStaleOnStart)
}

func TestWorkerConfig_ZeroValues(t *testing.T) {
	config := WorkerConfig{}

	assert.Empty(t, config.Name)
	assert.Zero(t, config.PollInterval)
	assert.Zero(t, config.BatchSize)
	assert.Zero(t, config.StaleThresholdMinutes)
	assert.False(t, config.RecoverStaleOnStart)
}

func TestWorkerMetricsStruct(t *testing.T) {
	t.Run("zero values", func(t *testing.T) {
		metrics := WorkerMetrics{}

		assert.Zero(t, metrics.Processed)
		assert.Zero(t, metrics.Succeeded)
		assert.Zero(t, metrics.Failed)
	})

	t.Run("custom values", func(t *testing.T) {
		metrics := WorkerMetrics{
			Processed: 100,
			Succeeded: 90,
			Failed:    10,
		}

		assert.Equal(t, int64(100), metrics.Processed)
		assert.Equal(t, int64(90), metrics.Succeeded)
		assert.Equal(t, int64(10), metrics.Failed)
	})

	t.Run("success rate calculation", func(t *testing.T) {
		metrics := WorkerMetrics{
			Processed: 100,
			Succeeded: 85,
			Failed:    15,
		}

		// Calculate success rate
		successRate := float64(metrics.Succeeded) / float64(metrics.Processed) * 100
		assert.Equal(t, 85.0, successRate)
	})
}

func TestWorker_IncrementProcessed(t *testing.T) {
	w := &Worker{}

	// Initial state
	metrics := w.Metrics()
	assert.Zero(t, metrics.Processed)
	assert.Zero(t, metrics.Succeeded)
	assert.Zero(t, metrics.Failed)

	// Increment processed
	w.IncrementProcessed()
	metrics = w.Metrics()
	assert.Equal(t, int64(1), metrics.Processed)
	assert.Zero(t, metrics.Succeeded)
	assert.Zero(t, metrics.Failed)

	// Increment again
	w.IncrementProcessed()
	w.IncrementProcessed()
	metrics = w.Metrics()
	assert.Equal(t, int64(3), metrics.Processed)
	assert.Zero(t, metrics.Succeeded)
	assert.Zero(t, metrics.Failed)
}

func TestWorker_IncrementSuccess(t *testing.T) {
	w := &Worker{}

	// Increment success (should increment both processed and succeeded)
	w.IncrementSuccess()
	metrics := w.Metrics()
	assert.Equal(t, int64(1), metrics.Processed)
	assert.Equal(t, int64(1), metrics.Succeeded)
	assert.Zero(t, metrics.Failed)

	// Increment more
	w.IncrementSuccess()
	w.IncrementSuccess()
	metrics = w.Metrics()
	assert.Equal(t, int64(3), metrics.Processed)
	assert.Equal(t, int64(3), metrics.Succeeded)
	assert.Zero(t, metrics.Failed)
}

func TestWorker_IncrementFailure(t *testing.T) {
	w := &Worker{}

	// Increment failure (should increment both processed and failed)
	w.IncrementFailure()
	metrics := w.Metrics()
	assert.Equal(t, int64(1), metrics.Processed)
	assert.Zero(t, metrics.Succeeded)
	assert.Equal(t, int64(1), metrics.Failed)

	// Increment more
	w.IncrementFailure()
	metrics = w.Metrics()
	assert.Equal(t, int64(2), metrics.Processed)
	assert.Zero(t, metrics.Succeeded)
	assert.Equal(t, int64(2), metrics.Failed)
}

func TestWorker_MixedIncrements(t *testing.T) {
	w := &Worker{}

	// Mix of all increment types
	w.IncrementSuccess()
	w.IncrementSuccess()
	w.IncrementFailure()
	w.IncrementProcessed() // processed but not success or failure

	metrics := w.Metrics()
	assert.Equal(t, int64(4), metrics.Processed)
	assert.Equal(t, int64(2), metrics.Succeeded)
	assert.Equal(t, int64(1), metrics.Failed)
}

func TestWorker_IsRunning(t *testing.T) {
	t.Run("initial state is not running", func(t *testing.T) {
		w := &Worker{}
		assert.False(t, w.IsRunning())
	})

	t.Run("running after setting flag", func(t *testing.T) {
		w := &Worker{running: true}
		assert.True(t, w.IsRunning())
	})

	t.Run("not running after clearing flag", func(t *testing.T) {
		w := &Worker{running: true}
		assert.True(t, w.IsRunning())

		w.mu.Lock()
		w.running = false
		w.mu.Unlock()

		assert.False(t, w.IsRunning())
	})
}

func TestWorker_Metrics_Concurrent(t *testing.T) {
	w := &Worker{}

	// Run concurrent increments
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				w.IncrementSuccess()
				w.IncrementFailure()
				_ = w.Metrics() // read while writing
			}
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	metrics := w.Metrics()
	// 10 goroutines * 100 iterations * 2 increments (success + failure)
	assert.Equal(t, int64(2000), metrics.Processed)
	assert.Equal(t, int64(1000), metrics.Succeeded)
	assert.Equal(t, int64(1000), metrics.Failed)
}
