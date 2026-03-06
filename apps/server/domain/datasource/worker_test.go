package datasource

import (
	"sync"
	"testing"
)

func TestWorker_Metrics(t *testing.T) {
	// Create a minimal worker for testing metrics functions
	w := &Worker{}

	t.Run("initial metrics are zero", func(t *testing.T) {
		m := w.Metrics()
		if m.Processed != 0 || m.Succeeded != 0 || m.Failed != 0 || m.DeadLetter != 0 {
			t.Errorf("initial metrics should all be 0, got %+v", m)
		}
	})

	t.Run("incrementSuccess", func(t *testing.T) {
		w := &Worker{} // fresh worker
		w.incrementSuccess()
		m := w.Metrics()
		if m.Processed != 1 {
			t.Errorf("Processed = %d, want 1", m.Processed)
		}
		if m.Succeeded != 1 {
			t.Errorf("Succeeded = %d, want 1", m.Succeeded)
		}
		if m.Failed != 0 || m.DeadLetter != 0 {
			t.Errorf("Failed and DeadLetter should be 0, got Failed=%d, DeadLetter=%d", m.Failed, m.DeadLetter)
		}
	})

	t.Run("incrementFailure", func(t *testing.T) {
		w := &Worker{} // fresh worker
		w.incrementFailure()
		m := w.Metrics()
		if m.Processed != 1 {
			t.Errorf("Processed = %d, want 1", m.Processed)
		}
		if m.Failed != 1 {
			t.Errorf("Failed = %d, want 1", m.Failed)
		}
		if m.Succeeded != 0 || m.DeadLetter != 0 {
			t.Errorf("Succeeded and DeadLetter should be 0, got Succeeded=%d, DeadLetter=%d", m.Succeeded, m.DeadLetter)
		}
	})

	t.Run("incrementDeadLetter", func(t *testing.T) {
		w := &Worker{} // fresh worker
		w.incrementDeadLetter()
		m := w.Metrics()
		if m.Processed != 1 {
			t.Errorf("Processed = %d, want 1", m.Processed)
		}
		if m.DeadLetter != 1 {
			t.Errorf("DeadLetter = %d, want 1", m.DeadLetter)
		}
		if m.Succeeded != 0 || m.Failed != 0 {
			t.Errorf("Succeeded and Failed should be 0, got Succeeded=%d, Failed=%d", m.Succeeded, m.Failed)
		}
	})

	t.Run("multiple increments", func(t *testing.T) {
		w := &Worker{} // fresh worker
		w.incrementSuccess()
		w.incrementSuccess()
		w.incrementFailure()
		w.incrementDeadLetter()
		w.incrementDeadLetter()
		w.incrementDeadLetter()

		m := w.Metrics()
		if m.Processed != 6 {
			t.Errorf("Processed = %d, want 6", m.Processed)
		}
		if m.Succeeded != 2 {
			t.Errorf("Succeeded = %d, want 2", m.Succeeded)
		}
		if m.Failed != 1 {
			t.Errorf("Failed = %d, want 1", m.Failed)
		}
		if m.DeadLetter != 3 {
			t.Errorf("DeadLetter = %d, want 3", m.DeadLetter)
		}
	})
}

func TestWorker_Metrics_Concurrent(t *testing.T) {
	w := &Worker{}
	const goroutines = 10
	const iterations = 100

	var wg sync.WaitGroup
	wg.Add(goroutines * 3)

	// Concurrent success increments
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				w.incrementSuccess()
			}
		}()
	}

	// Concurrent failure increments
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				w.incrementFailure()
			}
		}()
	}

	// Concurrent dead letter increments
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				w.incrementDeadLetter()
			}
		}()
	}

	wg.Wait()

	m := w.Metrics()
	expectedTotal := int64(goroutines * iterations * 3)
	if m.Processed != expectedTotal {
		t.Errorf("Processed = %d, want %d", m.Processed, expectedTotal)
	}
	if m.Succeeded != int64(goroutines*iterations) {
		t.Errorf("Succeeded = %d, want %d", m.Succeeded, goroutines*iterations)
	}
	if m.Failed != int64(goroutines*iterations) {
		t.Errorf("Failed = %d, want %d", m.Failed, goroutines*iterations)
	}
	if m.DeadLetter != int64(goroutines*iterations) {
		t.Errorf("DeadLetter = %d, want %d", m.DeadLetter, goroutines*iterations)
	}
}

func TestWorker_IsRunning(t *testing.T) {
	t.Run("initially not running", func(t *testing.T) {
		w := &Worker{}
		if w.IsRunning() {
			t.Error("IsRunning() = true, want false for new worker")
		}
	})

	t.Run("running when flag is set", func(t *testing.T) {
		w := &Worker{running: true}
		if !w.IsRunning() {
			t.Error("IsRunning() = false, want true when running flag is set")
		}
	})
}

func TestWorkerMetrics_SuccessRate(t *testing.T) {
	tests := []struct {
		name      string
		metrics   WorkerMetrics
		wantRate  float64
		wantValid bool // whether rate is meaningful (processed > 0)
	}{
		{
			name:      "zero processed",
			metrics:   WorkerMetrics{Processed: 0, Succeeded: 0, Failed: 0, DeadLetter: 0},
			wantRate:  0,
			wantValid: false,
		},
		{
			name:      "all success",
			metrics:   WorkerMetrics{Processed: 100, Succeeded: 100, Failed: 0, DeadLetter: 0},
			wantRate:  1.0,
			wantValid: true,
		},
		{
			name:      "all failure",
			metrics:   WorkerMetrics{Processed: 100, Succeeded: 0, Failed: 100, DeadLetter: 0},
			wantRate:  0.0,
			wantValid: true,
		},
		{
			name:      "50% success",
			metrics:   WorkerMetrics{Processed: 100, Succeeded: 50, Failed: 30, DeadLetter: 20},
			wantRate:  0.5,
			wantValid: true,
		},
		{
			name:      "one processed one success",
			metrics:   WorkerMetrics{Processed: 1, Succeeded: 1, Failed: 0, DeadLetter: 0},
			wantRate:  1.0,
			wantValid: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var rate float64
			if tt.metrics.Processed > 0 {
				rate = float64(tt.metrics.Succeeded) / float64(tt.metrics.Processed)
			}
			if tt.wantValid && rate != tt.wantRate {
				t.Errorf("success rate = %v, want %v", rate, tt.wantRate)
			}
			if !tt.wantValid && tt.metrics.Processed != 0 {
				t.Errorf("expected processed to be 0 for invalid case")
			}
		})
	}
}

func TestSyncError(t *testing.T) {
	tests := []struct {
		name    string
		message string
	}{
		{
			name:    "empty message",
			message: "",
		},
		{
			name:    "simple message",
			message: "sync failed",
		},
		{
			name:    "detailed message",
			message: "failed to sync data source: connection timeout after 30s",
		},
		{
			name:    "message with special chars",
			message: "error: file \"data.json\" not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := &syncError{message: tt.message}

			// Test Error() method
			if err.Error() != tt.message {
				t.Errorf("Error() = %q, want %q", err.Error(), tt.message)
			}

			// Verify it implements error interface
			var _ error = err
		})
	}
}
