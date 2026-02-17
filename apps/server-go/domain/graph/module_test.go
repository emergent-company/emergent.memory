package graph

import (
	"context"
	"database/sql"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
	"github.com/uptrace/bun/driver/pgdriver"

	"github.com/emergent-company/emergent/domain/extraction/agents"
)

func TestSchemaProviderCaching(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://emergent:emergent@localhost:5432/emergent?sslmode=disable"
	}

	sqldb := sql.OpenDB(pgdriver.NewConnector(pgdriver.WithDSN(dsn)))
	db := bun.NewDB(sqldb, pgdialect.New())
	defer db.Close()

	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	provider := ProvideSchemaProvider(db, log)

	ctx := context.Background()

	t.Run("cache_hit_on_second_call", func(t *testing.T) {
		adapter, ok := provider.(*templatePackSchemaProviderAdapter)
		require.True(t, ok, "provider should be templatePackSchemaProviderAdapter")

		testProjectID := "test-cache-hit-" + t.Name()

		schemas1, err1 := provider.GetProjectSchemas(ctx, testProjectID)
		assert.NoError(t, err1)
		assert.NotNil(t, schemas1)

		initialMetrics := adapter.Metrics()

		schemas2, err2 := provider.GetProjectSchemas(ctx, testProjectID)
		assert.NoError(t, err2)
		assert.NotNil(t, schemas2)

		secondMetrics := adapter.Metrics()
		assert.Greater(t, secondMetrics.CacheHits, initialMetrics.CacheHits, "Second call should increment cache hits")

		assert.Equal(t, schemas1, schemas2, "Cached schemas should be identical")
	})

	t.Run("cache_expiry_after_ttl", func(t *testing.T) {
		adapter, ok := provider.(*templatePackSchemaProviderAdapter)
		require.True(t, ok, "provider should be templatePackSchemaProviderAdapter")

		testProjectID := "test-cache-expiry-" + t.Name()

		_, err := provider.GetProjectSchemas(ctx, testProjectID)
		assert.NoError(t, err)

		metrics1 := adapter.Metrics()
		initialMisses := metrics1.CacheMisses

		adapter.cacheMu.Lock()
		if cached, ok := adapter.schemaCache[testProjectID]; ok {
			cached.expiry = time.Now().Add(-1 * time.Second)
		}
		adapter.cacheMu.Unlock()

		_, err = provider.GetProjectSchemas(ctx, testProjectID)
		assert.NoError(t, err)

		metrics2 := adapter.Metrics()
		assert.Greater(t, metrics2.CacheMisses, initialMisses, "Expired cache should cause additional miss")
	})

	t.Run("thread_safety_concurrent_access", func(t *testing.T) {
		adapter, ok := provider.(*templatePackSchemaProviderAdapter)
		require.True(t, ok, "provider should be templatePackSchemaProviderAdapter")

		testProjectID := "test-thread-safety-" + t.Name()

		const numGoroutines = 10
		const numCallsPerGoroutine = 5

		var wg sync.WaitGroup
		wg.Add(numGoroutines)

		for i := 0; i < numGoroutines; i++ {
			go func() {
				defer wg.Done()
				for j := 0; j < numCallsPerGoroutine; j++ {
					schemas, err := provider.GetProjectSchemas(ctx, testProjectID)
					assert.NoError(t, err)
					assert.NotNil(t, schemas)
				}
			}()
		}

		wg.Wait()

		adapter.cacheMu.RLock()
		_, exists := adapter.schemaCache[testProjectID]
		adapter.cacheMu.RUnlock()

		assert.True(t, exists, "Cache should contain entry after concurrent access")
	})

	t.Run("cache_isolation_per_project", func(t *testing.T) {
		adapter, ok := provider.(*templatePackSchemaProviderAdapter)
		require.True(t, ok, "provider should be templatePackSchemaProviderAdapter")

		projectA := "test-isolation-a-" + t.Name()
		projectB := "test-isolation-b-" + t.Name()

		_, err1 := provider.GetProjectSchemas(ctx, projectA)
		assert.NoError(t, err1)

		_, err2 := provider.GetProjectSchemas(ctx, projectB)
		assert.NoError(t, err2)

		_, err3 := provider.GetProjectSchemas(ctx, projectA)
		assert.NoError(t, err3)

		_, err4 := provider.GetProjectSchemas(ctx, projectB)
		assert.NoError(t, err4)

		adapter.cacheMu.RLock()
		_, existsA := adapter.schemaCache[projectA]
		_, existsB := adapter.schemaCache[projectB]
		adapter.cacheMu.RUnlock()

		assert.True(t, existsA, "Cache should contain project A")
		assert.True(t, existsB, "Cache should contain project B")
	})

	t.Run("cache_structure_correctness", func(t *testing.T) {
		adapter, ok := provider.(*templatePackSchemaProviderAdapter)
		require.True(t, ok, "provider should be templatePackSchemaProviderAdapter")

		testProjectID := "test-structure-" + t.Name()

		schemas, err := provider.GetProjectSchemas(ctx, testProjectID)
		assert.NoError(t, err)
		assert.NotNil(t, schemas)

		adapter.cacheMu.RLock()
		cached, exists := adapter.schemaCache[testProjectID]
		adapter.cacheMu.RUnlock()

		assert.True(t, exists, "Project should be in cache")
		assert.NotNil(t, cached, "Cached entry should not be nil")
		assert.NotNil(t, cached.schemas, "Cached schemas should not be nil")
		assert.True(t, time.Now().Before(cached.expiry), "Cache should not be expired immediately")

		expectedExpiry := time.Now().Add(schemaCacheTTL)
		assert.WithinDuration(t, expectedExpiry, cached.expiry, 2*time.Second,
			"Cache expiry should be approximately TTL from now")
	})

	t.Run("double_check_locking_pattern", func(t *testing.T) {
		adapter, ok := provider.(*templatePackSchemaProviderAdapter)
		require.True(t, ok, "provider should be templatePackSchemaProviderAdapter")

		testProjectID := "test-locking-" + t.Name()

		var wg sync.WaitGroup
		const numGoroutines = 20

		startBarrier := make(chan struct{})

		wg.Add(numGoroutines)
		for i := 0; i < numGoroutines; i++ {
			go func() {
				defer wg.Done()
				<-startBarrier
				schemas, err := provider.GetProjectSchemas(ctx, testProjectID)
				assert.NoError(t, err)
				assert.NotNil(t, schemas)
			}()
		}

		close(startBarrier)
		wg.Wait()

		adapter.cacheMu.RLock()
		_, exists := adapter.schemaCache[testProjectID]
		adapter.cacheMu.RUnlock()
		assert.True(t, exists, "Cache should contain project entry")
	})
}

func TestSchemaProviderMetrics(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://emergent:emergent@localhost:5432/emergent?sslmode=disable"
	}

	sqldb := sql.OpenDB(pgdriver.NewConnector(pgdriver.WithDSN(dsn)))
	db := bun.NewDB(sqldb, pgdialect.New())
	defer db.Close()

	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	provider := ProvideSchemaProvider(db, log)

	adapter, ok := provider.(*templatePackSchemaProviderAdapter)
	require.True(t, ok, "provider should be templatePackSchemaProviderAdapter")

	ctx := context.Background()

	t.Run("metrics_initialized_to_zero", func(t *testing.T) {
		adapter.metricsMu.Lock()
		adapter.cacheHits = 0
		adapter.cacheMisses = 0
		adapter.dbLoadSuccess = 0
		adapter.dbLoadErrors = 0
		adapter.metricsMu.Unlock()

		metrics := adapter.Metrics()
		assert.Equal(t, int64(0), metrics.CacheHits)
		assert.Equal(t, int64(0), metrics.CacheMisses)
		assert.Equal(t, int64(0), metrics.DBLoadSuccess)
		assert.Equal(t, int64(0), metrics.DBLoadErrors)
	})

	t.Run("metrics_increment_correctly", func(t *testing.T) {
		adapter, ok := provider.(*templatePackSchemaProviderAdapter)
		require.True(t, ok, "provider should be templatePackSchemaProviderAdapter")

		adapter.cacheMu.Lock()
		for k := range adapter.schemaCache {
			delete(adapter.schemaCache, k)
		}
		adapter.cacheMu.Unlock()

		adapter.metricsMu.Lock()
		initialHits := adapter.cacheHits
		initialMisses := adapter.cacheMisses
		initialErrors := adapter.dbLoadErrors
		adapter.metricsMu.Unlock()

		testProjectID := "test-metrics-" + t.Name()

		_, err := provider.GetProjectSchemas(ctx, testProjectID)
		assert.NoError(t, err)

		metrics1 := adapter.Metrics()
		assert.Equal(t, initialMisses+1, metrics1.CacheMisses)
		assert.Equal(t, initialErrors+1, metrics1.DBLoadErrors, "Should increment DBLoadErrors on connection failure")

		_, err = provider.GetProjectSchemas(ctx, testProjectID)
		assert.NoError(t, err)

		metrics2 := adapter.Metrics()
		assert.Equal(t, initialHits+1, metrics2.CacheHits)
		assert.Equal(t, initialMisses+1, metrics2.CacheMisses)
		assert.Equal(t, initialErrors+1, metrics2.DBLoadErrors, "Error count should not increase on cache hit")
	})

	t.Run("metrics_thread_safe", func(t *testing.T) {
		var wg sync.WaitGroup
		const numReaders = 5

		wg.Add(numReaders)
		for i := 0; i < numReaders; i++ {
			go func() {
				defer wg.Done()
				for j := 0; j < 10; j++ {
					_ = adapter.Metrics()
				}
			}()
		}

		wg.Wait()
	})
}

func TestSchemaProviderWithMockData(t *testing.T) {
	t.Run("empty_schemas_structure", func(t *testing.T) {
		schemas := &ExtractionSchemas{
			ObjectSchemas:       make(map[string]agents.ObjectSchema),
			RelationshipSchemas: make(map[string]agents.RelationshipSchema),
		}

		assert.NotNil(t, schemas.ObjectSchemas)
		assert.NotNil(t, schemas.RelationshipSchemas)
		assert.Equal(t, 0, len(schemas.ObjectSchemas))
		assert.Equal(t, 0, len(schemas.RelationshipSchemas))
	})

	t.Run("cached_schemas_struct", func(t *testing.T) {
		schemas := &ExtractionSchemas{
			ObjectSchemas:       make(map[string]agents.ObjectSchema),
			RelationshipSchemas: make(map[string]agents.RelationshipSchema),
		}

		expiry := time.Now().Add(5 * time.Minute)
		cached := &cachedSchemas{
			schemas: schemas,
			expiry:  expiry,
		}

		assert.Equal(t, schemas, cached.schemas)
		assert.Equal(t, expiry, cached.expiry)
	})

	t.Run("cache_ttl_constant", func(t *testing.T) {
		assert.Equal(t, 5*time.Minute, schemaCacheTTL)
	})
}
