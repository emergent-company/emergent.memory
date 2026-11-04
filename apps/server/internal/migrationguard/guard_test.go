package migrationguard

import (
	"strings"
	"testing"
)

func TestVersionFromFilename(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		want   int64
		wantOK bool
	}{
		{name: "valid lower snake", input: "00176_foo_bar.sql", want: 176, wantOK: true},
		{name: "bare number underscore", input: "00001_baseline.sql", want: 1, wantOK: true},
		{name: "root-relative path", input: "apps/server/migrations/00042_thing.sql", want: 42, wantOK: true},
		{name: "README", input: "README.md", want: 0, wantOK: false},
		{name: "no underscore version", input: "foo.sql", want: 0, wantOK: false},
		{name: "no leading zeros", input: "176_foo.sql", want: 0, wantOK: false},
		{name: "uppercase suffix", input: "00176_Foo.sql", want: 0, wantOK: false},
		{name: "missing extension", input: "00176_foo", want: 0, wantOK: false},
		{name: "empty", input: "", want: 0, wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := VersionFromFilename(tt.input)
			if ok != tt.wantOK {
				t.Fatalf("VersionFromFilename(%q) ok = %v, want %v", tt.input, ok, tt.wantOK)
			}
			if got != tt.want {
				t.Fatalf("VersionFromFilename(%q) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

func TestMaxVersion(t *testing.T) {
	tests := []struct {
		name   string
		names  []string
		want   int64
		wantOK bool
	}{
		{
			name:   "picks max",
			names:  []string{"00001_baseline.sql", "00176_foo_bar.sql", "00042_thing.sql"},
			want:   176,
			wantOK: true,
		},
		{
			name:   "ignores non-migrations",
			names:  []string{"README.md", "embed.go", "00176_foo_bar.sql"},
			want:   176,
			wantOK: true,
		},
		{
			name:   "root-relative paths",
			names:  []string{"apps/server/migrations/00001_baseline.sql", "apps/server/migrations/00176_foo_bar.sql"},
			want:   176,
			wantOK: true,
		},
		{
			name:   "only non-migrations",
			names:  []string{"README.md", "embed.go"},
			want:   0,
			wantOK: false,
		},
		{
			name:   "empty input",
			names:  []string{},
			want:   0,
			wantOK: false,
		},
		{
			name:   "nil input",
			names:  nil,
			want:   0,
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := MaxVersion(tt.names)
			if ok != tt.wantOK {
				t.Fatalf("MaxVersion(%v) ok = %v, want %v", tt.names, ok, tt.wantOK)
			}
			if got != tt.want {
				t.Fatalf("MaxVersion(%v) = %d, want %d", tt.names, got, tt.want)
			}
		})
	}
}

func TestFindViolations(t *testing.T) {
	tests := []struct {
		name    string
		added   []AddedMigration
		baseMax int64
		want    []Violation
	}{
		{
			name:    "no added migrations => none",
			added:   nil,
			baseMax: 176,
			want:    nil,
		},
		{
			name: "all added above baseMax => none",
			added: []AddedMigration{
				{Path: "apps/server/migrations/00177_a.sql", Version: 177},
				{Path: "apps/server/migrations/00178_b.sql", Version: 178},
			},
			baseMax: 176,
			want:    nil,
		},
		{
			name: "one added below baseMax => violation",
			added: []AddedMigration{
				{Path: "apps/server/migrations/00175_a.sql", Version: 175},
			},
			baseMax: 176,
			want: []Violation{
				{Path: "apps/server/migrations/00175_a.sql", Version: 175, BaseMax: 176},
			},
		},
		{
			name: "added below baseMax but exempt => none",
			added: []AddedMigration{
				{Path: "apps/server/migrations/00175_a.sql", Version: 175, Exempt: true, Reason: "filling gap"},
			},
			baseMax: 176,
			want:    nil,
		},
		{
			name: "baseMax 0 => none",
			added: []AddedMigration{
				{Path: "apps/server/migrations/00175_a.sql", Version: 175},
			},
			baseMax: 0,
			want:    nil,
		},
		{
			name: "negative baseMax => none",
			added: []AddedMigration{
				{Path: "apps/server/migrations/00001_a.sql", Version: 1},
			},
			baseMax: -1,
			want:    nil,
		},
		{
			name: "sorted by version then path",
			added: []AddedMigration{
				{Path: "apps/server/migrations/00174_z.sql", Version: 174},
				{Path: "apps/server/migrations/00172_b.sql", Version: 172},
				{Path: "apps/server/migrations/00174_a.sql", Version: 174},
			},
			baseMax: 176,
			want: []Violation{
				{Path: "apps/server/migrations/00172_b.sql", Version: 172, BaseMax: 176},
				{Path: "apps/server/migrations/00174_a.sql", Version: 174, BaseMax: 176},
				{Path: "apps/server/migrations/00174_z.sql", Version: 174, BaseMax: 176},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FindViolations(tt.added, tt.baseMax)
			if len(got) != len(tt.want) {
				t.Fatalf("FindViolations() len = %d, want %d; got = %#v", len(got), len(tt.want), got)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("FindViolations()[%d] = %#v, want %#v", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestExemptReason(t *testing.T) {
	tests := []struct {
		name    string
		content string
		wantOK  bool
		want    string
	}{
		{
			name:    "directive with reason",
			content: "-- out-of-order-migration-allowed: deliberately filling gap 00165-00169\n",
			wantOK:  true,
			want:    "deliberately filling gap 00165-00169",
		},
		{
			name:    "case-insensitive",
			content: "-- OUT-OF-ORDER-MIGRATION-ALLOWED: filling gap\n",
			wantOK:  true,
			want:    "filling gap",
		},
		{
			name:    "directive with empty reason",
			content: "-- out-of-order-migration-allowed:\n",
			wantOK:  false,
			want:    "",
		},
		{
			name:    "directive whitespace only reason",
			content: "-- out-of-order-migration-allowed:   \n",
			wantOK:  false,
			want:    "",
		},
		{
			name:    "no directive",
			content: "-- just a normal migration\nCREATE TABLE foo (id int);\n",
			wantOK:  false,
			want:    "",
		},
		{
			name:    "directive mid-file",
			content: "-- +goose Up\nCREATE TABLE foo (id int);\n-- out-of-order-migration-allowed: reason text\n",
			wantOK:  true,
			want:    "reason text",
		},
		{
			name:    "indented directive",
			content: "  -- out-of-order-migration-allowed: indented reason\n",
			wantOK:  true,
			want:    "indented reason",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := ExemptReason(tt.content)
			if ok != tt.wantOK {
				t.Fatalf("ExemptReason(%q) ok = %v, want %v", tt.content, ok, tt.wantOK)
			}
			if got != tt.want {
				t.Fatalf("ExemptReason(%q) = %q, want %q", tt.content, got, tt.want)
			}
		})
	}
}

func TestNextFreeVersion(t *testing.T) {
	tests := []struct {
		name    string
		baseMax int64
		added   []AddedMigration
		want    int64
	}{
		{name: "no additions", baseMax: 176, want: 177},
		{name: "low additions only", baseMax: 176, added: []AddedMigration{
			{Version: 172}, {Version: 175},
		}, want: 177},
		{name: "baseMax itself used by an addition", baseMax: 176, added: []AddedMigration{
			{Version: 176},
		}, want: 177},
		{name: "addition already occupies baseMax+1", baseMax: 176, added: []AddedMigration{
			{Version: 175}, {Version: 177},
		}, want: 178},
		{name: "consecutive high additions", baseMax: 176, added: []AddedMigration{
			{Version: 177}, {Version: 178}, {Version: 179},
		}, want: 180},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NextFreeVersion(tt.baseMax, tt.added); got != tt.want {
				t.Fatalf("NextFreeVersion(%d, %#v) = %d, want %d", tt.baseMax, tt.added, got, tt.want)
			}
		})
	}
}

func TestFormatViolationError(t *testing.T) {
	got := FormatViolationError(nil, 176, 177)
	if got != "" {
		t.Fatalf("FormatViolationError(nil) = %q, want empty", got)
	}

	violations := []Violation{
		{Path: "apps/server/migrations/00175_a.sql", Version: 175, BaseMax: 176},
	}
	got = FormatViolationError(violations, 176, 178)

	for _, want := range []string{
		"176",
		"apps/server/migrations/00175_a.sql",
		"175",
		"178",
		"git mv",
		"out-of-order-migration-allowed",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("FormatViolationError output missing %q:\n%s", want, got)
		}
	}
	// The suggested rename target must be the caller-supplied nextFree, not a
	// naive baseMax+1 that another added migration already occupies.
	if strings.Contains(got, "177_<same-name>.sql") {
		t.Errorf("FormatViolationError suggested a colliding version 177:\n%s", got)
	}
}
