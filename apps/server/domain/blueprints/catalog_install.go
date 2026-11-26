package blueprints

import (
	"context"
	"fmt"

	"github.com/emergent-company/emergent.memory/domain/schemas"
	"github.com/emergent-company/emergent.memory/pkg/apperror"
)

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

// InstallCatalog materializes the endpoint-gated projection of the bundled
// taxonomy catalog into a project schema and assigns it. It is idempotent:
// re-running reconciles, so installing a type later activates the deferred
// relationships whose other endpoint just arrived. With replace=true the
// request is authoritative and omitted catalog types are removed from the pack
// (deactivating their relationships); graph objects are untouched.
//
// requested names must exist in the catalog; unknown names are reported in the
// result, not installed. already-installed catalog types are unioned with the
// request so a reconcile never drops earlier types.
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

	cat, err := ListBuiltinTaxonomy()
	if err != nil {
		return nil, err
	}
	if len(cat.Types) == 0 {
		return nil, apperror.NewBadRequest("built-in taxonomy catalog is empty")
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
	// caller-controlled; without this check a name collision would let an
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

	proj := ProjectCatalog(cat, union)
	if len(proj.Types) == 0 {
		return nil, apperror.NewBadRequest("no catalog types requested or installed; name at least one type")
	}

	pack, created, err := s.resolvePack(ctx, projectID, OriginUser, proj.Pack(opts))
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

func describeCatalogInstall(packID string, created bool, p CatalogProjection, removed []string) *CatalogInstallResult {
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
		res.Relationships = append(res.Relationships, describeRelationship(r))
	}
	for _, r := range p.Deferred {
		res.Deferred = append(res.Deferred, describeRelationship(r))
	}
	return res
}

func describeRelationship(r RelationshipTypeDef) string {
	return fmt.Sprintf("%s(%s->%s)", r.Name, r.SourceType, r.TargetType)
}

// CatalogInstallRequest is the HTTP body for POST /api/blueprints/catalog/install.
type CatalogInstallRequest struct {
	// Types are the catalog type names to install (endpoint-gated). Re-running
	// with more types reconciles and activates newly-satisfiable relationships.
	Types       []string `json:"types"`
	Name        string   `json:"name,omitempty"`
	Version     string   `json:"version,omitempty"`
	Description string   `json:"description,omitempty"`
	Author      string   `json:"author,omitempty"`
	// Replace makes Types authoritative: catalog types omitted from Types are
	// removed from the pack, deactivating their relationships. Default false is
	// additive (installed types are kept).
	Replace bool `json:"replace,omitempty"`
}

// catalogPackConflict reports the first installed type that is not part of the
// catalog, i.e. evidence the named pack is not a catalog pack and must not be
// rewritten by install/replace.
func catalogPackConflict(catalogTypes map[string]bool, installed []string) (string, bool) {
	for _, name := range installed {
		if !catalogTypes[name] {
			return name, true
		}
	}
	return "", false
}
