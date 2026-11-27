package blueprints

import (
	"context"
	"strings"

	"github.com/emergent-company/emergent.memory/domain/catalog"
)

// ResolveCatalogTypes runs lexical resolution against the built-in catalog.
func (s *Service) ResolveCatalogTypes(query string, limit int) ([]catalog.TypeMatch, error) {
	cat, err := catalog.Load()
	if err != nil {
		return nil, err
	}
	return catalog.ResolveTypes(cat, query, limit), nil
}

// ResolveCatalogSemantic runs embedding-based resolution using the project's
// embedding model. Returns catalog.ErrEmbeddingsUnavailable when unavailable.
func (s *Service) ResolveCatalogSemantic(ctx context.Context, query string, limit int) ([]catalog.TypeMatch, error) {
	cat, err := catalog.Load()
	if err != nil {
		return nil, err
	}
	return catalog.Semantic(ctx, cat, s.catalogEmbedder, query, limit)
}

// ResolveCatalogHybrid fuses lexical and semantic resolution, degrading to
// lexical when the embedder is unavailable or the call fails.
func (s *Service) ResolveCatalogHybrid(ctx context.Context, query string, limit int) ([]catalog.TypeMatch, error) {
	cat, err := catalog.Load()
	if err != nil {
		return nil, err
	}
	return catalog.Hybrid(ctx, cat, s.catalogEmbedder, query, limit)
}

// ListBuiltinRelationshipTypes returns the built-in catalog's relationship
// vocabulary (unique by name) as manifest definitions, so discovery's
// relationship phase can prefer a canonical edge name (works_for, located_at, …)
// over an invented synonym. Endpoints are carried as a hint, not a constraint.
func ListBuiltinRelationshipTypes() ([]RelationshipTypeDef, error) {
	cat, err := catalog.Load()
	if err != nil {
		return nil, err
	}
	seen := make(map[string]bool, len(cat.Relationships))
	out := make([]RelationshipTypeDef, 0, len(cat.Relationships))
	for _, r := range cat.Relationships {
		if r.Name == "" || seen[r.Name] {
			continue
		}
		seen[r.Name] = true
		out = append(out, RelationshipTypeDef{
			Name:        r.Name,
			Label:       r.Label,
			Description: r.Description,
			SourceType:  r.SourceType,
			TargetType:  r.TargetType,
		})
	}
	return out, nil
}

// CatalogDetail loads the built-in catalog and returns one type's detail.
func (s *Service) CatalogDetail(name string) (*catalog.TypeDetail, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, nil
	}
	cat, err := catalog.Load()
	if err != nil {
		return nil, err
	}
	detail, ok := catalog.Detail(cat, name)
	if !ok {
		return nil, nil
	}
	return detail, nil
}
