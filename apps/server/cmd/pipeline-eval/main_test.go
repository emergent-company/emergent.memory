package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestScoreTypes(t *testing.T) {
	golden := []string{"Person", "Organization", "Project"}
	cases := []struct {
		name       string
		discovered []string
		wantPrec   float64
		wantRec    float64
	}{
		{"exact", []string{"Person", "Organization", "Project"}, 1.0, 1.0},
		{"miss one", []string{"Person", "Organization"}, 1.0, 2.0 / 3.0},
		{"hallucinate one", []string{"Person", "Organization", "Project", "Widget"}, 0.75, 1.0},
		{"none", []string{}, 0, 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := scoreTypes(golden, tc.discovered)
			if !almostEqual(s.Precision, tc.wantPrec) {
				t.Fatalf("precision = %v, want %v", s.Precision, tc.wantPrec)
			}
			if !almostEqual(s.Recall, tc.wantRec) {
				t.Fatalf("recall = %v, want %v", s.Recall, tc.wantRec)
			}
		})
	}
}

func TestScoreTypesDedupsAndTrims(t *testing.T) {
	s := scoreTypes([]string{"Person"}, []string{" Person ", "Person", ""})
	if s.Found != 1 {
		t.Fatalf("found = %d, want 1 (dedup + trim + empty skip)", s.Found)
	}
	if s.TruePos != 1 {
		t.Fatalf("tp = %d, want 1", s.TruePos)
	}
}

func TestScoreTypesAliases(t *testing.T) {
	cases := []struct {
		name       string
		golden     []string
		discovered []string
		wantTP     int
		wantPrec   float64
		wantRec    float64
	}{
		{"decision synonym", []string{"DecisionRecord"}, []string{"Decision"}, 1, 1.0, 1.0},
		{"energy storage synonym", []string{"StorageSystem"}, []string{"EnergyStorageTechnology"}, 1, 1.0, 1.0},
		{"action item synonym", []string{"Task"}, []string{"ActionItem"}, 1, 1.0, 1.0},
		{"synonym plus exact mix", []string{"DecisionRecord", "Person"}, []string{"Decision", "Person"}, 2, 1.0, 1.0},
		{"spaced normalization", []string{"DecisionRecord"}, []string{"decision record"}, 1, 1.0, 1.0},
		{"unknown stays false positive", []string{"Person"}, []string{"Widget"}, 0, 0.0, 0.0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := scoreTypes(tc.golden, tc.discovered)
			if s.TruePos != tc.wantTP {
				t.Fatalf("tp = %d, want %d", s.TruePos, tc.wantTP)
			}
			if !almostEqual(s.Precision, tc.wantPrec) {
				t.Fatalf("precision = %v, want %v", s.Precision, tc.wantPrec)
			}
			if !almostEqual(s.Recall, tc.wantRec) {
				t.Fatalf("recall = %v, want %v", s.Recall, tc.wantRec)
			}
		})
	}
}

func TestLoadCorpus(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `{"name":"t","golden_types":["Person","Organization"],"min_types":1}`
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "docs", "a.md"), []byte("# A\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "docs", "b.md"), []byte("# B\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A non-.md file must be ignored.
	if err := os.WriteFile(filepath.Join(dir, "docs", "skip.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	m, docs, err := loadCorpus(dir)
	if err != nil {
		t.Fatalf("loadCorpus: %v", err)
	}
	if len(m.GoldenTypes) != 2 || m.GoldenTypes[0] != "Person" {
		t.Fatalf("golden types = %v", m.GoldenTypes)
	}
	if len(docs) != 2 {
		t.Fatalf("docs = %d, want 2", len(docs))
	}
	if docs[0].name != "a.md" || docs[1].name != "b.md" {
		t.Fatalf("docs sorted names = %q, %q", docs[0].name, docs[1].name)
	}
}

func almostEqual(a, b float64) bool {
	const eps = 1e-9
	d := a - b
	return d < eps && d > -eps
}
