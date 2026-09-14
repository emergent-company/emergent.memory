package testutil

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
	"github.com/uptrace/bun/driver/pgdriver"
)

// DB wraps a bun.DB connection for testing.
type DB struct {
	*bun.DB
}

// ConnectDB creates a database connection from config.
func ConnectDB(cfg *Config) (*DB, error) {
	sqldb := sql.OpenDB(pgdriver.NewConnector(pgdriver.WithDSN(cfg.PostgresDSN())))
	db := bun.NewDB(sqldb, pgdialect.New())

	// Verify connection
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	return &DB{DB: db}, nil
}

// Close closes the database connection.
func (db *DB) Close() error {
	return db.DB.Close()
}

// TruncateTables truncates all test-related tables.
// This is used between test suites to ensure clean state.
func (db *DB) TruncateTables(ctx context.Context) error {
	tables := []string{
		// KB schema tables (order matters due to foreign keys)
		"kb.chunks",
		"kb.documents",
		"kb.graph_objects",
		"kb.graph_relationships",
		"kb.chat_messages",
		"kb.chat_conversations",
		"kb.project_memberships",
		"kb.organization_memberships",
		"kb.projects",
		"kb.orgs",
		"kb.object_extraction_jobs",
		"kb.document_parsing_jobs",
		"kb.chunk_embedding_jobs",
		"kb.graph_embedding_jobs",
		// Core schema tables
		"core.api_tokens",
		"core.user_emails",
		"core.user_profiles",
	}

	for _, table := range tables {
		_, err := db.NewRaw("TRUNCATE TABLE ? CASCADE", bun.Ident(table)).Exec(ctx)
		if err != nil {
			// Some tables might not exist, continue
			continue
		}
	}

	return nil
}

// CleanupTestData removes test data by prefix pattern.
// Useful for cleaning up data created during tests without full truncation.
func (db *DB) CleanupTestData(ctx context.Context, prefix string) error {
	// Delete documents with test prefix
	_, _ = db.NewRaw("DELETE FROM kb.documents WHERE filename LIKE ?", prefix+"%").Exec(ctx)
	_, _ = db.NewRaw("DELETE FROM kb.projects WHERE name LIKE ?", prefix+"%").Exec(ctx)
	_, _ = db.NewRaw("DELETE FROM kb.orgs WHERE name LIKE ?", prefix+"%").Exec(ctx)
	return nil
}
