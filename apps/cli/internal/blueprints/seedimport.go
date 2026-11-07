package blueprints

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// SeedImport is the result of reading the JSONL layout written by
// `memory blueprints dump`. Issues holds per-row parse and validation errors;
// a bad row is recorded and skipped instead of aborting the whole import.
type SeedImport struct {
	Objects       []SeedObjectRecord
	Relationships []SeedRelationshipRecord
	Issues        []BlueprintsResult
}

// FailedRows reports how many rows could not be read.
func (s *SeedImport) FailedRows() int {
	if s == nil {
		return 0
	}
	n := 0
	for _, r := range s.Issues {
		if r.Action == BlueprintsActionError {
			n++
		}
	}
	return n
}

// seedKind says how records from one input should be interpreted.
type seedKind int

const (
	seedKindAuto seedKind = iota
	seedKindObjects
	seedKindRelationships
)

// LoadSeedPath reads the JSONL seed layout produced by `memory blueprints dump`.
//
// Accepted paths:
//   - a dump root containing seed/objects/ and seed/relationships/
//   - a seed directory containing objects/ and relationships/
//   - the objects/ or relationships/ directory itself
//   - any directory of loose *.jsonl files (the record kind is detected per line)
//   - a single *.jsonl file
func LoadSeedPath(path string) (*SeedImport, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("reading seed path %q: %w", path, err)
	}
	if info.IsDir() {
		return loadSeedDir(path)
	}
	if !strings.EqualFold(filepath.Ext(path), ".jsonl") {
		return nil, fmt.Errorf("seed import expects a directory or a .jsonl file, got %q", path)
	}
	return loadSeedFile(path, seedKindAuto)
}

// loadSeedDir resolves the three dumper layouts and reads the records.
func loadSeedDir(dir string) (*SeedImport, error) {
	imp := &SeedImport{}

	root := dir
	if isDir(filepath.Join(dir, "seed")) {
		root = filepath.Join(dir, "seed")
	}

	objectsDir := filepath.Join(root, "objects")
	relsDir := filepath.Join(root, "relationships")
	hasObjects, hasRels := isDir(objectsDir), isDir(relsDir)
	if hasObjects || hasRels {
		if hasObjects {
			objs, issues := loadSeedObjects(objectsDir, nil)
			imp.Objects = append(imp.Objects, objs...)
			imp.Issues = append(imp.Issues, issues...)
		}
		if hasRels {
			rels, issues := loadSeedRelationships(relsDir, nil)
			imp.Relationships = append(imp.Relationships, rels...)
			imp.Issues = append(imp.Issues, issues...)
		}
		return imp, nil
	}

	// A bare directory of .jsonl files. Its name disambiguates the two seed
	// kinds when it is one of the standard leaf directories; otherwise each
	// line is classified on its own.
	kind := seedKindAuto
	switch strings.ToLower(filepath.Base(filepath.Clean(dir))) {
	case "objects":
		kind = seedKindObjects
	case "relationships":
		kind = seedKindRelationships
	}

	files, err := jsonlFiles(dir)
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no seed JSONL files found in %q: expected seed/objects/, seed/relationships/, or *.jsonl", dir)
	}

	for _, file := range files {
		part, ferr := loadSeedFile(file, kind)
		if ferr != nil {
			imp.Issues = append(imp.Issues, BlueprintsResult{
				ResourceType: "seed",
				Name:         filepath.Base(file),
				SourceFile:   file,
				Action:       BlueprintsActionError,
				Error:        ferr,
			})
			continue
		}
		imp.Objects = append(imp.Objects, part.Objects...)
		imp.Relationships = append(imp.Relationships, part.Relationships...)
		imp.Issues = append(imp.Issues, part.Issues...)
	}
	return imp, nil
}

// loadSeedFile reads one JSONL file. When kind is seedKindAuto each line is
// classified as an object or a relationship record independently.
func loadSeedFile(path string, kind seedKind) (*SeedImport, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}

	imp := &SeedImport{}
	untyped := isUntypedSeedFile(path)

	for i, raw := range splitLines(data) {
		lineNum := i + 1
		if len(raw) == 0 {
			continue
		}

		lineKind := kind
		if lineKind == seedKindAuto {
			lineKind = detectSeedLineKind(raw)
		}

		name := fmt.Sprintf("%s:%d", filepath.Base(path), lineNum)

		if lineKind == seedKindRelationships {
			var rec SeedRelationshipRecord
			if err := decodeJSONLine(raw, &rec); err != nil {
				imp.addIssue(path, name, err)
				continue
			}
			rec.SourceFile = path
			if rec.Type == "" && untyped {
				rec.Type = UntypedTypeName
			}
			if err := validateSeedRelationship(&rec); err != nil {
				imp.addIssue(path, name, err)
				continue
			}
			imp.Relationships = append(imp.Relationships, rec)
			continue
		}

		var rec SeedObjectRecord
		if err := decodeJSONLine(raw, &rec); err != nil {
			imp.addIssue(path, name, err)
			continue
		}
		rec.SourceFile = path
		if rec.Type == "" && untyped {
			rec.Type = UntypedTypeName
		}
		if err := validateSeedObject(&rec); err != nil {
			imp.addIssue(path, name, err)
			continue
		}
		imp.Objects = append(imp.Objects, rec)
	}

	return imp, nil
}

func (s *SeedImport) addIssue(path, name string, err error) {
	s.Issues = append(s.Issues, BlueprintsResult{
		ResourceType: "seed",
		Name:         name,
		SourceFile:   path,
		Action:       BlueprintsActionError,
		Error:        err,
	})
}

// detectSeedLineKind classifies one JSONL line. Relationship records carry a
// top-level srcKey/dstKey or srcId/dstId pair; anything else (including a line
// that is not valid JSON, so the caller can report the decode error) is
// treated as an object record.
func detectSeedLineKind(raw []byte) seedKind {
	var probe struct {
		SrcKey string `json:"srcKey"`
		DstKey string `json:"dstKey"`
		SrcID  string `json:"srcId"`
		DstID  string `json:"dstId"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return seedKindObjects
	}
	if probe.SrcKey != "" || probe.DstKey != "" || probe.SrcID != "" || probe.DstID != "" {
		return seedKindRelationships
	}
	return seedKindObjects
}

// jsonlFiles returns the .jsonl files directly inside dir, sorted so split
// parts (*.001.jsonl, *.002.jsonl, …) arrive in order.
func jsonlFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read dir %q: %w", dir, err)
	}
	var files []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".jsonl") {
			continue
		}
		files = append(files, filepath.Join(dir, entry.Name()))
	}
	sort.Strings(files)
	return files, nil
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
