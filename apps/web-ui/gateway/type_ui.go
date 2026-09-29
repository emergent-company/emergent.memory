package main

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/a-h/templ"
)

// typeUI carries one compiled object type's schema-declared ui accent: the
// per-type icon (iconify name like "lucide--note", or a plain glyph such as an
// emoji "📝") and color (an arbitrary CSS color such as "#4F46E5"). Both are
// optional; empty means "not declared, use the generic box".
type typeUI struct {
	Icon  string
	Color string
}

// objectTypeUIMap indexes compiled object types by name, keeping only those
// that declare a ui icon and/or color. Missing names (or a nil input) yield a
// nil map, so lookups fall back to the generic box accent.
func objectTypeUIMap(objectTypes []CompiledType) map[string]typeUI {
	var m map[string]typeUI
	for _, t := range objectTypes {
		ui := typeUI{Icon: compiledTypeIcon(t), Color: compiledTypeColor(t)}
		if ui.Icon == "" && ui.Color == "" {
			continue
		}
		if m == nil {
			m = map[string]typeUI{}
		}
		m[t.Name] = ui
	}
	return m
}

// typeUIOf returns the declared ui accent for name, or the zero typeUI when the
// type is absent from the map (callers then render the generic box icon).
func typeUIOf(m map[string]typeUI, name string) typeUI {
	if m == nil {
		return typeUI{}
	}
	return m[name]
}

// objectTypeUI is one compiled type's client-facing entry in the embedded type
// map: its human display label plus the optional declared ui color. The client
// uses Label to render object "sources" without the raw machine name, and Color
// to tint the type badge (mirroring typeNameChip server-side). Field names are
// the JSON keys the chat client reads.
type objectTypeUI struct {
	Label string `json:"label"`
	Color string `json:"color,omitempty"`
}

// objectTypeMapFor builds the name → {label,color} map embedded in the web
// shell so the client can render human type labels synchronously. Both object
// and relationship types are included (citations can be either). The first
// non-shadowed declaration for a name wins — because object types are processed
// before relationship types, an object type takes precedence when a name
// appears in both lists. Empty and shadowed names are skipped. A nil/empty
// input yields an empty (non-nil) map so the serializer always emits valid JSON.
func objectTypeMapFor(objectTypes, relationshipTypes []CompiledType) map[string]objectTypeUI {
	m := make(map[string]objectTypeUI, len(objectTypes)+len(relationshipTypes))
	add := func(types []CompiledType) {
		for _, t := range types {
			if t.Name == "" || t.Shadowed {
				continue
			}
			if _, ok := m[t.Name]; ok {
				continue
			}
			m[t.Name] = objectTypeUI{
				Label: compiledTypeLabel(t),
				Color: compiledTypeColor(t),
			}
		}
	}
	add(objectTypes)
	add(relationshipTypes)
	return m
}

// objectTypeMapJSON serializes the embedded type map for a JSON <script>
// element. encoding/json HTML-escapes '<', '>' and '&' by default, so the
// result cannot terminate the surrounding <script> or inject markup. On the
// (impossible for this shape) marshal error it degrades to "{}".
func objectTypeMapJSON(m map[string]objectTypeUI) string {
	if len(m) == 0 {
		return "{}"
	}
	b, err := json.Marshal(m)
	if err != nil {
		return "{}"
	}
	return string(b)
}

// objectTypeMapScript renders the embedded type map as a JSON <script> element
// for the shell. templ treats a <script> body as raw text and does not evaluate
// expressions inside it, so the whole element is emitted as a raw component;
// objectTypeMapJSON's HTML escaping makes that safe.
func objectTypeMapScript(m map[string]objectTypeUI) templ.Component {
	return templ.Raw(`<script type="application/json" id="memory-object-types">` + objectTypeMapJSON(m) + `</script>`)
}

// compiledTypesTTL bounds how long a project's compiled type map is cached
// before objectTypeUIMap re-reads GetCompiledTypes. Compiled types change only
// on blueprint apply/unapply (rare and user-driven), so a short TTL keeps the
// map self-healing after an install without wiring invalidation into every
// mutation handler, while taking the serial HTTP GET off the full-page-render
// path.
const compiledTypesTTL = 10 * time.Second

// compiledTypesEntry is one cached objectTypeUIMap result.
type compiledTypesEntry struct {
	ui        map[string]objectTypeUI
	expiresAt time.Time
}

// objectTypeUIMap returns the active project's compiled type name → {label,color}
// map for the chat client's citation sources, cached per project with
// singleflight coalescing of cold/expired misses. Best-effort like the other
// page() fetches: a fetch error returns nil (the client then falls back to
// humanized type names) and is captured for Sentry. Keyed by the same project
// the MemoryClient scopes requests to (session project, else static project).
func (s *Server) objectTypeUIMap(ctx context.Context) map[string]objectTypeUI {
	pid := s.cfg.MemoryProjectID
	if sc, ok := sessionContextFrom(ctx); ok && sc.ProjectID != "" {
		pid = sc.ProjectID
	}
	if pid == "" {
		// No project scope at all: nothing to key or cache on.
		compiled, err := s.memory.GetCompiledTypes(ctx)
		if err != nil {
			captureError(err)
			return nil
		}
		if compiled == nil {
			return nil
		}
		return objectTypeMapFor(compiled.ObjectTypes, compiled.RelationshipTypes)
	}

	// Fast path: a fresh cached entry skips the fetch entirely.
	s.compiledTypesMu.Lock()
	if e, ok := s.compiledTypesCache[pid]; ok && time.Now().Before(e.expiresAt) {
		s.compiledTypesMu.Unlock()
		return e.ui
	}
	s.compiledTypesMu.Unlock()

	// Coalesce concurrent cold/expired misses per project so a burst of full
	// page renders triggers exactly one GetCompiledTypes instead of a stampede.
	v, err, _ := s.compiledTypesGroup.Do(pid, func() (any, error) {
		// Another request may have populated the entry while we waited on the
		// flight — re-check before querying.
		s.compiledTypesMu.Lock()
		if e, ok := s.compiledTypesCache[pid]; ok && time.Now().Before(e.expiresAt) {
			s.compiledTypesMu.Unlock()
			return e.ui, nil
		}
		s.compiledTypesMu.Unlock()

		compiled, err := s.memory.GetCompiledTypes(ctx)
		if err != nil {
			captureError(err)
			return nil, err
		}
		var ui map[string]objectTypeUI
		if compiled != nil {
			ui = objectTypeMapFor(compiled.ObjectTypes, compiled.RelationshipTypes)
		}
		s.compiledTypesMu.Lock()
		if s.compiledTypesCache == nil {
			s.compiledTypesCache = make(map[string]compiledTypesEntry)
		}
		s.compiledTypesCache[pid] = compiledTypesEntry{ui: ui, expiresAt: time.Now().Add(compiledTypesTTL)}
		s.compiledTypesMu.Unlock()
		return ui, nil
	})
	if err != nil {
		return nil
	}
	return v.(map[string]objectTypeUI)
}

// typeColorStyle returns the inline style declarations that tint a chip or tile
// with a schema-declared color: the color's text/border plus a translucent
// background, matching the house "tone/10 bg + tone/15 border" tile look.
// Colors are arbitrary CSS values (hex in practice), so they are applied via a
// style attribute — never a class. Returns "" when no color is declared (the
// caller then keeps its default tone classes). An invalid color value is
// dropped by the browser declaration by declaration, leaving the default look.
func typeColorStyle(color string) string {
	c := strings.TrimSpace(color)
	if c == "" {
		return ""
	}
	return "color:" + c +
		";background-color:color-mix(in oklch," + c + " 10%,transparent)" +
		";border-color:color-mix(in oklch," + c + " 15%,transparent)"
}
