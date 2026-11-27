package blueprints

import (
	"context"
	"fmt"
	"strings"

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
	// Merged are the requested types the caller named in Extend, which the
	// catalog pack now owns: its declaration became the live definition and the
	// property definitions both packs declare were merged additively.
	Merged   []string `json:"merged,omitempty"`
	Unknown  []string `json:"unknown,omitempty"`
	Problems []string `json:"problems,omitempty"`
}

// CatalogTypeConflict names a requested catalog type whose name another pack
// already provides, so installing it would replace that pack's live definition
// with the catalog's and take the type over, not install a second one.
type CatalogTypeConflict struct {
	Name      string `json:"name"`
	OwnerPack string `json:"ownerPack"`
}

// InstallCatalog materializes the endpoint-gated projection of the built-in
// catalog into a project schema and assigns it. It is idempotent: re-running
// reconciles, so installing a type later activates the deferred relationships
// whose other endpoint just arrived. With replace=true the request is
// authoritative and omitted catalog types are removed from the pack
// (deactivating their relationships); graph objects are untouched.
//
// A requested type whose name another pack already provides is refused unless
// it is named in extend. The platform keeps one type per name and resolves it
// from the newest assignment, so installing such a name replaces the live
// definition with the catalog's declaration and moves the type to the catalog
// pack: the other pack's version is shadowed, and a later replace treats the
// name as a catalog type. Property definitions both declare keep the existing
// one (the registry schema is merged additively), but properties only the other
// pack declared are not part of the catalog's definition. extend makes that
// takeover an explicit, per-type request instead of a side effect of naming a
// type the project already has.
func (s *Service) InstallCatalog(ctx context.Context, projectID, userID string, requested []string, opts CatalogPackOptions, replace bool, extend []string) (*CatalogInstallResult, error) {
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

	// A name that another pack provides is the same type, not a second one, so
	// installing it would take the name over: the catalog's declaration becomes
	// the live definition and the catalog pack owns it. Refuse unless the caller
	// named it in extend; nothing about that takeover is implicit.
	extendSet := make(map[string]bool, len(extend))
	for _, name := range extend {
		extendSet[name] = true
	}
	if conflicts := catalogTypeConflicts(compiled, catalogTypes, requested, opts.Name, extendSet); len(conflicts) > 0 {
		return nil, apperror.NewBadRequest(catalogConflictMessage(conflicts))
	}
	if unknown := catalogExtendUnknown(extend, requested); len(unknown) > 0 {
		return nil, apperror.NewBadRequest(fmt.Sprintf(
			"extend names types that were not requested: %s", strings.Join(unknown, ", ")))
	}

	proj := catalog.Project(cat, union)
	if len(proj.Types) == 0 {
		return nil, apperror.NewBadRequest("no catalog types requested or installed; name at least one type")
	}

	pack, created, err := s.resolvePack(ctx, projectID, OriginUser, catalogPack(proj, opts))
	if err != nil {
		return nil, err
	}
	assign, err := s.schemasSvc.AssignPack(ctx, projectID, userID, &schemas.AssignPackRequest{
		SchemaID:      pack.ID,
		MergeTypes:    extend,
		AutoUninstall: true,
	})
	if err != nil {
		return nil, err
	}

	res := describeCatalogInstall(pack.ID, created, proj, removed)
	if assign != nil {
		res.Merged = assign.MergedTypes
	}
	return res, nil
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

// catalogTypeOwners maps each effective compiled object type to the pack that
// provides it. Compiled types are ordered by assignment and every losing
// same-name declaration is flagged Shadowed, so the unshadowed row is the
// definition the project actually resolves.
func catalogTypeOwners(compiled *schemas.CompiledTypesResponse) map[string]string {
	owners := make(map[string]string)
	if compiled == nil {
		return owners
	}
	for _, t := range compiled.ObjectTypes {
		if t.Shadowed || t.SchemaName == "" {
			continue
		}
		owners[t.Name] = t.SchemaName
	}
	return owners
}

// catalogTypeConflicts reports the requested catalog types whose name another
// pack already provides and whose takeover the caller did not accept via extend.
func catalogTypeConflicts(compiled *schemas.CompiledTypesResponse, catalogTypes map[string]bool, requested []string, packName string, extend map[string]bool) []CatalogTypeConflict {
	owners := catalogTypeOwners(compiled)
	var out []CatalogTypeConflict
	seen := make(map[string]bool, len(requested))
	for _, name := range requested {
		if seen[name] || extend[name] || !catalogTypes[name] {
			// Unknown names are reported by catalog.Project; a name the caller
			// accepted is taken over on purpose.
			continue
		}
		seen[name] = true
		if owner, ok := owners[name]; ok && owner != packName {
			out = append(out, CatalogTypeConflict{Name: name, OwnerPack: owner})
		}
	}
	return out
}

// catalogExtendUnknown returns the extend names that were not requested; they
// would be silently ignored otherwise.
func catalogExtendUnknown(extend, requested []string) []string {
	inRequest := make(map[string]bool, len(requested))
	for _, name := range requested {
		inRequest[name] = true
	}
	var out []string
	for _, name := range extend {
		if !inRequest[name] {
			out = append(out, name)
		}
	}
	return out
}

// catalogConflictMessage names every refused type and its owning pack, and the
// flag that accepts the takeover.
func catalogConflictMessage(conflicts []CatalogTypeConflict) string {
	parts := make([]string, 0, len(conflicts))
	for _, c := range conflicts {
		parts = append(parts, fmt.Sprintf("%s (already provided by pack %q)", c.Name, c.OwnerPack))
	}
	return fmt.Sprintf(
		"catalog types not installed: %s — a project keeps one type per name and resolves it from the newest install, so installing it would replace that pack's definition with the catalog's; list the names in \"extend\" to install the catalog's definition deliberately",
		strings.Join(parts, ", "))
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
	// Extend names requested types the caller accepts installing under the
	// catalog's definition even though another pack already provides the name.
	// Without it such a request is refused: the project resolves one type per
	// name from the newest install, so installing the name replaces the live
	// definition with the catalog's declaration and moves the type to the
	// catalog pack. The registry schema is merged additively (existing property
	// definitions win), but properties only the other pack declared are not
	// part of the catalog's version.
	Extend []string `json:"extend,omitempty"`
}
