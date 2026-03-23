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

	"github.com/emergent-company/emergent.memory/domain/extraction/agents"
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
		adapter, ok := provider.(*schemaProviderAdapter)
		require.True(t, ok, "provider should be schemaProviderAdapter")

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
		adapter, ok := provider.(*schemaProviderAdapter)
		require.True(t, ok, "provider should be schemaProviderAdapter")

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
		adapter, ok := provider.(*schemaProviderAdapter)
		require.True(t, ok, "provider should be schemaProviderAdapter")

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
		adapter, ok := provider.(*schemaProviderAdapter)
		require.True(t, ok, "provider should be schemaProviderAdapter")

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
		adapter, ok := provider.(*schemaProviderAdapter)
		require.True(t, ok, "provider should be schemaProviderAdapter")

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
		adapter, ok := provider.(*schemaProviderAdapter)
		require.True(t, ok, "provider should be schemaProviderAdapter")

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

	adapter, ok := provider.(*schemaProviderAdapter)
	require.True(t, ok, "provider should be schemaProviderAdapter")

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
		adapter, ok := provider.(*schemaProviderAdapter)
		require.True(t, ok, "provider should be schemaProviderAdapter")

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

// TestSchemaProviderExcludesRemovedSchemas is a regression test for issue #111:
// object_type_not_allowed for all types after schema reinstall.
//
// Root cause: GetProjectSchemas was missing "removed_at IS NULL" filter, so
// reinstalled schemas (which set removed_at on the old row) caused stale removed
// rows to be returned, leaving the type map empty.
func TestSchemaProviderExcludesRemovedSchemas(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://emergent:local-test-password@127.0.0.1:5436/emergent?sslmode=disable"
	}

	sqldb := sql.OpenDB(pgdriver.NewConnector(pgdriver.WithDSN(dsn)))
	db := bun.NewDB(sqldb, pgdialect.New())
	defer db.Close()

	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

	// Use stable UUIDs for test isolation
	orgID := "d0000000-0000-0000-0000-000000000001"
	projectID := "a0000000-0000-0000-0000-000000000001"
	schemaIDOld := "b0000000-0000-0000-0000-000000000001"
	schemaIDNew := "b0000000-0000-0000-0000-000000000002"
	assignmentIDOld := "c0000000-0000-0000-0000-000000000001"
	assignmentIDNew := "c0000000-0000-0000-0000-000000000002"

	objectTypeSchemasOld := `{"OldType":{"label":"Old Type","description":"Should not appear"}}`
	objectTypeSchemasNew := `{"NewType":{"label":"New Type","description":"Should appear"}}`

	// Seed org and project to satisfy FK constraints (ON CONFLICT DO NOTHING for idempotency)
	_, err := db.NewRaw(`
		INSERT INTO kb.orgs (id, name, created_at, updated_at)
		VALUES (?::uuid, 'test-org-regression-111', NOW(), NOW())
		ON CONFLICT (id) DO NOTHING
	`, orgID).Exec(ctx)
	require.NoError(t, err, "seeding org")

	_, err = db.NewRaw(`
		INSERT INTO kb.projects (id, organization_id, name, created_at, updated_at)
		VALUES (?::uuid, ?::uuid, 'test-project-regression-111', NOW(), NOW())
		ON CONFLICT (id) DO NOTHING
	`, projectID, orgID).Exec(ctx)
	require.NoError(t, err, "seeding project")

	// Seed: two graph_schemas rows (old version, new version)
	_, err = db.NewRaw(`
		INSERT INTO kb.graph_schemas (id, name, version, object_type_schemas, relationship_type_schemas, visibility, draft, created_at, updated_at)
		VALUES
			(?::uuid, 'test-schema-regression-111', '1.0.0', ?::jsonb, '{}'::jsonb, 'project', false, NOW(), NOW()),
			(?::uuid, 'test-schema-regression-111', '2.0.0', ?::jsonb, '{}'::jsonb, 'project', false, NOW(), NOW())
		ON CONFLICT (id) DO UPDATE SET
			object_type_schemas = EXCLUDED.object_type_schemas,
			relationship_type_schemas = EXCLUDED.relationship_type_schemas,
			updated_at = NOW()
	`, schemaIDOld, objectTypeSchemasOld, schemaIDNew, objectTypeSchemasNew).Exec(ctx)
	require.NoError(t, err, "seeding graph_schemas")

	now := time.Now()
	removedAt := now.Add(-1 * time.Minute)

	// Seed: old assignment with removed_at set (simulates schema reinstall),
	// new assignment active with no removed_at.
	_, err = db.NewRaw(`
		INSERT INTO kb.project_schemas (id, project_id, schema_id, active, installed_at, removed_at, created_at, updated_at)
		VALUES
			(?::uuid, ?::uuid, ?::uuid, true, ?, ?, NOW(), NOW()),
			(?::uuid, ?::uuid, ?::uuid, true, ?, NULL, NOW(), NOW())
		ON CONFLICT (id) DO UPDATE SET
			removed_at = EXCLUDED.removed_at,
			updated_at = NOW()
	`, assignmentIDOld, projectID, schemaIDOld, removedAt, removedAt,
		assignmentIDNew, projectID, schemaIDNew, now).Exec(ctx)
	require.NoError(t, err, "seeding project_schemas")

	// Cleanup in reverse FK order
	t.Cleanup(func() {
		_, _ = db.NewRaw(`DELETE FROM kb.project_schemas WHERE id IN (?::uuid, ?::uuid)`, assignmentIDOld, assignmentIDNew).Exec(ctx)
		_, _ = db.NewRaw(`DELETE FROM kb.graph_schemas WHERE id IN (?::uuid, ?::uuid)`, schemaIDOld, schemaIDNew).Exec(ctx)
		_, _ = db.NewRaw(`DELETE FROM kb.projects WHERE id = ?::uuid`, projectID).Exec(ctx)
		_, _ = db.NewRaw(`DELETE FROM kb.orgs WHERE id = ?::uuid`, orgID).Exec(ctx)
	})

	provider := ProvideSchemaProvider(db, log)
	schemas, err := provider.GetProjectSchemas(ctx, projectID)
	require.NoError(t, err)
	require.NotNil(t, schemas)

	// Should contain NewType from the active (non-removed) schema
	assert.Contains(t, schemas.ObjectSchemas, "NewType",
		"active schema type should be present")

	// Must NOT contain OldType from the removed schema
	assert.NotContains(t, schemas.ObjectSchemas, "OldType",
		"removed schema type must not appear (regression: issue #111)")
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
