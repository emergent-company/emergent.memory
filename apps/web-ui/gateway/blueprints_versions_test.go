package main

import (
	"slices"
	"testing"
)

func TestNextVersion(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"1.2.3", "1.2.4"},
		{"1.2", "1.2.1"},
		{"1", "1.0.1"},
		{"1.2.3-beta.1", "1.2.4"},
		{"1.2.3+build.5", "1.2.4"},
		{"0.0.0", "0.0.1"},
		{"2.0", "2.0.1"},
		{"10.20.30", "10.20.31"},
		{" 1.2.3 ", "1.2.4"},
		{"", "1.0.0"},
		{"abc", "1.0.0"},
		{"x1.2", "1.0.0"},
	}
	for _, tc := range cases {
		if got := nextVersion(tc.in); got != tc.want {
			t.Errorf("nextVersion(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestSortVersionsNewestFirst(t *testing.T) {
	t.Run("numeric ordering not lexicographic", func(t *testing.T) {
		vs := []BlueprintVersionView{
			{ID: "b", Version: "1.9.0"},
			{ID: "a", Version: "1.10.0"},
			{ID: "c", Version: "1.2.0"},
		}
		sortVersionsNewestFirst(vs)
		want := []string{"1.10.0", "1.9.0", "1.2.0"}
		for i := range want {
			if vs[i].Version != want[i] {
				t.Fatalf("order = %v, want %v", versionsOf(vs), want)
			}
		}
	})

	t.Run("CreatedAt then ID tie-break", func(t *testing.T) {
		vs := []BlueprintVersionView{
			{ID: "z", Version: "1.0.0", CreatedAt: "2026-01-01T00:00:00Z"},
			{ID: "a", Version: "1.0.0", CreatedAt: "2026-02-01T00:00:00Z"},
			{ID: "m", Version: "1.0.0", CreatedAt: "2026-01-01T00:00:00Z"},
		}
		sortVersionsNewestFirst(vs)
		want := []string{"a", "m", "z"} // newest CreatedAt first, then ID ASC
		for i := range want {
			if vs[i].ID != want[i] {
				t.Fatalf("order = %v, want %v", idsOf(vs), want)
			}
		}
	})
}

func versionsOf(vs []BlueprintVersionView) []string {
	out := make([]string, 0, len(vs))
	for _, v := range vs {
		out = append(out, v.Version)
	}
	return out
}

func idsOf(vs []BlueprintVersionView) []string {
	out := make([]string, 0, len(vs))
	for _, v := range vs {
		out = append(out, v.ID)
	}
	return out
}

func TestDiffBlueprintDetails(t *testing.T) {
	base := &BlueprintDetail{
		Version: "1.0.0",
		ObjectTypes: []ObjectTypeDetail{
			{Name: "person", Label: "Person", Description: "a person", Properties: []PropertyDetail{
				{Name: "age", Type: "number", Description: "age in years"},
				{Name: "name", Type: "string", Required: true},
			}},
			{Name: "task", Label: "Task"},
		},
		RelationshipTypes: []RelationshipTypeDetail{
			{Name: "assigned_to", Label: "Assigned to", SourceType: "task", TargetType: "person"},
			{Name: "removed_rel", Label: "Removed"},
		},
		Agents: []BundledAgent{
			{Name: "operator", Description: "base desc", Model: "openai/gpt", Tools: []string{"a", "b"}, Skills: []string{"s1"}, BannedTools: []string{"x"}},
			{Name: "old-agent", Description: "gone"},
		},
	}
	target := &BlueprintDetail{
		Version: "1.1.0",
		ObjectTypes: []ObjectTypeDetail{
			{Name: "person", Label: "Person", Description: "a person", Properties: []PropertyDetail{
				{Name: "age", Type: "string", Description: "age in years"}, // type changed
				{Name: "name", Type: "string", Required: true},
				{Name: "email", Type: "string"}, // added
			}},
			{Name: "company", Label: "Company"}, // added
		},
		RelationshipTypes: []RelationshipTypeDetail{
			{Name: "assigned_to", Label: "Assigned to", SourceType: "task", TargetType: "person"},
			{Name: "new_rel", Label: "New", SourceType: "task", TargetType: "company"},
		},
		Agents: []BundledAgent{
			{Name: "operator", Description: "new desc", Model: "openai/gpt", Tools: []string{"b", "c"}, Skills: []string{"s1", "s2"}, BannedTools: []string{"x"}},
			{Name: "new-agent", Description: "added", Tools: []string{"t1"}},
		},
	}

	d := diffBlueprintDetails(base, target)
	if d == nil {
		t.Fatal("diff must never be nil")
	}
	if d.BaseVersion != "1.0.0" || d.TargetVersion != "1.1.0" {
		t.Fatalf("versions = %q/%q", d.BaseVersion, d.TargetVersion)
	}

	// Object types: sorted union by name (company, person, task).
	if got := objectTypeNames(d.ObjectTypes); !slices.Equal(got, []string{"company", "person", "task"}) {
		t.Fatalf("object type order = %v", got)
	}
	company := findObjectType(t, d.ObjectTypes, "company")
	if company.Change != DiffChangeAdded {
		t.Errorf("company change = %q, want added", company.Change)
	}
	task := findObjectType(t, d.ObjectTypes, "task")
	if task.Change != DiffChangeRemoved {
		t.Errorf("task change = %q, want removed", task.Change)
	}
	if task.Label.Before != "Task" || task.Label.After != "" {
		t.Errorf("task label = %+v, want Before=Task", task.Label)
	}
	person := findObjectType(t, d.ObjectTypes, "person")
	if person.Change != DiffChangeChanged {
		t.Errorf("person change = %q, want changed", person.Change)
	}
	if person.Label.Change != DiffChangeNone || person.Description.Change != DiffChangeNone {
		t.Errorf("person label/desc should be unchanged: %+v / %+v", person.Label, person.Description)
	}

	// Property diff: union sorted (age, email, name).
	if got := propertyNames(person.Properties); !slices.Equal(got, []string{"age", "email", "name"}) {
		t.Fatalf("property order = %v", got)
	}
	age := findProperty(t, person.Properties, "age")
	if age.Change != DiffChangeChanged || age.Type.Change != DiffChangeChanged {
		t.Errorf("age = %+v, want changed type", age)
	}
	if age.Type.Before != "number" || age.Type.After != "string" {
		t.Errorf("age type before/after = %q/%q", age.Type.Before, age.Type.After)
	}
	email := findProperty(t, person.Properties, "email")
	if email.Change != DiffChangeAdded || email.Type.Change != DiffChangeAdded {
		t.Errorf("email = %+v, want added", email)
	}
	name := findProperty(t, person.Properties, "name")
	if name.Change != DiffChangeNone || name.Required.Change != DiffChangeNone {
		t.Errorf("name = %+v, want no change", name)
	}

	// Relationship types: sorted union.
	if got := relationshipNames(d.RelationshipTypes); !slices.Equal(got, []string{"assigned_to", "new_rel", "removed_rel"}) {
		t.Fatalf("relationship order = %v", got)
	}
	newRel := findRelationship(t, d.RelationshipTypes, "new_rel")
	if newRel.Change != DiffChangeAdded || newRel.SourceType.Change != DiffChangeAdded {
		t.Errorf("new_rel = %+v, want added", newRel)
	}
	removedRel := findRelationship(t, d.RelationshipTypes, "removed_rel")
	if removedRel.Change != DiffChangeRemoved || removedRel.Label.Change != DiffChangeRemoved {
		t.Errorf("removed_rel = %+v, want removed", removedRel)
	}

	// Agents: sorted union; chip diffs.
	if got := agentNamesOf(d.Agents); !slices.Equal(got, []string{"new-agent", "old-agent", "operator"}) {
		t.Fatalf("agent order = %v", got)
	}
	op := findAgent(t, d.Agents, "operator")
	if op.Change != DiffChangeChanged || op.Description.Change != DiffChangeChanged {
		t.Errorf("operator = %+v, want changed desc", op)
	}
	if op.Model.Change != DiffChangeNone {
		t.Errorf("operator model should be unchanged: %+v", op.Model)
	}
	if !slices.Equal(op.Tools.Added, []string{"c"}) || !slices.Equal(op.Tools.Removed, []string{"a"}) {
		t.Errorf("operator tools = %+v", op.Tools)
	}
	if !slices.Equal(op.Skills.Added, []string{"s2"}) || len(op.Skills.Removed) != 0 {
		t.Errorf("operator skills = %+v", op.Skills)
	}
	if len(op.BannedTools.Added) != 0 || len(op.BannedTools.Removed) != 0 {
		t.Errorf("operator banned tools = %+v", op.BannedTools)
	}
	newAgent := findAgent(t, d.Agents, "new-agent")
	if newAgent.Change != DiffChangeAdded || !slices.Equal(newAgent.Tools.Added, []string{"t1"}) {
		t.Errorf("new-agent = %+v, want added with t1", newAgent)
	}
	oldAgent := findAgent(t, d.Agents, "old-agent")
	if oldAgent.Change != DiffChangeRemoved || oldAgent.Description.Before != "gone" {
		t.Errorf("old-agent = %+v, want removed", oldAgent)
	}
}

func TestDiffBlueprintDetailsNoChange(t *testing.T) {
	detail := func() *BlueprintDetail {
		return &BlueprintDetail{
			Version: "1.0.0",
			ObjectTypes: []ObjectTypeDetail{
				{Name: "person", Label: "Person", Description: "a person", Properties: []PropertyDetail{
					{Name: "name", Type: "string", Required: true},
				}},
			},
			RelationshipTypes: []RelationshipTypeDetail{
				{Name: "knows", Label: "Knows", SourceType: "person", TargetType: "person"},
			},
			Agents: []BundledAgent{
				{Name: "operator", Description: "d", Model: "m", Tools: []string{"a"}, Skills: []string{"s"}},
			},
		}
	}
	d := diffBlueprintDetails(detail(), detail())
	if d == nil {
		t.Fatal("diff must never be nil")
	}
	for _, o := range d.ObjectTypes {
		if o.Change != DiffChangeNone || o.Label.Change != DiffChangeNone || o.Description.Change != DiffChangeNone {
			t.Errorf("unchanged object type %q reported change: %+v", o.Name, o)
		}
		for _, p := range o.Properties {
			if p.Change != DiffChangeNone || p.Type.Change != DiffChangeNone || p.Required.Change != DiffChangeNone {
				t.Errorf("unchanged property %q reported change: %+v", p.Name, p)
			}
		}
	}
	for _, r := range d.RelationshipTypes {
		if r.Change != DiffChangeNone || r.Label.Change != DiffChangeNone || r.SourceType.Change != DiffChangeNone || r.TargetType.Change != DiffChangeNone {
			t.Errorf("unchanged relationship %q reported change: %+v", r.Name, r)
		}
	}
	for _, a := range d.Agents {
		if a.Change != DiffChangeNone {
			t.Errorf("unchanged agent %q reported change: %+v", a.Name, a)
		}
		if len(a.Tools.Added) != 0 || len(a.Tools.Removed) != 0 ||
			len(a.Skills.Added) != 0 || len(a.Skills.Removed) != 0 ||
			len(a.BannedTools.Added) != 0 || len(a.BannedTools.Removed) != 0 {
			t.Errorf("unchanged agent %q reported chip diff: %+v", a.Name, a)
		}
	}
}

func TestDiffBlueprintDetailsNilInputs(t *testing.T) {
	d := diffBlueprintDetails(nil, nil)
	if d == nil {
		t.Fatal("nil inputs must still yield a non-nil diff")
	}
	if len(d.ObjectTypes) != 0 || len(d.RelationshipTypes) != 0 || len(d.Agents) != 0 {
		t.Errorf("nil inputs should produce empty sections, got %+v", d)
	}
}

func objectTypeNames(ts []ObjectTypeDiff) []string {
	out := make([]string, 0, len(ts))
	for _, t := range ts {
		out = append(out, t.Name)
	}
	return out
}

func propertyNames(ps []PropertyDiff) []string {
	out := make([]string, 0, len(ps))
	for _, p := range ps {
		out = append(out, p.Name)
	}
	return out
}

func relationshipNames(rs []RelationshipTypeDiff) []string {
	out := make([]string, 0, len(rs))
	for _, r := range rs {
		out = append(out, r.Name)
	}
	return out
}

func agentNamesOf(as []AgentDiff) []string {
	out := make([]string, 0, len(as))
	for _, a := range as {
		out = append(out, a.Name)
	}
	return out
}

func findObjectType(t *testing.T, ts []ObjectTypeDiff, name string) ObjectTypeDiff {
	t.Helper()
	for _, x := range ts {
		if x.Name == name {
			return x
		}
	}
	t.Fatalf("object type %q not found", name)
	return ObjectTypeDiff{}
}

func findProperty(t *testing.T, ps []PropertyDiff, name string) PropertyDiff {
	t.Helper()
	for _, x := range ps {
		if x.Name == name {
			return x
		}
	}
	t.Fatalf("property %q not found", name)
	return PropertyDiff{}
}

func findRelationship(t *testing.T, rs []RelationshipTypeDiff, name string) RelationshipTypeDiff {
	t.Helper()
	for _, x := range rs {
		if x.Name == name {
			return x
		}
	}
	t.Fatalf("relationship %q not found", name)
	return RelationshipTypeDiff{}
}

func findAgent(t *testing.T, as []AgentDiff, name string) AgentDiff {
	t.Helper()
	for _, x := range as {
		if x.Name == name {
			return x
		}
	}
	t.Fatalf("agent %q not found", name)
	return AgentDiff{}
}
