package citations

import (
	"strings"
	"testing"

	"github.com/emergent-company/emergent.memory/pkg/a2ui"
)

const (
	obj1 = "11111111-1111-1111-1111-111111111111"
	obj2 = "22222222-2222-2222-2222-222222222222"
	obj3 = "33333333-3333-3333-3333-333333333333"
	obj4 = "44444444-4444-4444-4444-444444444444"
	rel1 = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	rel2 = "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"
)

// searchHybridOutput builds a search-hybrid-shaped tool result for the given
// id/type/name tuples.
func searchHybridOutput(entries ...map[string]any) map[string]any {
	data := make([]map[string]any, 0, len(entries))
	for _, e := range entries {
		data = append(data, map[string]any{"object": e, "score": 0.9})
	}
	return map[string]any{"data": data, "total": len(entries), "has_more": false}
}

// relationshipListOutput builds a relationship-list-shaped tool result.
func relationshipListOutput(relID, typ, srcID, dstID string) map[string]any {
	return map[string]any{
		"data": []map[string]any{
			{"id": relID, "type": typ, "src_id": srcID, "dst_id": dstID},
		},
		"total":    1,
		"has_more": false,
	}
}

// edge builds an entity-edges-get edge entry.
func edge(relID, relType, ceID, ceKey, ceName, ceType string) map[string]any {
	return map[string]any{
		"relationship_id":   relID,
		"relationship_type": relType,
		"connected_entity": map[string]any{
			"id": ceID, "key": ceKey, "name": ceName, "type": ceType,
		},
		"properties": map[string]any{},
	}
}

// entityEdgesGetOutput builds an entity-edges-get-shaped tool result.
func entityEdgesGetOutput(entityID string, outgoing, incoming []map[string]any) map[string]any {
	return map[string]any{
		"ok":        true,
		"entity_id": entityID,
		"outgoing":  outgoing,
		"incoming":  incoming,
	}
}

func TestCandidates_SearchHybridObjects(t *testing.T) {
	cands := Candidates([]ToolCall{{Output: searchHybridOutput(
		map[string]any{"id": obj1, "type": "Company", "name": "Acme Corp", "key": "acme"},
		map[string]any{"id": obj2, "type": "Person", "key": "bob"},
	)}})

	if len(cands) != 2 {
		t.Fatalf("Candidates() = %d candidates, want 2", len(cands))
	}

	c1, ok := cands[obj1]
	if !ok {
		t.Fatalf("candidate %s missing", obj1)
	}
	if c1.Kind != "object" || c1.Type != "Company" || c1.Label != "Acme Corp" {
		t.Errorf("obj1 candidate = %+v, want object/Company/Acme Corp", c1)
	}

	c2, ok := cands[obj2]
	if !ok {
		t.Fatalf("candidate %s missing", obj2)
	}
	if c2.Kind != "object" || c2.Type != "Person" || c2.Label != "bob" {
		t.Errorf("obj2 candidate = %+v, want object/Person/bob (key fallback)", c2)
	}
}

func TestCandidates_RelationshipList(t *testing.T) {
	cands := Candidates([]ToolCall{{Output: relationshipListOutput(rel1, "works_at", obj1, obj2)}})

	if len(cands) != 1 {
		t.Fatalf("Candidates() = %d candidates, want 1", len(cands))
	}

	c, ok := cands[rel1]
	if !ok {
		t.Fatalf("candidate %s missing", rel1)
	}
	if c.Kind != "relationship" || c.Type != "works_at" {
		t.Errorf("rel candidate = %+v, want relationship/works_at", c)
	}
	wantLabel := obj1 + " —works_at→ " + obj2
	if c.Label != wantLabel {
		t.Errorf("rel label = %q, want %q", c.Label, wantLabel)
	}
	if c.SrcID != obj1 || c.DstID != obj2 {
		t.Errorf("rel src/dst = %q/%q, want %q/%q", c.SrcID, c.DstID, obj1, obj2)
	}
}

func TestCandidates_RelationshipLabelUsesObjectNames(t *testing.T) {
	cands := Candidates([]ToolCall{
		{Output: searchHybridOutput(map[string]any{"id": obj1, "type": "Company", "name": "Acme Corp"})},
		{Output: relationshipListOutput(rel1, "works_at", obj1, obj2)},
	})

	c, ok := cands[rel1]
	if !ok {
		t.Fatalf("candidate %s missing", rel1)
	}
	// src resolves to "Acme Corp"; dst has no object → raw id.
	wantLabel := "Acme Corp —works_at→ " + obj2
	if c.Label != wantLabel {
		t.Errorf("rel label = %q, want %q", c.Label, wantLabel)
	}
}

func TestCandidates_EntityEdgesGet(t *testing.T) {
	out := entityEdgesGetOutput(obj1,
		[]map[string]any{edge(rel1, "has_paragraph", obj2, "p2", "Paragraph 2", "LegalParagraph")},
		[]map[string]any{edge(rel2, "cites", obj3, "p3", "Paragraph 3", "LegalParagraph")},
	)

	cands := Candidates([]ToolCall{{Output: out}})

	// outgoing edge: src = envelope entity, dst = connected_entity
	c, ok := cands[rel1]
	if !ok {
		t.Fatalf("candidate %s missing", rel1)
	}
	if c.Kind != "relationship" || c.Type != "has_paragraph" || c.SrcID != obj1 || c.DstID != obj2 {
		t.Errorf("outgoing rel = %+v, want relationship/has_paragraph %s→%s", c, obj1, obj2)
	}

	// incoming edge: src = connected_entity, dst = envelope entity
	c, ok = cands[rel2]
	if !ok {
		t.Fatalf("candidate %s missing", rel2)
	}
	if c.Kind != "relationship" || c.Type != "cites" || c.SrcID != obj3 || c.DstID != obj1 {
		t.Errorf("incoming rel = %+v, want relationship/cites %s→%s", c, obj3, obj1)
	}

	// connected_entity maps must still become object candidates
	if _, ok := cands[obj2]; !ok {
		t.Errorf("connected_entity %s not collected as object candidate", obj2)
	}
	if _, ok := cands[obj3]; !ok {
		t.Errorf("connected_entity %s not collected as object candidate", obj3)
	}
}

func TestCandidates_IgnoresNonUUIDIDs(t *testing.T) {
	cands := Candidates([]ToolCall{{Output: map[string]any{
		"id":   "run_12345", // not a UUID — must be ignored
		"type": "Run",
		"data": []map[string]any{
			{"id": obj1, "type": "Company", "name": "Acme Corp"},
		},
	}}})

	if len(cands) != 1 {
		t.Fatalf("Candidates() = %d candidates, want 1 (non-UUID id ignored)", len(cands))
	}
	if _, ok := cands[obj1]; !ok {
		t.Fatalf("candidate %s missing", obj1)
	}
}

func TestDerive_Grounding(t *testing.T) {
	cands := Candidates([]ToolCall{{Output: searchHybridOutput(
		map[string]any{"id": obj1, "type": "Company", "name": "Acme Corp"},
		map[string]any{"id": obj2, "type": "Person", "name": "Bob"},
		map[string]any{"id": obj4, "type": "Note", "name": "Unreferenced"},
	)}})

	answer := "See [Acme Corp](/objects/" + obj1 + ") and /objects/" + obj2 + " but [Ghost](/objects/" + obj3 + ")"
	cits := Derive(answer, nil, cands)

	got := citationIDs(cits)
	// obj1 (markdown link) and obj2 (bare) kept; obj3 hallucinated dropped; obj4 retrieved-but-unreferenced dropped.
	want := map[string]bool{obj1: true, obj2: true}
	if len(got) != len(want) {
		t.Fatalf("Derive() returned ids %v, want %v", got, want)
	}
	for id := range want {
		if !got[id] {
			t.Errorf("expected citation for %s", id)
		}
	}
}

func TestDerive_RelationshipFragment(t *testing.T) {
	cands := Candidates([]ToolCall{
		{Output: searchHybridOutput(
			map[string]any{"id": obj1, "type": "Company", "name": "Acme Corp"},
			map[string]any{"id": obj2, "type": "Person", "name": "Bob"},
		)},
		{Output: relationshipListOutput(rel1, "works_at", obj1, obj2)},
	})

	answer := "[Acme —works_at→ Bob](/objects/" + obj1 + "#relationship-" + rel1 + ")"
	cits := Derive(answer, nil, cands)

	var rel *Citation
	for i := range cits {
		if cits[i].Kind == "relationship" {
			rel = &cits[i]
		}
	}
	if rel == nil {
		t.Fatalf("Derive() = %+v, want a relationship citation", cits)
	}
	if rel.ID != rel1 || rel.Type != "works_at" {
		t.Errorf("relationship citation = %+v, want id %s type works_at", *rel, rel1)
	}
	if rel.Label != "Acme —works_at→ Bob" {
		t.Errorf("relationship label = %q, want %q", rel.Label, "Acme —works_at→ Bob")
	}
	if rel.URL != "/objects/"+obj1 {
		t.Errorf("relationship url = %q, want %q", rel.URL, "/objects/"+obj1)
	}
}

func TestDerive_Surfaces(t *testing.T) {
	cands := Candidates([]ToolCall{{Output: searchHybridOutput(
		map[string]any{"id": obj1, "type": "Company", "name": "Acme Corp"},
	)}})

	surfaces := []a2ui.Message{{
		UpdateComponents: &a2ui.UpdateComponents{
			SurfaceID: "s1",
			Components: []a2ui.Component{
				{ID: obj1, Component: "entity", Props: map[string]any{"type": "Company", "name": "Acme Corp"}},
			},
		},
	}}

	cits := Derive("", surfaces, cands)
	if len(cits) != 1 {
		t.Fatalf("Derive() = %+v, want 1 citation", cits)
	}
	if cits[0].ID != obj1 || cits[0].Kind != "object" {
		t.Errorf("citation = %+v, want object %s", cits[0], obj1)
	}
	if cits[0].Label != "Acme Corp" {
		t.Errorf("citation label = %q, want Acme Corp (candidate label fallback)", cits[0].Label)
	}
}

func TestNeutralizeLinks(t *testing.T) {
	cited := []Citation{
		{Kind: "object", ID: obj1},
		{Kind: "relationship", ID: rel1},
	}

	answer := "[Acme](/objects/" + obj1 + ") and [Ghost](/objects/" + obj2 + ") and bare /objects/" + obj3 +
		" and [A —r→ B](/objects/" + obj1 + "#relationship-" + rel1 + ")" +
		" and [X —y→ Z](/objects/" + obj1 + "#relationship-" + rel2 + ")"

	out := NeutralizeLinks(answer, cited)

	// valid object link preserved
	if !contains(out, "[Acme](/objects/"+obj1+")") {
		t.Errorf("valid object link not preserved: %q", out)
	}
	// unknown markdown link demoted to label (brackets and URL removed)
	if contains(out, "/objects/"+obj2) {
		t.Errorf("unknown markdown link URL not removed: %q", out)
	}
	if !contains(out, "Ghost") {
		t.Errorf("unknown markdown link label not preserved as plain text: %q", out)
	}
	// bare unknown link demoted to plain id
	if contains(out, "/objects/"+obj3) {
		t.Errorf("bare unknown link not demoted: %q", out)
	}
	if !contains(out, obj3) {
		t.Errorf("bare unknown id should remain as plain text: %q", out)
	}
	// cited relationship fragment preserved
	if !contains(out, "[A —r→ B](/objects/"+obj1+"#relationship-"+rel1+")") {
		t.Errorf("cited relationship fragment not preserved: %q", out)
	}
	// uncited relationship fragment dropped, object link rule applied (obj1 cited)
	if contains(out, "#relationship-"+rel2) {
		t.Errorf("uncited relationship fragment not dropped: %q", out)
	}
	if !contains(out, "[X —y→ Z](/objects/"+obj1+")") {
		t.Errorf("uncited fragment should drop leaving object link: %q", out)
	}
}

func TestNeutralizeLinks_OtherPathsUntouched(t *testing.T) {
	out := NeutralizeLinks("[docs](/docs/x) and https://example.com/a", []Citation{{Kind: "object", ID: obj1}})
	if out != "[docs](/docs/x) and https://example.com/a" {
		t.Errorf("NeutralizeLinks touched non-object links: %q", out)
	}
}

func TestDerive_KeyReference(t *testing.T) {
	const key = "lov/2005-06-17-90"
	cands := Candidates([]ToolCall{{Output: searchHybridOutput(
		map[string]any{"id": obj1, "type": "Law", "name": "Lov", "key": key},
	)}})

	answer := "[Lov](/objects/" + key + ")"
	cits := Derive(answer, nil, cands)

	if len(cits) != 1 {
		t.Fatalf("Derive() = %+v, want 1 citation", cits)
	}
	c := cits[0]
	if c.Kind != "object" || c.ID != obj1 {
		t.Errorf("citation = %+v, want object id %s", c, obj1)
	}
	if c.Key != key {
		t.Errorf("citation.Key = %q, want %q", c.Key, key)
	}
	if c.URL != "/objects/"+obj1 {
		t.Errorf("citation.URL = %q, want %q", c.URL, "/objects/"+obj1)
	}
	if c.Label != "Lov" {
		t.Errorf("citation.Label = %q, want Lov", c.Label)
	}
}

func TestDerive_UnknownKey(t *testing.T) {
	cands := Candidates([]ToolCall{{Output: searchHybridOutput(
		map[string]any{"id": obj1, "type": "Law", "name": "Lov", "key": "lov/2005-06-17-90"},
	)}})

	answer := "[Ghost](/objects/unknown/key)"
	cits := Derive(answer, nil, cands)
	if len(cits) != 0 {
		t.Fatalf("Derive() = %+v, want no citations for unknown key", cits)
	}
}

func TestNeutralizeLinks_Key(t *testing.T) {
	const key = "lov/2005-06-17-90"
	cited := []Citation{
		{Kind: "object", ID: obj1, Key: key},
	}

	answer := "[Lov](/objects/" + key + ") and [Ghost](/objects/unknown/key) and bare /objects/" + key + " and [U](/objects/" + obj2 + ")"
	out := NeutralizeLinks(answer, cited)

	// known key markdown link re-targeted to canonical id
	if !contains(out, "[Lov](/objects/"+obj1+")") {
		t.Errorf("known key link not re-targeted: %q", out)
	}
	// no key path may remain (markdown or bare)
	if contains(out, "/objects/"+key) {
		t.Errorf("key path should be rewritten to canonical id: %q", out)
	}
	// unknown key markdown demoted to label
	if contains(out, "/objects/unknown/key") {
		t.Errorf("unknown key link not demoted: %q", out)
	}
	if !contains(out, "Ghost") {
		t.Errorf("unknown key label not preserved as plain text: %q", out)
	}
	// bare known key rewritten to canonical id
	if !contains(out, "/objects/"+obj1) {
		t.Errorf("bare known key not re-targeted: %q", out)
	}
	// unknown uuid still demoted
	if contains(out, "/objects/"+obj2) {
		t.Errorf("unknown uuid link not demoted: %q", out)
	}
}

func TestDerive_EntityEdgesRelationship(t *testing.T) {
	cands := Candidates([]ToolCall{{Output: entityEdgesGetOutput(obj1,
		[]map[string]any{edge(rel1, "has_paragraph", obj2, "p2", "Paragraph 2", "LegalParagraph")},
		nil,
	)}})

	answer := "[X —has_paragraph→ Y](/objects/" + obj1 + "#relationship-" + rel1 + ")"
	cits := Derive(answer, nil, cands)

	var rel *Citation
	for i := range cits {
		if cits[i].Kind == "relationship" {
			rel = &cits[i]
		}
	}
	if rel == nil {
		t.Fatalf("Derive() = %+v, want a relationship citation", cits)
	}
	if rel.ID != rel1 || rel.Type != "has_paragraph" {
		t.Errorf("relationship = %+v, want id %s type has_paragraph", *rel, rel1)
	}
	if rel.URL != "/objects/"+obj1 {
		t.Errorf("relationship url = %q, want %q", rel.URL, "/objects/"+obj1)
	}
}

func TestDerive_KeyAfterID(t *testing.T) {
	const key = "lov/2005-06-17-90"
	cands := Candidates([]ToolCall{{Output: searchHybridOutput(
		map[string]any{"id": obj1, "type": "Law", "name": "Lov", "key": key},
	)}})

	// canonical id first, then the same object by key
	answer := "[Lov](/objects/" + obj1 + ") and [Lov](/objects/" + key + ")"
	cits := Derive(answer, nil, cands)

	if len(cits) != 1 {
		t.Fatalf("Derive() = %+v, want 1 citation", cits)
	}
	c := cits[0]
	if c.Kind != "object" || c.ID != obj1 {
		t.Errorf("citation = %+v, want object id %s", c, obj1)
	}
	if c.Key != key {
		t.Errorf("citation.Key = %q, want %q", c.Key, key)
	}
	if c.URL != "/objects/"+obj1 {
		t.Errorf("citation.URL = %q, want %q", c.URL, "/objects/"+obj1)
	}
	if c.Label != "Lov" {
		t.Errorf("citation.Label = %q, want Lov", c.Label)
	}
}

func TestDerive_RelationshipWithKeySource(t *testing.T) {
	const key = "lov/2005-06-17-90"
	cands := Candidates([]ToolCall{
		{Output: searchHybridOutput(
			map[string]any{"id": obj1, "type": "Law", "name": "Lov", "key": key},
			map[string]any{"id": obj2, "type": "Person", "name": "Bob"},
		)},
		{Output: relationshipListOutput(rel1, "references", obj1, obj2)},
	})

	answer := "[Lov —references→ Bob](/objects/" + key + "#relationship-" + rel1 + ")"
	cits := Derive(answer, nil, cands)

	var rel *Citation
	for i := range cits {
		if cits[i].Kind == "relationship" {
			rel = &cits[i]
		}
	}
	if rel == nil {
		t.Fatalf("Derive() = %+v, want a relationship citation", cits)
	}
	if rel.ID != rel1 || rel.Type != "references" {
		t.Errorf("relationship = %+v, want id %s type references", *rel, rel1)
	}
	if rel.URL != "/objects/"+obj1 {
		t.Errorf("relationship url = %q, want canonical src %q", rel.URL, "/objects/"+obj1)
	}
}

func citationIDs(cits []Citation) map[string]bool {
	out := make(map[string]bool, len(cits))
	for _, c := range cits {
		out[c.ID] = true
	}
	return out
}

func contains(haystack, needle string) bool {
	return strings.Contains(haystack, needle)
}
