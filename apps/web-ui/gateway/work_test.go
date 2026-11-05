package main

import "testing"

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
		{Type: "Other", CanonicalID: "o1", Status: "draft"},
	}
	m := buildWorkMatrix("Task", items, compiled)
	if m.Total != 6 {
		t.Fatalf("total = %d, want 6 (other type excluded)", m.Total)
	}
	if len(m.Columns) != 3 || m.Columns[0].Status != "draft" || m.Columns[0].Count != 4 {
		t.Fatalf("columns = %#v", m.Columns)
	}
	want := map[string]map[string]int{
		"working":  {"draft": 1},
		"queued":   {"draft": 1},
		"retrying": {"draft": 1},
		"blocked":  {"draft": 1},
		"review":   {"review": 1},
		"idle":     {"done": 1},
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
}

func TestWorkTabClass(t *testing.T) {
	if workTabClass("board", "board") == workTabClass("board", "settings") {
		t.Fatal("active and inactive tab classes must differ")
	}
	if workTypeTabClass("Task", "Task") == workTypeTabClass("Bug", "Task") {
		t.Fatal("active and inactive type tab classes must differ")
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
