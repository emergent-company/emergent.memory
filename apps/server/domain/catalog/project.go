package catalog

import "fmt"

// Projection is the endpoint-gated subset of the catalog for a set of type
// names. Relationships are active only when BOTH endpoints are projected;
// Deferred holds the rest (activate them later by re-projecting with the
// missing type added). Unknown names requested and Problems in the catalog
// itself are reported rather than silently dropped.
type Projection struct {
	Types         []Type
	Relationships []Relationship
	Deferred      []Relationship
	Unknown       []string
	Problems      []string
}

// Project returns the projection for the requested type names. It is also the
// reconcile operation: call it with installed ∪ newly-requested types.
func Project(cat *Catalog, requested []string) Projection {
	var proj Projection
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
	for _, r := range cat.Relationships {
		pairs, err := expandPairs(r)
		if err != nil {
			proj.Problems = append(proj.Problems, err.Error())
			continue
		}
		for _, pair := range pairs {
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

// TypeDetail is one type with its hierarchy and the candidate relationships
// touching it (taxonomy browsing / a type picker).
type TypeDetail struct {
	Type                 Type           `json:"type"`
	Ancestors            []string       `json:"ancestors"`
	Children             []string       `json:"children"`
	RelatedRelationships []Relationship `json:"relatedRelationships"`
}

const maxAncestorDepth = 32

// Detail returns the detail for a catalog type by name. ok is false when the
// name is not in the catalog.
func Detail(cat *Catalog, name string) (*TypeDetail, bool) {
	if cat == nil {
		return nil, false
	}
	byName := make(map[string]Type, len(cat.Types))
	for _, t := range cat.Types {
		byName[t.Name] = t
	}
	t, ok := byName[name]
	if !ok {
		return nil, false
	}

	detail := &TypeDetail{Type: t}
	for _, other := range cat.Types {
		if other.Parent == name {
			detail.Children = append(detail.Children, other.Name)
		}
	}

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
		detail.Ancestors = append(detail.Ancestors, cur)
		break
	}

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
