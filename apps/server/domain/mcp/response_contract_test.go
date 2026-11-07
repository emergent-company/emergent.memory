package mcp

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/emergent-company/emergent.memory/domain/graph"
	"github.com/google/uuid"
)

// Small pointer helpers for building graph DTOs directly.
func rstr(s string) *string        { return &s }
func rint(i int) *int              { return &i }
func rf32(f float32) *float32      { return &f }
func rtime(t time.Time) *time.Time { return &t }
func ruuid(u uuid.UUID) *uuid.UUID { return &u }

func TestResponseOptsFromArgs(t *testing.T) {
	tests := []struct {
		name string
		args map[string]any
		want ResponseOpts
	}{
		{"nil args", nil, ResponseOpts{}},
		{"empty args", map[string]any{}, ResponseOpts{}},
		{"fields as []any", map[string]any{"fields": []any{"name", "age"}}, ResponseOpts{Fields: []string{"name", "age"}}},
		{"fields as []string", map[string]any{"fields": []string{"name", "age"}}, ResponseOpts{Fields: []string{"name", "age"}}},
		{"fields empty slice", map[string]any{"fields": []any{}}, ResponseOpts{}},
		{"fields skip empty strings", map[string]any{"fields": []any{"name", "", "age"}}, ResponseOpts{Fields: []string{"name", "age"}}},
		{"fields wrong type", map[string]any{"fields": "name"}, ResponseOpts{}},
		{"verbose true", map[string]any{"verbose": true}, ResponseOpts{Verbose: true}},
		{"verbose false", map[string]any{"verbose": false}, ResponseOpts{Verbose: false}},
		{"verbose non-bool ignored", map[string]any{"verbose": "yes"}, ResponseOpts{}},
		{"strategy compact", map[string]any{"field_strategy": "compact"}, ResponseOpts{FieldStrategy: "compact"}},
		{"strategy minimal", map[string]any{"field_strategy": "minimal"}, ResponseOpts{FieldStrategy: "minimal"}},
		{"strategy full", map[string]any{"field_strategy": "full"}, ResponseOpts{FieldStrategy: "full"}},
		{"strategy uppercase lowercased", map[string]any{"field_strategy": "FULL"}, ResponseOpts{FieldStrategy: "full"}},
		{"strategy invalid", map[string]any{"field_strategy": "bogus"}, ResponseOpts{FieldStrategy: ""}},
		{"strategy empty", map[string]any{"field_strategy": ""}, ResponseOpts{FieldStrategy: ""}},
		{"strategy non-string", map[string]any{"field_strategy": 42}, ResponseOpts{FieldStrategy: ""}},
		{"combined", map[string]any{"fields": []any{"name"}, "verbose": true, "field_strategy": "minimal"}, ResponseOpts{Fields: []string{"name"}, Verbose: true, FieldStrategy: "minimal"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := responseOptsFromArgs(tt.args)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("responseOptsFromArgs(%v) = %+v, want %+v", tt.args, got, tt.want)
			}
		})
	}
}

// testObject builds a fully-populated GraphObjectResponse.
func testObject() *graph.GraphObjectResponse {
	vid := uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	canonical := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	branch := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	supersedes := uuid.MustParse("33333333-3333-3333-3333-333333333333")
	created := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	org := "org-1"

	return &graph.GraphObjectResponse{
		ID:                vid,
		OrgID:             &org,
		ProjectID:         uuid.MustParse("44444444-4444-4444-4444-444444444444"),
		BranchID:          &branch,
		CanonicalID:       canonical,
		SupersedesID:      &supersedes,
		Version:           4,
		Type:              "person",
		Key:               rstr("alice"),
		Status:            rstr("active"),
		Namespace:         rstr("test"),
		Properties:        map[string]any{"name": "Alice", "age": 30, "city": "NYC"},
		Labels:            []string{"a", "b"},
		SchemaVersion:     rstr("1.0"),
		DeletedAt:         nil,
		ChangeSummary:     map[string]any{"added": "name"},
		ContentHash:       rstr("abc123"),
		ExternalSource:    rstr("csv"),
		ExternalID:        rstr("ext-1"),
		ExternalURL:       rstr("https://example.com"),
		ExternalParentID:  rstr("parent-1"),
		SyncedAt:          &created,
		ExternalUpdatedAt: &created,
		CreatedAt:         created,
		RevisionCount:     rint(3),
		RelationshipCount: rint(5),
	}
}

func TestSlimEntity(t *testing.T) {
	t.Run("nil object", func(t *testing.T) {
		if got := slimEntity(nil, ResponseOpts{}); got != nil {
			t.Errorf("slimEntity(nil) = %v, want nil", got)
		}
	})

	t.Run("default slim keys present, legacy/org/project absent", func(t *testing.T) {
		got := slimEntity(testObject(), ResponseOpts{})

		for _, k := range []string{"id", "type", "key", "name", "properties", "labels"} {
			if _, ok := got[k]; !ok {
				t.Errorf("expected key %q present, got %v", k, got)
			}
		}
		if got["id"] != testObject().CanonicalID.String() {
			t.Errorf("id = %v, want canonical id %v", got["id"], testObject().CanonicalID)
		}

		for _, k := range []string{"org_id", "project_id", "canonical_id", "entity_id"} {
			if _, ok := got[k]; ok {
				t.Errorf("did not expect key %q, got %v", k, got)
			}
		}
		for _, k := range []string{"version_id", "version", "status", "namespace", "branch_id", "schema_version", "created_at", "deleted_at", "change_summary", "content_hash", "external_source", "external_id", "external_url", "external_parent_id", "synced_at", "external_updated_at", "revision_count", "relationship_count", "supersedes_id"} {
			if _, ok := got[k]; ok {
				t.Errorf("did not expect verbose key %q in non-verbose output, got %v", k, got)
			}
		}
	})

	t.Run("omits key when empty and labels when empty", func(t *testing.T) {
		o := testObject()
		o.Key = nil
		o.Labels = nil
		got := slimEntity(o, ResponseOpts{})
		if _, ok := got["key"]; ok {
			t.Errorf("key should be omitted when nil, got %v", got)
		}
		if _, ok := got["labels"]; ok {
			t.Errorf("labels should be omitted when empty, got %v", got)
		}
	})

	t.Run("verbose adds fields", func(t *testing.T) {
		got := slimEntity(testObject(), ResponseOpts{Verbose: true})

		for _, k := range []string{"version_id", "version", "status", "namespace", "branch_id", "schema_version", "created_at", "change_summary", "content_hash", "external_source", "external_id", "external_url", "external_parent_id", "synced_at", "external_updated_at", "revision_count", "relationship_count", "supersedes_id"} {
			if _, ok := got[k]; !ok {
				t.Errorf("verbose: expected key %q present, got %v", k, got)
			}
		}
		if got["version_id"] != testObject().ID.String() {
			t.Errorf("version_id = %v, want %v", got["version_id"], testObject().ID.String())
		}
		if _, ok := got["deleted_at"]; ok {
			t.Errorf("deleted_at should be omitted when nil, got %v", got)
		}
		// legacy/org/project keys must still be absent in verbose
		for _, k := range []string{"org_id", "project_id", "canonical_id", "entity_id"} {
			if _, ok := got[k]; ok {
				t.Errorf("verbose: did not expect key %q, got %v", k, got)
			}
		}
	})

	t.Run("verbose omits zero fields", func(t *testing.T) {
		o := testObject()
		o.Version = 0
		o.Status = nil
		o.BranchID = nil
		o.RevisionCount = nil
		o.SupersedesID = nil
		got := slimEntity(o, ResponseOpts{Verbose: true})
		for _, k := range []string{"version", "status", "branch_id", "revision_count", "supersedes_id"} {
			if _, ok := got[k]; ok {
				t.Errorf("verbose: expected key %q omitted when zero/nil, got %v", k, got)
			}
		}
	})

	t.Run("minimal strategy drops properties and name", func(t *testing.T) {
		got := slimEntity(testObject(), ResponseOpts{FieldStrategy: "minimal"})
		if _, ok := got["properties"]; ok {
			t.Errorf("minimal: properties should be omitted, got %v", got)
		}
		if _, ok := got["name"]; ok {
			t.Errorf("minimal: name should be omitted, got %v", got)
		}
		if got["id"] != testObject().CanonicalID.String() {
			t.Errorf("minimal: id = %v, want canonical id", got["id"])
		}
	})

	t.Run("compact strategy drops properties but keeps name", func(t *testing.T) {
		got := slimEntity(testObject(), ResponseOpts{FieldStrategy: "compact"})
		if _, ok := got["properties"]; ok {
			t.Errorf("compact: properties should be omitted, got %v", got)
		}
		if got["name"] != "Alice" {
			t.Errorf("compact: name = %v, want Alice", got["name"])
		}
	})

	t.Run("full strategy keeps properties and name", func(t *testing.T) {
		got := slimEntity(testObject(), ResponseOpts{FieldStrategy: "full"})
		props, ok := got["properties"].(map[string]any)
		if !ok {
			t.Fatalf("full: expected properties map, got %v", got["properties"])
		}
		if len(props) != 3 {
			t.Errorf("full: expected all 3 properties, got %v", props)
		}
		if got["name"] != "Alice" {
			t.Errorf("full: name = %v, want Alice", got["name"])
		}
	})

	t.Run("name omitted when property is not a string", func(t *testing.T) {
		o := testObject()
		o.Properties = map[string]any{"name": 42}
		got := slimEntity(o, ResponseOpts{FieldStrategy: "full"})
		if _, ok := got["name"]; ok {
			t.Errorf("name should be omitted for non-string property, got %v", got)
		}
	})

	t.Run("name omitted when property name is empty string", func(t *testing.T) {
		o := testObject()
		o.Properties = map[string]any{"name": ""}
		got := slimEntity(o, ResponseOpts{FieldStrategy: "full"})
		if _, ok := got["name"]; ok {
			t.Errorf("name should be omitted when empty, got %v", got)
		}
	})

	t.Run("fields projection selects keys and prepends name", func(t *testing.T) {
		got := slimEntity(testObject(), ResponseOpts{Fields: []string{"age"}})
		props, ok := got["properties"].(map[string]any)
		if !ok {
			t.Fatalf("expected properties map, got %v", got["properties"])
		}
		if _, ok := props["age"]; !ok {
			t.Errorf("projection should include requested key age, got %v", props)
		}
		if _, ok := props["city"]; ok {
			t.Errorf("projection should exclude unrequested key city, got %v", props)
		}
		if _, ok := props["name"]; !ok {
			t.Errorf("projection should prepend name when absent from fields, got %v", props)
		}
	})

	t.Run("projection ignored under minimal", func(t *testing.T) {
		got := slimEntity(testObject(), ResponseOpts{Fields: []string{"age"}, FieldStrategy: "minimal"})
		if _, ok := got["properties"]; ok {
			t.Errorf("minimal: properties should be omitted even with fields, got %v", got)
		}
		if _, ok := got["name"]; ok {
			t.Errorf("minimal: name should be omitted, got %v", got)
		}
	})
}

func TestSlimRelationship(t *testing.T) {
	t.Run("nil relationship", func(t *testing.T) {
		if got := slimRelationship(nil, ResponseOpts{}); got != nil {
			t.Errorf("slimRelationship(nil) = %v, want nil", got)
		}
	})

	t.Run("default keys present, project_id/canonical_id/entity_id absent", func(t *testing.T) {
		r := &graph.GraphRelationshipResponse{
			ID:          uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"),
			ProjectID:   uuid.MustParse("44444444-4444-4444-4444-444444444444"),
			BranchID:    nil,
			CanonicalID: uuid.MustParse("11111111-1111-1111-1111-111111111111"),
			Version:     2,
			Type:        "knows",
			Label:       rstr("friend"),
			SrcID:       uuid.MustParse("55555555-5555-5555-5555-555555555555"),
			DstID:       uuid.MustParse("66666666-6666-6666-6666-666666666666"),
			Properties:  map[string]any{"since": 2020},
			Weight:      rf32(0.75),
		}
		got := slimRelationship(r, ResponseOpts{})

		for _, k := range []string{"id", "type", "src_id", "dst_id", "label", "weight", "properties"} {
			if _, ok := got[k]; !ok {
				t.Errorf("expected key %q present, got %v", k, got)
			}
		}
		if got["weight"] != float32(0.75) {
			t.Errorf("weight = %v, want 0.75", got["weight"])
		}
		for _, k := range []string{"project_id", "canonical_id", "entity_id", "version_id", "version", "branch_id", "created_at", "deleted_at", "change_summary", "inverse_relationship"} {
			if _, ok := got[k]; ok {
				t.Errorf("did not expect key %q in non-verbose, got %v", k, got)
			}
		}
	})

	t.Run("weight omitted when nil, properties omitted when empty, label omitted when nil", func(t *testing.T) {
		r := &graph.GraphRelationshipResponse{
			ID:          uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"),
			ProjectID:   uuid.MustParse("44444444-4444-4444-4444-444444444444"),
			BranchID:    nil,
			CanonicalID: uuid.MustParse("11111111-1111-1111-1111-111111111111"),
			Version:     1,
			Type:        "knows",
			SrcID:       uuid.MustParse("55555555-5555-5555-5555-555555555555"),
			DstID:       uuid.MustParse("66666666-6666-6666-6666-666666666666"),
		}
		got := slimRelationship(r, ResponseOpts{})
		for _, k := range []string{"weight", "label", "properties"} {
			if _, ok := got[k]; ok {
				t.Errorf("expected key %q omitted, got %v", k, got)
			}
		}
	})

	t.Run("verbose adds version fields and inverse relationship", func(t *testing.T) {
		created := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
		inverse := &graph.GraphRelationshipResponse{
			ID:          uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"),
			ProjectID:   uuid.MustParse("44444444-4444-4444-4444-444444444444"),
			BranchID:    nil,
			CanonicalID: uuid.MustParse("22222222-2222-2222-2222-222222222222"),
			Version:     1,
			Type:        "known_by",
			SrcID:       uuid.MustParse("66666666-6666-6666-6666-666666666666"),
			DstID:       uuid.MustParse("55555555-5555-5555-5555-555555555555"),
		}
		r := &graph.GraphRelationshipResponse{
			ID:                  uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"),
			ProjectID:           uuid.MustParse("44444444-4444-4444-4444-444444444444"),
			BranchID:            ruuid(uuid.MustParse("22222222-2222-2222-2222-222222222222")),
			CanonicalID:         uuid.MustParse("11111111-1111-1111-1111-111111111111"),
			Version:             2,
			Type:                "knows",
			SrcID:               uuid.MustParse("55555555-5555-5555-5555-555555555555"),
			DstID:               uuid.MustParse("66666666-6666-6666-6666-666666666666"),
			DeletedAt:           nil,
			ChangeSummary:       map[string]any{"added": "label"},
			CreatedAt:           created,
			InverseRelationship: inverse,
		}
		got := slimRelationship(r, ResponseOpts{Verbose: true})

		for _, k := range []string{"version_id", "version", "branch_id", "created_at", "change_summary", "inverse_relationship"} {
			if _, ok := got[k]; !ok {
				t.Errorf("verbose: expected key %q present, got %v", k, got)
			}
		}
		if _, ok := got["deleted_at"]; ok {
			t.Errorf("deleted_at should be omitted when nil, got %v", got)
		}
		inv, ok := got["inverse_relationship"].(map[string]any)
		if !ok {
			t.Fatalf("inverse_relationship should be a map, got %T", got["inverse_relationship"])
		}
		if inv["id"] != inverse.CanonicalID.String() {
			t.Errorf("inverse id = %v, want %v", inv["id"], inverse.CanonicalID.String())
		}
	})

	t.Run("strategy does not affect relationship properties", func(t *testing.T) {
		r := &graph.GraphRelationshipResponse{
			ID:          uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"),
			CanonicalID: uuid.MustParse("11111111-1111-1111-1111-111111111111"),
			Type:        "knows",
			SrcID:       uuid.MustParse("55555555-5555-5555-5555-555555555555"),
			DstID:       uuid.MustParse("66666666-6666-6666-6666-666666666666"),
			Properties:  map[string]any{"since": 2020},
		}
		got := slimRelationship(r, ResponseOpts{FieldStrategy: "minimal"})
		if _, ok := got["properties"]; !ok {
			t.Errorf("minimal strategy should not strip relationship properties, got %v", got)
		}
	})
}

func TestSlimSearchResponse(t *testing.T) {
	t.Run("nil response", func(t *testing.T) {
		if got := slimSearchResponse(nil, ResponseOpts{}); got != nil {
			t.Errorf("slimSearchResponse(nil) = %v, want nil", got)
		}
	})

	t.Run("data items have object and score", func(t *testing.T) {
		o := testObject()
		res := &graph.SearchResponse{
			Data: []*graph.SearchResultItem{
				{Object: o, Score: 0.9},
			},
			Total:   1,
			HasMore: false,
			Offset:  0,
		}
		got := slimSearchResponse(res, ResponseOpts{})

		if got["total"] != 1 {
			t.Errorf("total = %v, want 1", got["total"])
		}
		if got["has_more"] != false {
			t.Errorf("has_more = %v, want false", got["has_more"])
		}
		if _, ok := got["meta"]; ok {
			t.Errorf("meta should be omitted in non-verbose, got %v", got)
		}
		data, ok := got["data"].([]map[string]any)
		if !ok || len(data) != 1 {
			t.Fatalf("data = %v, want 1 item", got["data"])
		}
		item := data[0]
		if item["score"] != float32(0.9) {
			t.Errorf("score = %v, want 0.9", item["score"])
		}
		obj, ok := item["object"].(map[string]any)
		if !ok {
			t.Fatalf("object should be a slim map, got %T", item["object"])
		}
		if obj["id"] != o.CanonicalID.String() {
			t.Errorf("object id = %v, want %v", obj["id"], o.CanonicalID.String())
		}
		for _, k := range []string{"lexical_score", "vector_score", "vector_dist"} {
			if _, ok := item[k]; ok {
				t.Errorf("did not expect %q in non-verbose, got %v", k, item)
			}
		}
	})

	t.Run("verbose adds per-item scores and meta", func(t *testing.T) {
		ls := float32(0.5)
		vs := float32(0.4)
		vd := float32(0.1)
		res := &graph.SearchResponse{
			Data: []*graph.SearchResultItem{
				{Object: testObject(), Score: 0.9, LexicalScore: &ls, VectorScore: &vs, VectorDist: &vd},
			},
			Total:   1,
			HasMore: true,
			Offset:  0,
			Meta:    &graph.SearchResponseMeta{ElapsedMs: 12.5},
		}
		got := slimSearchResponse(res, ResponseOpts{Verbose: true})

		data := got["data"].([]map[string]any)
		item := data[0]
		if item["lexical_score"] != float32(0.5) {
			t.Errorf("lexical_score = %v, want 0.5", item["lexical_score"])
		}
		if item["vector_score"] != float32(0.4) {
			t.Errorf("vector_score = %v, want 0.4", item["vector_score"])
		}
		if item["vector_dist"] != float32(0.1) {
			t.Errorf("vector_dist = %v, want 0.1", item["vector_dist"])
		}
		meta, ok := got["meta"].(*graph.SearchResponseMeta)
		if !ok {
			t.Fatalf("meta should be passed through as *SearchResponseMeta, got %T", got["meta"])
		}
		if meta.ElapsedMs != 12.5 {
			t.Errorf("meta.ElapsedMs = %v, want 12.5", meta.ElapsedMs)
		}
	})

	t.Run("verbose omits nil scores", func(t *testing.T) {
		res := &graph.SearchResponse{
			Data: []*graph.SearchResultItem{
				{Object: testObject(), Score: 0.9},
			},
			Total: 1,
		}
		got := slimSearchResponse(res, ResponseOpts{Verbose: true})
		item := got["data"].([]map[string]any)[0]
		for _, k := range []string{"lexical_score", "vector_score", "vector_dist"} {
			if _, ok := item[k]; ok {
				t.Errorf("expected %q omitted when nil, got %v", k, item)
			}
		}
		if _, ok := got["meta"]; ok {
			t.Errorf("meta should be omitted when nil, got %v", got)
		}
	})
}

func TestSlimTraverseResponse(t *testing.T) {
	t.Run("nil response", func(t *testing.T) {
		if got := slimTraverseResponse(nil, ResponseOpts{}); got != nil {
			t.Errorf("slimTraverseResponse(nil) = %v, want nil", got)
		}
	})

	t.Run("nodes and edges slim, pagination absent", func(t *testing.T) {
		res := &graph.TraverseGraphResponse{
			Roots: []uuid.UUID{uuid.MustParse("11111111-1111-1111-1111-111111111111")},
			Nodes: []*graph.TraverseNode{
				{
					ID:          uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"),
					CanonicalID: uuid.MustParse("11111111-1111-1111-1111-111111111111"),
					Depth:       0,
					Type:        "person",
					Key:         rstr("alice"),
					Labels:      []string{"a"},
					Properties:  map[string]any{"name": "Alice"},
				},
			},
			Edges: []*graph.TraverseEdge{
				{
					ID:    uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"),
					Type:  "knows",
					SrcID: uuid.MustParse("11111111-1111-1111-1111-111111111111"),
					DstID: uuid.MustParse("22222222-2222-2222-2222-222222222222"),
				},
			},
			Truncated: false,
		}
		got := slimTraverseResponse(res, ResponseOpts{})

		if got["truncated"] != false {
			t.Errorf("truncated = %v, want false", got["truncated"])
		}
		roots, ok := got["roots"].([]uuid.UUID)
		if !ok || len(roots) != 1 {
			t.Errorf("roots = %v, want 1 root", got["roots"])
		}
		nodes := got["nodes"].([]map[string]any)
		node := nodes[0]
		for _, k := range []string{"id", "type", "key", "depth", "labels", "name", "properties"} {
			if _, ok := node[k]; !ok {
				t.Errorf("node: expected key %q present, got %v", k, node)
			}
		}
		if node["id"] != res.Nodes[0].CanonicalID.String() {
			t.Errorf("node id = %v, want canonical id", node["id"])
		}
		if node["depth"] != 0 {
			t.Errorf("node depth = %v, want 0", node["depth"])
		}
		for _, k := range []string{"version_id", "phase_index", "paths"} {
			if _, ok := node[k]; ok {
				t.Errorf("node: did not expect %q in non-verbose, got %v", k, node)
			}
		}
		edges := got["edges"].([]map[string]any)
		edge := edges[0]
		for _, k := range []string{"id", "type", "src_id", "dst_id"} {
			if _, ok := edge[k]; !ok {
				t.Errorf("edge: expected key %q present, got %v", k, edge)
			}
		}
		for _, k := range []string{"max_depth_reached", "total_nodes", "has_next_page", "has_previous_page", "next_cursor", "previous_cursor", "approx_position_start", "approx_position_end", "page_direction", "query_time_ms", "result_count"} {
			if _, ok := got[k]; ok {
				t.Errorf("did not expect verbose key %q, got %v", k, got)
			}
		}
	})

	t.Run("verbose adds node details and pagination", func(t *testing.T) {
		pi := 1
		qm := 3.5
		rc := 10
		next := "cursor-next"
		res := &graph.TraverseGraphResponse{
			Roots: []uuid.UUID{uuid.MustParse("11111111-1111-1111-1111-111111111111")},
			Nodes: []*graph.TraverseNode{
				{
					ID:          uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"),
					CanonicalID: uuid.MustParse("11111111-1111-1111-1111-111111111111"),
					Depth:       0,
					Type:        "person",
					Properties:  map[string]any{"name": "Alice"},
					PhaseIndex:  &pi,
					Paths:       [][]string{{"11111111-1111-1111-1111-111111111111"}},
				},
			},
			Edges:               []*graph.TraverseEdge{},
			Truncated:           true,
			MaxDepthReached:     3,
			TotalNodes:          10,
			HasNextPage:         true,
			HasPreviousPage:     false,
			NextCursor:          &next,
			PreviousCursor:      nil,
			ApproxPositionStart: 0,
			ApproxPositionEnd:   10,
			PageDirection:       "forward",
			QueryTimeMs:         &qm,
			ResultCount:         &rc,
		}
		got := slimTraverseResponse(res, ResponseOpts{Verbose: true})

		for _, k := range []string{"max_depth_reached", "total_nodes", "has_next_page", "has_previous_page", "next_cursor", "approx_position_start", "approx_position_end", "page_direction", "query_time_ms", "result_count"} {
			if _, ok := got[k]; !ok {
				t.Errorf("verbose: expected key %q present, got %v", k, got)
			}
		}
		if got["next_cursor"] != "cursor-next" {
			t.Errorf("next_cursor = %v, want cursor-next", got["next_cursor"])
		}
		if _, ok := got["previous_cursor"]; ok {
			t.Errorf("previous_cursor should be omitted when nil, got %v", got)
		}
		if got["query_time_ms"] != 3.5 {
			t.Errorf("query_time_ms = %v, want 3.5", got["query_time_ms"])
		}
		if got["result_count"] != 10 {
			t.Errorf("result_count = %v, want 10", got["result_count"])
		}

		node := got["nodes"].([]map[string]any)[0]
		if node["version_id"] != res.Nodes[0].ID.String() {
			t.Errorf("node version_id = %v, want %v", node["version_id"], res.Nodes[0].ID.String())
		}
		if node["phase_index"] != 1 {
			t.Errorf("phase_index = %v, want 1", node["phase_index"])
		}
		paths, ok := node["paths"].([][]string)
		if !ok || len(paths) != 1 {
			t.Errorf("paths = %v, want 1 path", node["paths"])
		}
	})
}

func TestSlimSimilarResult(t *testing.T) {
	t.Run("nil result", func(t *testing.T) {
		if got := slimSimilarResult(nil, ResponseOpts{}); got != nil {
			t.Errorf("slimSimilarResult(nil) = %v, want nil", got)
		}
	})

	t.Run("id falls back to physical ID when canonical nil", func(t *testing.T) {
		sr := &graph.SimilarObjectResult{
			ID:          uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"),
			CanonicalID: nil,
			Version:     rint(2),
			Distance:    0.3,
			BranchID:    ruuid(uuid.MustParse("22222222-2222-2222-2222-222222222222")),
			Type:        "person",
			Key:         rstr("alice"),
			Properties:  map[string]any{"name": "Alice"},
			Labels:      []string{"a"},
		}
		got := slimSimilarResult(sr, ResponseOpts{})
		if got["id"] != sr.ID.String() {
			t.Errorf("id = %v, want physical id %v", got["id"], sr.ID.String())
		}
		if got["distance"] != float32(0.3) {
			t.Errorf("distance = %v, want 0.3", got["distance"])
		}
		for _, k := range []string{"version", "status", "branch_id", "created_at", "labels"} {
			if _, ok := got[k]; ok {
				t.Errorf("did not expect verbose key %q, got %v", k, got)
			}
		}
	})

	t.Run("uses canonical id when present", func(t *testing.T) {
		canonical := uuid.MustParse("11111111-1111-1111-1111-111111111111")
		sr := &graph.SimilarObjectResult{
			ID:          uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"),
			CanonicalID: &canonical,
			Type:        "person",
			Distance:    0.5,
		}
		got := slimSimilarResult(sr, ResponseOpts{})
		if got["id"] != canonical.String() {
			t.Errorf("id = %v, want canonical id %v", got["id"], canonical.String())
		}
	})

	t.Run("verbose adds fields, minimal strategy strips properties", func(t *testing.T) {
		canonical := uuid.MustParse("11111111-1111-1111-1111-111111111111")
		created := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
		sr := &graph.SimilarObjectResult{
			ID:          uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"),
			CanonicalID: &canonical,
			Version:     rint(2),
			Distance:    0.3,
			BranchID:    ruuid(uuid.MustParse("22222222-2222-2222-2222-222222222222")),
			Type:        "person",
			Status:      rstr("active"),
			Properties:  map[string]any{"name": "Alice"},
			Labels:      []string{"a"},
			CreatedAt:   &created,
		}
		got := slimSimilarResult(sr, ResponseOpts{Verbose: true})
		for _, k := range []string{"version", "status", "branch_id", "created_at", "labels"} {
			if _, ok := got[k]; !ok {
				t.Errorf("verbose: expected key %q present, got %v", k, got)
			}
		}

		gotMin := slimSimilarResult(sr, ResponseOpts{Verbose: true, FieldStrategy: "minimal"})
		if _, ok := gotMin["properties"]; ok {
			t.Errorf("minimal: properties should be omitted, got %v", gotMin)
		}
		if _, ok := gotMin["name"]; ok {
			t.Errorf("minimal: name should be omitted, got %v", gotMin)
		}
	})
}

func TestWrapResultCompact(t *testing.T) {
	svc := &Service{}

	t.Run("produces text content with compact json", func(t *testing.T) {
		result, err := svc.wrapResultCompact(map[string]any{"ok": true, "count": 1})
		if err != nil {
			t.Fatalf("wrapResultCompact error: %v", err)
		}
		if len(result.Content) != 1 {
			t.Errorf("Content length = %d, want 1", len(result.Content))
		}
		if result.Content[0].Type != "text" {
			t.Errorf("Content[0].Type = %q, want \"text\"", result.Content[0].Type)
		}
		if strings.Contains(result.Content[0].Text, "\n") {
			t.Errorf("compact JSON should not contain newlines, got %q", result.Content[0].Text)
		}
		if strings.Contains(result.Content[0].Text, "  ") {
			t.Errorf("compact JSON should not contain indentation, got %q", result.Content[0].Text)
		}
		var decoded map[string]any
		if err := json.Unmarshal([]byte(result.Content[0].Text), &decoded); err != nil {
			t.Errorf("compact JSON should be valid: %v", err)
		}
	})

	t.Run("marshal error is wrapped", func(t *testing.T) {
		_, err := svc.wrapResultCompact(map[string]any{"bad": make(chan int)})
		if err == nil {
			t.Fatal("expected marshal error")
		}
		if !strings.Contains(err.Error(), "marshal result:") {
			t.Errorf("error should be wrapped with context, got %v", err)
		}
	})
}

// Text chunks (and synthetic relationship wrappers) carry only a version ID, so
// slimEntity must fall back to it instead of printing the zero UUID.
func TestSlimEntity_ZeroCanonicalFallsBackToVersionID(t *testing.T) {
	chunkID := uuid.New()
	o := &graph.GraphObjectResponse{
		ID:          chunkID,
		Type:        textChunkType,
		CanonicalID: uuid.Nil,
	}
	got := slimEntity(o, ResponseOpts{FieldStrategy: "verbose"})
	if got["id"] != chunkID.String() {
		t.Fatalf("id = %v, want the version id %s", got["id"], chunkID)
	}
}

// A chunk hit must keep its snippet and document_id under the default compact
// field strategy, which strips `properties` from graph objects.
func TestSlimSearchData_TextChunkKeepsPayload(t *testing.T) {
	chunkID := uuid.New()
	docID := uuid.New()
	res := &graph.SearchResponse{
		Data: []*graph.SearchResultItem{
			{
				Object: &graph.GraphObjectResponse{
					ID:          chunkID,
					CanonicalID: uuid.Nil,
					Type:        textChunkType,
					Properties: map[string]any{
						"snippet":     "HUMA Energy is a company",
						"document_id": docID.String(),
					},
				},
				Score: 0.42,
			},
		},
		Total: 1,
	}

	got := slimSearchResponse(res, ResponseOpts{FieldStrategy: "compact"})
	data := got["data"].([]map[string]any)
	obj := data[0]["object"].(map[string]any)

	if obj["id"] != chunkID.String() {
		t.Errorf("id = %v, want %s", obj["id"], chunkID)
	}
	if obj["type"] != textChunkType {
		t.Errorf("type = %v, want %s", obj["type"], textChunkType)
	}
	if obj["snippet"] != "HUMA Energy is a company" {
		t.Errorf("snippet = %v, want the chunk text", obj["snippet"])
	}
	if obj["document_id"] != docID.String() {
		t.Errorf("document_id = %v, want %s", obj["document_id"], docID)
	}
}

func TestSlimSearchResponse_BranchProvenanceAndScopeNote(t *testing.T) {
	branchID := uuid.New().String()
	mainItem := &graph.SearchResultItem{Object: testObject(), Score: 0.9}
	branchItem := &graph.SearchResultItem{
		Object:            testObject(),
		Score:             0.8,
		BranchID:          rstr(branchID),
		BranchName:        rstr("extraction/Invoice.pdf/1"),
		BranchOrigin:      rstr("extraction"),
		BranchOriginLabel: rstr("Invoice.pdf"),
	}
	res := &graph.SearchResponse{
		Data:  []*graph.SearchResultItem{mainItem, branchItem},
		Total: 2,
		Meta: &graph.SearchResponseMeta{
			BranchScopeNote: "read 2 of 3 matching branches",
			BranchesRead:    []string{"extraction/Invoice.pdf/1", "extraction/Payslip.pdf/1"},
			BranchesOmitted: 1,
		},
	}

	// Compact mode (the default for an agent) must still carry the provenance and
	// the note: an unmerged result presented without them reads as settled fact.
	got := slimSearchResponse(res, ResponseOpts{FieldStrategy: "compact"})
	data := got["data"].([]map[string]any)

	if _, ok := data[0]["branch_id"]; ok {
		t.Errorf("a main-graph item must carry no branch provenance: %v", data[0])
	}
	for key, want := range map[string]string{
		"branch_id":    branchID,
		"branch_name":  "extraction/Invoice.pdf/1",
		"origin":       "extraction",
		"origin_label": "Invoice.pdf",
	} {
		if got, _ := data[1][key].(string); got != want {
			t.Errorf("item[1][%q] = %q, want %q", key, got, want)
		}
	}

	meta, ok := got["meta"].(*graph.SearchResponseMeta)
	if !ok {
		t.Fatalf("meta should be present in compact mode when the search was scoped, got %T", got["meta"])
	}
	if meta.BranchesOmitted != 1 || len(meta.BranchesRead) != 2 {
		t.Errorf("meta = %+v, want 2 read and 1 omitted", meta)
	}

	// An unscoped response stays quiet in compact mode.
	unscoped := slimSearchResponse(&graph.SearchResponse{Data: []*graph.SearchResultItem{mainItem}, Total: 1, Meta: &graph.SearchResponseMeta{}}, ResponseOpts{})
	if _, ok := unscoped["meta"]; ok {
		t.Errorf("meta should stay verbose-only when no branch scope ran: %v", unscoped)
	}
}
