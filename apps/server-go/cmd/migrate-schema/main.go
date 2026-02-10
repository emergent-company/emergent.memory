package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"log/slog"
	"os"

	"github.com/emergent/emergent-core/domain/extraction/agents"
	"github.com/emergent/emergent-core/domain/graph"
	"github.com/google/uuid"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
	"github.com/uptrace/bun/driver/pgdriver"
)

func main() {
	projectID := flag.String("project", "", "Project UUID (required)")
	fromVersion := flag.String("from", "", "Source schema version (required)")
	toVersion := flag.String("to", "", "Target schema version (required)")
	dryRun := flag.Bool("dry-run", true, "Dry run mode (default: true)")
	batchSize := flag.Int("batch", 100, "Batch size for processing")
	flag.Parse()

	if *projectID == "" || *fromVersion == "" || *toVersion == "" {
		fmt.Println("Usage: migrate-schema -project <uuid> -from <version> -to <version> [-dry-run=true] [-batch=100]")
		fmt.Println("\nExample:")
		fmt.Println("  migrate-schema -project a1b2c3d4-... -from 1.0.0 -to 2.0.0 -dry-run=false")
		os.Exit(1)
	}

	projectUUID, err := uuid.Parse(*projectID)
	if err != nil {
		fmt.Printf("Error: Invalid project UUID: %v\n", err)
		os.Exit(1)
	}

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dbHost := getEnv("POSTGRES_HOST", "localhost")
		dbPort := getEnv("POSTGRES_PORT", "5432")
		dbUser := getEnv("POSTGRES_USER", "emergent")
		dbPass := getEnv("POSTGRES_PASSWORD", "emergent")
		dbName := getEnv("POSTGRES_DB", "emergent")
		dsn = fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable", dbUser, dbPass, dbHost, dbPort, dbName)
	}

	sqldb := sql.OpenDB(pgdriver.NewConnector(pgdriver.WithDSN(dsn)))
	db := bun.NewDB(sqldb, pgdialect.New())
	defer db.Close()

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	ctx := context.Background()

	fromSchemas, err := loadSchemas(ctx, db, projectUUID, *fromVersion)
	if err != nil {
		logger.Error("Failed to load source schema", slog.String("version", *fromVersion), slog.String("error", err.Error()))
		os.Exit(1)
	}

	toSchemas, err := loadSchemas(ctx, db, projectUUID, *toVersion)
	if err != nil {
		logger.Error("Failed to load target schema", slog.String("version", *toVersion), slog.String("error", err.Error()))
		os.Exit(1)
	}

	validator := graph.NewPropertyValidator()
	migrator := graph.NewSchemaMigrator(validator, logger)

	stats := &MigrationStats{}

	offset := 0
	for {
		var objects []graph.GraphObject
		err := db.NewSelect().
			Model(&objects).
			Where("project_id = ?", projectUUID).
			Where("schema_version = ?", *fromVersion).
			Limit(*batchSize).
			Offset(offset).
			Scan(ctx)

		if err != nil {
			logger.Error("Failed to fetch objects", slog.String("error", err.Error()))
			os.Exit(1)
		}

		if len(objects) == 0 {
			break
		}

		for _, obj := range objects {
			fromSchema := findObjectSchema(fromSchemas, obj.Type)
			toSchema := findObjectSchema(toSchemas, obj.Type)

			if fromSchema == nil || toSchema == nil {
				logger.Warn("Schema not found for object type",
					slog.String("type", obj.Type),
					slog.String("object_id", obj.ID.String()))
				stats.Skipped++
				continue
			}

			result := migrator.MigrateObject(ctx, &obj, fromSchema, toSchema, *fromVersion, *toVersion)

			stats.Total++

			if result.Success {
				stats.Successful++

				if !*dryRun {
					_, err := db.NewUpdate().
						Model(&obj).
						Set("properties = ?", result.NewProperties).
						Set("schema_version = ?", *toVersion).
						Where("id = ?", obj.ID).
						Exec(ctx)

					if err != nil {
						logger.Error("Failed to update object",
							slog.String("object_id", obj.ID.String()),
							slog.String("error", err.Error()))
						stats.Failed++
					}
				}

				if len(result.Issues) > 0 {
					stats.WithWarnings++
					for _, issue := range result.Issues {
						if issue.Severity == "warning" {
							logger.Warn("Migration warning",
								slog.String("object_id", obj.ID.String()),
								slog.String("field", issue.Field),
								slog.String("type", string(issue.Type)),
								slog.String("suggestion", issue.Suggestion))
						}
					}
				}
			} else {
				stats.Failed++
				logger.Error("Migration failed",
					slog.String("object_id", obj.ID.String()),
					slog.String("type", obj.Type))

				for _, issue := range result.Issues {
					if issue.Severity == "error" {
						logger.Error("Migration error",
							slog.String("field", issue.Field),
							slog.String("type", string(issue.Type)),
							slog.String("description", issue.Description),
							slog.String("suggestion", issue.Suggestion))
					}
				}
			}
		}

		offset += *batchSize
		logger.Info("Batch processed",
			slog.Int("batch_size", len(objects)),
			slog.Int("total_processed", offset))
	}

	printSummary(stats, *dryRun)

	if stats.Failed > 0 {
		os.Exit(1)
	}
}

type MigrationStats struct {
	Total        int
	Successful   int
	Failed       int
	Skipped      int
	WithWarnings int
}

func printSummary(stats *MigrationStats, dryRun bool) {
	fmt.Println("\n=== Migration Summary ===")
	if dryRun {
		fmt.Println("Mode: DRY RUN (no changes applied)")
	} else {
		fmt.Println("Mode: LIVE (changes applied)")
	}
	fmt.Printf("Total objects:     %d\n", stats.Total)
	fmt.Printf("Successful:        %d\n", stats.Successful)
	fmt.Printf("Failed:            %d\n", stats.Failed)
	fmt.Printf("Skipped:           %d\n", stats.Skipped)
	fmt.Printf("With warnings:     %d\n", stats.WithWarnings)
	fmt.Println("========================")
}

func loadSchemas(ctx context.Context, db bun.IDB, projectID uuid.UUID, version string) (*graph.ExtractionSchemas, error) {
	type GraphTemplatePack struct {
		bun.BaseModel           `bun:"kb.graph_template_packs,alias:gtp"`
		ID                      string         `bun:"id,pk,type:uuid"`
		Name                    string         `bun:"name,notnull"`
		Version                 string         `bun:"version,notnull"`
		ObjectTypeSchemas       map[string]any `bun:"object_type_schemas,type:jsonb,notnull"`
		RelationshipTypeSchemas map[string]any `bun:"relationship_type_schemas,type:jsonb,default:'{}'"`
	}

	type ProjectTemplatePack struct {
		bun.BaseModel  `bun:"kb.project_template_packs,alias:ptp"`
		ProjectID      uuid.UUID          `bun:"project_id,notnull,type:uuid"`
		TemplatePackID string             `bun:"template_pack_id,notnull,type:uuid"`
		Active         bool               `bun:"active,default:true"`
		TemplatePack   *GraphTemplatePack `bun:"rel:belongs-to,join:template_pack_id=id"`
	}

	var assignments []ProjectTemplatePack
	err := db.NewSelect().
		Model(&assignments).
		Relation("TemplatePack").
		Where("ptp.project_id = ?", projectID).
		Where("ptp.active = true").
		Where("gtp.version = ?", version).
		Scan(ctx)

	if err != nil {
		return nil, fmt.Errorf("failed to load schemas: %w", err)
	}

	if len(assignments) == 0 {
		return nil, fmt.Errorf("no active template pack found for version %s", version)
	}

	objectSchemas := make(map[string]agents.ObjectSchema)
	relationshipSchemas := make(map[string]agents.RelationshipSchema)

	for _, assignment := range assignments {
		if assignment.TemplatePack == nil {
			continue
		}

		pack := assignment.TemplatePack

		for typeName, schemaRaw := range pack.ObjectTypeSchemas {
			schemaMap, ok := schemaRaw.(map[string]any)
			if !ok {
				continue
			}

			schema := agents.ObjectSchema{Name: typeName}

			if desc, ok := schemaMap["description"].(string); ok {
				schema.Description = desc
			}

			if props, ok := schemaMap["properties"].(map[string]any); ok {
				schema.Properties = make(map[string]agents.PropertyDef)
				for propName, propRaw := range props {
					propMap, ok := propRaw.(map[string]any)
					if !ok {
						continue
					}
					propDef := agents.PropertyDef{}
					if t, ok := propMap["type"].(string); ok {
						propDef.Type = t
					}
					if d, ok := propMap["description"].(string); ok {
						propDef.Description = d
					}
					schema.Properties[propName] = propDef
				}
			}

			if req, ok := schemaMap["required"].([]any); ok {
				for _, r := range req {
					if s, ok := r.(string); ok {
						schema.Required = append(schema.Required, s)
					}
				}
			}

			objectSchemas[typeName] = schema
		}
	}

	return &graph.ExtractionSchemas{
		ObjectSchemas:       objectSchemas,
		RelationshipSchemas: relationshipSchemas,
	}, nil
}

func findObjectSchema(schemas *graph.ExtractionSchemas, typeName string) *agents.ObjectSchema {
	if schema, ok := schemas.ObjectSchemas[typeName]; ok {
		return &schema
	}
	return nil
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
