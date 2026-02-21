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

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/emergent-company/emergent/pkg/embeddings"
	"github.com/emergent-company/emergent/pkg/embeddings/genai"
	"github.com/emergent-company/emergent/pkg/embeddings/vertex"
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
	flag.StringVar(&projectID, "project-id", "", "Filter to a specific project UUID (optional)")
	flag.Parse()

	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

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
	client, err := newEmbeddingClient(ctx, log)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating embedding client: %v\n", err)
		os.Exit(1)
	}
	log.Info("embedding client initialized")

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

			vec, err := client.EmbedQuery(ctx, tripletText)
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

// newEmbeddingClient creates an embeddings.Client based on environment variables.
func newEmbeddingClient(ctx context.Context, log *slog.Logger) (embeddings.Client, error) {
	model := getEnvDefault("EMBEDDING_MODEL", "gemini-embedding-001")
	gcpProject := os.Getenv("GCP_PROJECT_ID")
	vertexLocation := getEnvDefault("VERTEX_AI_LOCATION", "us-central1")
	googleAPIKey := os.Getenv("GOOGLE_API_KEY")

	if gcpProject != "" && vertexLocation != "" {
		log.Info("using Vertex AI embeddings",
			slog.String("project", gcpProject),
			slog.String("location", vertexLocation),
			slog.String("model", model),
		)
		return vertex.NewClient(ctx, vertex.Config{
			ProjectID: gcpProject,
			Location:  vertexLocation,
			Model:     model,
		}, vertex.WithLogger(log))
	}

	if googleAPIKey != "" {
		log.Info("using Google Generative AI embeddings",
			slog.String("model", model),
		)
		return genai.NewClient(ctx, genai.Config{
			APIKey: googleAPIKey,
			Model:  model,
		}, genai.WithLogger(log))
	}

	return nil, fmt.Errorf("no embedding configuration found: set GCP_PROJECT_ID+VERTEX_AI_LOCATION or GOOGLE_API_KEY")
}

// countNullEmbeddings returns the number of relationships with NULL embeddings.
func countNullEmbeddings(ctx context.Context, db *sql.DB, projectID string) (int64, error) {
	query := `SELECT COUNT(*) FROM kb.graph_relationships WHERE embedding IS NULL AND deleted_at IS NULL`
	args := []any{}

	if projectID != "" {
		query += ` AND project_id = $1`
		args = append(args, projectID)
	}

	var count int64
	if err := db.QueryRowContext(ctx, query, args...).Scan(&count); err != nil {
		return 0, fmt.Errorf("count query: %w", err)
	}
	return count, nil
}

// fetchBatch retrieves the next batch of relationships with NULL embeddings.
// Because we update rows (setting embedding to non-NULL), subsequent queries
// naturally exclude already-processed rows — no OFFSET needed.
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
		ORDER BY r.created_at
		LIMIT $1`

	args := []any{limit}

	if projectID != "" {
		query = `
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
		args = append(args, projectID)
	}

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
