package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"

	"github.com/emergent/emergent-core/domain/graph"
)

func main() {
	var (
		projectID string
		dryRun    bool
		batchSize int
		showHelp  bool
	)

	flag.StringVar(&projectID, "project", "", "Project ID to validate (required)")
	flag.BoolVar(&dryRun, "dry-run", true, "Preview changes without applying (default: true)")
	flag.IntVar(&batchSize, "batch-size", 100, "Number of entities to process in each batch")
	flag.BoolVar(&showHelp, "h", false, "Show help")
	flag.Parse()

	if showHelp {
		printUsage()
		os.Exit(0)
	}

	if projectID == "" {
		fmt.Fprintln(os.Stderr, "Error: -project flag is required")
		printUsage()
		os.Exit(1)
	}

	projectUUID, err := uuid.Parse(projectID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: invalid project ID: %v\n", err)
		os.Exit(1)
	}

	dbURL := buildDatabaseURL()

	sqlDB, err := sql.Open("pgx", dbURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error connecting to database: %v\n", err)
		os.Exit(1)
	}
	defer sqlDB.Close()

	if err := sqlDB.Ping(); err != nil {
		fmt.Fprintf(os.Stderr, "Error pinging database: %v\n", err)
		os.Exit(1)
	}

	db := bun.NewDB(sqlDB, pgdialect.New())
	ctx := context.Background()

	schemaProvider := graph.ProvideSchemaProvider(db, nil)

	schemas, err := schemaProvider.GetProjectSchemas(ctx, projectID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading project schemas: %v\n", err)
		os.Exit(1)
	}

	if len(schemas.ObjectSchemas) == 0 {
		fmt.Println("No schemas found for this project. Nothing to validate.")
		os.Exit(0)
	}

	fmt.Printf("Found %d object schemas for project %s\n", len(schemas.ObjectSchemas), projectID)
	for typeName := range schemas.ObjectSchemas {
		fmt.Printf("  - %s\n", typeName)
	}
	fmt.Println()

	var objects []struct {
		ID         uuid.UUID      `bun:"id"`
		Type       string         `bun:"type"`
		Properties map[string]any `bun:"properties,type:jsonb"`
	}

	err = db.NewSelect().
		TableExpr("kb.graph_objects").
		Column("id", "type", "properties").
		Where("project_id = ?", projectUUID).
		Where("deleted_at IS NULL").
		Order("created_at ASC").
		Limit(batchSize*10).
		Scan(ctx, &objects)

	if err != nil {
		fmt.Fprintf(os.Stderr, "Error fetching objects: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Scanned %d graph objects\n\n", len(objects))

	if dryRun {
		fmt.Println("=== DRY RUN MODE - No changes will be applied ===\n")
	}

	validated := 0
	errors := 0
	unchanged := 0

	for _, obj := range objects {
		schema, hasSchema := schemas.ObjectSchemas[obj.Type]
		if !hasSchema {
			unchanged++
			continue
		}

		originalProps := obj.Properties
		validatedProps, err := graph.ValidateAndCoerceProperties(originalProps, schema)

		if err != nil {
			errors++
			fmt.Printf("[ERROR] Object %s (type: %s): %v\n", obj.ID, obj.Type, err)
			continue
		}

		if !propsEqual(originalProps, validatedProps) {
			validated++
			fmt.Printf("[CHANGED] Object %s (type: %s)\n", obj.ID, obj.Type)
			printPropertyChanges(originalProps, validatedProps)

			if !dryRun {
				_, err = db.NewUpdate().
					Table("kb.graph_objects").
					Set("properties = ?", validatedProps).
					Where("id = ?", obj.ID).
					Exec(ctx)

				if err != nil {
					fmt.Printf("  [UPDATE FAILED] %v\n", err)
					errors++
				} else {
					fmt.Printf("  [UPDATED]\n")
				}
			}
		} else {
			unchanged++
		}
	}

	fmt.Println()
	fmt.Println("=== SUMMARY ===")
	fmt.Printf("Total objects scanned:  %d\n", len(objects))
	fmt.Printf("Properties changed:     %d\n", validated)
	fmt.Printf("Validation errors:      %d\n", errors)
	fmt.Printf("Unchanged:              %d\n", unchanged)

	if dryRun {
		fmt.Println("\nRun with -dry-run=false to apply changes")
	}
}

func buildDatabaseURL() string {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL != "" {
		return dbURL
	}

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

	return fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=%s",
		user, pass, host, port, name, sslMode)
}

func getEnvDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func propsEqual(a, b map[string]any) bool {
	if len(a) != len(b) {
		return false
	}
	for k, va := range a {
		vb, ok := b[k]
		if !ok {
			return false
		}
		if !valueEqual(va, vb) {
			return false
		}
	}
	return true
}

func valueEqual(a, b any) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}

	switch va := a.(type) {
	case string:
		vb, ok := b.(string)
		return ok && va == vb
	case float64:
		vb, ok := b.(float64)
		return ok && va == vb
	case bool:
		vb, ok := b.(bool)
		return ok && va == vb
	case time.Time:
		vb, ok := b.(time.Time)
		return ok && va.Equal(vb)
	default:
		return fmt.Sprintf("%v", a) == fmt.Sprintf("%v", b)
	}
}

func printPropertyChanges(original, validated map[string]any) {
	for key, origVal := range original {
		validVal, ok := validated[key]
		if !ok {
			fmt.Printf("  %s: REMOVED\n", key)
			continue
		}
		if !valueEqual(origVal, validVal) {
			fmt.Printf("  %s: %v (%T) → %v (%T)\n", key, origVal, origVal, validVal, validVal)
		}
	}
	for key, validVal := range validated {
		if _, ok := original[key]; !ok {
			fmt.Printf("  %s: ADDED (%v)\n", key, validVal)
		}
	}
}

func printUsage() {
	fmt.Println(`Graph Property Validation Tool

Scans graph objects and validates/coerces properties based on template pack schemas.

Usage:
  validate-properties -project <uuid> [options]

Flags:
  -project <uuid>     Project ID to validate (required)
  -dry-run            Preview changes without applying (default: true)
  -batch-size <int>   Number of entities per batch (default: 100)
  -h                  Show this help

Environment Variables:
  DATABASE_URL         Full PostgreSQL connection string
  -- or --
  DB_HOST              Database host (default: localhost)
  DB_PORT              Database port (default: 5432)
  POSTGRES_USER        Database user (default: emergent)
  POSTGRES_PASSWORD    Database password (required)
  POSTGRES_DATABASE    Database name (default: emergent)
  DB_SSL_MODE          SSL mode (default: disable)

Examples:
  # Preview changes (dry run)
  ./validate-properties -project 123e4567-e89b-12d3-a456-426614174000

  # Apply changes
  ./validate-properties -project 123e4567-e89b-12d3-a456-426614174000 -dry-run=false

  # Process in smaller batches
  ./validate-properties -project 123e4567-e89b-12d3-a456-426614174000 -batch-size=50`)
}
