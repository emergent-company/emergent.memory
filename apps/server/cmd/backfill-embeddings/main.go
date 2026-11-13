package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"

	"github.com/emergent-company/emergent.memory/domain/modelconfig"
	"github.com/emergent-company/emergent.memory/domain/provider"
	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/pkg/auth"
	"github.com/emergent-company/emergent.memory/pkg/embeddings"
)

// relationshipRow holds the fields scanned from the backfill query.
type relationshipRow struct {
	ID            string
	Type          string
	SrcID         string
	DstID         string
	SrcProperties []byte // JSONB → []byte from database/sql
	SrcKey        *string
	DstProperties []byte
	DstKey        *string
}

func main() {
	var (
		batchSize int
		delayMs   int
		dryRun    bool
		projectID string
	)

	flag.IntVar(&batchSize, "batch-size", 100, "Number of relationships per batch")
	flag.IntVar(&delayMs, "delay", 100, "Milliseconds to sleep between batches")
	flag.BoolVar(&dryRun, "dry-run", false, "Print what would be done without writing to DB")
	flag.StringVar(&projectID, "project-id", "", "Project UUID whose configured embedding provider is used (required)")
	flag.Parse()

	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	if projectID == "" {
		fmt.Fprintln(os.Stderr, "Error: --project-id is required — embeddings are resolved from the project's configured provider")
		os.Exit(2)
	}
	if _, err := uuid.Parse(projectID); err != nil {
		fmt.Fprintf(os.Stderr, "Error: invalid --project-id %q: %v\n", projectID, err)
		os.Exit(2)
	}

	if dryRun {
		log.Info("DRY RUN mode enabled — no database writes will occur")
	}

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		host := getEnvDefault("DB_HOST", "localhost")
		port := getEnvDefault("DB_PORT", "5432")
		user := getEnvDefault("POSTGRES_USER", "emergent")
		pass := os.Getenv("POSTGRES_PASSWORD")
		name := getEnvDefault("POSTGRES_DATABASE", "emergent")
		sslMode := getEnvDefault("DB_SSL_MODE", "disable")

		if pass == "" {
			fmt.Fprintln(os.Stderr, "Error: POSTGRES_PASSWORD or DATABASE_URL must be set")
			os.Exit(1)
		}

		dbURL = fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=%s",
			user, pass, host, port, name, sslMode)
	}

	db, err := sql.Open("pgx", dbURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error connecting to database: %v\n", err)
		os.Exit(1)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		fmt.Fprintf(os.Stderr, "Error pinging database: %v\n", err)
		os.Exit(1)
	}
	log.Info("connected to database")

	ctx := context.Background()
	embedder, model, err := newEmbeddingService(ctx, db, log, projectID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating embedding service: %v\n", err)
		os.Exit(1)
	}
	log.Info("embedding provider resolved from the project",
		slog.String("project_id", projectID),
		slog.String("model", model),
	)

	// Embeddings resolve from the project in context, exactly as the server does.
	projectCtx := auth.ContextWithProjectID(ctx, projectID)

	total, err := countNullEmbeddings(ctx, db, projectID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error counting relationships: %v\n", err)
		os.Exit(1)
	}
	if total == 0 {
		log.Info("no relationships with NULL embeddings found — nothing to do")
		return
	}
	log.Info("starting backfill", slog.Int64("total", total), slog.Int("batch_size", batchSize))

	var processed, embedded, errCount, skipped int64

	for {
		rows, err := fetchBatch(ctx, db, batchSize, projectID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error fetching batch: %v\n", err)
			os.Exit(1)
		}
		if len(rows) == 0 {
			break // No more rows with NULL embeddings
		}

		for _, row := range rows {
			processed++

			srcProps := parseProperties(row.SrcProperties)
			dstProps := parseProperties(row.DstProperties)

			srcName := getDisplayName(srcProps, row.SrcKey, row.SrcID)
			dstName := getDisplayName(dstProps, row.DstKey, row.DstID)
			tripletText := generateTripletText(srcName, dstName, row.Type)

			if dryRun {
				log.Info("would embed",
					slog.String("id", row.ID),
					slog.String("triplet", tripletText),
				)
				continue
			}

			// Document path, not EmbedQuery: triplet text is stored corpus
			// text, and a query-format vector here would be incomparable with
			// the document-format vectors search compares against (#615).
			vecs, err := embedder.EmbedDocuments(projectCtx, []string{tripletText})
			var vec []float32
			if err == nil {
				if len(vecs) == 0 {
					err = fmt.Errorf("no embedding returned")
				} else {
					vec = vecs[0]
				}
			}
			if err != nil {
				errCount++
				log.Warn("failed to embed relationship",
					slog.String("id", row.ID),
					slog.String("triplet", tripletText),
					slog.String("error", err.Error()),
				)
				continue
			}

			if vec == nil || len(vec) == 0 {
				skipped++
				log.Warn("embedding returned nil",
					slog.String("id", row.ID),
					slog.String("triplet", tripletText),
				)
				continue
			}

			if err := updateEmbedding(ctx, db, row.ID, vec); err != nil {
				errCount++
				log.Warn("failed to update embedding",
					slog.String("id", row.ID),
					slog.String("error", err.Error()),
				)
				continue
			}

			embedded++
		}

		log.Info("progress",
			slog.Int64("processed", processed),
			slog.Int64("total", total),
			slog.Int64("embedded", embedded),
			slog.Int64("errors", errCount),
		)

		// Sleep between batches to avoid hammering the embedding API
		if delayMs > 0 && !dryRun {
			time.Sleep(time.Duration(delayMs) * time.Millisecond)
		}
	}

	log.Info("backfill complete",
		slog.Int64("processed", processed),
		slog.Int64("embedded", embedded),
		slog.Int64("errors", errCount),
		slog.Int64("skipped", skipped),
	)
}

// newEmbeddingService builds an embeddings service that resolves the project's
// configured provider and model through the same code path the server uses.
// There is no environment-provided embedding provider: GCP_PROJECT_ID,
// GOOGLE_API_KEY and EMBEDDING_MODEL are not consulted. The project must have an
// embedding model configured (via 'memory projects set-models --embedding …' or
// 'memory provider configure-project … --embedding-model …').
func newEmbeddingService(ctx context.Context, sqlDB *sql.DB, log *slog.Logger, projectID string) (embeddings.Client, string, error) {
	cfg, err := config.NewConfig(log)
	if err != nil {
		return nil, "", fmt.Errorf("load config: %w", err)
	}

	db := bun.NewDB(sqlDB, pgdialect.New())

	repo := provider.NewRepository(db, log)
	catalog := provider.NewModelCatalogService(repo, log, provider.WithNetworkDisabled(true))
	credSvc := provider.NewCredentialService(repo, provider.NewRegistry(), catalog, cfg, log)
	modelSvc := modelconfig.NewService(modelconfig.NewStore(db, log), log).
		WithEmbeddingDefaultResolver(credSvc)

	projectUUID, err := uuid.Parse(projectID)
	if err != nil {
		return nil, "", fmt.Errorf("invalid project id %q: %w", projectID, err)
	}
	model, source, err := modelSvc.ResolveEmbeddingModel(auth.ContextWithProjectID(ctx, projectID), projectUUID)
	if err != nil {
		return nil, "", fmt.Errorf("resolve embedding model for project %s: %w", projectID, err)
	}
	if model == "" {
		return nil, "", fmt.Errorf("embedding model was not set for project %s — run 'memory projects set-models --embedding <provider>/<model>' or 'memory provider configure-project <provider> --embedding-model <model>'", projectID)
	}
	log.Info("resolved embedding model from the project",
		slog.String("model", model),
		slog.String("source", string(source)),
	)

	svc := embeddings.NewResolverService(modelconfig.NewEmbeddingResolverAdapter(modelSvc, credSvc), log)
	return svc, model, nil
}

// countNullEmbeddings returns the number of relationships with NULL embeddings
// in the project. The project is mandatory: embeddings are resolved from the
// project's own provider, so a cross-project pass could not embed anything.
func countNullEmbeddings(ctx context.Context, db *sql.DB, projectID string) (int64, error) {
	query := `SELECT COUNT(*) FROM kb.graph_relationships WHERE embedding IS NULL AND deleted_at IS NULL AND project_id = $1`

	var count int64
	if err := db.QueryRowContext(ctx, query, projectID).Scan(&count); err != nil {
		return 0, fmt.Errorf("count query: %w", err)
	}
	return count, nil
}

// fetchBatch retrieves the next batch of relationships with NULL embeddings in
// the project. Because we update rows (setting embedding to non-NULL),
// subsequent queries naturally exclude already-processed rows — no OFFSET needed.
func fetchBatch(ctx context.Context, db *sql.DB, limit int, projectID string) ([]relationshipRow, error) {
	query := `
		SELECT r.id, r.type, r.src_id::text, r.dst_id::text,
		       src.properties, src.key,
		       dst.properties, dst.key
		FROM kb.graph_relationships r
		JOIN kb.graph_objects src ON src.id = r.src_id
		JOIN kb.graph_objects dst ON dst.id = r.dst_id
		WHERE r.embedding IS NULL
		  AND r.deleted_at IS NULL
		  AND src.deleted_at IS NULL
		  AND dst.deleted_at IS NULL
		  AND r.project_id = $2
		ORDER BY r.created_at
		LIMIT $1`

	args := []any{limit, projectID}

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("batch query: %w", err)
	}
	defer rows.Close()

	var result []relationshipRow
	for rows.Next() {
		var r relationshipRow
		if err := rows.Scan(
			&r.ID, &r.Type, &r.SrcID, &r.DstID,
			&r.SrcProperties, &r.SrcKey,
			&r.DstProperties, &r.DstKey,
		); err != nil {
			return nil, fmt.Errorf("scan row: %w", err)
		}
		result = append(result, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration: %w", err)
	}
	return result, nil
}

// updateEmbedding writes the embedding vector and timestamp to a relationship row.
func updateEmbedding(ctx context.Context, db *sql.DB, id string, vec []float32) error {
	_, err := db.ExecContext(ctx,
		`UPDATE kb.graph_relationships SET embedding = $1::vector, embedding_updated_at = $2 WHERE id = $3`,
		vectorToString(vec), time.Now(), id,
	)
	if err != nil {
		return fmt.Errorf("update embedding: %w", err)
	}
	return nil
}

// Triplet text helpers — duplicated from domain/graph/service.go (unexported there).

// humanizeRelationType converts SCREAMING_SNAKE_CASE to lower case with spaces.
func humanizeRelationType(relType string) string {
	return strings.ToLower(strings.ReplaceAll(relType, "_", " "))
}

// getDisplayName picks the best display name from object properties/key/id.
func getDisplayName(properties map[string]any, key *string, id string) string {
	if properties != nil {
		if name, ok := properties["name"].(string); ok && name != "" {
			return name
		}
	}
	if key != nil && *key != "" {
		return *key
	}
	return id
}

// generateTripletText creates a natural language triplet string.
func generateTripletText(sourceName, targetName, relType string) string {
	return fmt.Sprintf("%s %s %s", sourceName, humanizeRelationType(relType), targetName)
}

// vectorToString converts a float32 slice to pgvector string format.
func vectorToString(v []float32) string {
	if len(v) == 0 {
		return "[]"
	}
	result := "["
	for i, val := range v {
		if i > 0 {
			result += ","
		}
		result += fmt.Sprintf("%f", val)
	}
	result += "]"
	return result
}

// parseProperties unmarshals JSONB bytes into a map.
func parseProperties(data []byte) map[string]any {
	if len(data) == 0 {
		return nil
	}
	var props map[string]any
	if err := json.Unmarshal(data, &props); err != nil {
		return nil
	}
	return props
}

// getEnvDefault returns the environment variable value or a default.
func getEnvDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
