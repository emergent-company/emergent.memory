package main

import (
	"encoding/json"
	"log"
	"path/filepath"
	"sort"
)

type Diff struct {
	CreatedAt string
	Forward   []byte
	Backward  []byte
}

func loadCompanyDiffs(dumpDir string, limit int) (map[string][]Diff, error) {
	log.Println("Loading company_diffs.csv.gz into memory...")
	ch, err := streamCSV(filepath.Join(dumpDir, "company_diffs.csv.gz"))
	if err != nil {
		return nil, err
	}

	diffs := make(map[string][]Diff)
	count := 0

	for row := range ch {
		if limit > 0 && count >= limit {
			break
		}

		companyID := row["company_id"]
		if companyID == "" {
			continue
		}

		diffs[companyID] = append(diffs[companyID], Diff{
			CreatedAt: row["created_at"],
			Forward:   []byte(row["forward"]),
			Backward:  []byte(row["backward"]),
		})
		count++
		if count%1000000 == 0 {
			log.Printf("Loaded %d diffs", count)
		}
	}

	log.Printf("Loaded %d diffs total for %d companies. Sorting chronologically...", count, len(diffs))

	for _, dList := range diffs {
		sort.Slice(dList, func(i, j int) bool {
			return dList[i].CreatedAt < dList[j].CreatedAt
		})
	}
	log.Println("Diffs sorted.")
	return diffs, nil
}

func applyPatch(state map[string]any, patch []byte) {
	if len(patch) == 0 {
		return
	}
	var p map[string]any
	if err := json.Unmarshal(patch, &p); err != nil {
		return
	}
	for k, v := range p {
		state[k] = v
	}
}

// applyPatchInverse applies the "backward" patch to undo changes, going backward in time.
// Note: backward contains the OLD value.
func applyPatchInverse(state map[string]any, backward []byte) {
	if len(backward) == 0 {
		return
	}
	var p map[string]any
	if err := json.Unmarshal(backward, &p); err != nil {
		return
	}
	for k, v := range p {
		state[k] = v
	}
}

func deepCopyMap(m map[string]any) map[string]any {
	res := make(map[string]any, len(m))
	for k, v := range m {
		res[k] = v
	}
	return res
}
