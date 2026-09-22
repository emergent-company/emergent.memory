package migrate

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

const rawGapError = `error: found 2 missing migrations before current version 172:
	version 170: 00170_embedding_indexes_hnsw.sql
	version 171: 00171_graph_relationships_embedding_hnsw.sql`

func TestDiagnoseError(t *testing.T) {
	tests := []struct {
		name        string
		in          error
		wantChanged bool // whether a *MissingMigrationsError is expected (vs unchanged)
		wantCount   int
		wantCurrent int64
		wantMissing []MissingMigration
	}{
		{
			name:        "nil returns nil",
			in:          nil,
			wantChanged: false,
		},
		{
			name:        "non-gap error returned unchanged",
			in:          errors.New("some unrelated failure"),
			wantChanged: false,
		},
		{
			name:        "raw goose gap error",
			in:          errors.New(rawGapError),
			wantChanged: true,
			wantCount:   2,
			wantCurrent: 172,
			wantMissing: []MissingMigration{
				{Version: 170, Filename: "00170_embedding_indexes_hnsw.sql"},
				{Version: 171, Filename: "00171_graph_relationships_embedding_hnsw.sql"},
			},
		},
		{
			name:        "wrapped gap error",
			in:          fmt.Errorf("failed to run migrations: %w", errors.New(rawGapError)),
			wantChanged: true,
			wantCount:   2,
			wantCurrent: 172,
			wantMissing: []MissingMigration{
				{Version: 170, Filename: "00170_embedding_indexes_hnsw.sql"},
				{Version: 171, Filename: "00171_graph_relationships_embedding_hnsw.sql"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DiagnoseError(tt.in)

			if !tt.wantChanged {
				if tt.in == nil {
					if got != nil {
						t.Fatalf("DiagnoseError(nil) = %v, want nil", got)
					}
					return
				}
				if got != tt.in {
					t.Fatalf("DiagnoseError(%v) = %v, want unchanged original", tt.in, got)
				}
				return
			}

			me, ok := got.(*MissingMigrationsError)
			if !ok {
				t.Fatalf("DiagnoseError(%v) = %T, want *MissingMigrationsError", tt.in, got)
			}

			if me.Count != tt.wantCount {
				t.Errorf("Count = %d, want %d", me.Count, tt.wantCount)
			}
			if me.CurrentVersion != tt.wantCurrent {
				t.Errorf("CurrentVersion = %d, want %d", me.CurrentVersion, tt.wantCurrent)
			}
			if len(me.Missing) != len(tt.wantMissing) {
				t.Fatalf("len(Missing) = %d, want %d", len(me.Missing), len(tt.wantMissing))
			}
			for i, want := range tt.wantMissing {
				if me.Missing[i] != want {
					t.Errorf("Missing[%d] = %+v, want %+v", i, me.Missing[i], want)
				}
			}

			text := me.Error()
			if !strings.Contains(text, "-allow-missing") {
				t.Errorf("Error() text missing remediation string -allow-missing:\n%s", text)
			}
			if !strings.Contains(text, "172") {
				t.Errorf("Error() text missing current version:\n%s", text)
			}
			for _, want := range tt.wantMissing {
				if !strings.Contains(text, fmt.Sprintf("version %d: %s", want.Version, want.Filename)) {
					t.Errorf("Error() text missing entry version %d: %s:\n%s", want.Version, want.Filename, text)
				}
			}
			if !strings.Contains(text, rawGapError) {
				t.Errorf("Error() text should append raw goose error:\n%s", text)
			}
		})
	}
}

func TestMissingMigrationsErrorUnwrap(t *testing.T) {
	raw := errors.New(rawGapError)
	wrapped := fmt.Errorf("failed to run migrations: %w", raw)

	got := DiagnoseError(wrapped)
	me, ok := got.(*MissingMigrationsError)
	if !ok {
		t.Fatalf("DiagnoseError(%v) = %T, want *MissingMigrationsError", wrapped, got)
	}

	if me.Unwrap() != wrapped {
		t.Errorf("Unwrap() = %v, want the wrapped error %v", me.Unwrap(), wrapped)
	}

	// errors.Is traverses the Unwrap chain to the raw goose error.
	if !errors.Is(got, raw) {
		t.Errorf("errors.Is(%v, raw) = false, want true", got)
	}

	// The wrapped error must remain discoverable through Unwrap too.
	var me2 *MissingMigrationsError
	if !errors.As(got, &me2) {
		t.Errorf("errors.As(got, *MissingMigrationsError) = false, want true")
	}
	if me2 != me {
		t.Errorf("errors.As(got) = %p, want the same error %p", me2, me)
	}
}
