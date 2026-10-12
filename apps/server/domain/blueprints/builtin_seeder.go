package blueprints

import (
	"context"
	"embed"
	"encoding/json"
	"io/fs"
	"log/slog"
	"path"

	"go.uber.org/fx"

	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/domain/schemas"
	"github.com/emergent-company/emergent.memory/pkg/logger"
)

// builtinPacksFS embeds the bundled blueprint packs (builtin_packs/) into the
// server binary, so the platform's built-in blueprints are available in any
// deployment without a runtime filesystem dependency.
//
//go:embed builtin_packs
var builtinPacksFS embed.FS

// BuiltinSeeder seeds the platform's built-in blueprints at server startup: each
// bundled pack becomes a global published blueprint (project_id NULL,
// origin='builtin'). It runs on every boot and is idempotent by (name, version).
//
// It does NOT seed schema packs: built-in blueprint packs materialize only when
// their blueprint is applied (the defensive resolvePack -> upsertBuiltinSchemaPack
// path in apply.go), and are never auto-assigned to projects.
type BuiltinSeeder struct {
	repo *Repository
	log  *slog.Logger
}

// NewBuiltinSeeder creates a new BuiltinSeeder.
func NewBuiltinSeeder(repo *Repository, log *slog.Logger) *BuiltinSeeder {
	return &BuiltinSeeder{
		repo: repo,
		log:  log.With(logger.Scope("blueprints.builtin_seeder")),
	}
}

// RegisterBuiltinSeederLifecycle registers the seeder with fx lifecycle.
func RegisterBuiltinSeederLifecycle(lc fx.Lifecycle, seeder *BuiltinSeeder) {
	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			return seeder.Seed(ctx)
		},
	})
}

// Seed upserts every bundled pack as a global published blueprint. Idempotent by
// (name, version); safe to call on every boot. Built-in schema packs are NOT
// seeded here — they materialize only on blueprint apply.
func (s *BuiltinSeeder) Seed(ctx context.Context) error {
	entries, err := fs.ReadDir(builtinPacksFS, "builtin_packs")
	if err != nil {
		return err
	}

	seeded := 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		desc, err := loadBlueprintDir(builtinPacksFS, path.Join("builtin_packs", name), name)
		if err != nil {
			s.log.Error("failed to load builtin blueprint pack", slog.String("pack", name), logger.Error(err))
			return err
		}

		manifest, err := json.Marshal(desc.BlueprintManifest)
		if err != nil {
			s.log.Error("failed to serialize builtin blueprint manifest", slog.String("pack", name), logger.Error(err))
			return err
		}
		checksum := manifestChecksum(manifest)

		if err := s.upsertBlueprint(ctx, desc, manifest, checksum); err != nil {
			s.log.Error("failed to seed builtin blueprint", slog.String("pack", name), logger.Error(err))
			return err
		}

		seeded++
	}

	s.log.Info("builtin blueprints seeded", slog.Int("count", seeded))
	return nil
}

// upsertBlueprint creates or updates the global published blueprint row for a
// builtin pack. Idempotent by (name, version) via the
// blueprints_name_version_global_key partial unique index.
func (s *BuiltinSeeder) upsertBlueprint(ctx context.Context, desc *blueprintDescriptor, manifest json.RawMessage, checksum string) error {
	_, err := s.repo.DB().NewRaw(`
		INSERT INTO kb.blueprints
			(name, version, description, author, status, project_id, manifest, checksum, origin, created_at, updated_at)
		VALUES (?, ?, ?, ?, 'published', NULL, ?, ?, 'builtin', now(), now())
		ON CONFLICT (name, version) WHERE project_id IS NULL
		DO UPDATE SET
			description = EXCLUDED.description,
			author = EXCLUDED.author,
			manifest = EXCLUDED.manifest,
			checksum = EXCLUDED.checksum,
			status = 'published',
			origin = 'builtin',
			updated_at = now()
	`, desc.Name, desc.Version, desc.Description, desc.Author, string(manifest), checksum).Exec(ctx)
	if err != nil {
		return err
	}
	return nil
}

// upsertBuiltinSchemaPack creates-or-updates the global source='builtin' schema
// pack for a manifest pack, returning the row (with ID). Idempotent by (name,
// version) via the graph_schemas_builtin_name_version_key partial unique index.
// Shared by the built-in seeder and the builtin apply path so the two produce
// identical registry rows.
func upsertBuiltinSchemaPack(ctx context.Context, db bun.IDB, p PackManifest) (*schemas.GraphMemorySchema, error) {
	objectTypes, relationshipTypes, err := packSchemaJSON(p)
	if err != nil {
		return nil, err
	}

	var migrationsJSON any
	if p.Migrations != nil {
		b, err := json.Marshal(p.Migrations)
		if err != nil {
			return nil, err
		}
		migrationsJSON = string(b)
	}

	pack := &schemas.GraphMemorySchema{}
	err = db.NewRaw(`
		INSERT INTO kb.graph_schemas
			(name, version, description, author, source, object_type_schemas, relationship_type_schemas, migrations, published_at, created_at, updated_at)
		VALUES (?, ?, ?, ?, 'builtin', ?, ?, ?, now(), now(), now())
		ON CONFLICT (name, version) WHERE source = 'builtin'
		DO UPDATE SET
			description = EXCLUDED.description,
			author = EXCLUDED.author,
			object_type_schemas = EXCLUDED.object_type_schemas,
			relationship_type_schemas = EXCLUDED.relationship_type_schemas,
			migrations = EXCLUDED.migrations,
			published_at = EXCLUDED.published_at,
			updated_at = now()
		RETURNING id
	`, p.Name, p.Version, p.Description, p.Author, string(objectTypes), string(relationshipTypes), migrationsJSON).Scan(ctx, &pack.ID)
	if err != nil {
		return nil, err
	}
	return pack, nil
}
