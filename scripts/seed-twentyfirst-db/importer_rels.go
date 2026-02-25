package main

import (
	"context"
	"log"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
)

func processSubsidiaries(ctx context.Context, api *ApiClient, dumpDir string, limit int, idMap map[string]string, relsDone map[int]bool) {
	log.Println("Streaming companies.csv.gz for SUBSIDIARY_OF relationships...")
	ch, err := streamCSV(filepath.Join(dumpDir, "companies.csv.gz"))
	if err != nil {
		log.Fatalf("Error streaming companies: %v", err)
	}

	var batch []CreateGraphRelationshipRequest
	batchIdx := 0
	var mu sync.Mutex

	dispatch := func(b []CreateGraphRelationshipRequest, idx int) {
		if relsDone[idx] {
			return
		}
		res, err := api.BulkCreateRelationships(ctx, b)
		if err != nil {
			log.Printf("Failed rel batch %d: %v", idx, err)
			mu.Lock()
			appendRelFailed(b)
			mu.Unlock()
			return
		}
		if res.Failed > 0 {
			log.Printf("Batch %d had %d failed relationships", idx, res.Failed)
		}
		mu.Lock()
		appendRelDone(idx)
		relsDone[idx] = true
		mu.Unlock()
	}

	var count int32

	for row := range ch {
		if limit > 0 && int(count) >= limit {
			break
		}
		if row["is_test"] == "t" {
			continue
		}

		parentID := row["parent_id"]
		if parentID == "" {
			continue
		}

		childOrgNo := row["org_no"]

		dstID, ok1 := idMap["CompanyID:"+parentID] // parent
		srcID, ok2 := idMap["Company:"+childOrgNo] // child

		if ok1 && ok2 {
			batch = append(batch, CreateGraphRelationshipRequest{
				Type:  "SUBSIDIARY_OF",
				SrcID: srcID,
				DstID: dstID,
			})

			if len(batch) >= 100 {
				dispatch(batch, batchIdx)
				batch = nil
				batchIdx++
			}
			atomic.AddInt32(&count, 1)
		}
	}
	if len(batch) > 0 {
		dispatch(batch, batchIdx)
	}
	log.Printf("Finished processing %d SUBSIDIARY_OF relationships", count)
}

func processCompanyRoles(ctx context.Context, api *ApiClient, dumpDir string, limit int, idMap map[string]string, relsDone map[int]bool) {
	log.Println("Streaming company_roles.csv.gz...")

	// Load role groups into memory (approx 2.5M rows -> 50-100MB)
	roleGroups := make(map[string]string)
	rgCh, _ := streamCSV(filepath.Join(dumpDir, "company_role_groups.csv.gz"))
	for row := range rgCh {
		roleGroups[row["id"]] = row["type"]
	}

	ch, err := streamCSV(filepath.Join(dumpDir, "company_roles.csv.gz"))
	if err != nil {
		log.Fatalf("Error streaming roles: %v", err)
	}

	var batch []CreateGraphRelationshipRequest
	batchIdx := 10000 // offset to avoid collision with subsidiaries
	var mu sync.Mutex

	dispatch := func(b []CreateGraphRelationshipRequest, idx int) {
		if relsDone[idx] {
			return
		}
		res, err := api.BulkCreateRelationships(ctx, b)
		if err != nil {
			mu.Lock()
			appendRelFailed(b)
			mu.Unlock()
			return
		}
		if res.Failed > 0 {
			log.Printf("Batch %d had %d failed roles", idx, res.Failed)
		}
		mu.Lock()
		appendRelDone(idx)
		relsDone[idx] = true
		mu.Unlock()
	}

	var count int32

	for row := range ch {
		if limit > 0 && int(count) >= limit {
			break
		}

		companyID := row["company_id"]
		if companyID == "" {
			continue
		}

		dstID, ok1 := idMap["CompanyID:"+companyID]
		if !ok1 {
			continue
		}

		var srcID string
		var ok2 bool

		if row["person_id"] != "" {
			srcID, ok2 = idMap["Person:"+row["person_id"]]
		} else {
			// some roles are held by other companies
		}

		if ok1 && ok2 {
			props := map[string]any{
				"role_type": row["type"],
			}
			if row["resigned"] == "t" {
				props["resigned"] = true
			}
			if groupType, found := roleGroups[row["group_id"]]; found {
				props["group_type"] = groupType
			}

			batch = append(batch, CreateGraphRelationshipRequest{
				Type:       "HAS_ROLE",
				SrcID:      srcID,
				DstID:      dstID,
				Properties: props,
			})

			if len(batch) >= 100 {
				dispatch(batch, batchIdx)
				batch = nil
				batchIdx++
			}
			atomic.AddInt32(&count, 1)
		}
	}
	if len(batch) > 0 {
		dispatch(batch, batchIdx)
	}
	log.Printf("Finished processing %d HAS_ROLE relationships", count)
}

type ReportMeta struct {
	companyID string
	year      string
}

func processShareholders(ctx context.Context, api *ApiClient, dumpDir string, limit int, idMap map[string]string, relsDone map[int]bool) {
	log.Println("Streaming shareholders_report_entries.csv.gz...")

	reports := make(map[string]ReportMeta)
	shCh, _ := streamCSV(filepath.Join(dumpDir, "shareholders_reports.csv.gz"))
	for row := range shCh {
		reports[row["id"]] = ReportMeta{
			companyID: row["company_id"],
			year:      row["year"],
		}
	}

	ch, err := streamCSV(filepath.Join(dumpDir, "shareholders_report_entries.csv.gz"))
	if err != nil {
		log.Fatalf("Error streaming shareholders: %v", err)
	}

	var batch []CreateGraphRelationshipRequest
	batchIdx := 2000000
	var mu sync.Mutex

	dispatch := func(b []CreateGraphRelationshipRequest, idx int) {
		if relsDone[idx] {
			return
		}
		res, err := api.BulkCreateRelationships(ctx, b)
		if err != nil {
			mu.Lock()
			appendRelFailed(b)
			mu.Unlock()
			return
		}
		if res.Failed > 0 {
			log.Printf("Batch %d had %d failed shares", idx, res.Failed)
		}
		mu.Lock()
		appendRelDone(idx)
		relsDone[idx] = true
		mu.Unlock()
	}

	var count int32

	for row := range ch {
		if limit > 0 && int(count) >= limit {
			break
		}

		reportID := row["shareholders_report_id"]
		report, found := reports[reportID]
		if !found {
			continue
		}

		dstID, ok1 := idMap["CompanyID:"+report.companyID]
		if !ok1 {
			continue
		}

		var srcID string
		var ok2 bool

		if row["person_id"] != "" {
			srcID, ok2 = idMap["Person:"+row["person_id"]]
		} else if row["company_id"] != "" {
			srcID, ok2 = idMap["CompanyID:"+row["company_id"]]
		}

		if ok1 && ok2 {
			props := map[string]any{
				"shares":      row["shares"],
				"share_class": row["share_class"],
				"year":        report.year,
			}

			batch = append(batch, CreateGraphRelationshipRequest{
				Type:       "OWNS_SHARES_IN",
				SrcID:      srcID,
				DstID:      dstID,
				Properties: props,
			})

			if len(batch) >= 100 {
				dispatch(batch, batchIdx)
				batch = nil
				batchIdx++
			}
			atomic.AddInt32(&count, 1)
		}
	}
	if len(batch) > 0 {
		dispatch(batch, batchIdx)
	}
	log.Printf("Finished processing %d OWNS_SHARES_IN relationships", count)
}

func processFinancialReportsRels(ctx context.Context, api *ApiClient, dumpDir string, limit int, idMap map[string]string, relsDone map[int]bool) {
	log.Println("Streaming accounts_reports.csv.gz for HAS_FINANCIAL_REPORT...")
	ch, err := streamCSV(filepath.Join(dumpDir, "accounts_reports.csv.gz"))
	if err != nil {
		log.Fatalf("Error streaming accounts_reports: %v", err)
	}

	var batch []CreateGraphRelationshipRequest
	batchIdx := 3000000
	var mu sync.Mutex

	dispatch := func(b []CreateGraphRelationshipRequest, idx int) {
		if relsDone[idx] {
			return
		}
		res, err := api.BulkCreateRelationships(ctx, b)
		if err != nil {
			mu.Lock()
			appendRelFailed(b)
			mu.Unlock()
			return
		}
		if res.Failed > 0 {
			log.Printf("Batch %d had %d failed report rels", idx, res.Failed)
		}
		mu.Lock()
		appendRelDone(idx)
		relsDone[idx] = true
		mu.Unlock()
	}

	var count int32

	for row := range ch {
		if limit > 0 && int(count) >= limit {
			break
		}

		companyID := row["company_id"]
		fromDate := row["from_date"]
		toDate := row["to_date"]

		if companyID == "" || fromDate == "" || toDate == "" {
			continue
		}

		key := strings.Join([]string{companyID, fromDate, toDate}, ":")

		srcID, ok1 := idMap["CompanyID:"+companyID]
		dstID, ok2 := idMap["FinancialReport:"+key]

		if ok1 && ok2 {
			batch = append(batch, CreateGraphRelationshipRequest{
				Type:  "HAS_FINANCIAL_REPORT",
				SrcID: srcID,
				DstID: dstID,
			})

			if len(batch) >= 100 {
				dispatch(batch, batchIdx)
				batch = nil
				batchIdx++
			}
			atomic.AddInt32(&count, 1)
		}
	}
	if len(batch) > 0 {
		dispatch(batch, batchIdx)
	}
	log.Printf("Finished processing %d HAS_FINANCIAL_REPORT relationships", count)
}

func retryRelationshipBatches(ctx context.Context, api *ApiClient, batches [][]CreateGraphRelationshipRequest) {
	log.Printf("Retrying %d failed relationship batches...", len(batches))

	var wg sync.WaitGroup
	var succeeded, failed atomic.Int32

	numWorkers := 10
	work := make(chan []CreateGraphRelationshipRequest, len(batches))

	for _, batch := range batches {
		work <- batch
	}
	close(work)

	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for b := range work {
				if ctx.Err() != nil {
					return
				}

				res, err := api.BulkCreateRelationships(ctx, b)
				if err != nil {
					log.Printf("  [retry] batch failed again: %v — re-saving", err)
					appendRelFailed(b)
					failed.Add(1)
					continue
				}

				if res != nil && res.Failed > 0 {
					log.Printf("  [retry] batch had %d relationship errors", res.Failed)
				}

				succeeded.Add(1)
			}
		}()
	}

	wg.Wait()
	log.Printf("Retry complete: %d batches succeeded, %d batches failed again.", succeeded.Load(), failed.Load())
}
