package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

func TestCompiledTypeCountLabel(t *testing.T) {
	cases := []struct {
		name string
		in   *CompiledSchemaTypes
		want string
	}{
		{"nil", nil, "0 types"},
		{"empty", &CompiledSchemaTypes{}, "0 types"},
		{"one", &CompiledSchemaTypes{ObjectTypes: []CompiledType{{Name: "person"}}}, "1 type"},
		{"three", &CompiledSchemaTypes{
			ObjectTypes:       []CompiledType{{Name: "person"}, {Name: "task"}},
			RelationshipTypes: []CompiledType{{Name: "assigned_to"}},
		}, "3 types"},
	}
	for _, c := range cases {
		if got := compiledTypeCountLabel(c.in); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestVisibleCompiledTypesDropsShadowed(t *testing.T) {
	types := []CompiledType{
		{Name: "note", SchemaName: "agent-notes", SchemaVersion: "2.0.0", Shadowed: true},
		{Name: "note", SchemaName: "project-schema-overrides", SchemaVersion: "1.0.0"},
		{Name: "person"},
	}
	got := visibleCompiledTypes(types)
	if len(got) != 2 {
		t.Fatalf("visible = %+v, want 2 (shadowed note dropped)", got)
	}
	if got[0].Name != "note" || got[0].SchemaName != "project-schema-overrides" {
		t.Errorf("winner = %+v, want the non-shadowed note", got[0])
	}
	if got[1].Name != "person" {
		t.Errorf("order not preserved: %+v", got)
	}

	if all := visibleCompiledTypes([]CompiledType{{Name: "x", Shadowed: true}}); len(all) != 0 {
		t.Errorf("all-shadowed = %+v, want empty", all)
	}
}

func TestCompiledTypeSchemaLabel(t *testing.T) {
	cases := []struct {
		name string
		in   CompiledType
		want string
	}{
		{"empty", CompiledType{}, "—"},
		{"no version", CompiledType{SchemaName: "personal-memory"}, "personal-memory"},
		{"with version", CompiledType{SchemaName: "personal-memory", SchemaVersion: "1.0.0"}, "personal-memory v1.0.0"},
	}
	for _, c := range cases {
		if got := compiledTypeSchemaLabel(c.in); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestCompiledTypeLabel(t *testing.T) {
	cases := []struct {
		name string
		in   CompiledType
		want string
	}{
		{"label", CompiledType{Name: "person", Label: "Person"}, "Person"},
		{"fallback", CompiledType{Name: "person"}, "person"},
	}
	for _, c := range cases {
		if got := compiledTypeLabel(c.in); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestObjectTypeLabel(t *testing.T) {
	if got := objectTypeLabel(&ObjectTypeDetail{Name: "person", Label: "Person"}); got != "Person" {
		t.Errorf("label = %q, want Person", got)
	}
	if got := objectTypeLabel(&ObjectTypeDetail{Name: "person"}); got != "person" {
		t.Errorf("fallback = %q, want person", got)
	}
	if got := objectTypeLabel(nil); got != "" {
		t.Errorf("nil = %q, want empty", got)
	}
}

func TestRenderSchemaPage(t *testing.T) {
	compiled := &CompiledSchemaTypes{
		ObjectTypes: []CompiledType{
			{Name: "person", Label: "Person", Description: "a person", SchemaName: "personal-memory", SchemaVersion: "1.0.0"},
		},
		RelationshipTypes: []CompiledType{
			{Name: "assigned_to", SourceType: "task", TargetType: "person", SchemaName: "personal-memory"},
		},
	}
	html := renderHTML(t, SchemaPage(&SchemaListView{Compiled: compiled}, nil))
	for _, want := range []string{
		"Schema", "person", "Person", "assigned_to", "task → person",
		"Object types", "Relationship types",
		"personal-memory v1.0.0",
		`href="/schema/object-types/person"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("schema page missing %q", want)
		}
	}

	htmlEmpty := renderHTML(t, SchemaPage(nil, nil))
	if !strings.Contains(htmlEmpty, "No compiled types") {
		t.Error("empty state missing")
	}

	htmlErr := renderHTML(t, SchemaPage(nil, errTest))
	if !strings.Contains(htmlErr, "Failed to load schema") {
		t.Error("error state missing")
	}
}

func TestRenderSchemaObjectTypePage(t *testing.T) {
	detail := &ObjectTypeDetail{
		Name:        "person",
		Label:       "Person",
		Description: "a person",
		Properties: []PropertyDetail{
			{Name: "first_name", Type: "string", Description: "First name"},
			{Name: "active", Type: "boolean", Required: true},
		},
	}
	rels := []RelationshipTypeDetail{{Name: "assigned_to", SourceType: "task", TargetType: "person"}}
	html := renderHTML(t, SchemaObjectTypePage(&SchemaObjectTypeView{Detail: detail, Relationships: rels}, nil))
	for _, want := range []string{
		"Object type", "Person", "a person", "Properties",
		"first_name", "string", "required",
		"Relationships", "assigned_to", "task → person",
		`href="/schema"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("object type page missing %q", want)
		}
	}

	htmlErr := renderHTML(t, SchemaObjectTypePage(nil, errTest))
	if !strings.Contains(htmlErr, "Object type unavailable") {
		t.Error("error state missing")
	}
}

func TestUIRouteSchema(t *testing.T) {
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: &fakeMemory{compiled: &CompiledSchemaTypes{ObjectTypes: []CompiledType{{Name: "person"}}}}}
	e := echo.New()
	e.GET("/schema", s.uiSchema)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/schema", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "person") {
		t.Errorf("schema page missing person: %s", rec.Body.String())
	}
}

func TestUIRouteSchemaHidesShadowedTypes(t *testing.T) {
	// Real case: agent-notes 2.0.0 and project-schema-overrides 1.0.0 both
	// declare Note; the earlier pack is marked shadowed and must not render.
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: &fakeMemory{compiled: &CompiledSchemaTypes{
		ObjectTypes: []CompiledType{
			{Name: "note", SchemaName: "agent-notes", SchemaVersion: "2.0.0", Shadowed: true},
			{Name: "note", SchemaName: "project-schema-overrides", SchemaVersion: "1.0.0"},
		},
		RelationshipTypes: []CompiledType{
			{Name: "annotates", SourceType: "note", TargetType: "note", Shadowed: true},
		},
	}}}
	e := echo.New()
	e.GET("/schema", s.uiSchema)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/schema", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "project-schema-overrides") {
		t.Errorf("winning note missing from schema page: %s", body)
	}
	if strings.Contains(body, "agent-notes") {
		t.Errorf("shadowed note (agent-notes) must not render: %s", body)
	}
	if strings.Contains(body, "annotates") {
		t.Errorf("shadowed relationship must not render: %s", body)
	}
}

func TestUIRouteSchemaObjectType(t *testing.T) {
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: &fakeMemory{compiled: &CompiledSchemaTypes{
		ObjectTypes: []CompiledType{
			{Name: "person", Label: "Person", Properties: json.RawMessage(`{"first_name":{"type":"string"}}`)},
		},
		RelationshipTypes: []CompiledType{
			{Name: "assigned_to", SourceType: "task", TargetType: "person"},
		},
	}}}
	e := echo.New()
	e.GET("/schema/object-types/:name", s.uiSchemaObjectType)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/schema/object-types/person", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{"first_name", "assigned_to"} {
		if !strings.Contains(body, want) {
			t.Errorf("object type page missing %q: %s", want, body)
		}
	}
}

func TestRenderSchemaPageTypeUI(t *testing.T) {
	// declared icon/color renders on the type list row; undeclared types keep
	// the plain text row (no tile) exactly as before.
	compiled := &CompiledSchemaTypes{
		ObjectTypes: []CompiledType{
			{Name: "note", Label: "Note", Description: "a note", UI: json.RawMessage(`{"icon":"📝","color":"#4F46E5"}`), SchemaName: "agent-notes"},
			{Name: "plain", Label: "Plain", UI: json.RawMessage(`{"color":"#7C3AED"}`)},
			{Name: "bare"},
		},
	}
	html := renderHTML(t, SchemaPage(&SchemaListView{Compiled: compiled}, nil))
	for _, want := range []string{"📝", "color:#4F46E5", "color:#7C3AED"} {
		if !strings.Contains(html, want) {
			t.Errorf("schema page type rows missing %q in:\n%s", want, html)
		}
	}
	// color-only type tints the generic box icon; emoji row renders no box
	if !strings.Contains(html, "lucide--box") {
		t.Error("color-only type row should keep the box icon, tinted")
	}
}

func TestRenderSchemaObjectTypePageTypeUI(t *testing.T) {
	detail := &ObjectTypeDetail{Name: "note", Label: "Note"}
	rels := []RelationshipTypeDetail{{Name: "annotates", SourceType: "note", TargetType: "document"}}
	html := renderHTML(t, SchemaObjectTypePage(&SchemaObjectTypeView{Detail: detail, Relationships: rels, Icon: "📝", Color: "#4F46E5"}, nil))
	for _, want := range []string{"Object type", "note", "📝", "color:#4F46E5", "text-base-content"} {
		if !strings.Contains(html, want) {
			t.Errorf("object type page header chip missing %q in:\n%s", want, html)
		}
	}
	// emoji chip carries no iconify box fallback (the box on this page is the
	// "No properties" empty-state icon, which renders at a different size)
	if strings.Contains(html, "iconify lucide--box") {
		t.Errorf("declared-emoji header chip must not render the box icon in:\n%s", html)
	}
}

func TestRenderObjectsTypeIcons(t *testing.T) {
	objects := []GraphObject{
		{ID: "o1", Type: "note", Key: "shopping", CreatedAt: "2026-08-26T10:00:00Z"},
		{ID: "o2", Type: "plain", Key: "other", CreatedAt: "2026-08-26T11:00:00Z"},
	}
	uiByType := map[string]typeUI{
		"note":  {Icon: "📝", Color: "#4F46E5"},
		"plain": {Color: "#7C3AED"},
	}
	html := renderHTML(t, ObjectsPage(objects, nil, "", nil, "", uiByType, nil))
	for _, want := range []string{"shopping", "other", "📝", "color:#4F46E5", "color:#7C3AED"} {
		if !strings.Contains(html, want) {
			t.Errorf("objects page rows missing %q in:\n%s", want, html)
		}
	}

	// object rows without any type declaration keep the plain box tile
	htmlPlain := renderHTML(t, ObjectsPage(objects, nil, "", nil, "", nil, nil))
	if !strings.Contains(htmlPlain, "lucide--box") {
		t.Errorf("undeclared rows should keep the box tile in:\n%s", htmlPlain)
	}
}

func TestRenderObjectDetailTypeIcon(t *testing.T) {
	obj := &GraphObject{ID: "o1", Type: "note", Key: "shopping"}
	html := renderHTML(t, ObjectDetailPage(obj, nil, nil, nil, nil, nil, "", nil, nil, nil, map[string]typeUI{
		"note": {Icon: "📝", Color: "#4F46E5"},
	}))
	for _, want := range []string{"note", "📝", "color:#4F46E5", "text-base-content"} {
		if !strings.Contains(html, want) {
			t.Errorf("object detail header chip missing %q in:\n%s", want, html)
		}
	}
	if strings.Contains(html, "badge-primary") {
		t.Errorf("colored chip should drop the daisy variant class in:\n%s", html)
	}
}

func TestCompiledTypeUI(t *testing.T) {
	cases := []struct {
		name  string
		ui    json.RawMessage
		icon  string
		color string
	}{
		{"none", nil, "", ""},
		{"empty", json.RawMessage(`{}`), "", ""},
		{"icon and color", json.RawMessage(`{"icon":"📝","color":"#4F46E5"}`), "📝", "#4F46E5"},
		{"lucide icon", json.RawMessage(`{"icon":"lucide--note"}`), "lucide--note", ""},
		{"non-object", json.RawMessage(`["x"]`), "", ""},
		{"non-string values", json.RawMessage(`{"icon":1,"color":false}`), "", ""},
	}
	for _, c := range cases {
		ct := CompiledType{Name: "note", UI: c.ui}
		if got := compiledTypeIcon(ct); got != c.icon {
			t.Errorf("%s: icon = %q, want %q", c.name, got, c.icon)
		}
		if got := compiledTypeColor(ct); got != c.color {
			t.Errorf("%s: color = %q, want %q", c.name, got, c.color)
		}
	}
}

// TestFindCompiledObjectTypePrefersWinner guards the shadowed-override bug: a
// project-owned override pack shadows the blueprint pack of the same type name,
// so compiled-types lists both. The detail/editor pages must resolve the
// effective (non-shadowed) winner, not the first (shadowed) entry.
func TestFindCompiledObjectTypePrefersWinner(t *testing.T) {
	compiled := &CompiledSchemaTypes{ObjectTypes: []CompiledType{
		{Name: "Person", Description: "blueprint", Shadowed: true},
		{Name: "Person", Description: "override", Shadowed: false},
		{Name: "Task", Description: "task", Shadowed: false},
	}}
	if got := findCompiledObjectType(compiled, "Person"); got == nil || got.Description != "override" {
		t.Fatalf("Person = %+v, want the non-shadowed override", got)
	}
	if got := findCompiledObjectType(compiled, "Missing"); got != nil {
		t.Fatalf("Missing = %+v, want nil", got)
	}
	if got := findCompiledObjectType(nil, "Person"); got != nil {
		t.Fatalf("nil compiled = %+v, want nil", got)
	}

	// No winner: fall back to the shadowed entry rather than nil.
	onlyShadowed := &CompiledSchemaTypes{ObjectTypes: []CompiledType{{Name: "Person", Description: "lost", Shadowed: true}}}
	if got := findCompiledObjectType(onlyShadowed, "Person"); got == nil || got.Description != "lost" {
		t.Fatalf("only-shadowed Person = %+v, want the shadowed fallback", got)
	}
}
