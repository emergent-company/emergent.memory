package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
)

func main() {
	serverURL := os.Getenv("SERVER_URL")
	if serverURL == "" {
		serverURL = "http://localhost:3002"
	}
	apiKey := os.Getenv("API_KEY")
	if apiKey == "" {
		log.Fatal("API_KEY environment variable is required")
	}
	projectID := os.Getenv("PROJECT_ID")
	if projectID == "" {
		log.Fatal("PROJECT_ID environment variable is required")
	}

	dumpDir := os.Getenv("DUMP_DIR")
	if dumpDir == "" {
		dumpDir = "/root/data/company-catalog"
	}

	limit := 0
	dryRun := os.Getenv("DRY_RUN") == "true" || os.Getenv("DRY_RUN") == "1"
	if dryRun {
		limit = 100
		log.Println("DRY_RUN enabled: Limiting to 100 rows.")
	} else if l := os.Getenv("SEED_LIMIT"); l != "" {
		limit, _ = strconv.Atoi(l)
		log.Printf("SEED_LIMIT set: Limiting to %d rows.\n", limit)
	}

	retryFailed := os.Getenv("RETRY_FAILED") == "true" || os.Getenv("RETRY_FAILED") == "1"

	api := NewApiClient(serverURL, apiKey, projectID, dryRun)

	log.Printf("Starting twentyfirst-db seeder to %s (Project: %s)", serverURL, projectID)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		log.Printf("Received signal %s - shutting down gracefully", sig)
		cancel()
	}()

	state := loadState()
	log.Printf("Resuming from phase: %s", state.Phase)

	// RETRY_FAILED mode: just replay failed batches and exit
	if retryFailed {
		log.Println("RETRY_FAILED mode: replaying rels_failed.jsonl only")
		failedBatches := loadRelsFailed()
		if len(failedBatches) == 0 {
			log.Println("No failed batches to retry.")
			return
		}

		// Clear the file before retrying — will re-append any that fail again
		os.Remove(filepath.Join(StateDir, "rels_failed.jsonl"))

		retryRelationshipBatches(ctx, api, failedBatches)
		log.Println("Retry complete.")
		return
	}

	var idMap map[string]string

	if state.Phase == PhaseCompaniesPending {
		idMap = make(map[string]string)

		diffs, err := loadCompanyDiffs(dumpDir, limit)
		if err != nil {
			log.Fatalf("Failed to load diffs: %v", err)
		}

		processCompanies(ctx, api, dumpDir, diffs, limit, idMap)

		if ctx.Err() != nil {
			log.Println("Interrupted during companies phase.")
			return
		}

		saveIDMap(idMap)
		state.Phase = PhaseCompaniesDone
		saveState(state)
	}

	if state.Phase == PhaseCompaniesDone {
		if idMap == nil {
			idMap, _ = loadIDMap()
		}

		processPeople(ctx, api, dumpDir, limit, idMap)

		if ctx.Err() != nil {
			return
		}

		saveIDMap(idMap)
		state.Phase = PhasePeopleDone
		saveState(state)
	}

	if state.Phase == PhasePeopleDone {
		if idMap == nil {
			idMap, _ = loadIDMap()
		}

		processFinancialReports(ctx, api, dumpDir, limit, idMap)

		if ctx.Err() != nil {
			return
		}

		saveIDMap(idMap)
		state.Phase = PhaseFinancialReportsDone
		saveState(state)
		state.Phase = PhaseRelsPending
		saveState(state)
	}

	if state.Phase == PhaseRelsPending {
		if idMap == nil {
			idMap, _ = loadIDMap()
		}

		relsDone := loadRelsDone()

		processSubsidiaries(ctx, api, dumpDir, limit, idMap, relsDone)
		if ctx.Err() != nil {
			return
		}

		processCompanyRoles(ctx, api, dumpDir, limit, idMap, relsDone)
		if ctx.Err() != nil {
			return
		}

		processShareholders(ctx, api, dumpDir, limit, idMap, relsDone)
		if ctx.Err() != nil {
			return
		}

		processFinancialReportsRels(ctx, api, dumpDir, limit, idMap, relsDone)
		if ctx.Err() != nil {
			return
		}

		state.Phase = PhaseDone
		saveState(state)
	}

	if state.Phase == PhaseDone {
		log.Println("Seeding complete! Everything is processed.")
	}
}
