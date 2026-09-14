package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"1.0.0", "1.0.0", 0},
		{"1.0.0", "1.0.1", -1},
		{"1.2.0", "1.10.0", -1}, // numeric, not lexicographic
		{"2.0.0", "1.9.9", 1},
		{"1.0", "1.0.1", -1}, // fewer segments pads with zero
		{"2", "1.9.9", 1},
		{"1.0.0-beta", "1.0.0", 1}, // non-numeric falls back to string compare
		{"v1", "v2", -1},
	}
	for _, c := range cases {
		if got := compareVersions(c.a, c.b); got != c.want {
			t.Errorf("compareVersions(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestSuggestMigration(t *testing.T) {
	hist := func(id, name, ver string, active bool) SchemaHistoryItem {
		return SchemaHistoryItem{SchemaID: id, Name: name, Version: ver, Active: active}
	}
	tests := []struct {
		name     string
		history  []SchemaHistoryItem
		wantFrom string
		wantTo   string
	}{
		{
			name: "active outdated version gets upgrade path",
			history: []SchemaHistoryItem{
				hist("s1", "agent-notes", "1.0.0", true),
				hist("s2", "agent-notes", "1.2.0", false),
				hist("s3", "agent-notes", "1.1.0", false),
			},
			wantFrom: "s1", wantTo: "s2",
		},
		{
			name: "active is highest version, no upgrade path",
			history: []SchemaHistoryItem{
				hist("s1", "agent-notes", "1.0.0", false),
				hist("s2", "agent-notes", "1.1.0", true),
			},
			wantFrom: "", wantTo: "",
		},
		{
			name: "no active version at all",
			history: []SchemaHistoryItem{
				hist("s1", "agent-notes", "1.0.0", false),
				hist("s2", "agent-notes", "1.1.0", false),
			},
			wantFrom: "", wantTo: "",
		},
		{
			name: "first group with an upgrade path wins",
			history: []SchemaHistoryItem{
				hist("b1", "agent-notes", "1.0.0", true),
				hist("b2", "agent-notes", "1.5.0", false),
				hist("a1", "multi-agent", "2.0.0", true), // active == highest, no path
				hist("a2", "multi-agent", "2.1.0", false),
			},
			wantFrom: "b1", wantTo: "b2",
		},
		{
			name: "versions sort numerically",
			history: []SchemaHistoryItem{
				hist("s1", "agent-notes", "1.9.0", true),
				hist("s2", "agent-notes", "1.10.0", false),
			},
			wantFrom: "s1", wantTo: "s2",
		},
		{
			name: "single entry, no group",
			history: []SchemaHistoryItem{
				hist("s1", "agent-notes", "1.0.0", true),
			},
			wantFrom: "", wantTo: "",
		},
		{
			name: "active version equal to highest version id differs, no path",
			history: []SchemaHistoryItem{
				hist("s1", "agent-notes", "1.0.0", true),
				hist("s2", "agent-notes", "1.0.0", false),
			},
			wantFrom: "", wantTo: "",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			from, to := suggestMigration(tc.history)
			if from != tc.wantFrom || to != tc.wantTo {
				t.Errorf("suggestMigration() = (%q, %q), want (%q, %q)", from, to, tc.wantFrom, tc.wantTo)
			}
		})
	}
}

// TestMigrationCardKindFor verifies the top-of-page card classification: an
// upgrade path (distinct from/to) plus stale objects shows the migrate form; a
// from==to pair shows the resync migrate form; stale objects with no usable
// from/to show the neutral fallback card; everything else (clean, or no
// validation data) shows the all-clear card.
func TestMigrationCardKindFor(t *testing.T) {
	stale := &SchemaValidationResult{TotalObjects: 10, StaleObjects: 3}
	clean := &SchemaValidationResult{TotalObjects: 10}
	tests := []struct {
		name string
		data migrationData
		want migrationCardKind
	}{
		{name: "nil validation renders all-clear", data: migrationData{}, want: migrationCardNone},
		{name: "unequal path + stale renders migrate form",
			data: migrationData{FromID: "s1", ToID: "s2", Validation: stale}, want: migrationCardUpgrade},
		{name: "equal ids + stale renders resync form",
			data: migrationData{FromID: "s1", ToID: "s1", Validation: stale}, want: migrationCardResync},
		{name: "stale without from/to renders fallback card",
			data: migrationData{Validation: stale}, want: migrationCardFallback},
		{name: "one-sided id treated as no usable pair",
			data: migrationData{FromID: "s1", Validation: stale}, want: migrationCardFallback},
		{name: "path + clean renders all-clear",
			data: migrationData{FromID: "s1", ToID: "s2", Validation: clean}, want: migrationCardNone},
		{name: "no path + clean renders all-clear",
			data: migrationData{Validation: clean}, want: migrationCardNone},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := migrationCardKindFor(tc.data); got != tc.want {
				t.Errorf("migrationCardKindFor() = %d, want %d", got, tc.want)
			}
		})
	}
}

// TestStaleObjectsMessage verifies the schema-health alert copy is keyed to the
// actual page state: an upgrade path says run a migration, a resync path says
// re-stamp the lagging objects, and the fallback carries neutral drift copy.
// None of the branches promise a /blueprints blueprint-upgrade remedy.
func TestStaleObjectsMessage(t *testing.T) {
	v := &SchemaValidationResult{TotalObjects: 10, StaleObjects: 3}
	prefix := "3 of 10 objects are stale (schema drifted). "
	upgrade := staleObjectsMessage(v, migrationCardUpgrade)
	if !strings.Contains(upgrade, prefix) || !strings.Contains(upgrade, "Run a migration to fix.") {
		t.Errorf("upgrade message = %q", upgrade)
	}
	resync := staleObjectsMessage(v, migrationCardResync)
	if !strings.Contains(resync, prefix) || !strings.Contains(resync, "Objects lag the installed schema version") {
		t.Errorf("resync message = %q", resync)
	}
	fallback := staleObjectsMessage(v, migrationCardFallback)
	if !strings.Contains(fallback, prefix) || !strings.Contains(fallback, "No active schema install is recorded") {
		t.Errorf("fallback message = %q", fallback)
	}
	for _, msg := range []string{upgrade, resync, fallback} {
		if strings.Contains(msg, "Blueprints") || strings.Contains(msg, "upgrade the owning blueprint") {
			t.Errorf("message must not promise a blueprint upgrade: %q", msg)
		}
	}
}

// TestSuggestResync verifies the resync target picker: the SchemaID of the
// most-recently-installed ACTIVE history row.
func TestSuggestResync(t *testing.T) {
	hist := func(id string, active bool, at string) SchemaHistoryItem {
		return SchemaHistoryItem{SchemaID: id, Name: "agent-notes", Version: "1.1.0", Active: active, InstalledAt: at}
	}
	tests := []struct {
		name    string
		history []SchemaHistoryItem
		want    string
	}{
		{name: "no history", history: nil, want: ""},
		{name: "none active", history: []SchemaHistoryItem{
			hist("s1", false, "2026-01-01T00:00:00Z"),
			hist("s2", false, "2026-01-02T00:00:00Z"),
		}, want: ""},
		{name: "single active", history: []SchemaHistoryItem{
			hist("s1", false, "2026-01-01T00:00:00Z"),
			hist("s2", true, "2026-01-02T00:00:00Z"),
		}, want: "s2"},
		{name: "multiple actives picks most recent install",
			history: []SchemaHistoryItem{
				hist("s1", true, "2026-01-03T00:00:00Z"),
				hist("s2", false, "2026-01-02T00:00:00Z"),
				hist("s3", true, "2026-01-05T00:00:00Z"),
			}, want: "s3"},
		{name: "active without timestamp is oldest",
			history: []SchemaHistoryItem{
				hist("s1", true, ""),
				hist("s2", true, "2026-01-01T00:00:00Z"),
			}, want: "s2"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := suggestResync(tc.history); got != tc.want {
				t.Errorf("suggestResync() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestActivePacks verifies the resync target list: only ACTIVE rows, deduped by
// pack name keeping the most recent install, sorted by name.
func TestActivePacks(t *testing.T) {
	history := []SchemaHistoryItem{
		{SchemaID: "s1", Name: "agent-notes", Version: "2.0.0", Active: true, InstalledAt: "2026-01-01T00:00:00Z"},
		{SchemaID: "s2", Name: "agent-notes", Version: "2.1.0", Active: true, InstalledAt: "2026-01-03T00:00:00Z"},
		{SchemaID: "s3", Name: "personal-memory", Version: "1.0.0", Active: true, InstalledAt: "2026-01-02T00:00:00Z"},
		{SchemaID: "s4", Name: "dreaming-agent", Version: "1.0.0", Active: false, InstalledAt: "2026-01-04T00:00:00Z"},
	}
	got := activePacks(history)
	if len(got) != 2 {
		t.Fatalf("activePacks() = %+v, want 2 options", got)
	}
	if got[0].Name != "agent-notes" || got[0].SchemaID != "s2" || got[0].Version != "2.1.0" {
		t.Errorf("first option = %+v, want agent-notes s2 v2.1.0 (most recent)", got[0])
	}
	if got[1].Name != "personal-memory" || got[1].SchemaID != "s3" {
		t.Errorf("second option = %+v, want personal-memory s3", got[1])
	}
	if len(activePacks(nil)) != 0 {
		t.Error("activePacks(nil) should be empty")
	}
}

// TestGroupStaleObjects verifies stale results group by owning pack then type,
// named packs before unresolved, types sorted, and objects sorted by key.
func TestGroupStaleObjects(t *testing.T) {
	typePack := map[string]string{"Note": "agent-notes", "Task": "personal-memory"}
	results := []ObjectValidationResult{
		{EntityID: "e3", Type: "Widget", Key: "w1"},
		{EntityID: "e2", Type: "Note", Key: "b-note"},
		{EntityID: "e1", Type: "Note", Key: "a-note"},
		{EntityID: "e4", Type: "Task", Key: "t1"},
	}
	groups := groupStaleObjects(results, typePack)
	if len(groups) != 3 {
		t.Fatalf("groups = %+v, want 3", groups)
	}
	if groups[0].Pack != "agent-notes" || groups[0].Count != 2 || len(groups[0].Types) != 1 {
		t.Fatalf("group[0] = %+v", groups[0])
	}
	if groups[0].Types[0].Type != "Note" || groups[0].Types[0].Items[0].Key != "a-note" || groups[0].Types[0].Items[1].Key != "b-note" {
		t.Errorf("Note group not sorted by key: %+v", groups[0].Types[0].Items)
	}
	if groups[1].Pack != "personal-memory" || groups[1].Count != 1 {
		t.Errorf("group[1] = %+v, want personal-memory", groups[1])
	}
	if groups[2].Pack != "" || groups[2].Types[0].Type != "Widget" {
		t.Errorf("group[2] = %+v, want unresolved Widget last", groups[2])
	}
	if groupStaleObjects(nil, typePack) != nil {
		t.Error("groupStaleObjects(nil) should be nil")
	}
}

// TestSuggestResyncPack verifies the resync target is the active pack owning the
// most stale objects, tie-broken by most recent install, and zero when nothing
// maps to a pack (caller falls back).
func TestSuggestResyncPack(t *testing.T) {
	packs := []activePackOption{
		{SchemaID: "s1", Name: "agent-notes", InstalledAt: "2026-01-01T00:00:00Z"},
		{SchemaID: "s3", Name: "personal-memory", InstalledAt: "2026-01-05T00:00:00Z"},
	}
	tests := []struct {
		name   string
		groups []stalePackGroup
		want   string
	}{
		{name: "no groups", groups: nil, want: ""},
		{name: "unresolved only", groups: []stalePackGroup{{Pack: "", Count: 5}}, want: ""},
		{name: "most stale wins", groups: []stalePackGroup{
			{Pack: "agent-notes", Count: 2},
			{Pack: "personal-memory", Count: 1},
		}, want: "s1"},
		{name: "tie broken by most recent", groups: []stalePackGroup{
			{Pack: "agent-notes", Count: 1},
			{Pack: "personal-memory", Count: 1},
		}, want: "s3"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := suggestResyncPack(tc.groups, packs).SchemaID; got != tc.want {
				t.Errorf("suggestResyncPack() = %q, want %q", got, tc.want)
			}
		})
	}
	if got := suggestResyncPack([]stalePackGroup{{Pack: "agent-notes", Count: 1}}, nil); got.SchemaID != "" {
		t.Errorf("no active packs should return zero option, got %+v", got)
	}
}

// TestStaleIssueLabel verifies the drift-table issue classification for the
// strings memory actually emits (NULL stamp vs stamp lag) plus content write.
func TestStaleIssueLabel(t *testing.T) {
	cases := []struct{ in, want string }{
		{"schema_version not set (current: 2.0.0)", "NULL stamp"},
		{`schema_version mismatch: object has "1.0.0", current is "2.0.0"`, "stamp lag"},
		{"content written without a schema_version", "content write"},
		{"something entirely different", "something entirely different"},
	}
	for _, c := range cases {
		if got := staleIssueLabel(c.in); got != c.want {
			t.Errorf("staleIssueLabel(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestMigrationsDataRetainsStaleDetail verifies migrationsData keeps memory's
// per-object results (grouped by owning pack via compiled-types) and offers the
// active packs as resync targets, suggesting the pack owning the most drift.
func TestMigrationsDataRetainsStaleDetail(t *testing.T) {
	f := &fakeMemory{
		history: []SchemaHistoryItem{
			{SchemaID: "s1", Name: "agent-notes", Version: "2.0.0", Active: true, InstalledAt: "2026-01-01T00:00:00Z"},
			{SchemaID: "s3", Name: "personal-memory", Version: "1.0.0", Active: true, InstalledAt: "2026-01-05T00:00:00Z"},
		},
		validation: &SchemaValidationResult{
			TotalObjects: 10, StaleObjects: 3,
			Results: []ObjectValidationResult{
				{EntityID: "e1", Type: "Note", Key: "note-1", Issues: []string{"schema_version not set (current: 2.0.0)"}},
				{EntityID: "e2", Type: "Note", Key: "note-2", Issues: []string{`schema_version mismatch: object has "1.0.0", current is "2.0.0"`}},
				{EntityID: "e3", Type: "Task", Key: "task-1", Issues: []string{"schema_version not set (current: 1.0.0)"}},
			},
		},
		compiled: &CompiledSchemaTypes{ObjectTypes: []CompiledType{
			{Name: "Note", SchemaName: "agent-notes"},
			{Name: "Task", SchemaName: "personal-memory"},
		}},
	}
	s := &Server{memory: f}
	e := echo.New()
	c := e.NewContext(httptest.NewRequest(http.MethodGet, "/blueprints/migrations", nil), httptest.NewRecorder())
	data := s.migrationsData(c, f.history)

	if len(data.StaleGroups) != 2 {
		t.Fatalf("StaleGroups = %+v, want 2 pack groups (agent-notes, personal-memory)", data.StaleGroups)
	}
	if data.StaleGroups[0].Pack != "agent-notes" || data.StaleGroups[0].Count != 2 {
		t.Errorf("StaleGroups[0] = %+v, want agent-notes x2", data.StaleGroups[0])
	}
	if len(data.ActivePacks) != 2 {
		t.Errorf("ActivePacks = %+v, want 2", data.ActivePacks)
	}
	if data.SuggestedPack != "agent-notes" || data.FromID != "s1" || data.ToID != "s1" {
		t.Errorf("suggested resync = pack %q from %q to %q, want agent-notes s1==s1", data.SuggestedPack, data.FromID, data.ToID)
	}
}

// TestMigrationsPageStaleDetailRender verifies the Schema health section renders
// the grouped per-object detail: pack heading, type, object key, and issue.
func TestMigrationsPageStaleDetailRender(t *testing.T) {
	data := migrationData{
		Validation:    &SchemaValidationResult{TotalObjects: 10, StaleObjects: 2},
		FromID:        "s1",
		ToID:          "s1",
		SuggestedPack: "agent-notes",
		ActivePacks:   []activePackOption{{SchemaID: "s1", Name: "agent-notes", Version: "2.0.0"}},
		StaleGroups: []stalePackGroup{{
			Pack: "agent-notes", Count: 2,
			Types: []staleTypeGroup{{Type: "Note", Items: []ObjectValidationResult{
				{EntityID: "e1", Type: "Note", Key: "note-1", Issues: []string{`schema_version mismatch: object has "1.0.0", current is "2.0.0"`}},
				{EntityID: "e2", Type: "Note", Key: "note-2", Issues: []string{"schema_version not set (current: 2.0.0)"}},
			}}},
		}},
	}
	html := renderHTML(t, MigrationsPage(data))
	for _, want := range []string{
		"Schema health",
		"agent-notes",
		"2 stale",
		"Note",
		"note-1",
		"note-2",
		"stamp lag",
		"NULL stamp",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("Schema health detail missing %q", want)
		}
	}
}

// TestMigrationsPageResyncPackPicker verifies the resync form is pack-aware:
// with more than one active pack the user picks the pack to re-stamp; with a
// single active pack the target pack is labelled and from/to selects stay.
func TestMigrationsPageResyncPackPicker(t *testing.T) {
	base := migrationData{
		Validation:    &SchemaValidationResult{TotalObjects: 10, StaleObjects: 2},
		FromID:        "s1",
		ToID:          "s1",
		SuggestedPack: "agent-notes",
	}
	multi := base
	multi.ActivePacks = []activePackOption{
		{SchemaID: "s1", Name: "agent-notes", Version: "2.0.0"},
		{SchemaID: "s3", Name: "personal-memory", Version: "1.0.0"},
	}
	html := renderHTML(t, MigrationsPage(multi))
	for _, want := range []string{
		`name="resyncPack"`,
		"Pack to re-stamp",
		"agent-notes v2.0.0 (active)",
		"personal-memory v1.0.0 (active)",
		`value="preview"`,
		`value="execute"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("multi-pack resync form missing %q", want)
		}
	}
	if strings.Contains(html, `name="from"`) {
		t.Error("multi-pack resync should not also render from/to selects")
	}

	single := base
	single.ActivePacks = []activePackOption{{SchemaID: "s1", Name: "agent-notes", Version: "2.0.0"}}
	htmlSingle := renderHTML(t, MigrationsPage(single))
	if strings.Contains(htmlSingle, `name="resyncPack"`) {
		t.Error("single-pack resync should not render a picker")
	}
	for _, want := range []string{"Suggested target:", "agent-notes", `name="from"`, `name="to"`} {
		if !strings.Contains(htmlSingle, want) {
			t.Errorf("single-pack resync form missing %q", want)
		}
	}
}

// TestUIMigrateResyncPackGating verifies a pack-targeted resync posts that pack
// as from==to to preview, that a bare submit previews (never executes), and
// that Execute remains behind the explicit action.
func TestUIMigrateResyncPackGating(t *testing.T) {
	f := &fakeMemory{history: []SchemaHistoryItem{
		{SchemaID: "s3", Name: "personal-memory", Version: "1.0.0", Active: true, InstalledAt: "2026-01-05T00:00:00Z"},
	}}
	s := &Server{memory: f}
	e := echo.New()
	e.POST("/blueprints/migrate", s.uiMigrate)

	post := func(form url.Values) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, "/blueprints/migrate", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		return rec
	}

	// Bare submit (no action) is a preview: 200 with the dry-run plan, never a
	// redirect that would signal an execute round-trip.
	rec := post(url.Values{"resyncPack": {"s3"}})
	if rec.Code != http.StatusOK {
		t.Fatalf("bare submit status = %d, want 200 (preview)", rec.Code)
	}
	if f.migrationFrom != "s3" || f.migrationTo != "s3" {
		t.Errorf("preview target = (%q, %q), want s3==s3", f.migrationFrom, f.migrationTo)
	}
	if body := rec.Body.String(); !strings.Contains(body, "Dry-run plan") {
		t.Error("bare submit must render the preview plan")
	}

	// Explicit execute still posts the chosen pack as from==to and redirects.
	rec = post(url.Values{"resyncPack": {"s3"}, "action": {"execute"}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("execute status = %d, want 303", rec.Code)
	}
	if f.migrationFrom != "s3" || f.migrationTo != "s3" {
		t.Errorf("execute target = (%q, %q), want s3==s3", f.migrationFrom, f.migrationTo)
	}
}
