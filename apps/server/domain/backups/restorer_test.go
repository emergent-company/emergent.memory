package backups

import (
	"testing"
)

func TestBindValue(t *testing.T) {
	tests := []struct {
		name    string
		col     dbColumn
		v       any
		wantExp string
		wantArg any
		wantNil bool
		wantErr bool
	}{
		{
			name:    "null value",
			col:     dbColumn{Name: "properties", DataType: "jsonb", UDTName: "jsonb"},
			v:       nil,
			wantExp: "NULL", wantNil: true,
		},
		{
			name:    "vector udt",
			col:     dbColumn{Name: "embedding", DataType: "USER-DEFINED", UDTName: "vector"},
			v:       []any{0.1, 0.2, 0.3},
			wantExp: "?::vector", wantArg: "[0.1,0.2,0.3]",
		},
		{
			name:    "vector empty",
			col:     dbColumn{Name: "embedding", DataType: "USER-DEFINED", UDTName: "vector"},
			v:       []any{},
			wantExp: "NULL", wantNil: true,
		},
		{
			name:    "vector bad type",
			col:     dbColumn{Name: "embedding", DataType: "USER-DEFINED", UDTName: "vector"},
			v:       "not-an-array",
			wantErr: true,
		},
		{
			name:    "halfvec udt",
			col:     dbColumn{Name: "embedding", DataType: "USER-DEFINED", UDTName: "halfvec"},
			v:       []any{1.5, 2.5},
			wantExp: "?::halfvec", wantArg: "[1.5,2.5]",
		},
		{
			name:    "jsonb string (exporter ::text cast)",
			col:     dbColumn{Name: "properties", DataType: "jsonb", UDTName: "jsonb"},
			v:       `{"age": 26}`,
			wantExp: "?::jsonb", wantArg: `{"age": 26}`,
		},
		{
			name:    "jsonb object",
			col:     dbColumn{Name: "properties", DataType: "jsonb", UDTName: "jsonb"},
			v:       map[string]any{"a": float64(1)},
			wantExp: "?::jsonb", wantArg: `{"a":1}`,
		},
		{
			name:    "json datatype string",
			col:     dbColumn{Name: "meta", DataType: "json", UDTName: "json"},
			v:       `[1,2]`,
			wantExp: "?::jsonb", wantArg: `[1,2]`,
		},
		{
			name:    "array string literal (exporter ::text cast)",
			col:     dbColumn{Name: "labels", DataType: "ARRAY", UDTName: "_text"},
			v:       `{note,fact}`,
			wantExp: "?::_text", wantArg: `{note,fact}`,
		},
		{
			name:    "array json array",
			col:     dbColumn{Name: "labels", DataType: "ARRAY", UDTName: "_text"},
			v:       []any{"note", "fact"},
			wantExp: "?::_text", wantArg: `{"note","fact"}`,
		},
		{
			name:    "array uuid json array",
			col:     dbColumn{Name: "created_object_ids", DataType: "ARRAY", UDTName: "_uuid"},
			v:       []any{"11111111-1111-1111-1111-111111111111", "22222222-2222-2222-2222-222222222222"},
			wantExp: "?::_uuid",
			wantArg: `{"11111111-1111-1111-1111-111111111111","22222222-2222-2222-2222-222222222222"}`,
		},
		{
			name:    "array bad type",
			col:     dbColumn{Name: "labels", DataType: "ARRAY", UDTName: "_text"},
			v:       int64(42),
			wantErr: true,
		},
		{
			name:    "bytea",
			col:     dbColumn{Name: "data", DataType: "bytea", UDTName: "bytea"},
			v:       "aGVsbG8=",
			wantExp: "decode(?::text, 'hex')", wantArg: "68656c6c6f",
		},
		{
			name:    "boolean",
			col:     dbColumn{Name: "flag", DataType: "boolean"},
			v:       true,
			wantExp: "?::boolean", wantArg: "true",
		},
		{
			name:    "integer",
			col:     dbColumn{Name: "count", DataType: "integer"},
			v:       float64(42),
			wantExp: "?::integer", wantArg: "42",
		},
		{
			name:    "default text",
			col:     dbColumn{Name: "name", DataType: "text", UDTName: "text"},
			v:       "hello",
			wantExp: "?::text", wantArg: "hello",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expr, arg, isNull, err := bindValue(tt.col, tt.v)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got expr=%q arg=%v null=%v", expr, arg, isNull)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if expr != tt.wantExp {
				t.Errorf("expr = %q, want %q", expr, tt.wantExp)
			}
			if isNull != tt.wantNil {
				t.Errorf("isNull = %v, want %v", isNull, tt.wantNil)
			}
			if !tt.wantNil && arg != tt.wantArg {
				t.Errorf("arg = %v (%T), want %v (%T)", arg, arg, tt.wantArg, tt.wantArg)
			}
		})
	}
}

func TestFormatArrayLiteral(t *testing.T) {
	tests := []struct {
		name    string
		v       any
		want    string
		wantErr bool
	}{
		{name: "empty", v: []any{}, want: "{}"},
		{name: "strings", v: []any{"a", "b"}, want: `{"a","b"}`},
		{name: "escapes quote and backslash", v: []any{`a"b`, `c\d`}, want: `{"a\"b","c\\d"}`},
		{name: "mixed nil number bool", v: []any{nil, 1.5, true}, want: `{NULL,1.5,true}`},
		{name: "non-array", v: "x", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := formatArrayLiteral(tt.v)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestToFloat32Slice(t *testing.T) {
	got, ok := toFloat32Slice([]any{0.5, 1.5})
	if !ok || len(got) != 2 || got[0] != 0.5 || got[1] != 1.5 {
		t.Fatalf("toFloat32Slice([]any{0.5,1.5}) = %v, %v", got, ok)
	}
	if _, ok := toFloat32Slice("nope"); ok {
		t.Fatal("expected ok=false for non-array")
	}
	if _, ok := toFloat32Slice([]any{"x"}); ok {
		t.Fatal("expected ok=false for non-numeric element")
	}
}

func TestScalarHelpers(t *testing.T) {
	if got := isNumericType("bigint"); !got {
		t.Error("isNumericType(bigint) = false")
	}
	if got := isNumericType("text"); got {
		t.Error("isNumericType(text) = true")
	}
	if got := scalarString("hi"); got != "hi" {
		t.Errorf("scalarString(hi) = %q", got)
	}
	if got := boolString(true); got != "true" {
		t.Errorf("boolString(true) = %q", got)
	}
	if got := stringValue(nil); got != "" {
		t.Errorf("stringValue(nil) = %q", got)
	}
	if got := quoteArrayElement(`a"b`); got != `"a\"b"` {
		t.Errorf("quoteArrayElement = %q", got)
	}
}

func TestRemapRowUUIDsNullsOwnerUserID(t *testing.T) {
	remap := map[string]string{}
	cols := []dbColumn{
		{Name: "id", DataType: "uuid", UDTName: "uuid"},
		{Name: "owner_user_id", DataType: "uuid", UDTName: "uuid"},
	}
	row := map[string]any{
		"id":            "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa",
		"owner_user_id": "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb",
	}
	remapRowUUIDs(row, remap, []string{"owner_user_id"}, cols)

	if row["id"] == "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa" {
		t.Error("row id was not remapped to a fresh UUID")
	}
	if row["owner_user_id"] != nil {
		t.Errorf("owner_user_id = %v, want nil (foreign owner must be nulled on clone)", row["owner_user_id"])
	}
}

func TestFilterCloneMembershipsDropsForeignUsers(t *testing.T) {
	r := &Restorer{}
	m := &membershipFilter{
		sameOrg:         false, // imported archives always filter
		targetOrgUsers:  map[string]bool{"member-1": true},
		createdBy:       "restorer-1",
		targetProjectID: "proj-1",
	}
	rows := []map[string]any{
		{"user_id": "member-1", "role": "project_admin"},
		{"user_id": "foreign-1", "role": "editor"},
	}

	got := r.filterCloneMemberships(rows, m)
	if len(got) != 2 {
		t.Fatalf("filterCloneMemberships returned %d rows, want 2 (member-1 + restorer-1)", len(got))
	}

	seen := map[string]bool{}
	for _, row := range got {
		seen[stringValue(row["user_id"])] = true
	}
	if !seen["member-1"] {
		t.Error("expected member-1 to be retained")
	}
	if seen["foreign-1"] {
		t.Error("expected foreign-1 to be dropped")
	}
	if !seen["restorer-1"] {
		t.Error("expected restorer-1 to be added")
	}
}
