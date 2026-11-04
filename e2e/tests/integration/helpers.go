package integration

import (
	"encoding/json"
	"sort"
	"strings"
	"testing"
	"time"

	framework "github.com/emergent-company/runlog"
)

type graphObjectRow struct {
	ID          string         `json:"id,omitempty"`
	Type        string         `json:"type"`
	Key         string         `json:"key,omitempty"`
	CanonicalID string         `json:"canonical_id,omitempty"`
	Version     int            `json:"version"`
	DeletedAt   string         `json:"deleted_at,omitempty"`
	BranchID    string         `json:"branch_id,omitempty"`
	Properties  map[string]any `json:"properties,omitempty"`
}

type graphObjListResp struct {
	Items []graphObjectRow `json:"items"`
	Total int              `json:"total"`
}

type graphRelRow struct {
	ID      string `json:"id,omitempty"`
	Type    string `json:"type"`
	SrcID   string `json:"src_id,omitempty"`
	DstID   string `json:"dst_id,omitempty"`
	SrcType string `json:"src_type,omitempty"`
	DstType string `json:"dst_type,omitempty"`
}

type graphRelListResp struct {
	Items []graphRelRow `json:"items"`
	Total int           `json:"total"`
}

type extractionSnapshot struct {
	ObjectStats       map[string]objTypeStats
	RelStats          map[string]relTypeStats
	ExtractionJobInfo string
}

type objTypeStats struct {
	TotalObjects int
	OnBranch     int
	OnMain       int
	Version1     int
	VersionGe2   int
	Deleted      int
	WithKey      int
	WithoutKey   int
}

type relTypeStats struct {
	Total int
}

func snapshotExtraction(t *testing.T, rl *framework.RunLog, projectID string) extractionSnapshot {
	s := extractionSnapshot{
		ObjectStats: make(map[string]objTypeStats),
		RelStats:    make(map[string]relTypeStats),
	}

	// Query graph objects via CLI
	out, err := framework.RunCLIInDirWithHome(t, ".", "", "graph", "objects", "list", "--project", projectID, "--json")
	if err == nil && out != "" {
		var resp graphObjListResp
		if json.Unmarshal([]byte(out), &resp) == nil {
			for _, row := range resp.Items {
				stats := s.ObjectStats[row.Type]
				stats.TotalObjects++
				if row.BranchID != "" {
					stats.OnBranch++
				} else {
					stats.OnMain++
				}
				if row.DeletedAt != "" {
					stats.Deleted++
				}
				if row.Version == 1 {
					stats.Version1++
				} else {
					stats.VersionGe2++
				}
				if row.Key != "" {
					stats.WithKey++
				} else {
					stats.WithoutKey++
				}
				s.ObjectStats[row.Type] = stats
			}
		}
	}

	// Query relationships via CLI
	relOut, relErr := framework.RunCLIInDirWithHome(t, ".", "", "graph", "relationships", "list", "--project", projectID, "--json")
	if relErr == nil && relOut != "" {
		var relResp graphRelListResp
		if json.Unmarshal([]byte(relOut), &relResp) == nil {
			for _, row := range relResp.Items {
				rs := s.RelStats[row.Type]
				rs.Total++
				s.RelStats[row.Type] = rs
			}
		}
	}

	return s
}

func printExtractionSummary(t *testing.T, rl *framework.RunLog, label string, snap extractionSnapshot) {
	rl.Printf("=== %s ===", label)

	var typeNames []string
	for tn := range snap.ObjectStats {
		typeNames = append(typeNames, tn)
	}
	sort.Strings(typeNames)

	var totalObj, totalKey, totalNoKey, totalV1, totalVge2, totalDel, totalBranch, totalMain int
	for _, tn := range typeNames {
		os := snap.ObjectStats[tn]
		totalObj += os.TotalObjects
		totalKey += os.WithKey
		totalNoKey += os.WithoutKey
		totalV1 += os.Version1
		totalVge2 += os.VersionGe2
		totalDel += os.Deleted
		totalBranch += os.OnBranch
		totalMain += os.OnMain
		rl.Printf("  Objects %q: total=%d  branch=%d  main=%d  v1=%d  v2+=%d  del=%d  key=%d  nokey=%d",
			tn, os.TotalObjects, os.OnBranch, os.OnMain, os.Version1, os.VersionGe2, os.Deleted, os.WithKey, os.WithoutKey)
	}
	rl.Printf("  OBJECT TOTALS: %d objects  |  branch=%d  main=%d  |  key=%d  nokey=%d  |  v1=%d  v2+=%d  del=%d",
		totalObj, totalBranch, totalMain, totalKey, totalNoKey, totalV1, totalVge2, totalDel)

	var relTypeNames []string
	for rn := range snap.RelStats {
		relTypeNames = append(relTypeNames, rn)
	}
	sort.Strings(relTypeNames)
	var totalRel int
	for _, rn := range relTypeNames {
		totalRel += snap.RelStats[rn].Total
		rl.Printf("  Relationships %q: %d", rn, snap.RelStats[rn].Total)
	}
	rl.Printf("  RELATIONSHIP TOTAL: %d", totalRel)
}

func printDiffTable(t *testing.T, rl *framework.RunLog, pass1, pass2 extractionSnapshot) {
	rl.Printf("")
	rl.Printf("╔══════════════════════════════════════════════════════════════════════╗")
	rl.Printf("║              RE-EXTRACTION COMPARISON SUMMARY                      ║")
	rl.Printf("╚══════════════════════════════════════════════════════════════════════╝")

	allObjTypes := map[string]bool{}
	for tn := range pass1.ObjectStats {
		allObjTypes[tn] = true
	}
	for tn := range pass2.ObjectStats {
		allObjTypes[tn] = true
	}
	var sortedObjTypes []string
	for tn := range allObjTypes {
		sortedObjTypes = append(sortedObjTypes, tn)
	}
	sort.Strings(sortedObjTypes)

	rl.Printf("")
	rl.Printf("── Graph Objects (by type) ──")
	rl.Printf("  %-25s %6s %6s %+6s %+6s", "TYPE", "PASS1", "PASS2", "NEW", "DEL")
	rl.Printf("  %-25s %6s %6s %6s %6s", strings.Repeat("─", 25), strings.Repeat("─", 6), strings.Repeat("─", 6), strings.Repeat("─", 6), strings.Repeat("─", 6))
	for _, tn := range sortedObjTypes {
		o1 := pass1.ObjectStats[tn]
		o2 := pass2.ObjectStats[tn]
		rl.Printf("  %-25s %6d %6d %+6d %+6d",
			tn, o1.TotalObjects, o2.TotalObjects, o2.TotalObjects-o1.TotalObjects, o2.Deleted-o1.Deleted)
	}

	var total1, total2, total2Vge2 int
	for _, os := range pass1.ObjectStats {
		total1 += os.TotalObjects
	}
	for _, os := range pass2.ObjectStats {
		total2 += os.TotalObjects
		total2Vge2 += os.VersionGe2
	}

	rl.Printf("")
	rl.Printf("── Overall Summary ──")
	rl.Printf("  Total objects pass1: %d  |  pass2: %d", total1, total2)
	rl.Printf("  Net new objects:     %+d", total2-total1)
	rl.Printf("  v2+ in pass2:        %d", total2Vge2)

	if total2Vge2 == 0 && (total2-total1) == 0 {
		rl.Printf("  ✅ IDENTICAL — dedup avoided duplicates")
	} else if total2Vge2 > 0 && (total2-total1) >= 0 {
		rl.Printf("  ⚠ New versions or objects created")
	} else {
		rl.Printf("  ⚠ Object count changed: diff=%+d  v2+=%d", total2-total1, total2Vge2)
	}

	allRelTypes := map[string]bool{}
	for rn := range pass1.RelStats {
		allRelTypes[rn] = true
	}
	for rn := range pass2.RelStats {
		allRelTypes[rn] = true
	}
	var sortedRelTypes []string
	for rn := range allRelTypes {
		sortedRelTypes = append(sortedRelTypes, rn)
	}
	sort.Strings(sortedRelTypes)

	rl.Printf("")
	rl.Printf("── Relationships (by type) ──")
	rl.Printf("  %-30s %8s %8s %8s", "TYPE", "PASS1", "PASS2", "DIFF")
	rl.Printf("  %-30s %8s %8s %8s", strings.Repeat("─", 30), strings.Repeat("─", 8), strings.Repeat("─", 8), strings.Repeat("─", 8))
	var relTotal1, relTotal2 int
	for _, rn := range sortedRelTypes {
		r1 := pass1.RelStats[rn].Total
		r2 := pass2.RelStats[rn].Total
		relTotal1 += r1
		relTotal2 += r2
		rl.Printf("  %-30s %8d %8d %+8d", rn, r1, r2, r2-r1)
	}
	rl.Printf("  %-30s %8s %8s %8s", strings.Repeat("─", 30), strings.Repeat("─", 8), strings.Repeat("─", 8), strings.Repeat("─", 8))
	rl.Printf("  %-30s %8d %8d %+8d", "TOTAL", relTotal1, relTotal2, relTotal2-relTotal1)
}

func progressDiff(t *testing.T, rl *framework.RunLog, before, after extractionSnapshot, labelA, labelB string) {
	allTypes := map[string]bool{}
	for tn := range before.ObjectStats {
		allTypes[tn] = true
	}
	for tn := range after.ObjectStats {
		allTypes[tn] = true
	}
	var sorted []string
	for tn := range allTypes {
		sorted = append(sorted, tn)
	}
	sort.Strings(sorted)

	rl.Printf("  %-25s %10s %10s %8s %8s", "TYPE", labelA, labelB, "NEW", "UPDATED")
	rl.Printf("  %-25s %10s %10s %8s %8s", strings.Repeat("─", 25), strings.Repeat("─", 10), strings.Repeat("─", 10), strings.Repeat("─", 8), strings.Repeat("─", 8))
	var totA, totB, totNew, totUpd int
	for _, tn := range sorted {
		a := before.ObjectStats[tn]
		b := after.ObjectStats[tn]
		newCount := b.TotalObjects - a.TotalObjects
		updCount := b.VersionGe2 - a.VersionGe2
		totA += a.TotalObjects
		totB += b.TotalObjects
		totNew += max(0, newCount)
		totUpd += max(0, updCount)
		if newCount != 0 || updCount != 0 {
			rl.Printf("  %-25s %10d %10d %+8d %+8d", tn, a.TotalObjects, b.TotalObjects, newCount, updCount)
		}
	}

	relsA, relsB := 0, 0
	for _, rs := range before.RelStats {
		relsA += rs.Total
	}
	for _, rs := range after.RelStats {
		relsB += rs.Total
	}

	rl.Printf("  %-25s %10s %10s %8s %8s", strings.Repeat("─", 25), strings.Repeat("─", 10), strings.Repeat("─", 10), strings.Repeat("─", 8), strings.Repeat("─", 8))
	rl.Printf("  %-25s %10d %10d %+8d %+8d", "TOTAL", totA, totB, totB-totA, totUpd)
	rl.Printf("  relationships: %d → %d (+%d)", relsA, relsB, relsB-relsA)
}

func (s extractionSnapshot) TotalObjects() int {
	n := 0
	for _, os := range s.ObjectStats {
		n += os.TotalObjects
	}
	return n
}

// ---------------------------------------------------------------------------
// Entity verification helpers (CLI-based)
// ---------------------------------------------------------------------------

// findEntityByName searches graph objects by type and name via CLI.
func findEntityByName(t *testing.T, rl *framework.RunLog, projectID, typeName, name string) *graphObjectRow {
	out, err := framework.RunCLIInDirWithHome(t, ".", "", "graph", "objects", "list",
		"--project", projectID, "--type", typeName, "--json")
	if err != nil || out == "" {
		return nil
	}
	var resp graphObjListResp
	if json.Unmarshal([]byte(out), &resp) != nil {
		return nil
	}
	for _, row := range resp.Items {
		if row.DeletedAt != "" {
			continue
		}
		for _, v := range row.Properties {
			if s, ok := v.(string); ok {
				if caseInsensitiveContains(s, name) {
					return &row
				}
			}
		}
	}
	return nil
}

func caseInsensitiveContains(s, substr string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(substr))
}

// assertEntityExists fuzzy-matches an entity by name across possible types.
func assertEntityExists(t *testing.T, rl *framework.RunLog, projectID string, e groundTruthEntity, timeout time.Duration) *graphObjectRow {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		for _, typeName := range e.TypeHints {
			if row := findEntityByName(t, rl, projectID, typeName, e.Name); row != nil {
				rl.Printf("  ✅ %q → %s (key=%s, v%d, cid=%s)",
					e.Name, row.Type, row.Key, row.Version, truncateCID(row.CanonicalID))
				return row
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	rl.Printf("  ❌ %q not found via %v after %v", e.Name, e.TypeHints, timeout)
	return nil
}

func truncateCID(cid string) string {
	if len(cid) > 8 {
		return cid[:8]
	}
	return cid
}

func assertNoDanglingRelationships(t *testing.T, rl *framework.RunLog, projectID string) {
	objOut, _ := framework.RunCLIInDirWithHome(t, ".", "", "graph", "objects", "list", "--project", projectID, "--json")
	relOut, _ := framework.RunCLIInDirWithHome(t, ".", "", "graph", "relationships", "list", "--project", projectID, "--json")

	var objResp graphObjListResp
	var relResp graphRelListResp

	json.Unmarshal([]byte(objOut), &objResp)
	json.Unmarshal([]byte(relOut), &relResp)

	ids := make(map[string]bool)
	for _, o := range objResp.Items {
		ids[o.ID] = true
	}

	var dangling int
	for _, r := range relResp.Items {
		if !ids[r.SrcID] || !ids[r.DstID] {
			dangling++
		}
	}

	if dangling > 0 {
		rl.Printf("  ❌ found %d dangling relationship references", dangling)
	} else {
		rl.Printf("  ✅ no dangling relationship references")
	}
}

var capturedCanonicalIDs = map[string]string{}

func verifyExtraction(t *testing.T, rl *framework.RunLog, projectID, label string, expected []groundTruthEntity) {
	rl.Printf("  ── Ground-truth verification for %s ──", label)
	found := 0
	for _, e := range expected {
		obj := assertEntityExists(t, rl, projectID, e, 10*time.Second)
		if obj == nil {
			continue
		}
		found++

		cidStr := obj.CanonicalID
		if prev, seen := capturedCanonicalIDs[e.Name]; seen {
			if cidStr != prev {
				rl.Printf("  ❌ %q canonical_id changed: %s → %s", e.Name, truncateCID(prev), truncateCID(cidStr))
			}
		} else {
			capturedCanonicalIDs[e.Name] = cidStr
		}
	}
	rl.Printf("  ✅ %d/%d expected entities verified for %s", found, len(expected), label)
	if found < len(expected) {
		rl.Printf("  ⚠ %d missing — LLM-dependent (non-fatal)", len(expected)-found)
	}
}
