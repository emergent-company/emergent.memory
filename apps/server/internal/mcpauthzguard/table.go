package mcpauthzguard

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
)

// tableVersion is the current on-disk format version.
const tableVersion = 1

// tableHeader is prepended to the generated JSON so a human reader immediately
// understands what the file is and how it is maintained. It must stay stable so
// that -update regenerating an unchanged tree produces a byte-identical file.
//
// json.MarshalIndent is used, so the JSON body is deterministic given a sorted
// RawSQL slice.
const tableHeader = `// Raw-access census for apps/server/domain/mcp.
//
// This file enumerates every function under domain/mcp that issues raw SQL on
// the Service's bun.IDB handle (s.db.*) — SQL against other domains' tables
// that no import rule can see (#1089). Each entry names why the site is
// legitimate (read-only, project-scoped, or mcp-internal data-plane). The
// mcp-authz-guard fails when a function issues raw s.db SQL but is not listed
// here, so a NEW raw-access site must be classified and reviewed.
//
// Regenerate with: go run ./cmd/mcp-authz-guard -update
// This is a tripwire, not a proof of authorization: it catches NEW sites, not
// an indirect bypass through an already-classified function, nor a mutation of
// an existing query's WHERE clause. See internal/mcpauthzguard for the
// method-level Authorize* pairing rule that closes the #1040 shape.
`

// RawSQLEntry is one declared raw-SQL function.
type RawSQLEntry struct {
	File string `json:"file"`
	Func string `json:"func"`
	Why  string `json:"why"`
}

// Table is the on-disk raw-access census.
type Table struct {
	Version int           `json:"version"`
	RawSQL  []RawSQLEntry `json:"raw_sql"`
}

// LoadTable reads and validates the declared table from disk.
func LoadTable(path string) (*Table, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	// Strip the // comment header lines; json does not allow comments.
	var cleaned strings.Builder
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "//") {
			continue
		}
		cleaned.WriteString(line)
		cleaned.WriteString("\n")
	}
	var t Table
	if err := json.Unmarshal([]byte(cleaned.String()), &t); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if t.Version != tableVersion {
		return nil, fmt.Errorf("unsupported table version %d (want %d)", t.Version, tableVersion)
	}
	seen := map[string]bool{}
	for i, e := range t.RawSQL {
		if e.File == "" || e.Func == "" {
			return nil, fmt.Errorf("%s: entry %d has empty file/func", path, i)
		}
		if strings.TrimSpace(e.Why) == "" {
			return nil, fmt.Errorf("%s: entry %d (%s %s) has empty justification", path, i, e.File, e.Func)
		}
		key := e.File + " " + e.Func
		if seen[key] {
			return nil, fmt.Errorf("%s: duplicate raw-sql entry %s %s", path, e.File, e.Func)
		}
		seen[key] = true
	}
	sortRawSQL(t.RawSQL)
	return &t, nil
}

// SaveTable writes the table to disk, prepending the stable header.
func SaveTable(path string, t *Table) error {
	sortRawSQL(t.RawSQL)
	t.Version = tableVersion
	body, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal table: %w", err)
	}
	out := tableHeader + "\n" + string(body) + "\n"
	if err := os.WriteFile(path, []byte(out), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

func sortRawSQL(entries []RawSQLEntry) {
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].File != entries[j].File {
			return entries[i].File < entries[j].File
		}
		return entries[i].Func < entries[j].Func
	})
}
