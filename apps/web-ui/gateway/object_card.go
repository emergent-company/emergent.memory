package main

import (
	"context"
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"
)

// Device object card (iOS): the JSON twin of the web preview drawer.
//
// The web chat opens an object reference ([Name](/objects/<id>) in assistant
// markdown, or a row of the in-bubble Sources footer) in a read-only drawer
// rendered by uiObjectPreviewPartial. A native client cannot use that HTML, so
// these two routes hand it the same content as data:
//
//	GET /api/objects/:id/card[?rel=<relationship-id>]  ↔  GET /objects/:id/preview
//	GET /api/objects/:id/history                        ↔  GET /objects/:id/history/partial
//
// Every label, field row, ordering rule and change line is computed by the
// helpers the drawer itself renders with (objectPreviewBuiltinFields,
// objectPreviewTypeFields, objectPreviewChanges, ...), so the two surfaces
// cannot drift: a schema title, a folded `status`, or a "No recorded changes"
// note reads the same on the phone as in the browser.
//
// Both routes are read-only and device-reachable (deviceObjectReadPath in
// auth.go). Scoping is the memory call's: the request's credential carries the
// project, exactly as for /api/agents/:id/objects.

// objectCardType is the type chip and icon tile of the card header.
type objectCardType struct {
	// Name is the raw schema type name ("person").
	Name string `json:"name"`
	// Label is the schema's display label, falling back to Name.
	Label string `json:"label"`
	// Icon is the normalized Lucide icon name the web resolves the type's icon
	// to ("user", "file-text"; "box" when undeclared or unknown). A client maps
	// it to its own glyph set. Empty when Glyph is set.
	Icon string `json:"icon,omitempty"`
	// Glyph is a declared non-ASCII icon (an emoji) the web draws as text.
	Glyph string `json:"glyph,omitempty"`
	// Color is the schema-declared CSS color, verbatim; empty when undeclared.
	Color string `json:"color,omitempty"`
}

// objectCardStatus is a status value plus the label and tone the web badge
// uses (boardStatusLabel / boardStatusIntent).
type objectCardStatus struct {
	Value string `json:"value"`
	Label string `json:"label"`
	// Tone is the badge intent without its prefix: success, warning, accent,
	// info, error or neutral.
	Tone string `json:"tone"`
}

// objectCardField is one read-only row: objectPreviewField as JSON. The field
// order matches objectPreviewField so the two convert directly.
type objectCardField struct {
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
	Value       string `json:"value"`
	Name        string `json:"name,omitempty"`
	Empty       bool   `json:"empty,omitempty"`
	Mono        bool   `json:"mono,omitempty"`
}

// objectCardSection is a titled list of rows.
type objectCardSection struct {
	Title  string            `json:"title"`
	Fields []objectCardField `json:"fields"`
}

// objectCardWorkflow is the work strip of a workflow-declaring type. The web
// drawer renders it as editable controls; the card carries it read-only.
type objectCardWorkflow struct {
	Status   *objectCardStatus `json:"status,omitempty"`
	Assignee string            `json:"assignee,omitempty"`
}

// objectCard is the GET /api/objects/:id/card response.
type objectCard struct {
	ID string `json:"id"`
	// CopyID is the identifier the copy control yields: the canonical id, not
	// the version row that happened to be previewed.
	CopyID string `json:"copy_id"`
	// Title is the card heading (graphObjectLabel: key, else type + short id).
	Title   string         `json:"title"`
	Key     string         `json:"key,omitempty"`
	Version int            `json:"version,omitempty"`
	Type    objectCardType `json:"type"`
	// Status is the read-only header badge, set only for a type without a
	// workflow (a workflow type carries it in Workflow instead, as the web
	// drawer moves it into the editable strip).
	Status   *objectCardStatus   `json:"status,omitempty"`
	Workflow *objectCardWorkflow `json:"workflow,omitempty"`
	// ReferencedVia humanises the ?rel= relationship the reference pointed at
	// ("Referenced via works at"); empty without relationship context.
	ReferencedVia string `json:"referenced_via,omitempty"`
	// Sections are the drawer's panels in order: "Properties" (platform
	// fields), then "<Type> Properties" when the type has any.
	Sections []objectCardSection `json:"sections"`
	Labels   []string            `json:"labels"`
}

// objectCardChange is one change line of a history entry (objectChange).
type objectCardChange struct {
	// Kind is add, remove, update, changed or note.
	Kind  string `json:"kind"`
	Label string `json:"label"`
	From  string `json:"from,omitempty"`
	To    string `json:"to,omitempty"`
}

// objectCardVersion is one entry of the version timeline, newest first.
type objectCardVersion struct {
	Version   int                `json:"version"`
	Label     string             `json:"label"`
	Status    *objectCardStatus  `json:"status,omitempty"`
	Key       string             `json:"key,omitempty"`
	CreatedAt string             `json:"created_at,omitempty"`
	Changes   []objectCardChange `json:"changes"`
}

// getObjectCard implements GET /api/objects/:id/card.
func (s *Server) getObjectCard(c echo.Context) error {
	ctx, cancel := context.WithTimeout(c.Request().Context(), optionalFetchTimeout)
	defer cancel()

	obj, status, msg := s.resolveCardObject(ctx, c.Param("id"))
	if obj == nil {
		return c.JSON(status, map[string]string{"error": msg})
	}

	schema := objectPreviewSchema{TypeLabel: obj.Type}
	var typeUIByType map[string]typeUI
	statusControl := &objectStatusControl{}
	compiled, compiledErr := s.memory.GetCompiledTypes(ctx)
	// The card still renders without the schema (raw type name, no declared
	// labels), exactly like the drawer.
	captureError(compiledErr)
	if compiled != nil {
		typeUIByType = objectTypeUIMap(compiled.ObjectTypes)
		statusControl = objectStatusControlFor(compiled.ObjectTypes, obj.Type)
		schema = objectPreviewSchema{
			TypeLabel:   objectPreviewTypeLabel(compiled.ObjectTypes, obj.Type),
			Properties:  compiledTypePropertyDefs(compiled.ObjectTypes, obj.Type),
			HasWorkflow: statusControl.Show,
		}
	}

	return c.JSON(http.StatusOK, buildDeviceObjectCard(obj, typeUIByType, schema,
		s.objectPreviewRelationshipLabel(ctx, obj.ID, c.QueryParam("rel"))))
}

// getObjectCardHistory implements GET /api/objects/:id/history: the version
// timeline the drawer's History section lazy-loads, newest first.
func (s *Server) getObjectCardHistory(c echo.Context) error {
	ctx, cancel := context.WithTimeout(c.Request().Context(), optionalFetchTimeout)
	defer cancel()

	obj, status, msg := s.resolveCardObject(ctx, c.Param("id"))
	if obj == nil {
		return c.JSON(status, map[string]string{"error": msg})
	}
	versions, err := s.memory.GetObjectHistory(ctx, obj.ID)
	if err != nil {
		captureError(err)
		return c.JSON(http.StatusBadGateway, map[string]string{"error": "history unavailable"})
	}
	return c.JSON(http.StatusOK, map[string]any{"versions": buildDeviceObjectHistory(versions)})
}

// resolveCardObject loads the referenced object. A chat reference is an id
// ([Name](/objects/<uuid>), the cited form) or, from older answers, an object
// key; a non-UUID ref is looked up by key. Returns the HTTP status and message
// to answer with when nothing resolves.
func (s *Server) resolveCardObject(ctx context.Context, ref string) (*GraphObject, int, string) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return nil, http.StatusBadRequest, "object reference required"
	}
	if uuidLike(ref) {
		obj, err := s.memory.GetGraphObject(ctx, ref)
		if err == nil && obj != nil {
			return obj, 0, ""
		}
		if err != nil && !isMemoryNotFound(err) {
			captureError(err)
			return nil, http.StatusBadGateway, "memory service unavailable"
		}
		return nil, http.StatusNotFound, "object not found"
	}
	obj, err := s.memory.LookupGraphObjectByKey(ctx, "", ref)
	if err != nil {
		captureError(err)
		return nil, http.StatusBadGateway, "memory service unavailable"
	}
	if obj == nil {
		return nil, http.StatusNotFound, "object not found"
	}
	return obj, 0, ""
}

// buildDeviceObjectCard assembles the card from the drawer's own helpers.
func buildDeviceObjectCard(obj *GraphObject, typeUIByType map[string]typeUI, schema objectPreviewSchema, relLabel string) objectCard {
	tui := typeUIOf(typeUIByType, obj.Type)
	card := objectCard{
		ID:            obj.ID,
		CopyID:        objectPreviewCanonicalID(*obj),
		Title:         graphObjectLabel(*obj),
		Key:           obj.Key,
		Version:       obj.Version,
		Type:          objectCardTypeOf(obj.Type, schema.TypeLabel, tui),
		ReferencedVia: relLabel,
		Labels:        obj.Labels,
	}
	if card.Labels == nil {
		card.Labels = []string{}
	}

	if schema.HasWorkflow {
		wf := &objectCardWorkflow{Assignee: obj.Assignee}
		if obj.Status != "" {
			wf.Status = objectCardStatusOf(obj.Status)
		}
		card.Workflow = wf
	} else if obj.Status != "" {
		card.Status = objectCardStatusOf(obj.Status)
	}

	card.Sections = append(card.Sections, objectCardSection{
		Title:  "Properties",
		Fields: objectCardFields(objectPreviewBuiltinFields(obj)),
	})
	if typeFields := objectPreviewTypeFields(*obj, schema); len(typeFields) > 0 {
		card.Sections = append(card.Sections, objectCardSection{
			Title:  schema.typePanelTitle(),
			Fields: objectCardFields(typeFields),
		})
	}
	return card
}

// buildDeviceObjectHistory maps the version-DESC history onto timeline entries,
// each carrying the change list the drawer expands.
func buildDeviceObjectHistory(versions []GraphObject) []objectCardVersion {
	out := make([]objectCardVersion, 0, len(versions))
	for i, v := range versions {
		entry := objectCardVersion{
			Version:   v.Version,
			Label:     objectPreviewVersionLabel(v.Version),
			Key:       v.Key,
			CreatedAt: v.CreatedAt,
		}
		if v.Status != "" {
			entry.Status = objectCardStatusOf(v.Status)
		}
		for _, ch := range objectPreviewChanges(v, objectPreviewPrevious(versions, i)) {
			entry.Changes = append(entry.Changes, objectCardChange(ch))
		}
		out = append(out, entry)
	}
	return out
}

// objectCardTypeOf resolves the header's type identity the way typeIconTile
// does: a non-ASCII icon is a glyph drawn as text; anything else resolves
// through the supported Lucide set, defaulting to "box".
func objectCardTypeOf(name, label string, tui typeUI) objectCardType {
	t := objectCardType{Name: name, Label: label, Color: strings.TrimSpace(tui.Color)}
	if t.Label == "" {
		t.Label = name
	}
	if cls := typeIconClass(tui.Icon); cls != "" {
		t.Icon = strings.TrimPrefix(cls, "lucide--")
	} else {
		t.Glyph = strings.TrimSpace(tui.Icon)
	}
	return t
}

func objectCardStatusOf(status string) *objectCardStatus {
	return &objectCardStatus{
		Value: status,
		Label: boardStatusLabel(status),
		Tone:  strings.TrimPrefix(string(boardStatusIntent(status)), "badge-"),
	}
}

func objectCardFields(fields []objectPreviewField) []objectCardField {
	out := make([]objectCardField, 0, len(fields))
	for _, f := range fields {
		out = append(out, objectCardField(f))
	}
	return out
}

// uuidLike reports whether ref has the 8-4-4-4-12 hex shape of an object id.
func uuidLike(ref string) bool {
	if len(ref) != 36 {
		return false
	}
	for i, r := range ref {
		switch i {
		case 8, 13, 18, 23:
			if r != '-' {
				return false
			}
		default:
			if !strings.ContainsRune("0123456789abcdefABCDEF", r) {
				return false
			}
		}
	}
	return true
}
