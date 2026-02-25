package main

import (
	"context"
	"log"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
)

func processCompanies(ctx context.Context, api *ApiClient, dumpDir string, diffs map[string][]Diff, limit int, idMap map[string]string) {
	log.Println("Streaming companies.csv.gz...")
	ch, err := streamCSV(filepath.Join(dumpDir, "companies.csv.gz"))
	if err != nil {
		log.Fatalf("Error streaming companies: %v", err)
	}

	var batch []CreateGraphObjectRequest
	var batchInternalIDs []string
	var mu sync.Mutex

	dispatchBatch := func(b []CreateGraphObjectRequest, internalIDs []string) {
		if len(b) == 0 {
			return
		}

		res, err := api.BulkCreateObjects(ctx, b)
		if err != nil {
			log.Printf("Failed bulk insert for companies: %v", err)
			return
		}

		mu.Lock()
		for _, r := range res.Results {
			if r.Success && r.Object != nil && b[r.Index].Key != nil {
				orgNo := *b[r.Index].Key
				internalID := internalIDs[r.Index]

				idMap["Company:"+orgNo] = r.Object.CanonicalID
				appendToIdMap("Company:"+orgNo, r.Object.CanonicalID)

				idMap["CompanyID:"+internalID] = r.Object.CanonicalID
				appendToIdMap("CompanyID:"+internalID, r.Object.CanonicalID)
			}
		}
		mu.Unlock()
	}

	// Concurrent workers for versioned companies
	type VersionJob struct {
		companyID    string
		orgNo        string
		props        map[string]any
		companyDiffs []Diff
	}

	versionJobs := make(chan VersionJob, 1000)
	var wg sync.WaitGroup
	numWorkers := 30 // moderate concurrency to protect the database!

	var count int32
	var versionedCount int32

	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range versionJobs {
				if ctx.Err() != nil {
					return
				}

				// Oldest state
				oldestProps := deepCopyMap(job.props)
				for j := len(job.companyDiffs) - 1; j >= 0; j-- {
					applyPatchInverse(oldestProps, job.companyDiffs[j].Backward)
				}

				// 1. Upsert oldest state
				oldestObj := CreateGraphObjectRequest{
					Type:       "Company",
					Key:        &job.orgNo,
					Properties: oldestProps,
				}
				res, err := api.UpsertObject(ctx, oldestObj)
				if err != nil {
					log.Printf("Failed to upsert oldest state for company %s: %v", job.orgNo, err)
					continue // move to next job, do not kill the worker!
				}

				mu.Lock()
				idMap["Company:"+job.orgNo] = res.CanonicalID
				appendToIdMap("Company:"+job.orgNo, res.CanonicalID)

				idMap["CompanyID:"+job.companyID] = res.CanonicalID
				appendToIdMap("CompanyID:"+job.companyID, res.CanonicalID)
				mu.Unlock()

				// 2. Play forward patches
				for _, diff := range job.companyDiffs {
					patchObj := PatchGraphObjectRequest{
						Properties: make(map[string]any),
					}
					applyPatch(patchObj.Properties, diff.Forward)
					_, err := api.PatchObject(ctx, res.CanonicalID, patchObj)
					if err != nil {
						log.Printf("Failed to patch version for company %s: %v", job.orgNo, err)
					}
				}

				atomic.AddInt32(&versionedCount, 1)
				atomic.AddInt32(&count, 1)
				currCount := atomic.LoadInt32(&count)
				if currCount%1000 == 0 {
					log.Printf("Processed %d companies (versioned: %d)", currCount, atomic.LoadInt32(&versionedCount))
				}
			}
		}()
	}

	for row := range ch {
		if limit > 0 && atomic.LoadInt32(&count) >= int32(limit) {
			break
		}
		if row["is_test"] == "t" {
			continue
		}

		orgNo := row["org_no"]
		if orgNo == "" {
			continue
		}

		// Resume/Checkpoint skip
		mu.Lock()
		_, alreadyDone := idMap["Company:"+orgNo]
		mu.Unlock()
		if alreadyDone {
			continue // skip this, we already ingested it!
		}

		companyID := row["id"]
		props := make(map[string]any)
		for k, v := range row {
			if v != "" {
				props[k] = v
			}
		}

		companyDiffs := diffs[companyID]
		if len(companyDiffs) == 0 {
			// No history, standard bulk insert
			obj := CreateGraphObjectRequest{
				Type:       "Company",
				Key:        &orgNo,
				Properties: props,
			}
			batch = append(batch, obj)
			batchInternalIDs = append(batchInternalIDs, companyID)

			if len(batch) >= 100 {
				dispatchBatch(batch, batchInternalIDs)
				batch = nil
				batchInternalIDs = nil
			}
			atomic.AddInt32(&count, 1)
			currCount := atomic.LoadInt32(&count)
			if currCount%1000 == 0 {
				log.Printf("Processed %d companies (versioned: %d)", currCount, atomic.LoadInt32(&versionedCount))
			}
		} else {
			// Queue job for concurrent processing
			versionJobs <- VersionJob{
				companyID:    companyID,
				orgNo:        orgNo,
				props:        props,
				companyDiffs: companyDiffs,
			}
		}
	}

	close(versionJobs)
	wg.Wait() // wait for all versioned companies to finish patching

	if len(batch) > 0 {
		dispatchBatch(batch, batchInternalIDs)
	}

	log.Printf("Finished processing %d companies (versioned: %d)", atomic.LoadInt32(&count), atomic.LoadInt32(&versionedCount))
}

func processPeople(ctx context.Context, api *ApiClient, dumpDir string, limit int, idMap map[string]string) {
	log.Println("Streaming people.csv.gz...")
	ch, err := streamCSV(filepath.Join(dumpDir, "people.csv.gz"))
	if err != nil {
		log.Fatalf("Error streaming people: %v", err)
	}

	var batch []CreateGraphObjectRequest
	var mu sync.Mutex
	var processed int32

	dispatch := func(b []CreateGraphObjectRequest) {
		res, err := api.BulkCreateObjects(ctx, b)
		if err != nil {
			log.Printf("Bulk API error people: %v", err)
			return
		}
		mu.Lock()
		for _, r := range res.Results {
			if r.Success && r.Object != nil && b[r.Index].Key != nil {
				idMap["Person:"+*b[r.Index].Key] = r.Object.CanonicalID
				appendToIdMap("Person:"+*b[r.Index].Key, r.Object.CanonicalID)
			}
		}
		mu.Unlock()
	}

	for row := range ch {
		if limit > 0 && int(processed) >= limit {
			break
		}
		if row["is_test"] == "t" {
			continue
		}

		personID := row["id"]
		if personID == "" {
			continue
		}

		// Resume skip
		mu.Lock()
		_, alreadyDone := idMap["Person:"+personID]
		mu.Unlock()
		if alreadyDone {
			continue
		}

		props := make(map[string]any)
		for k, v := range row {
			if v != "" {
				props[k] = v
			}
		}

		batch = append(batch, CreateGraphObjectRequest{
			Type:       "Person",
			Key:        &personID,
			Properties: props,
		})

		if len(batch) >= 100 {
			dispatch(batch)
			batch = nil
		}

		atomic.AddInt32(&processed, 1)
		if processed%1000 == 0 {
			log.Printf("Processed %d people", processed)
		}
	}
	if len(batch) > 0 {
		dispatch(batch)
	}
	log.Printf("Finished processing %d people", processed)
}

func processFinancialReports(ctx context.Context, api *ApiClient, dumpDir string, limit int, idMap map[string]string) {
	log.Println("Streaming accounts_reports.csv.gz...")
	ch, err := streamCSV(filepath.Join(dumpDir, "accounts_reports.csv.gz"))
	if err != nil {
		log.Fatalf("Error streaming accounts_reports: %v", err)
	}

	var batch []CreateGraphObjectRequest
	var mu sync.Mutex
	var processed int32

	dispatch := func(b []CreateGraphObjectRequest) {
		res, err := api.BulkCreateObjects(ctx, b)
		if err != nil {
			log.Printf("Bulk API error accounts_reports: %v", err)
			return
		}
		mu.Lock()
		for _, r := range res.Results {
			if r.Success && r.Object != nil && b[r.Index].Key != nil {
				idMap["FinancialReport:"+*b[r.Index].Key] = r.Object.CanonicalID
				appendToIdMap("FinancialReport:"+*b[r.Index].Key, r.Object.CanonicalID)
			}
		}
		mu.Unlock()
	}

	for row := range ch {
		if limit > 0 && int(processed) >= limit {
			break
		}

		companyID := row["company_id"]
		fromDate := row["from_date"]
		toDate := row["to_date"]

		if companyID == "" || fromDate == "" || toDate == "" {
			continue
		}

		key := strings.Join([]string{companyID, fromDate, toDate}, ":")

		mu.Lock()
		_, alreadyDone := idMap["FinancialReport:"+key]
		mu.Unlock()
		if alreadyDone {
			continue
		}

		props := make(map[string]any)
		for k, v := range row {
			if v != "" {
				props[k] = v
			}
		}

		batch = append(batch, CreateGraphObjectRequest{
			Type:       "FinancialReport",
			Key:        &key,
			Properties: props,
		})

		if len(batch) >= 100 {
			dispatch(batch)
			batch = nil
		}

		atomic.AddInt32(&processed, 1)
		if processed%1000 == 0 {
			log.Printf("Processed %d financial reports", processed)
		}
	}
	if len(batch) > 0 {
		dispatch(batch)
	}
	log.Printf("Finished processing %d financial reports", processed)
}
