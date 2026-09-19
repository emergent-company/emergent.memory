package scheduler

import (
	"strings"
	"testing"
)

func TestPgMajorFromVersionNum(t *testing.T) {
	tests := []struct {
		name string
		num  int
		want int
	}{
		{name: "pg17.11", num: 170011, want: 17},
		{name: "pg16.5", num: 160005, want: 16},
		{name: "pg15", num: 150000, want: 15},
		{name: "zero", num: 0, want: 0},
		{name: "future pg18", num: 180000, want: 18},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := pgMajorFromVersionNum(tt.num); got != tt.want {
				t.Errorf("pgMajorFromVersionNum(%d) = %d, want %d", tt.num, got, tt.want)
			}
		})
	}
}

func TestPgDumpMajorFromVersionOutput(t *testing.T) {
	tests := []struct {
		name    string
		out     string
		want    int
		wantErr bool
	}{
		{
			name: "debian pgdg 17",
			out:  "pg_dump (PostgreSQL) 17.11 (Debian 17.11-3.pgdg12+1)",
			want: 17,
		},
		{
			name: "plain 16",
			out:  "pg_dump (PostgreSQL) 16.5",
			want: 16,
		},
		{
			name:    "missing marker",
			out:     "some unrelated output",
			wantErr: true,
		},
		{
			name:    "empty output",
			out:     "",
			wantErr: true,
		},
		{
			name:    "no number after marker",
			out:     "pg_dump (PostgreSQL) ",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := pgDumpMajorFromVersionOutput(tt.out)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("pgDumpMajorFromVersionOutput(%q) = %d, want error", tt.out, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("pgDumpMajorFromVersionOutput(%q) unexpected error: %v", tt.out, err)
			}
			if got != tt.want {
				t.Errorf("pgDumpMajorFromVersionOutput(%q) = %d, want %d", tt.out, got, tt.want)
			}
		})
	}
}

func TestCheckPgDumpCompatible(t *testing.T) {
	tests := []struct {
		name        string
		dumpMajor   int
		serverMajor int
		wantErr     bool
	}{
		{name: "equal majors", dumpMajor: 17, serverMajor: 17, wantErr: false},
		{name: "newer client", dumpMajor: 18, serverMajor: 17, wantErr: false},
		{name: "older client", dumpMajor: 16, serverMajor: 17, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := checkPgDumpCompatible(tt.dumpMajor, tt.serverMajor)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("checkPgDumpCompatible(%d, %d) = nil, want error", tt.dumpMajor, tt.serverMajor)
				}
				if !strings.Contains(err.Error(), "PG_CLIENT_MAJOR") {
					t.Errorf("error %q should name PG_CLIENT_MAJOR", err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("checkPgDumpCompatible(%d, %d) unexpected error: %v", tt.dumpMajor, tt.serverMajor, err)
			}
		})
	}
}
