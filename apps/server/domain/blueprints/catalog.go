package blueprints

import (
	"fmt"
	"io/fs"
	"log/slog"
	"path"

	"github.com/emergent-company/emergent.memory/pkg/logger"
)

// TaxonomyCatalog is the read-only reference vocabulary loaded from the
// bundled `taxonomy: true` packs: canonical object types plus the candidate
// relationship types between them. It is a source for installation, never
// compiled into a project wholesale.
type TaxonomyCatalog struct {
	Types         []ObjectTypeDef
	Relationships []RelationshipTypeDef
}

// CatalogProjection is the endpoint-gated result of asking the catalog for a
// set of types. Types are the requested (known) types; Relationships are the
// candidate edges whose BOTH endpoints are among them (active); Deferred are
// edges that reference at least one unrequested type and can be activated
// later by re-projecting with that type added (reconcile). Unknown records
// requested names that are not in the catalog; Problems records declared edges
// with an endpoint the catalog does not define.
type CatalogProjection struct {
	Types         []ObjectTypeDef
	Relationships []RelationshipTypeDef
	Deferred      []RelationshipTypeDef
	Unknown       []string
	Problems      []string
}

// CatalogPackOptions names and describes the project schema a projection is
// materialized as.
type CatalogPackOptions struct {
	Name        string
	Version     string
	Description string
	Author      string
}

// ListBuiltinTaxonomy loads the embedded taxonomy packs' object types and
// candidate relationship types. Types are deduplicated by name (first pack in
// lexical order wins); relationships are deduplicated by (name, source, target)
// after plural expansion. A malformed pack is skipped with a warning, mirroring
// ListBuiltinObjectTypes.
func ListBuiltinTaxonomy() (*TaxonomyCatalog, error) {
	return listBuiltinTaxonomy(builtinPacksFS, "builtin_packs")
}

func listBuiltinTaxonomy(fsys fs.FS, root string) (*TaxonomyCatalog, error) {
	entries, err := fs.ReadDir(fsys, root)
	if err != nil {
		return nil, err
	}
	cat := &TaxonomyCatalog{}
	seenType := make(map[string]string)
	seenRel := make(map[string]bool)
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		desc, err := loadBlueprintDir(fsys, path.Join(root, name), name)
		if err != nil {
			slog.Warn("skipping unloadable builtin blueprint pack",
				slog.String("pack", name), logger.Error(err))
			continue
		}
		for _, p := range desc.Packs {
			if !p.Taxonomy {
				continue
			}
			for _, ot := range p.ObjectTypes {
				if prev, ok := seenType[ot.Name]; ok {
					slog.Debug("skipping duplicate taxonomy object type",
						slog.String("type", ot.Name),
						slog.String("kept_pack", prev),
						slog.String("skipped_pack", p.Name))
					continue
				}
				seenType[ot.Name] = p.Name
				cat.Types = append(cat.Types, ot)
			}
			for _, rt := range p.RelationshipTypes {
				for _, pair := range expandRelationshipPairs(rt) {
					key := relationshipKey(pair)
					if seenRel[key] {
						continue
					}
					seenRel[key] = true
					cat.Relationships = append(cat.Relationships, pair)
				}
			}
		}
	}
	return cat, nil
}

// ProjectCatalog returns the endpoint-gated subset of the catalog for the
// requested type names. This is also the reconcile operation: call it with the
// union of the currently installed catalog types and the newly requested ones
// to activate deferred edges whose second endpoint just arrived.
func ProjectCatalog(cat *TaxonomyCatalog, requested []string) CatalogProjection {
	var proj CatalogProjection
	if cat == nil {
		return proj
	}

	inCatalog := make(map[string]bool, len(cat.Types))
	for _, t := range cat.Types {
		inCatalog[t.Name] = true
	}

	chosen := make(map[string]bool, len(requested))
	seenUnknown := make(map[string]bool)
	for _, name := range requested {
		if !inCatalog[name] {
			if !seenUnknown[name] {
				seenUnknown[name] = true
				proj.Unknown = append(proj.Unknown, name)
			}
			continue
		}
		chosen[name] = true
	}

	for _, t := range cat.Types {
		if chosen[t.Name] {
			proj.Types = append(proj.Types, t)
		}
	}

	seenEdge := make(map[string]bool)
	for _, rt := range cat.Relationships {
		for _, pair := range expandRelationshipPairs(rt) {
			if !inCatalog[pair.SourceType] || !inCatalog[pair.TargetType] {
				proj.Problems = append(proj.Problems, fmt.Sprintf(
					"relationship %q references a type not in the catalog: %s -> %s",
					pair.Name, pair.SourceType, pair.TargetType))
				continue
			}
			key := relationshipKey(pair)
			if seenEdge[key] {
				continue
			}
			seenEdge[key] = true
			if chosen[pair.SourceType] && chosen[pair.TargetType] {
				proj.Relationships = append(proj.Relationships, pair)
			} else {
				proj.Deferred = append(proj.Deferred, pair)
			}
		}
	}
	return proj
}

// Pack materializes a projection as a schema pack ready for blueprint apply:
// only the projected types and active relationships are included; deferred
// edges stay out of the pack until a later reconcile activates them.
func (p CatalogProjection) Pack(opts CatalogPackOptions) PackManifest {
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
		ObjectTypes:       p.Types,
		RelationshipTypes: p.Relationships,
	}
}

// expandRelationshipPairs expands a possibly-plural relationship declaration
// into singular source/target pairs, reusing the same expansion the apply path
// uses so projection and installation cannot disagree.
func expandRelationshipPairs(rt RelationshipTypeDef) []RelationshipTypeDef {
	out, err := expandRelationshipTypes([]RelationshipTypeDef{rt})
	if err != nil {
		return nil
	}
	return out
}

func relationshipKey(rt RelationshipTypeDef) string {
	return rt.Name + "\x00" + rt.SourceType + "\x00" + rt.TargetType
}

// planCatalogUnion computes the desired type set for an install. Additive
// (replace=false) keeps every installed type and adds the request. Replace
// (replace=true) makes the request authoritative and reports the installed
// types that will be dropped, so a caller can remove a type and deactivate the
// relationships that depended on it.
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
