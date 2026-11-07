package blueprints

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
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
// It also retires built-ins the binary no longer bundles, so a pack that moved
// out to an external blueprint stops being offered as a stale Built-in entry.
//
// It does NOT seed schema packs: built-in blueprint packs materialize when their
// blueprint is applied — and are refreshed from the embedded manifest on every
// re-apply (resolvePack -> upsertBuiltinSchemaPack in apply.go), so a pack whose
// content changes under the same version still reaches applied projects. They are
// never auto-assigned to projects.
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

// Seed upserts every bundled pack as a global published blueprint, then retires
// any global built-in blueprint this binary no longer bundles. Idempotent by
// (name, version); safe to call on every boot. Built-in schema packs are NOT
// seeded here — they materialize, and are refreshed from the manifest, only when
// their blueprint is applied.
func (s *BuiltinSeeder) Seed(ctx context.Context) error {
	entries, err := fs.ReadDir(builtinPacksFS, "builtin_packs")
	if err != nil {
		return err
	}

	bundled := make(map[string]struct{}, len(entries))
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

		bundled[builtinKey(desc.Name, desc.Version)] = struct{}{}
		seeded++
	}

	s.log.Info("builtin blueprints seeded", slog.Int("count", seeded))

	if err := s.retireStaleBuiltins(ctx, bundled); err != nil {
		return err
	}
	return nil
}

// builtinKey identifies a bundled built-in blueprint by the pair that the
// global (name, version) unique index uses.
func builtinKey(name, version string) string {
	return name + "@" + version
}

// staleBuiltin is one global origin='builtin' row found in the database.
type staleBuiltin struct {
	ID       string `bun:"id"`
	Name     string `bun:"name"`
	Version  string `bun:"version"`
	Status   string `bun:"status"`
	Checksum string `bun:"checksum"`
}

// errLiveApplication aborts the retirement of one blueprint without failing the
// pass: a project started applying it between the listing read and the
// transaction, so the row is still live.
var errLiveApplication = errors.New("blueprint carries a live application")

// retireStaleBuiltins deletes global origin='builtin' blueprints the binary no
// longer bundles. A built-in is delivered by the binary, so a row whose
// (name, version) has disappeared from builtin_packs/ is a residue of an earlier
// release — typically a pack that was moved out to an external GitHub blueprint
// (personal-memory was, in #1513) — and no surface can remove it: built-ins
// carry no Remove action, and DeleteBlueprint refuses every global blueprint
// (guardMutable rejects project_id IS NULL) altogether. Without this step every
// long-lived deployment keeps offering a stale Built-in entry forever.
//
// Retirement is deliberately narrow:
//   - it only retires rows this seeder could have written. A global
//     origin='builtin' row that is not `published` with a checksum never came
//     from Seed (upsertBlueprint always writes both), so it is a row the
//     origin backfill in 00228 labelled 'builtin' — or one inserted outside the
//     seeder — and it is left alone with a warning instead of being hard-deleted
//     as "stale".
//   - it never touches a blueprint with a live application. `applied` means a
//     project is currently on it, so deleting the record would silently detach
//     an installed blueprint; the row is left in place and a warning names it.
//     Superseded and unapplied rows are history and are cleared, because the
//     non-cascading blueprint_id foreign keys otherwise block the delete.
//   - it never touches a schema pack. The packs a retired blueprint claimed
//     were materialized into the projects that applied it, and those rows — and
//     the objects written against them — are project data that must outlive the
//     gallery entry. It only clears the attribution the deleted blueprint owned
//     (source_blueprint_id on the project's schema assignments and agent
//     definitions), so the pointer cannot dangle and block a later same-name
//     stamp.
//   - it does nothing when the embedded set is empty, so a mis-built binary
//     cannot wipe the registry.
//
// It is hygiene, not correctness: every failure is logged and the pass
// continues, because a stale gallery entry must never stop the server booting.
func (s *BuiltinSeeder) retireStaleBuiltins(ctx context.Context, bundled map[string]struct{}) error {
	if len(bundled) == 0 {
		s.log.Warn("no builtin blueprints are bundled; skipping stale-builtin retirement")
		return nil
	}

	var rows []staleBuiltin
	if err := s.repo.DB().NewRaw(`
		SELECT id::text AS id, name, version, status, checksum
		FROM kb.blueprints
		WHERE project_id IS NULL AND origin = 'builtin'
		ORDER BY name, version
	`).Scan(ctx, &rows); err != nil {
		s.log.Error("failed to list builtin blueprints for retirement", logger.Error(err))
		return err
	}

	retired := 0
	for _, row := range rows {
		if _, ok := bundled[builtinKey(row.Name, row.Version)]; ok {
			continue
		}

		if row.Status != StatusPublished || row.Checksum == "" {
			s.log.Warn("global builtin blueprint was not written by this binary; leaving it in place",
				slog.String("name", row.Name),
				slog.String("version", row.Version),
				slog.String("status", row.Status),
			)
			continue
		}

		err := s.retireBuiltin(ctx, row.ID)
		switch {
		case errors.Is(err, errLiveApplication):
			s.log.Warn("builtin blueprint is no longer bundled but is still applied; leaving it in place",
				slog.String("name", row.Name),
				slog.String("version", row.Version),
			)
			continue
		case err != nil:
			s.log.Error("failed to retire builtin blueprint",
				slog.String("name", row.Name),
				slog.String("version", row.Version),
				logger.Error(err),
			)
			continue
		}

		retired++
		s.log.Info("retired builtin blueprint that is no longer bundled",
			slog.String("name", row.Name),
			slog.String("version", row.Version),
		)
	}

	if retired > 0 {
		s.log.Info("builtin blueprints retired", slog.Int("count", retired))
	}
	return nil
}

// retireBuiltin removes one retired built-in in a single transaction, so a
// concurrent apply can never observe a half-deleted blueprint. The order is
// forced by the non-cascading blueprint_id foreign keys: claims and historical
// applications first, then attribution, then the blueprint itself. A row that
// still has a live application rolls the whole transaction back with
// errLiveApplication; the foreign key also refuses the final delete if an apply
// commits in between, which fails the row and leaves it intact.
func (s *BuiltinSeeder) retireBuiltin(ctx context.Context, blueprintID string) error {
	return s.repo.DB().RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if _, err := tx.NewRaw(`
			DELETE FROM kb.blueprint_applications
			WHERE blueprint_id = ? AND status IN ('superseded', 'unapplied')
		`, blueprintID).Exec(ctx); err != nil {
			return err
		}

		var live int
		if err := tx.NewRaw(`
			SELECT COUNT(*) FROM kb.blueprint_applications WHERE blueprint_id = ?
		`, blueprintID).Scan(ctx, &live); err != nil {
			return err
		}
		if live > 0 {
			return errLiveApplication
		}

		if _, err := tx.NewRaw(`DELETE FROM kb.blueprint_pack_claims WHERE blueprint_id = ?`, blueprintID).Exec(ctx); err != nil {
			return err
		}
		if _, err := tx.NewRaw(`
			UPDATE kb.project_schemas SET source_blueprint_id = NULL, updated_at = now()
			WHERE source_blueprint_id = ?
		`, blueprintID).Exec(ctx); err != nil {
			return err
		}
		if _, err := tx.NewRaw(`
			UPDATE kb.agent_definitions SET source_blueprint_id = NULL, updated_at = now()
			WHERE source_blueprint_id = ?
		`, blueprintID).Exec(ctx); err != nil {
			return err
		}

		_, err := tx.NewRaw(`DELETE FROM kb.blueprints WHERE id = ?`, blueprintID).Exec(ctx)
		return err
	})
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
