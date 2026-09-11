package schemas

import (
	"encoding/json"
	"testing"
)

// TestGroupPackClaims covers the pack-centric grouping of joined
// kb.blueprint_pack_claims rows: rows for the same pack merge into one claim,
// pack order follows first appearance (query orders by pack name), blueprint
// order within a pack is preserved, and an empty input yields a non-nil slice
// that marshals to JSON [] rather than null.
func TestGroupPackClaims(t *testing.T) {
	t.Run("empty input yields empty JSON array", func(t *testing.T) {
		claims := groupPackClaims(nil)
		if claims == nil {
			t.Fatal("expected non-nil slice, got nil")
		}
		if len(claims) != 0 {
			t.Fatalf("expected 0 claims, got %d", len(claims))
		}
		raw, err := json.Marshal(claims)
		if err != nil {
			t.Fatalf("marshal claims: %v", err)
		}
		if string(raw) != "[]" {
			t.Fatalf("expected JSON [], got %s", raw)
		}
	})

	t.Run("groups rows by schema preserving order", func(t *testing.T) {
		rows := []packClaimRow{
			{SchemaID: "s1", Name: "Alpha", Version: "1.0.0", BlueprintID: "b1", BlueprintName: "First", BlueprintVersion: "2.0.0"},
			{SchemaID: "s1", Name: "Alpha", Version: "1.0.0", BlueprintID: "b2", BlueprintName: "Second", BlueprintVersion: "3.0.0"},
			{SchemaID: "s2", Name: "Beta", Version: "1.2.0", BlueprintID: "b3", BlueprintName: "Third", BlueprintVersion: "4.0.0"},
		}

		claims := groupPackClaims(rows)
		if len(claims) != 2 {
			t.Fatalf("expected 2 claims, got %d", len(claims))
		}

		if claims[0].SchemaID != "s1" || claims[0].Name != "Alpha" || claims[0].Version != "1.0.0" {
			t.Fatalf("unexpected first claim: %+v", claims[0])
		}
		if len(claims[0].Blueprints) != 2 {
			t.Fatalf("expected 2 blueprints on s1, got %d", len(claims[0].Blueprints))
		}
		if claims[0].Blueprints[0].BlueprintID != "b1" || claims[0].Blueprints[1].BlueprintID != "b2" {
			t.Fatalf("blueprint order not preserved: %+v", claims[0].Blueprints)
		}
		if claims[0].Blueprints[0].Name != "First" || claims[0].Blueprints[0].Version != "2.0.0" {
			t.Fatalf("unexpected first blueprint: %+v", claims[0].Blueprints[0])
		}

		if claims[1].SchemaID != "s2" || claims[1].Name != "Beta" || len(claims[1].Blueprints) != 1 {
			t.Fatalf("unexpected second claim: %+v", claims[1])
		}
	})

	t.Run("same schema id split across rows merges into first position", func(t *testing.T) {
		rows := []packClaimRow{
			{SchemaID: "s1", Name: "Alpha", Version: "1.0.0", BlueprintID: "b1", BlueprintName: "First", BlueprintVersion: "2.0.0"},
			{SchemaID: "s2", Name: "Beta", Version: "1.2.0", BlueprintID: "b3", BlueprintName: "Third", BlueprintVersion: "4.0.0"},
			{SchemaID: "s1", Name: "Alpha", Version: "1.0.0", BlueprintID: "b2", BlueprintName: "Second", BlueprintVersion: "3.0.0"},
		}

		claims := groupPackClaims(rows)
		if len(claims) != 2 {
			t.Fatalf("expected 2 claims, got %d", len(claims))
		}
		if claims[0].SchemaID != "s1" || len(claims[0].Blueprints) != 2 {
			t.Fatalf("expected s1 to hold both blueprints, got %+v", claims[0])
		}
		if claims[1].SchemaID != "s2" || len(claims[1].Blueprints) != 1 {
			t.Fatalf("unexpected s2 claim: %+v", claims[1])
		}
	})
}
