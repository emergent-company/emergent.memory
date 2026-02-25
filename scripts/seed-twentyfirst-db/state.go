package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const StateDir = "/tmp/twentyfirst_seed_state"

const (
	PhaseCompaniesPending        = "companies_pending"
	PhaseCompaniesDone           = "companies_done"
	PhasePeoplePending           = "people_pending"
	PhasePeopleDone              = "people_done"
	PhaseFinancialReportsPending = "financial_reports_pending"
	PhaseFinancialReportsDone    = "financial_reports_done"
	PhaseRelsPending             = "rels_pending"
	PhaseDone                    = "done"
)

type SeedState struct {
	Phase string `json:"phase"`
}

func loadState() SeedState {
	data, err := os.ReadFile(filepath.Join(StateDir, "state.json"))
	if err != nil {
		return SeedState{Phase: PhaseCompaniesPending}
	}
	var s SeedState
	if err := json.Unmarshal(data, &s); err != nil {
		return SeedState{Phase: PhaseCompaniesPending}
	}
	return s
}

func saveState(s SeedState) {
	os.MkdirAll(StateDir, 0755)
	data, _ := json.MarshalIndent(s, "", "  ")
	os.WriteFile(filepath.Join(StateDir, "state.json"), data, 0644)
}

func loadIDMap() (map[string]string, error) {
	m := make(map[string]string)

	// Legacy json map
	data, err := os.ReadFile(filepath.Join(StateDir, "idmap.json"))
	if err == nil {
		json.Unmarshal(data, &m)
	}

	// Incremental text map
	f, err := os.Open(filepath.Join(StateDir, "idmap_incremental.txt"))
	if err == nil {
		defer f.Close()
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			parts := strings.SplitN(scanner.Text(), "\t", 2)
			if len(parts) == 2 {
				m[parts[0]] = parts[1]
			}
		}
	}
	return m, nil
}

func saveIDMap(idMap map[string]string) {
	// Not needed anymore since we append incrementally, but we keep it for backward compatibility
	os.MkdirAll(StateDir, 0755)
	data, _ := json.Marshal(idMap)
	os.WriteFile(filepath.Join(StateDir, "idmap.json"), data, 0644)
}

func appendToIdMap(key, canonicalID string) {
	f, err := os.OpenFile(filepath.Join(StateDir, "idmap_incremental.txt"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err == nil {
		defer f.Close()
		fmt.Fprintf(f, "%s\t%s\n", key, canonicalID)
	}
}

func loadRelsDone() map[int]bool {
	done := make(map[int]bool)
	f, err := os.Open(filepath.Join(StateDir, "rels_done.txt"))
	if err != nil {
		return done
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if idx, err := strconv.Atoi(line); err == nil {
			done[idx] = true
		}
	}
	return done
}

func appendRelDone(idx int) {
	f, err := os.OpenFile(filepath.Join(StateDir, "rels_done.txt"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%d\n", idx)
}

func appendRelFailed(items []CreateGraphRelationshipRequest) {
	f, err := os.OpenFile(filepath.Join(StateDir, "rels_failed.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	data, _ := json.Marshal(items)
	f.Write(data)
	f.Write([]byte("\n"))
}

func loadRelsFailed() [][]CreateGraphRelationshipRequest {
	var batches [][]CreateGraphRelationshipRequest
	f, err := os.Open(filepath.Join(StateDir, "rels_failed.jsonl"))
	if err != nil {
		return batches
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 10*1024*1024), 10*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var batch []CreateGraphRelationshipRequest
		if err := json.Unmarshal(line, &batch); err == nil {
			batches = append(batches, batch)
		}
	}
	return batches
}
