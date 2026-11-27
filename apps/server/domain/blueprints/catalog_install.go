package blueprints

import (
	"context"
	"fmt"

	"github.com/emergent-company/emergent.memory/domain/catalog"
	"github.com/emergent-company/emergent.memory/domain/schemas"
	"github.com/emergent-company/emergent.memory/pkg/apperror"
)

// CatalogPackOptions names and describes the project schema a catalog
// projection is materialized as.
type CatalogPackOptions struct {
	Name        string
	Version     string
	Description string
	Author      string
}

// CatalogInstallResult reports what a catalog install/reconcile did.
type CatalogInstallResult struct {
	PackID        string   `json:"packId"`
	Created       bool     `json:"created"`
	Types         []string `json:"types"`
	Relationships []string `json:"relationships"`
	Deferred      []string `json:"deferred"`
	Removed       []string `json:"removed,omitempty"`
	Unknown       []string `json:"unknown,omitempty"`
	Problems      []string `json:"problems,omitempty"`
}

// InstallCatalog materializes the endpoint-gated projection of the built-in
// catalog into a project schema and assigns it. It is idempotent: re-running
// reconciles, so installing a type later activates the deferred relationships
// whose other endpoint just arrived. With replace=true the request is
// authoritative and omitted catalog types are removed from the pack
// (deactivating their relationships); graph objects are untouched.
func (s *Service) InstallCatalog(ctx context.Context, projectID, userID string, requested []string, opts CatalogPackOptions, replace bool) (*CatalogInstallResult, error) {
	if projectID == "" {
		return nil, apperror.NewBadRequest("project id is required")
	}
	if opts.Name == "" {
		opts.Name = "schemaorg-catalog"
	}
	if opts.Version == "" {
		opts.Version = "1.0.0"
	}

	cat, err := catalog.Load()
	if err != nil {
		return nil, err
	}
	if len(cat.Types) == 0 {
		return nil, apperror.NewBadRequest("built-in catalog is empty")
	}
	catalogTypes := make(map[string]bool, len(cat.Types))
	for _, t := range cat.Types {
		catalogTypes[t.Name] = true
	}

	compiled, err := s.schemasSvc.GetCompiledTypes(ctx, projectID)
	if err != nil {
		return nil, err
	}
	installed := catalogInstalledTypes(compiled, opts.Name)
	// Refuse to rewrite a pack that is not a catalog pack. The pack name is
	// caller-controlled; without this check a name collision could let an
	// additive install (which unions installed types and then drops catalog
	// misses) or a replace install delete an unrelated project pack's types.
	if conflict, ok := catalogPackConflict(catalogTypes, installed); ok {
		return nil, apperror.NewBadRequest(fmt.Sprintf(
			"pack %q already contains non-catalog type %q; catalog install will not rewrite an unrelated pack — choose a different pack name",
			opts.Name, conflict))
	}

	union, removed := planCatalogUnion(installed, requested, replace)
	if len(union) == 0 {
		return nil, apperror.NewBadRequest("no catalog types requested; name at least one type")
	}

	proj := catalog.Project(cat, union)
	if len(proj.Types) == 0 {
		return nil, apperror.NewBadRequest("no catalog types requested or installed; name at least one type")
	}

	pack, created, err := s.resolvePack(ctx, projectID, OriginUser, catalogPack(proj, opts))
	if err != nil {
		return nil, err
	}
	if _, err := s.schemasSvc.AssignPack(ctx, projectID, userID, &schemas.AssignPackRequest{
		SchemaID:      pack.ID,
		Merge:         true,
		AutoUninstall: true,
	}); err != nil {
		return nil, err
	}

	return describeCatalogInstall(pack.ID, created, proj, removed), nil
}

// catalogPack converts a projection into the schema pack the apply path
// consumes. Deferred relationships are excluded by construction.
func catalogPack(p catalog.Projection, opts CatalogPackOptions) PackManifest {
	name := opts.Name
	if name == "" {
		name = "catalog"
	}
	version := opts.Version
	if version == "" {
		version = "1.0.0"
	}
	return PackManifest{
		Name:              name,
		Version:           version,
		Description:       opts.Description,
		Author:            opts.Author,
		ObjectTypes:       catalogObjectTypeDefs(p.Types),
		RelationshipTypes: catalogRelationshipDefs(p.Relationships),
	}
}

// catalogObjectTypeDefs converts catalog types to the pack definition shape.
func catalogObjectTypeDefs(types []catalog.Type) []ObjectTypeDef {
	out := make([]ObjectTypeDef, 0, len(types))
	for _, t := range types {
		out = append(out, ObjectTypeDef{
			Name:        t.Name,
			Label:       t.Label,
			Description: t.Description,
			Category:    t.Category,
			Parent:      t.Parent,
			Properties:  t.Properties,
			Required:    t.Required,
			Labels:      t.Labels,
			UI:          t.UI,
		})
	}
	return out
}

// catalogRelationshipDefs converts catalog relationships to the pack shape.
func catalogRelationshipDefs(rels []catalog.Relationship) []RelationshipTypeDef {
	out := make([]RelationshipTypeDef, 0, len(rels))
	for _, r := range rels {
		out = append(out, RelationshipTypeDef{
			Name:        r.Name,
			Label:       r.Label,
			Description: r.Description,
			SourceType:  r.SourceType,
			TargetType:  r.TargetType,
			Properties:  r.Properties,
			Required:    r.Required,
		})
	}
	return out
}

// catalogInstalledTypes returns the object-type names already contributed by the
// named catalog pack in a project's compiled view. It is the reconcile input.
func catalogInstalledTypes(compiled *schemas.CompiledTypesResponse, packName string) []string {
	if compiled == nil || packName == "" {
		return nil
	}
	out := make([]string, 0, len(compiled.ObjectTypes))
	seen := make(map[string]bool, len(compiled.ObjectTypes))
	for _, t := range compiled.ObjectTypes {
		if t.SchemaName != packName || seen[t.Name] {
			continue
		}
		seen[t.Name] = true
		out = append(out, t.Name)
	}
	return out
}

// planCatalogUnion computes the desired type set for an install. Additive
// (replace=false) keeps every installed type and adds the request. Replace
// (replace=true) makes the request authoritative and reports the installed
// types that will be dropped.
func planCatalogUnion(installed, requested []string, replace bool) (union, removed []string) {
	dedupe := func(in []string) []string {
		seen := make(map[string]bool, len(in))
		out := make([]string, 0, len(in))
		for _, s := range in {
			if s == "" || seen[s] {
				continue
			}
			seen[s] = true
			out = append(out, s)
		}
		return out
	}

	if replace {
		union = dedupe(requested)
		want := make(map[string]bool, len(union))
		for _, s := range union {
			want[s] = true
		}
		for _, s := range dedupe(installed) {
			if !want[s] {
				removed = append(removed, s)
			}
		}
		return union, removed
	}
	return dedupe(append(append([]string{}, installed...), requested...)), nil
}

// catalogPackConflict reports the first installed type that is not part of the
// catalog — evidence the named pack is not a catalog pack.
func catalogPackConflict(catalogTypes map[string]bool, installed []string) (string, bool) {
	for _, name := range installed {
		if !catalogTypes[name] {
			return name, true
		}
	}
	return "", false
}

func describeCatalogInstall(packID string, created bool, p catalog.Projection, removed []string) *CatalogInstallResult {
	res := &CatalogInstallResult{
		PackID:   packID,
		Created:  created,
		Removed:  removed,
		Unknown:  p.Unknown,
		Problems: p.Problems,
	}
	for _, t := range p.Types {
		res.Types = append(res.Types, t.Name)
	}
	for _, r := range p.Relationships {
		res.Relationships = append(res.Relationships, fmt.Sprintf("%s(%s->%s)", r.Name, r.SourceType, r.TargetType))
	}
	for _, r := range p.Deferred {
		res.Deferred = append(res.Deferred, fmt.Sprintf("%s(%s->%s)", r.Name, r.SourceType, r.TargetType))
	}
	return res
}

// CatalogInstallRequest is the HTTP body for POST /api/blueprints/catalog/install.
type CatalogInstallRequest struct {
	Types       []string `json:"types"`
	Name        string   `json:"name,omitempty"`
	Version     string   `json:"version,omitempty"`
	Description string   `json:"description,omitempty"`
	Author      string   `json:"author,omitempty"`
	// Replace makes Types authoritative: catalog types omitted from Types are
	// removed from the pack, deactivating their relationships.
	Replace bool `json:"replace,omitempty"`
}
