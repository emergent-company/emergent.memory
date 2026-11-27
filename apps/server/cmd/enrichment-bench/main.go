// enrichment-bench: progressive enrichment eval over the Friends transcripts.
//
// Uploads the five Friends episodes one at a time, runs the auto knowledge
// pipeline (discovery -> auto-apply -> backfill -> extract -> auto-merge) after
// each upload, and scores the graph against the cumulative ground truth: entity
// recall, precision (hallucinated entities), and identity-resolution duplicates
// (one ground-truth entity represented by more than one graph object).
//
// The point of the one-at-a-time run is to expose how discovery and extraction
// behave when the graph already holds the previous episodes' entities — i.e.
// whether later documents reuse existing entities or mint duplicates.
//
// Usage:
//
//	go run ./cmd/enrichment-bench/ \
//	  --host http://localhost:3002 --api-key dev-local-key \
//	  --project-id <uuid> --corpus-dir apps/server/testdata/friends
//
// Each episode appends one JSON line to --log-file for cross-run comparison.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const benchVersion = "1.0.0"

// ─── Corpus ─────────────────────────────────────────────────────────────────

type entity struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

type episode struct {
	ID       string   `json:"id"`
	File     string   `json:"file"`
	Entities []entity `json:"entities"`
}

type manifest struct {
	Name     string    `json:"name"`
	Episodes []episode `json:"episodes"`
}

// loadCorpus reads manifest.json and each episode file under dir.
func loadCorpus(dir string) (*manifest, map[string]string, error) {
	raw, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		return nil, nil, fmt.Errorf("read manifest: %w", err)
	}
	var m manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, nil, fmt.Errorf("parse manifest: %w", err)
	}
	docs := map[string]string{}
	for _, ep := range m.Episodes {
		b, err := os.ReadFile(filepath.Join(dir, ep.File))
		if err != nil {
			return nil, nil, fmt.Errorf("read %s: %w", ep.File, err)
		}
		docs[ep.ID] = string(b)
	}
	return &m, docs, nil
}

// ─── Scoring ────────────────────────────────────────────────────────────────

type graphObject struct {
	EntityID string `json:"entity_id"`
	Name     string `json:"name"`
	Type     string `json:"type"`
	Key      string `json:"key"`
}

type episodeScore struct {
	Episode        string   `json:"episode"`
	Expected       int      `json:"expected"`        // cumulative ground-truth entities
	Found          int      `json:"found"`           // cumulative entities matched (lenient)
	FoundExact     int      `json:"found_exact"`     // cumulative entities matched by exact normalized name
	Recall         float64  `json:"recall"`          // found / expected (lenient)
	RecallExact    float64  `json:"recall_exact"`    // found_exact / expected
	Objects        int      `json:"objects"`         // non-document graph objects
	Unmatched      int      `json:"unmatched"`       // objects matching no ground-truth entity (may include real-but-unlisted entities)
	Precision      float64  `json:"precision"`       // (objects - unmatched) / objects
	DuplicateNames int      `json:"duplicate_names"` // ground-truth names mapped to >1 object
	Edges          int      `json:"edges"`           // cumulative graph relationships
	Types          []string `json:"types"`           // distinct object types observed
}

// normalize folds a name to a comparable form: lowercase, alphanumerics only.
func normalize(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	for _, r := range s {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
		} else {
			b.WriteRune(' ')
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

// matchEntity reports whether an object name and a ground-truth name refer to
// the same entity. Matching is exact after normalization, or lenient: the
// shorter name's tokens must form an ordered subset of the longer name's tokens
// and the longer name may carry at most 2 extra tokens. That keeps "Central
// Perk" matching "Central Perk coffeehouse" while rejecting "Carol" matching
// "Carol and Ross's baby".
func matchEntity(objName, gtName string) bool {
	a, b := normalize(objName), normalize(gtName)
	if a == "" || b == "" {
		return false
	}
	if a == b {
		return true
	}
	at := strings.Fields(a)
	bt := strings.Fields(b)
	short, long := at, bt
	if len(bt) < len(at) {
		short, long = bt, at
	}
	if len(short) == 0 || len(long) == 0 {
		return false
	}
	if len(long)-len(short) > 2 {
		return false
	}
	return orderedSubset(short, long)
}

// exactMatch reports whether two names are identical after normalization.
func exactMatch(objName, gtName string) bool {
	a, b := normalize(objName), normalize(gtName)
	return a != "" && a == b
}

// orderedSubset reports whether short appears as an in-order subsequence of long.
func orderedSubset(short, long []string) bool {
	i := 0
	for _, t := range long {
		if i < len(short) && short[i] == t {
			i++
		}
	}
	return i == len(short)
}

// isDocumentType reports whether an object type is a source-document type, which
// is expected (one object per uploaded transcript) and excluded from the
// entity precision count.
func isDocumentType(t string) bool {
	n := normalize(t)
	return strings.Contains(n, "document") || strings.Contains(n, "episode") || strings.Contains(n, "transcript")
}

// scoreEpisode scores the graph objects against the cumulative ground truth.
func scoreEpisode(epID string, cumulative []entity, objects []graphObject, edges int) episodeScore {
	s := episodeScore{Episode: epID, Expected: len(cumulative), Edges: edges}

	// Map each ground-truth entity to the distinct object EntityIDs matching it,
	// separately for exact and lenient name matches.
	byEntity := map[string]map[string]bool{}      // gt name -> set of entity IDs (lenient)
	byEntityExact := map[string]map[string]bool{} // gt name -> set of entity IDs (exact)
	for i := range cumulative {
		byEntity[cumulative[i].Name] = map[string]bool{}
		byEntityExact[cumulative[i].Name] = map[string]bool{}
	}

	entityCounted := map[string]bool{} // object entity IDs that matched a ground-truth name
	types := map[string]bool{}
	nonDoc := 0
	for _, o := range objects {
		types[o.Type] = true
		if isDocumentType(o.Type) {
			continue
		}
		nonDoc++
		name := o.Name
		if name == "" {
			name = o.Key
		}
		for _, e := range cumulative {
			if exactMatch(name, e.Name) {
				byEntityExact[e.Name][o.EntityID] = true
			}
			if matchEntity(name, e.Name) {
				byEntity[e.Name][o.EntityID] = true
				entityCounted[o.EntityID] = true
				break
			}
		}
	}
	s.Objects = nonDoc
	s.Unmatched = nonDoc - len(entityCounted)
	if nonDoc > 0 {
		s.Precision = float64(nonDoc-s.Unmatched) / float64(nonDoc)
	} else {
		s.Precision = 1
	}
	for _, e := range cumulative {
		if len(byEntity[e.Name]) > 0 {
			s.Found++
		}
		if len(byEntityExact[e.Name]) > 0 {
			s.FoundExact++
		}
		if len(byEntity[e.Name]) > 1 {
			s.DuplicateNames++
		}
	}
	if s.Expected > 0 {
		s.Recall = float64(s.Found) / float64(s.Expected)
		s.RecallExact = float64(s.FoundExact) / float64(s.Expected)
	}
	for t := range types {
		s.Types = append(s.Types, t)
	}
	sort.Strings(s.Types)
	return s
}

// ─── HTTP helpers ───────────────────────────────────────────────────────────

func apiRequest(method, url, apiKey, projectID string, body io.Reader, contentType string) (*http.Response, error) {
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-API-Key", apiKey)
	req.Header.Set("Authorization", "Bearer "+apiKey)
	if projectID != "" {
		req.Header.Set("X-Project-ID", projectID)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	return http.DefaultClient.Do(req)
}

func doJSON(method, url, apiKey, projectID string, body any, into any) error {
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		r = bytes.NewReader(b)
	}
	resp, err := apiRequest(method, url, apiKey, projectID, r, "application/json")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(b))
	}
	if into == nil {
		return nil
	}
	if err := json.Unmarshal(b, into); err != nil {
		return fmt.Errorf("decode: %w (%s)", err, string(b)[:min(len(b), 200)])
	}
	return nil
}

// ─── Pipeline steps ─────────────────────────────────────────────────────────

func uploadDoc(ctx context.Context, host, key, projectID, filename, content string) (string, error) {
	var out struct {
		ID string `json:"id"`
	}
	if err := doJSON("POST", host+"/api/documents", key, projectID, map[string]string{"filename": filename, "content": content}, &out); err != nil {
		return "", err
	}
	return out.ID, nil
}

func startDiscovery(ctx context.Context, host, key, projectID, docID string) (string, error) {
	var out struct {
		JobID string `json:"job_id"`
	}
	if err := doJSON("POST", host+"/api/discovery-jobs/projects/"+projectID+"/start", key, projectID,
		map[string]any{"scope": "documents", "document_ids": []string{docID}}, &out); err != nil {
		return "", err
	}
	return out.JobID, nil
}

func waitDiscovery(ctx context.Context, host, key, projectID, jobID string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		var job struct {
			Status       string  `json:"status"`
			ErrorMessage *string `json:"error_message"`
		}
		if err := doJSON("GET", host+"/api/discovery-jobs/"+jobID, key, projectID, nil, &job); err != nil {
			return err
		}
		switch job.Status {
		case "completed":
			return nil
		case "failed":
			msg := "unknown"
			if job.ErrorMessage != nil {
				msg = *job.ErrorMessage
			}
			return fmt.Errorf("discovery failed: %s", msg)
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for discovery job %s", jobID)
		}
		time.Sleep(2 * time.Second)
	}
}

// waitExtractionDone polls until the project has no pending/processing
// extraction jobs, so the auto-backfill enqueued by the apply has finished.
func waitExtractionDone(ctx context.Context, host, key, projectID string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		var list struct {
			Data struct {
				Jobs []struct {
					Status string `json:"status"`
				} `json:"jobs"`
				Total int `json:"total"`
			} `json:"data"`
		}
		if err := doJSON("GET", host+"/api/admin/extraction-jobs/projects/"+projectID+"?limit=100", key, projectID, nil, &list); err != nil {
			return err
		}
		active := 0
		for _, j := range list.Data.Jobs {
			switch j.Status {
			case "pending", "processing", "queued", "running":
				active++
			}
		}
		if list.Data.Total > 0 && active == 0 {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for extraction jobs (%d active, %d total)", active, list.Data.Total)
		}
		time.Sleep(3 * time.Second)
	}
}

func fetchObjects(ctx context.Context, host, key, projectID string) ([]graphObject, error) {
	var out struct {
		Items []graphObject `json:"items"`
	}
	if err := doJSON("GET", host+"/api/graph/objects/search?limit=500", key, projectID, nil, &out); err != nil {
		return nil, err
	}
	return out.Items, nil
}

// fetchRelationshipCount returns the number of graph relationships (edges) in
// the project, using the relationships/search endpoint's exact total. It falls
// back to counting returned items if the total is omitted.
func fetchRelationshipCount(ctx context.Context, host, key, projectID string) (int, error) {
	var out struct {
		Total int               `json:"total"`
		Items []json.RawMessage `json:"items"`
	}
	if err := doJSON("GET", host+"/api/graph/relationships/search?limit=500", key, projectID, nil, &out); err != nil {
		return 0, err
	}
	if out.Total >= len(out.Items) {
		return out.Total, nil
	}
	return len(out.Items), nil
}

// ─── Main ───────────────────────────────────────────────────────────────────

type runRecord struct {
	Version   string         `json:"version"`
	Timestamp time.Time      `json:"timestamp"`
	Host      string         `json:"host"`
	ProjectID string         `json:"project_id"`
	Corpus    string         `json:"corpus"`
	Episodes  []episodeScore `json:"episodes"`
}

func appendLog(path string, rec runRecord) error {
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, _ := json.Marshal(rec)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(append(b, '\n'))
	return err
}

func main() {
	host := flag.String("host", "", "API base URL (required)")
	apiKey := flag.String("api-key", "", "API key (required)")
	projectID := flag.String("project-id", "", "Target project id (required)")
	corpusDir := flag.String("corpus-dir", "apps/server/testdata/friends", "Corpus directory with manifest.json + episodes/*.txt")
	logFile := flag.String("log-file", "docs/tests/enrichment_bench_log.jsonl", "JSONL run record path")
	flag.Parse()

	if *host == "" || *apiKey == "" || *projectID == "" {
		log.Fatal("--host, --api-key and --project-id are required")
	}

	m, docs, err := loadCorpus(*corpusDir)
	if err != nil {
		log.Fatalf("load corpus: %v", err)
	}

	ctx := context.Background()
	rec := runRecord{Version: benchVersion, Timestamp: time.Now().UTC(), Host: *host, ProjectID: *projectID, Corpus: m.Name}

	var cumulative []entity
	for _, ep := range m.Episodes {
		content, ok := docs[ep.ID]
		if !ok {
			log.Fatalf("episode %s not found in corpus", ep.ID)
		}
		log.Printf("=== %s (%d new entities) ===", ep.ID, len(ep.Entities))

		docID, err := uploadDoc(ctx, *host, *apiKey, *projectID, ep.ID+".txt", content)
		if err != nil {
			log.Fatalf("upload %s: %v", ep.ID, err)
		}

		jobID, err := startDiscovery(ctx, *host, *apiKey, *projectID, docID)
		if err != nil {
			log.Fatalf("discover %s: %v", ep.ID, err)
		}
		if err := waitDiscovery(ctx, *host, *apiKey, *projectID, jobID, 180*time.Second); err != nil {
			log.Fatalf("wait discovery %s: %v", ep.ID, err)
		}
		if err := waitExtractionDone(ctx, *host, *apiKey, *projectID, 180*time.Second); err != nil {
			log.Printf("  warning: %v", err)
		}

		cumulative = append(cumulative, ep.Entities...)
		objects, err := fetchObjects(ctx, *host, *apiKey, *projectID)
		if err != nil {
			log.Fatalf("fetch objects %s: %v", ep.ID, err)
		}
		edges, err := fetchRelationshipCount(ctx, *host, *apiKey, *projectID)
		if err != nil {
			log.Printf("  warning: fetch relationships %s: %v", ep.ID, err)
			edges = 0
		}
		s := scoreEpisode(ep.ID, cumulative, objects, edges)
		rec.Episodes = append(rec.Episodes, s)
		log.Printf("  expected=%d found=%d (exact=%d) recall=%.2f (exact=%.2f) objects=%d unmatched=%d precision=%.2f duplicates=%d edges=%d types=%v",
			s.Expected, s.Found, s.FoundExact, s.Recall, s.RecallExact, s.Objects, s.Unmatched, s.Precision, s.DuplicateNames, s.Edges, s.Types)
	}

	if err := appendLog(*logFile, rec); err != nil {
		log.Printf("warning: append log: %v", err)
	}
	fmt.Printf("enrichment-bench complete: %d episodes\n", len(rec.Episodes))
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
