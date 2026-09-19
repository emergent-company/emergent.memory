package backups

import "testing"

func TestSelectColumnExpr(t *testing.T) {
	tests := []struct {
		name         string
		col          colInfo
		vectorListed bool
		wantExpr     string
		wantVector   bool
	}{
		{
			name:     "vector by udt",
			col:      colInfo{Name: "embedding", DataType: "USER-DEFINED", UDTName: "vector"},
			wantExpr: `t."embedding"::text AS "embedding"`, wantVector: true,
		},
		{
			name:     "halfvec by udt",
			col:      colInfo{Name: "embedding", DataType: "USER-DEFINED", UDTName: "halfvec"},
			wantExpr: `t."embedding"::text AS "embedding"`, wantVector: true,
		},
		{
			name:         "vector by config list",
			col:          colInfo{Name: "description_embedding", DataType: "USER-DEFINED", UDTName: "vector"},
			vectorListed: true,
			wantExpr:     `t."description_embedding"::text AS "description_embedding"`, wantVector: true,
		},
		{
			name:     "jsonb",
			col:      colInfo{Name: "properties", DataType: "jsonb", UDTName: "jsonb"},
			wantExpr: `t."properties"::text AS "properties"`, wantVector: false,
		},
		{
			name:     "json datatype",
			col:      colInfo{Name: "meta", DataType: "json", UDTName: "json"},
			wantExpr: `t."meta"::text AS "meta"`, wantVector: false,
		},
		{
			name:     "text array",
			col:      colInfo{Name: "labels", DataType: "ARRAY", UDTName: "_text"},
			wantExpr: `t."labels"::text AS "labels"`, wantVector: false,
		},
		{
			name:     "uuid array",
			col:      colInfo{Name: "created_object_ids", DataType: "ARRAY", UDTName: "_uuid"},
			wantExpr: `t."created_object_ids"::text AS "created_object_ids"`, wantVector: false,
		},
		{
			name:     "plain scalar",
			col:      colInfo{Name: "name", DataType: "text", UDTName: "text"},
			wantExpr: `t."name"`, wantVector: false,
		},
		{
			name:     "bytea stays raw",
			col:      colInfo{Name: "data", DataType: "bytea", UDTName: "bytea"},
			wantExpr: `t."data"`, wantVector: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expr, isVector := selectColumnExpr(tt.col, tt.vectorListed)
			if expr != tt.wantExpr {
				t.Errorf("expr = %q, want %q", expr, tt.wantExpr)
			}
			if isVector != tt.wantVector {
				t.Errorf("isVector = %v, want %v", isVector, tt.wantVector)
			}
		})
	}
}

func TestQuoteIdentAndSplitTable(t *testing.T) {
	if got := quoteIdent("weird\"name"); got != `"weird""name"` {
		t.Errorf("quoteIdent = %q", got)
	}
	schema, table := splitTable("kb.documents")
	if schema != "kb" || table != "documents" {
		t.Errorf("splitTable(kb.documents) = %q, %q", schema, table)
	}
	if schema, table := splitTable("documents"); schema != "kb" || table != "documents" {
		t.Errorf("splitTable(documents) = %q, %q", schema, table)
	}
}

func chatConversationsConfig(t *testing.T) tableConfig {
	t.Helper()
	for _, cfg := range allExportTables() {
		if cfg.name == "chat_conversations" {
			return cfg
		}
	}
	t.Fatal("chat_conversations not found in export tables")
	return tableConfig{}
}

func TestSoftNullResolution(t *testing.T) {
	cfg := chatConversationsConfig(t)
	colSet := map[string]bool{"object_id": true}

	t.Run("nulls reference when deleted excluded", func(t *testing.T) {
		joins, exprs := softNullResolution(cfg, false, colSet)
		if len(joins) != 1 || joins[0] != "LEFT JOIN kb.graph_objects go ON go.id = t.object_id" {
			t.Errorf("joins = %v, want [LEFT JOIN kb.graph_objects go ON go.id = t.object_id]", joins)
		}
		want := `CASE WHEN go.id IS NULL OR go.deleted_at IS NOT NULL THEN NULL ELSE t."object_id" END AS "object_id"`
		if exprs["object_id"] != want {
			t.Errorf("object_id expr = %q, want %q", exprs["object_id"], want)
		}
	})

	t.Run("keeps reference when deleted included", func(t *testing.T) {
		joins, exprs := softNullResolution(cfg, true, colSet)
		if joins != nil || exprs != nil {
			t.Errorf("joins=%v exprs=%v, want nil/nil", joins, exprs)
		}
	})

	t.Run("skips column absent from live schema", func(t *testing.T) {
		joins, exprs := softNullResolution(cfg, false, map[string]bool{"id": true})
		if joins != nil || exprs != nil {
			t.Errorf("joins=%v exprs=%v, want nil/nil", joins, exprs)
		}
	})
}

func TestDeletedColumnFilter(t *testing.T) {
	t.Run("filters when column present and deleted excluded", func(t *testing.T) {
		got := deletedColumnFilter(tableConfig{name: "documents", deletedColumn: "deleted_at"}, false, map[string]bool{"deleted_at": true})
		if got != `t."deleted_at" IS NULL` {
			t.Errorf("got %q, want %q", got, `t."deleted_at" IS NULL`)
		}
	})

	t.Run("no filter when deleted included", func(t *testing.T) {
		got := deletedColumnFilter(tableConfig{name: "documents", deletedColumn: "deleted_at"}, true, map[string]bool{"deleted_at": true})
		if got != "" {
			t.Errorf("got %q, want empty", got)
		}
	})

	t.Run("no filter when column absent from live schema", func(t *testing.T) {
		got := deletedColumnFilter(tableConfig{name: "documents", deletedColumn: "deleted_at"}, false, map[string]bool{"id": true})
		if got != "" {
			t.Errorf("got %q, want empty", got)
		}
	})

	t.Run("no filter when no deletedColumn configured", func(t *testing.T) {
		got := deletedColumnFilter(tableConfig{name: "tags"}, false, map[string]bool{"id": true})
		if got != "" {
			t.Errorf("got %q, want empty", got)
		}
	})
}
