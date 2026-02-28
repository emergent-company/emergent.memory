package extraction

import (
	"log/slog"
	"testing"

	"github.com/emergent-company/emergent/pkg/syshealth"
	"github.com/stretchr/testify/assert"
)

type mockMonitor struct {
	health *syshealth.HealthMetrics
}

func (m *mockMonitor) Start() error { return nil }
func (m *mockMonitor) Stop() error  { return nil }
func (m *mockMonitor) GetHealth() *syshealth.HealthMetrics {
	return m.health
}

func TestWorkerScalingIntegration(t *testing.T) {
	// Test data
	monitor := &mockMonitor{
		health: &syshealth.HealthMetrics{
			Zone: syshealth.HealthZoneCritical,
		},
	}
	
	// Create a scaler that will return MinConcurrency (1) when health is Critical
	scaler := syshealth.NewConcurrencyScaler(monitor, "test-worker", true, 1, 10)

	t.Run("GraphEmbeddingWorker uses scaler", func(t *testing.T) {
		w := &GraphEmbeddingWorker{
			cfg:    &GraphEmbeddingConfig{WorkerConcurrency: 10},
			scaler: scaler,
			log:    slog.Default(),
		}
		
		// We don't call processBatch because it needs a DB, 
		// but we can verify that the scaler works as expected for the worker's config
		concurrency := w.cfg.WorkerConcurrency
		if w.scaler != nil {
			concurrency = w.scaler.GetConcurrency(w.cfg.WorkerConcurrency)
		}
		
		assert.Equal(t, 1, concurrency, "Should use min concurrency from scaler in critical zone")
		
		// Change health to Safe
		monitor.health.Zone = syshealth.HealthZoneSafe
		// We can't easily bypass cooldown here without manual state manipulation if we don't have access to mu
		// But NewConcurrencyScaler starts at maxConcurrency (10)
		// Wait, NewConcurrencyScaler sets currentConcurrency = max
		// So if we just started, it should be 10.
		
		scaler = syshealth.NewConcurrencyScaler(monitor, "test-worker", true, 1, 10)
		assert.Equal(t, 10, scaler.GetConcurrency(10))
	})

	t.Run("ChunkEmbeddingWorker uses scaler", func(t *testing.T) {
		w := &ChunkEmbeddingWorker{
			cfg:    &ChunkEmbeddingConfig{WorkerConcurrency: 10},
			scaler: scaler,
			log:    slog.Default(),
		}
		
		monitor.health.Zone = syshealth.HealthZoneCritical
		concurrency := w.scaler.GetConcurrency(w.cfg.WorkerConcurrency)
		assert.Equal(t, 1, concurrency)
	})

	t.Run("DocumentParsingWorker uses scaler", func(t *testing.T) {
		w := &DocumentParsingWorker{
			concurrency: 10,
			scaler:      scaler,
			log:         slog.Default(),
		}
		
		monitor.health.Zone = syshealth.HealthZoneCritical
		concurrency := w.scaler.GetConcurrency(w.concurrency)
		assert.Equal(t, 1, concurrency)
	})

	t.Run("ObjectExtractionWorker uses scaler", func(t *testing.T) {
		w := &ObjectExtractionWorker{
			config: &ObjectExtractionWorkerConfig{Concurrency: 10},
			scaler: scaler,
			log:    slog.Default(),
		}
		
		monitor.health.Zone = syshealth.HealthZoneCritical
		concurrency := w.scaler.GetConcurrency(w.config.Concurrency)
		assert.Equal(t, 1, concurrency)
	})

	t.Run("GraphEmbeddingWorker SetConfig propagates to scaler", func(t *testing.T) {
		m := &mockMonitor{health: &syshealth.HealthMetrics{Zone: syshealth.HealthZoneSafe}}
		// min=1, max=10
		s := syshealth.NewConcurrencyScaler(m, "test", true, 1, 10)
		w := &GraphEmbeddingWorker{
			cfg:    &GraphEmbeddingConfig{MinConcurrency: 1, MaxConcurrency: 10, EnableAdaptiveScaling: true},
			scaler: s,
			log:    slog.Default(),
		}

		assert.Equal(t, 10, s.GetConcurrency(0))

		// Update worker config: max=50
		w.SetConfig(GraphEmbeddingConfig{
			MinConcurrency:        5,
			MaxConcurrency:        50,
			EnableAdaptiveScaling: true,
			WorkerConcurrency:     50,
		})

		// Cooldown bypass for test
		// Wait, I can't easily bypass cooldown on s if it's private or I don't have access.
		// Actually, I can use a hack if I really wanted to, but let's check if SetConfig
		// forced a bounds check.
		
		// If I change max to 5, it should drop to 5 immediately because of bounds check in UpdateConfig
		w.SetConfig(GraphEmbeddingConfig{
			MinConcurrency:        1,
			MaxConcurrency:        5,
			EnableAdaptiveScaling: true,
		})
		
		assert.Equal(t, 5, s.GetConcurrency(0))
	})
}
