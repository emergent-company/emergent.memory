package blueprints

import (
	"strings"
)

// CatalogTypeDetail is one catalog type with its hierarchy and the candidate
// relationships that touch it. It powers taxonomy browsing / a type picker.
type CatalogTypeDetail struct {
	Type                 ObjectTypeDef         `json:"type"`
	Ancestors            []string              `json:"ancestors"`
	Children             []string              `json:"children"`
	RelatedRelationships []RelationshipTypeDef `json:"relatedRelationships"`
}

// maxAncestorDepth guards a malformed catalog (a parent cycle) from looping.
const maxAncestorDepth = 32

// CatalogDetail returns the detail for a catalog type by name. ok is false when
// the name is not in the catalog.
func CatalogDetail(cat *TaxonomyCatalog, name string) (*CatalogTypeDetail, bool) {
	if cat == nil {
		return nil, false
	}
	byName := make(map[string]ObjectTypeDef, len(cat.Types))
	for _, t := range cat.Types {
		byName[t.Name] = t
	}
	t, ok := byName[name]
	if !ok {
		return nil, false
	}

	detail := &CatalogTypeDetail{Type: t}

	// Children: direct subtypes (parent == name), in catalog order.
	for _, other := range cat.Types {
		if other.Parent == name {
			detail.Children = append(detail.Children, other.Name)
		}
	}

	// Ancestors: walk the parent chain, guarding cycles and depth.
	seen := map[string]bool{name: true}
	cur := t.Parent
	for depth := 0; cur != "" && depth < maxAncestorDepth; depth++ {
		if seen[cur] {
			break
		}
		seen[cur] = true
		if parent, ok := byName[cur]; ok {
			detail.Ancestors = append(detail.Ancestors, parent.Name)
			cur = parent.Parent
			continue
		}
		// Parent name is declared but missing from the catalog; report it and
		// stop rather than inventing a node.
		detail.Ancestors = append(detail.Ancestors, cur)
		break
	}

	// Relationships touching this type (either endpoint), deduplicated by pair.
	seenRel := make(map[string]bool)
	for _, r := range cat.Relationships {
		if r.SourceType != name && r.TargetType != name {
			continue
		}
		key := relationshipKey(r)
		if seenRel[key] {
			continue
		}
		seenRel[key] = true
		detail.RelatedRelationships = append(detail.RelatedRelationships, r)
	}

	return detail, true
}

// CatalogDetail loads the bundled catalog and returns one type's detail.
func (s *Service) CatalogDetail(name string) (*CatalogTypeDetail, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, nil
	}
	cat, err := ListBuiltinTaxonomy()
	if err != nil {
		return nil, err
	}
	detail, ok := CatalogDetail(cat, name)
	if !ok {
		return nil, nil
	}
	return detail, nil
}
