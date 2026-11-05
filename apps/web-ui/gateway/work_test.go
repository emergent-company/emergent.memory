package main

import (
	"strings"
	"testing"
)

func TestBuildWorkMatrixBuckets(t *testing.T) {
	compiled := &CompiledSchemaTypes{ObjectTypes: []CompiledType{
		{Name: "Task", BoardEnabled: true, AllowedStatuses: []string{"draft", "review", "done"}},
	}}
	items := []WorkItem{
		{Type: "Task", CanonicalID: "t1", Status: "draft", IsWorking: true, LatestRunStatus: "running"},
		{Type: "Task", CanonicalID: "t2", Status: "draft", IsWorking: true, LatestRunStatus: "queued"},
		{Type: "Task", CanonicalID: "t3", Status: "draft", IsWorking: true, LatestRunStatus: "queued", NextRunAt: "2026-01-01T00:00:00Z"},
		{Type: "Task", CanonicalID: "t4", Status: "draft", Blocked: true, BlockReason: "missing"},
		{Type: "Task", CanonicalID: "t5", Status: "review", NeedsReview: true},
		{Type: "Task", CanonicalID: "t6", Status: "done"},
		{Type: "Task", CanonicalID: "t7", Status: "suggested"},
		{Type: "Other", CanonicalID: "o1", Status: "draft"},
	}
	m := buildWorkMatrix("Task", items, compiled)
	if m.Total != 7 {
		t.Fatalf("total = %d, want 7 (other type excluded)", m.Total)
	}
	if len(m.Columns) != 4 || m.Columns[0].Status != "" || m.Columns[0].Label != "No status" || m.Columns[0].Count != 1 {
		t.Fatalf("columns = %#v", m.Columns)
	}
	if m.Columns[1].Status != "draft" || m.Columns[1].Count != 4 {
		t.Fatalf("draft column = %#v", m.Columns[1])
	}
	want := map[string]map[string]int{
		"working":  {"draft": 1},
		"queued":   {"draft": 1},
		"retrying": {"draft": 1},
		"blocked":  {"draft": 1},
		"review":   {"review": 1},
		"idle":     {"done": 1, "": 1},
	}
	seen := map[string]int{}
	for _, g := range m.Groups {
		for _, row := range g.Rows {
			for status, cell := range row.Cells {
				seen[row.Key+"/"+status] = len(cell)
			}
		}
	}
	for row, cells := range want {
		for status, n := range cells {
			if seen[row+"/"+status] != n {
				t.Fatalf("%s/%s = %d, want %d (all=%v)", row, status, seen[row+"/"+status], n, seen)
			}
		}
	}
	// Every item appears exactly once.
	if got := workColumnItems(m, "draft"); len(got) != 4 {
		t.Fatalf("draft column = %d items, want 4", len(got))
	}
	if got := workColumnItems(m, ""); len(got) != 1 || got[0].Status != "suggested" {
		t.Fatalf("no-status column = %#v", got)
	}
}

func TestRulesForDefinition(t *testing.T) {
	def := &AgentDefinition{
		ID: "a1", Name: "worker",
		WorkConfig: &AgentWorkConfig{ObjectRules: []AgentObjectRule{{ObjectType: "Task"}}},
	}
	rows := rulesForDefinition(def)
	if len(rows) != 1 || rows[0].AgentName != "worker" || rows[0].ObjectType != "Task" {
		t.Fatalf("rows = %#v", rows)
	}
}

func TestWorkRuleEditHref(t *testing.T) {
	got := workRuleEditHref(workRuleRow{AgentID: "a 1", ObjectType: "Task/One"})
	want := "/work/settings/rules/edit?agent=a+1&type=Task%2FOne"
	if got != want {
		t.Fatalf("href = %q, want %q", got, want)
	}
}

func TestWorkRulePageRenders(t *testing.T) {
	html := renderHTML(t, WorkRulePage(workRuleFormData{
		Mode:            "new",
		Agents:          []AgentDefinitionSummary{{ID: "a1", Name: "worker"}},
		ObjectTypes:     []CompiledType{{Name: "Task"}},
		SelectedAgentID: "",
		SelectedType:    "",
		Rule:            AgentObjectRule{CanCreate: boolPtr(true), CanUpdate: boolPtr(true), CanDelete: boolPtr(true)},
	}))
	for _, want := range []string{"New rule", "Create rule", "worker", "Task", `name="mode"`, `name="ruleTriggers"`, "Cancel"} {
		if !strings.Contains(html, want) {
			t.Errorf("new rule page missing %q", want)
		}
	}
}

func TestWorkPageHasNoInPageTabs(t *testing.T) {
	html := renderHTML(t, WorkPage(workData{Section: workSettingsSection}))
	for _, unwanted := range []string{`>Board</a>`, `>Settings</a>`} {
		if strings.Contains(html, unwanted) {
			t.Errorf("Work page still renders in-page tab link %q", unwanted)
		}
	}
}

func TestResolveWorkTypeFilter(t *testing.T) {
	board := []CompiledType{{Name: "Bug"}, {Name: "Task"}}
	if got := resolveWorkTypeFilter(board, nil, false); len(got) != 2 || got[0] != "Bug" || got[1] != "Task" {
		t.Fatalf("all-types filter = %#v", got)
	}
	if got := resolveWorkTypeFilter(board, []string{"Task", "Task", "Nope"}, true); len(got) != 1 || got[0] != "Task" {
		t.Fatalf("explicit filter = %#v", got)
	}
	if got := resolveWorkTypeFilter(board, nil, true); len(got) != 0 {
		t.Fatalf("empty explicit filter = %#v", got)
	}
}

func TestWorkBoardReturnPath(t *testing.T) {
	if got := workBoardReturnPath(nil); got != "/work/board" {
		t.Fatalf("all types path = %q", got)
	}
	got := workBoardReturnPath([]string{"Task", "Bug"})
	if !strings.Contains(got, "filter=1") || !strings.Contains(got, "type=Task") || !strings.Contains(got, "type=Bug") {
		t.Fatalf("filtered path = %q", got)
	}
}

func TestWorkTypeFilterRendersIconsAndColors(t *testing.T) {
	uiJSON := []byte(`{"icon":"lucide--file-text","color":"#ff0000"}`)
	data := workData{
		BoardTypes: []CompiledType{
			{Name: "Task", Label: "Task", UI: uiJSON},
			{Name: "Bug"},
		},
		TypeCounts:    map[string]int{"Task": 2},
		SelectedTypes: []string{"Task", "Bug"},
	}
	html := renderHTML(t, workTypeFilter(data))
	for _, want := range []string{"Task", "Bug", "file-text", "#ff0000", "lucide--box", "All types"} {
		if !strings.Contains(html, want) {
			t.Errorf("filter missing %q", want)
		}
	}
}

func TestWorkStatusClasses(t *testing.T) {
	if workStatusAccentClass("done") != "border-l-success" {
		t.Fatal("done accent")
	}
	if workStatusDotClass("review") != "bg-warning" {
		t.Fatal("review dot")
	}
	if workStatusAccentClass("custom") != "border-l-base-content/20" {
		t.Fatal("custom accent must be neutral")
	}
}

func TestWorkCardIsInteractive(t *testing.T) {
	html := renderHTML(t, workCard(WorkItem{
		CanonicalID:        "c1",
		Key:                "article-1",
		Type:               "Article",
		Status:             "review",
		Assignee:           "Ada Lovelace",
		IsWorking:          true,
		LatestRunStatus:    "running",
		LatestRunAgentName: "editor-agent",
	}, []string{"Article"}))
	for _, want := range []string{
		`data-board-open-preview`,
		`data-board-card`,
		`draggable="true"`,
		`data-board-status="review"`,
		`border-l-warning`,
		"Ada Lovelace",
		"editor-agent",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("work card missing %q", want)
		}
	}
	if strings.Contains(html, `Label: "review"`) || strings.Contains(html, `>review<`) {
		t.Error("card must not render a review badge")
	}
}

func TestWorkLanePrefs(t *testing.T) {
	data := workData{ScopeBase: "/o/o1/p/p1", LaneOpen: map[string]bool{"/o/o1/p/p1|Task|idle": true, "/o/o1/p/p1|Task|active": false}}
	if !workLaneOpen(data, "Task", "idle") {
		t.Fatal("stored open idle lane ignored")
	}
	if workLaneOpen(data, "Task", "active") {
		t.Fatal("stored closed active lane ignored")
	}
	if !workLaneOpen(data, "Task", "attention") {
		t.Fatal("default attention lane should be open")
	}
	if workLaneOpen(data, "Task", "idle") == workLaneOpen(workData{ScopeBase: "/o/o1/p/p1"}, "Task", "idle") {
		t.Fatal("a different scope must not inherit another project's prefs")
	}
	key := workLaneKey(data, "Task", "idle")
	if key != "/o/o1/p/p1|Task|idle" {
		t.Fatalf("lane key = %q", key)
	}
}

func TestBuildWorkMatrixNoStatusHiddenWhenUnused(t *testing.T) {
	compiled := &CompiledSchemaTypes{ObjectTypes: []CompiledType{
		{Name: "Task", BoardEnabled: true, AllowedStatuses: []string{"ready", "done"}},
	}}
	m := buildWorkMatrix("Task", []WorkItem{{Type: "Task", CanonicalID: "t1", Status: "ready"}}, compiled)
	for _, c := range m.Columns {
		if c.Status == "" {
			t.Fatalf("empty No status column rendered without no-status items: %#v", m.Columns)
		}
	}
}
