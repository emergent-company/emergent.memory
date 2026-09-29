package main

import (
	"context"
	"sort"
	"strings"

	"github.com/emergent-company/go-daisy/render"
	"github.com/labstack/echo/v4"
)

// uiObjectPreviewPartial renders the read-only object summary swapped into the
// global preview drawer (#object-preview-body) when a chat object reference is
// clicked. It reuses the same data seam as the object detail page
// (GetGraphObject + GetCompiledTypes) so the preview never diverges from the
// edit view; failures degrade to a small "unavailable" state rather than
// leaving the drawer blank. The optional ?rel= fragment carries the
// relationship id a `/objects/<src>#relationship-<rel>` reference pointed at,
// which is humanised into a subtle "Referenced via …" line.
func (s *Server) uiObjectPreviewPartial(c echo.Context) error {
	ctx, cancel := context.WithTimeout(c.Request().Context(), optionalFetchTimeout)
	defer cancel()

	id := c.Param("id")
	relID := c.QueryParam("rel")

	obj, err := s.memory.GetGraphObject(ctx, id)
	if err != nil {
		captureError(err)
		render.RenderPartial(c.Response().Writer, c.Request(), objectPreviewNotFound())
		return nil
	}

	var propDefs []objectPropertyDef
	var typeUIByType map[string]typeUI
	compiled, compiledErr := s.memory.GetCompiledTypes(ctx)
	captureError(compiledErr)
	if compiled != nil {
		propDefs = compiledTypePropertyDefs(compiled.ObjectTypes, obj.Type)
		typeUIByType = objectTypeUIMap(compiled.ObjectTypes)
	}

	render.RenderPartial(c.Response().Writer, c.Request(),
		objectPreviewContent(obj, propDefs, typeUIByType, s.objectPreviewRelationshipLabel(ctx, id, relID)))
	return nil
}

// objectPreviewRelationshipLabel resolves a relationship id carried by a chat
// reference to its humanised relationship type, or "" when there is no
// relationship context or it cannot be resolved. Best-effort: a failed edge
// fetch just omits the context line rather than degrading the whole preview.
func (s *Server) objectPreviewRelationshipLabel(ctx context.Context, objectID, relID string) string {
	if relID == "" {
		return ""
	}
	edges, err := s.memory.GetObjectEdges(ctx, objectID)
	if err != nil {
		captureError(err)
		return ""
	}
	for _, e := range edges {
		if e.ID == relID {
			return relationshipTypeLabel(e.Type)
		}
	}
	return ""
}

// objectPreviewProperties lists an object's display properties in key order.
// Unlike graphObjectProperties (which drops empties for the compact list
// render), every non-internal property is returned — including empty ones — so
// the preview shows the full shape of the object. The deferred "hide empties"
// refinement can filter rows by their data-empty marker without changing this.
func objectPreviewProperties(o GraphObject) []propertyKV {
	keys := make([]string, 0, len(o.Properties))
	for k := range o.Properties {
		if strings.HasPrefix(k, "_") {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]propertyKV, 0, len(keys))
	for _, k := range keys {
		out = append(out, propertyKV{Key: k, Value: objectPreviewValue(o.Properties[k])})
	}
	return out
}

// objectPreviewValue renders a stored property value for read-only display.
// Lists join with commas; non-scalars fall back to their JSON form (reusing
// propValues, the same conversion the edit form uses).
func objectPreviewValue(v any) string {
	return strings.Join(propValues(v), ", ")
}

// objectPreviewCreated formats an object's creation timestamp for the preview's
// Details list: relative time when parseable, otherwise the raw value, else "".
func objectPreviewCreated(iso string) string {
	if iso == "" {
		return ""
	}
	return relTime(iso)
}
