package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"log/slog"
	"os"

	"github.com/emergent-company/emergent/domain/extraction/agents"
	"github.com/emergent-company/emergent/domain/graph"
	"github.com/google/uuid"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
	"github.com/uptrace/bun/driver/pgdriver"
)

func main() {
	projectID := flag.String("project", "", "Project UUID (required)")
	fromVersion := flag.String("from", "", "Source schema version (required for migration)")
	toVersion := flag.String("to", "", "Target schema version (required for migration)")
	dryRun := flag.Bool("dry-run", true, "Dry run mode (default: true)")
	batchSize := flag.Int("batch", 100, "Batch size for processing")
	force := flag.Bool("force", false, "Allow risky migrations (1-2 fields dropped)")
	confirmDataLoss := flag.Bool("confirm-data-loss", false, "Allow dangerous migrations (3+ fields dropped)")
	skipArchive := flag.Bool("skip-archive", false, "Skip archiving dropped fields (NOT RECOMMENDED)")
	rollback := flag.Bool("rollback", false, "Rollback mode - restore objects from migration archive")
	rollbackTargetVersion := flag.String("rollback-version", "", "Target version to rollback (archive entry with to_version = this)")
	flag.Parse()

	if *projectID == "" {
		printUsage()
		os.Exit(1)
	}

	if *rollback {
		if *rollbackTargetVersion == "" {
			fmt.Println("Error: --rollback-version is required for rollback mode")
			printUsage()
			os.Exit(1)
		}
		runRollback(*projectID, *rollbackTargetVersion, *dryRun, *batchSize)
		return
	}

	if *fromVersion == "" || *toVersion == "" {
		fmt.Println("Error: -from and -to are required for migration mode")
		printUsage()
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

			if !result.CanProceed {
				handleBlockedMigration(logger, stats, result, *force, *confirmDataLoss)
				continue
			}

			if result.Success {
				stats.Successful++

				switch result.RiskLevel {
				case graph.RiskLevelSafe:
					stats.RiskySafe++
				case graph.RiskLevelCautious:
					stats.RiskyCautious++
				case graph.RiskLevelRisky:
					stats.RiskyRisky++
				case graph.RiskLevelDangerous:
					stats.RiskyDangerous++
				}

				if !*dryRun {
					updateData := map[string]any{
						"properties":     result.NewProperties,
						"schema_version": *toVersion,
					}

					if len(result.DroppedProps) > 0 && !*skipArchive {
						updateData["migration_archive"] = obj.MigrationArchive
					}

					_, err := db.NewUpdate().
						Model(&obj).
						Set("properties = ?", result.NewProperties).
						Set("schema_version = ?", *toVersion).
						Set("migration_archive = ?", obj.MigrationArchive).
						Where("id = ?", obj.ID).
						Exec(ctx)

					if err != nil {
						logger.Error("Failed to update object",
							slog.String("object_id", obj.ID.String()),
							slog.String("error", err.Error()))
						stats.Failed++
					}
				}

				logMigrationResult(logger, result, &obj)
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
	Total          int
	Successful     int
	Failed         int
	Skipped        int
	Blocked        int
	WithWarnings   int
	RiskySafe      int
	RiskyCautious  int
	RiskyRisky     int
	RiskyDangerous int
}

func handleBlockedMigration(logger *slog.Logger, stats *MigrationStats, result *graph.MigrationResult, force bool, confirmDataLoss bool) {
	stats.Blocked++

	switch result.RiskLevel {
	case graph.RiskLevelRisky:
		stats.RiskyRisky++
		if !force {
			logger.Warn("Migration blocked - risky",
				slog.String("object_id", result.ObjectID.String()),
				slog.String("risk_level", string(result.RiskLevel)),
				slog.String("reason", result.BlockReason),
				slog.Int("dropped_fields", len(result.DroppedProps)))
			return
		}
	case graph.RiskLevelDangerous:
		stats.RiskyDangerous++
		if !force || !confirmDataLoss {
			logger.Error("Migration blocked - dangerous",
				slog.String("object_id", result.ObjectID.String()),
				slog.String("risk_level", string(result.RiskLevel)),
				slog.String("reason", result.BlockReason),
				slog.Int("dropped_fields", len(result.DroppedProps)))
			return
		}
	}
}

func logMigrationResult(logger *slog.Logger, result *graph.MigrationResult, obj *graph.GraphObject) {
	riskColor := getRiskColor(result.RiskLevel)
	logger.Info(fmt.Sprintf("%s Migration successful", riskColor),
		slog.String("object_id", obj.ID.String()),
		slog.String("risk_level", string(result.RiskLevel)),
		slog.Int("migrated", len(result.MigratedProps)),
		slog.Int("dropped", len(result.DroppedProps)),
		slog.Int("coerced", len(result.CoercedProps)))

	if len(result.Issues) > 0 {
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
}

func getRiskColor(level graph.MigrationRiskLevel) string {
	switch level {
	case graph.RiskLevelSafe:
		return "✓"
	case graph.RiskLevelCautious:
		return "⚠"
	case graph.RiskLevelRisky:
		return "⚠⚠"
	case graph.RiskLevelDangerous:
		return "✗"
	default:
		return "?"
	}
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
	fmt.Printf("Blocked:           %d\n", stats.Blocked)
	fmt.Printf("Skipped:           %d\n", stats.Skipped)
	fmt.Printf("With warnings:     %d\n", stats.WithWarnings)

	fmt.Println("\n--- Risk Assessment ---")
	fmt.Printf("✓ Safe:            %d\n", stats.RiskySafe)
	fmt.Printf("⚠ Cautious:        %d\n", stats.RiskyCautious)
	fmt.Printf("⚠⚠ Risky:          %d\n", stats.RiskyRisky)
	fmt.Printf("✗ Dangerous:       %d\n", stats.RiskyDangerous)
	fmt.Println("========================")

	if stats.Blocked > 0 {
		fmt.Println("\n⚠️  Some migrations were blocked due to safety restrictions.")
		fmt.Println("Use --force for risky migrations (1-2 fields dropped)")
		fmt.Println("Use --force --confirm-data-loss for dangerous migrations (3+ fields dropped)")
	}
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

func printUsage() {
	fmt.Println("Schema Migration Tool")
	fmt.Println("\nMigration Mode:")
	fmt.Println("  migrate-schema -project <uuid> -from <version> -to <version> [-dry-run=true] [-batch=100]")
	fmt.Println("\nRollback Mode:")
	fmt.Println("  migrate-schema -project <uuid> --rollback --rollback-version <version> [-dry-run=true] [-batch=100]")
	fmt.Println("\nExamples:")
	fmt.Println("  # Dry-run migration from 1.0.0 to 2.0.0")
	fmt.Println("  migrate-schema -project a1b2c3d4-... -from 1.0.0 -to 2.0.0")
	fmt.Println("\n  # Execute risky migration (1-2 fields dropped)")
	fmt.Println("  migrate-schema -project a1b2c3d4-... -from 1.0.0 -to 2.0.0 -dry-run=false --force")
	fmt.Println("\n  # Execute dangerous migration (3+ fields dropped)")
	fmt.Println("  migrate-schema -project a1b2c3d4-... -from 1.0.0 -to 2.0.0 -dry-run=false --force --confirm-data-loss")
	fmt.Println("\n  # Dry-run rollback to 2.0.0")
	fmt.Println("  migrate-schema -project a1b2c3d4-... --rollback --rollback-version 2.0.0")
	fmt.Println("\n  # Execute rollback")
	fmt.Println("  migrate-schema -project a1b2c3d4-... --rollback --rollback-version 2.0.0 -dry-run=false")
}

func runRollback(projectIDStr string, targetVersion string, dryRun bool, batchSize int) {
	projectUUID, err := uuid.Parse(projectIDStr)
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
	validator := graph.NewPropertyValidator()
	migrator := graph.NewSchemaMigrator(validator, logger)

	ctx := context.Background()

	stats := &RollbackStats{}

	offset := 0
	for {
		var objects []graph.GraphObject
		err := db.NewSelect().
			Model(&objects).
			Where("project_id = ?", projectUUID).
			Where("migration_archive IS NOT NULL").
			Where("jsonb_array_length(migration_archive) > 0").
			Limit(batchSize).
			Offset(offset).
			Scan(ctx)

		if err != nil {
			logger.Error("Failed to fetch objects with archives", slog.String("error", err.Error()))
			os.Exit(1)
		}

		if len(objects) == 0 {
			break
		}

		for _, obj := range objects {
			stats.Total++

			result := migrator.RollbackObject(&obj, targetVersion)

			if !result.Success {
				stats.Failed++
				logger.Error("Rollback failed",
					slog.String("object_id", obj.ID.String()),
					slog.String("error", result.Error))
				continue
			}

			if len(result.RestoredProps) == 0 {
				stats.Skipped++
				logger.Warn("Rollback skipped",
					slog.String("object_id", obj.ID.String()),
					slog.String("reason", "No archive found for target version"))
				continue
			}

			stats.Successful++
			logger.Info("✓ Rollback successful",
				slog.String("object_id", obj.ID.String()),
				slog.String("from_version", result.FromVersion),
				slog.String("to_version", result.ToVersion),
				slog.Int("restored_fields", len(result.RestoredProps)))

			if !dryRun {
				_, err := db.NewUpdate().
					Model(&obj).
					Set("properties = ?", obj.Properties).
					Set("schema_version = ?", result.FromVersion).
					Set("migration_archive = ?", obj.MigrationArchive).
					Where("id = ?", obj.ID).
					Exec(ctx)

				if err != nil {
					logger.Error("Failed to update object after rollback",
						slog.String("object_id", obj.ID.String()),
						slog.String("error", err.Error()))
					stats.Failed++
				}
			}
		}

		offset += batchSize
		logger.Info("Batch processed",
			slog.Int("batch_size", len(objects)),
			slog.Int("total_processed", offset))
	}

	printRollbackSummary(stats, dryRun)

	if stats.Failed > 0 {
		os.Exit(1)
	}
}

type RollbackStats struct {
	Total      int
	Successful int
	Failed     int
	Skipped    int
}

func printRollbackSummary(stats *RollbackStats, dryRun bool) {
	fmt.Println("\n=== Rollback Summary ===")
	if dryRun {
		fmt.Println("Mode: DRY RUN (no changes applied)")
	} else {
		fmt.Println("Mode: LIVE (changes applied)")
	}
	fmt.Printf("Total objects:     %d\n", stats.Total)
	fmt.Printf("Successful:        %d\n", stats.Successful)
	fmt.Printf("Failed:            %d\n", stats.Failed)
	fmt.Printf("Skipped:           %d\n", stats.Skipped)
	fmt.Println("========================")
}
