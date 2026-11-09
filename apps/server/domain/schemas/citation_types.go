package schemas

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/emergent-company/emergent.memory/domain/chat/citations"
)

// CitationTypeMetaFromCompiled maps each compiled object/relationship type name
// to its display metadata (declared label and ui accent color) so citations can
// carry type_label/type_color. Only a declared label or color is emitted — a
// type with neither is left out, so citations for it keep empty (omitted)
// label/color and the client falls back to its own humanizer/map.
//
// The first non-shadowed declaration for a name wins; object types are
// processed before relationship types, so an object type takes precedence when
// a name appears in both lists — the same rule the gateway's embedded type map
// follows. Relationship types declare no ui block, so they contribute a label
// only.
func CitationTypeMetaFromCompiled(compiled *CompiledTypesResponse) map[string]citations.TypeMeta {
	if compiled == nil {
		return nil
	}
	m := make(map[string]citations.TypeMeta, len(compiled.ObjectTypes)+len(compiled.RelationshipTypes))
	add := func(name, label, color string) {
		if name == "" || (label == "" && color == "") {
			return
		}
		if _, ok := m[name]; ok {
			return
		}
		m[name] = citations.TypeMeta{Label: label, Color: color}
	}
	for _, t := range compiled.ObjectTypes {
		if t.Shadowed {
			continue
		}
		add(t.Name, t.Label, typeUIColor(t.UI))
	}
	for _, t := range compiled.RelationshipTypes {
		if t.Shadowed {
			continue
		}
		add(t.Name, t.Label, "")
	}
	return m
}

// CitationTypeMeta loads the project's compiled type metadata for citation
// enrichment. Best-effort: a failed read logs and yields nil, so callers keep
// empty (omitted) labels and never fail a turn/transcript read because of it.
func (r *Repository) CitationTypeMeta(ctx context.Context, projectID string) map[string]citations.TypeMeta {
	compiled, err := r.GetCompiledTypesByProject(ctx, projectID)
	if err != nil {
		log := r.log
		if log == nil {
			log = slog.Default()
		}
		log.Warn("failed to load compiled types for citations",
			slog.String("project_id", projectID),
			slog.String("error", err.Error()),
		)
		return nil
	}
	return CitationTypeMetaFromCompiled(compiled)
}

// typeUIColor reads the schema-declared "color" out of an object type's ui
// block (e.g. {"icon":"lucide--note","color":"#4F46E5"}). Returns "" when
// absent or malformed.
func typeUIColor(ui json.RawMessage) string {
	if len(ui) == 0 {
		return ""
	}
	var m map[string]any
	if err := json.Unmarshal(ui, &m); err != nil {
		return ""
	}
	if v, ok := m["color"].(string); ok {
		return v
	}
	return ""
}
