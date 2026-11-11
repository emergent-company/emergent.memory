package sources

import (
	"context"

	"github.com/google/uuid"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
)

// UpsertSourceObjects inserts or updates typed-object mappings by their stable
// identity (source, external item, object type, object key). A re-sync updates
// the current object version and reactivates a previously deleted mapping.
func (r *Repository) UpsertSourceObjects(ctx context.Context, objects []*SourceObject) error {
	if len(objects) == 0 {
		return nil
	}
	_, err := r.db.NewInsert().
		Model(&objects).
		On("CONFLICT (source_id, external_id, object_type, object_key) DO UPDATE").
		Set("object_id = EXCLUDED.object_id").
		Set("object_canonical_id = EXCLUDED.object_canonical_id").
		Set("document_id = COALESCE(EXCLUDED.document_id, so.document_id)").
		Set("revision_id = EXCLUDED.revision_id").
		Set("status = 'active'").
		Set("synced_at = EXCLUDED.synced_at").
		Set("updated_at = now()").
		Exec(ctx)
	if err != nil {
		r.log.Error("failed to upsert source objects", "error", err)
		return apperror.NewDatabase("failed to upsert source objects", err)
	}
	return nil
}

// ListActiveSourceObjects returns the active typed-object mappings for a source.
func (r *Repository) ListActiveSourceObjects(ctx context.Context, projectID, sourceID uuid.UUID) ([]*SourceObject, error) {
	var objects []*SourceObject
	err := r.db.NewSelect().
		Model(&objects).
		Where("project_id = ?", projectID).
		Where("source_id = ?", sourceID).
		Where("status = ?", SourceObjectStatusActive).
		Scan(ctx)
	if err != nil {
		r.log.Error("failed to list source objects", "error", err)
		return nil, apperror.NewDatabase("failed to list source objects", err)
	}
	return objects, nil
}

// MarkSourceObjectsDeleted marks mappings deleted-from-source.
func (r *Repository) MarkSourceObjectsDeleted(ctx context.Context, ids []uuid.UUID) error {
	if len(ids) == 0 {
		return nil
	}
	_, err := r.db.NewUpdate().
		Model((*SourceObject)(nil)).
		Set("status = ?", SourceObjectStatusDeletedFromSource).
		Set("updated_at = now()").
		Where("id IN (?)", ids).
		Exec(ctx)
	if err != nil {
		r.log.Error("failed to mark source objects deleted", "error", err)
		return apperror.NewDatabase("failed to mark source objects deleted", err)
	}
	return nil
}

// CountSourceObjects returns the number of active typed objects for a source.
func (r *Repository) CountSourceObjects(ctx context.Context, projectID, sourceID uuid.UUID) (int, error) {
	return r.db.NewSelect().
		Model((*SourceObject)(nil)).
		Where("project_id = ?", projectID).
		Where("source_id = ?", sourceID).
		Where("status = ?", SourceObjectStatusActive).
		Count(ctx)
}

// ListMcpSources returns the project's sources that expose an MCP server,
// without credential blobs.
func (r *Repository) ListMcpSources(ctx context.Context, projectID uuid.UUID) ([]*Source, error) {
	var sources []*Source
	err := r.db.NewSelect().
		Model(&sources).
		Where("project_id = ?", projectID).
		Where("mcp_slug IS NOT NULL").
		ExcludeColumn("encrypted_credentials").
		Order("created_at ASC").
		Scan(ctx)
	if err != nil {
		r.log.Error("failed to list mcp sources", "error", err)
		return nil, apperror.NewDatabase("failed to list mcp sources", err)
	}
	return sources, nil
}

// McpSlugExists reports whether another source in the project already uses the
// slug.
func (r *Repository) McpSlugExists(ctx context.Context, projectID uuid.UUID, slug string, exceptID uuid.UUID) (bool, error) {
	exists, err := r.db.NewSelect().
		Model((*Source)(nil)).
		Where("project_id = ?", projectID).
		Where("mcp_slug = ?", slug).
		Where("id != ?", exceptID).
		Exists(ctx)
	if err != nil {
		return false, apperror.NewDatabase("check mcp slug", err)
	}
	return exists, nil
}

// ListSourcesMissingMcpSlug pages the sources of the given provider types that
// still have no MCP slug, for the startup backfill. Scoping the query to rows
// that need work keeps a boot from listing every source in every project and
// re-provisioning servers that already exist.
func (r *Repository) ListSourcesMissingMcpSlug(ctx context.Context, providers []string, limit int) ([]*Source, error) {
	if len(providers) == 0 || limit <= 0 {
		return nil, nil
	}
	var sources []*Source
	err := r.db.NewSelect().
		Model(&sources).
		Where("provider IN (?)", bun.In(providers)).
		Where("mcp_slug IS NULL").
		ExcludeColumn("encrypted_credentials").
		Order("created_at ASC", "id ASC").
		Limit(limit).
		Scan(ctx)
	if err != nil {
		r.log.Error("failed to list sources missing an mcp slug", "error", err)
		return nil, apperror.NewDatabase("failed to list sources missing an mcp slug", err)
	}
	return sources, nil
}

// SetMcpSlug updates only the source's MCP slug.
func (r *Repository) SetMcpSlug(ctx context.Context, projectID, sourceID uuid.UUID, slug string) error {
	_, err := r.db.NewUpdate().
		Model((*Source)(nil)).
		Set("mcp_slug = ?", slug).
		Set("updated_at = now()").
		Where("id = ?", sourceID).
		Where("project_id = ?", projectID).
		Exec(ctx)
	if err != nil {
		return apperror.NewDatabase("set source mcp slug", err)
	}
	return nil
}

// McpServerNameExists reports whether any MCP server row (of any type) already
// uses the name in the project.
func (r *Repository) McpServerNameExists(ctx context.Context, projectID uuid.UUID, name string) (bool, error) {
	exists, err := r.db.NewSelect().
		TableExpr("kb.mcp_servers").
		Where("project_id = ?", projectID).
		Where("name = ?", name).
		Exists(ctx)
	if err != nil {
		return false, apperror.NewDatabase("check mcp server name", err)
	}
	return exists, nil
}
