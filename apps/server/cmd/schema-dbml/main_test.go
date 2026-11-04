package main

import (
	"strings"
	"testing"
)

func TestMapType(t *testing.T) {
	tests := map[string]string{
		"character varying(255)":      "varchar(255)",
		"character(2)":                "char(2)",
		"timestamp with time zone":    "timestamptz",
		"timestamp without time zone": "timestamp",
		"time with time zone":         "timetz",
		"double precision":            "float8",
		"bit varying(8)":              "varbit(8)",
		"uuid":                        "uuid",
		"vector(768)":                 "vector(768)",
		"text[]":                      "text[]",
	}
	for in, want := range tests {
		if got := mapType(in); got != want {
			t.Errorf("mapType(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFormatDefault(t *testing.T) {
	tests := map[string]string{
		"gen_random_uuid()":        "default: `gen_random_uuid()`",
		"'upload'::text":           "default: 'upload'",
		"'{}'::jsonb":              "default: '{}'",
		"true":                     "default: true",
		"false":                    "default: false",
		"1":                        "default: 1",
		"-0.5":                     "default: -0.5",
		"0":                        "default: 0",
		"nextval('seq'::regclass)": "default: `nextval('seq'::regclass)`",
		"now()::timestamptz":       "default: `now()`",
	}
	for in, want := range tests {
		if got := formatDefault(in); got != want {
			t.Errorf("formatDefault(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRenderIndexCols(t *testing.T) {
	tests := []struct {
		in   []string
		want string
	}{
		{[]string{"project_id"}, "(project_id)"},
		{[]string{"project_id", "content_hash"}, "(project_id, content_hash)"},
		{[]string{"lower(email)"}, "(`lower(email)`)"},
	}
	for _, tc := range tests {
		if got := renderIndexCols(tc.in); got != tc.want {
			t.Errorf("renderIndexCols(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestQuote(t *testing.T) {
	if got := quote("kb"); got != `"kb"` {
		t.Errorf("quote(kb) = %s", got)
	}
	if got := quote(`we"ird`); got != `"we""ird"` {
		t.Errorf("quote escaped = %s", got)
	}
	if got := qualify("kb", "documents"); got != `"kb"."documents"` {
		t.Errorf("qualify = %s", got)
	}
}

func TestEscapeString(t *testing.T) {
	if got := escapeString(`it's a \ note`); got != `it\'s a \\ note` {
		t.Errorf("escapeString = %q", got)
	}
}

func TestDefaultSetting(t *testing.T) {
	tests := []struct {
		typ, expr, want string
	}{
		// Serial/identity column: represented as increment, not nextval().
		{"integer", "nextval('typeorm_migrations_id_seq'::regclass)", "increment"},
		{"bigint", "nextval('x_seq'::regclass)", "increment"},
		// A sequence default on a non-integer type stays a plain default.
		{"text", "nextval('x_seq'::regclass)", "default: `nextval('x_seq'::regclass)`"},
		{"uuid", "gen_random_uuid()", "default: `gen_random_uuid()`"},
		{"integer", "1", "default: 1"},
		{"integer", "", ""},
	}
	for _, tc := range tests {
		if got := defaultSetting(tc.typ, tc.expr); got != tc.want {
			t.Errorf("defaultSetting(%q, %q) = %q, want %q", tc.typ, tc.expr, got, tc.want)
		}
	}
}

func TestRenderRefs(t *testing.T) {
	c := &catalog{
		fks: []foreignKey{
			{
				key:       tableKey{"kb", "chunks"},
				cols:      []string{"document_id"},
				refSchema: "kb", refTable: "documents", refCols: []string{"id"},
			},
			{
				// Part of the table's composite primary key: still many-to-one,
				// because link_id alone is not unique (one row per period).
				key:       tableKey{"kb", "agent_share_usage"},
				cols:      []string{"link_id"},
				refSchema: "kb", refTable: "agent_share_links", refCols: []string{"id"},
			},
			{
				key:       tableKey{"kb", "adk_states"},
				cols:      []string{"app_name", "user_id", "session_id"},
				refSchema: "kb", refTable: "adk_sessions", refCols: []string{"app_name", "user_id", "id"},
			},
			{
				// Duplicate of the first key: must not be emitted twice.
				key:       tableKey{"kb", "chunks"},
				cols:      []string{"document_id"},
				refSchema: "kb", refTable: "documents", refCols: []string{"id"},
			},
		},
	}

	got := renderRefs(c)
	want := []string{
		"// References",
		`Ref: "kb"."chunks"."document_id" > "kb"."documents"."id"`,
		`Ref: "kb"."agent_share_usage"."link_id" > "kb"."agent_share_links"."id"`,
		`Ref: "kb"."adk_states".("app_name", "user_id", "session_id") > "kb"."adk_sessions".("app_name", "user_id", "id")`,
	}

	if len(got) != len(want) {
		t.Fatalf("renderRefs returned %d lines, want %d:\n%s", len(got), len(want), strings.Join(got, "\n"))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d = %q, want %q", i, got[i], want[i])
		}
	}
}
