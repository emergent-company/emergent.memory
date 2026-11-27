// pipeline-eval: end-to-end evaluation for the automated knowledge pipeline.
//
// Loads a synthetic corpus (markdown docs) plus a golden type manifest, uploads
// the docs to a project, runs schema discovery under pipeline_mode=auto, and
// scores the discovered object types against the ground truth using precision
// and recall. It is the discovery-stage counterpart of extraction-bench: it
// measures how well the LLM proposes the types the synthetic data actually
// contains, so a regression in discovery prompt or gating shows up as a score
// drop before a real project is touched.
//
// Usage:
//
//	go run ./cmd/pipeline-eval/ \
//	  --host https://api.dev.memory.emergent-company.ai \
//	  --api-key emt_xxx... \
//	  --project-id <uuid> \
//	  --corpus-dir apps/server/testdata/synth-corpus
//
// Optional flags:
//
//	--min-type-precision float  Minimum discovered-type precision (default 0.50)
//	--min-type-recall    float  Minimum discovered-type recall (default 0.60)
//	--poll-timeout       int    Seconds to wait for the discovery job (default 180)
//	--discovery-limit    int    Max docs per discovery run (default 20)
//	--log-file           string JSONL run record (default docs/tests/pipeline_eval_log.jsonl)
//
// Each run appends one JSON line to --log-file for cross-run comparison.
// Exit code 0 = all thresholds met; exit code 1 = threshold failure or error.
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

const evalVersion = "1.0.0"

// ─── Corpus ─────────────────────────────────────────────────────────────────

// manifest is the golden expectation for one synthetic corpus.
type manifest struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	GoldenTypes []string `json:"golden_types"`
	MinTypes    int      `json:"min_types"`
}

type doc struct {
	name    string
	content string
}

// loadCorpus reads manifest.json and every docs/*.md under dir.
func loadCorpus(dir string) (*manifest, []doc, error) {
	raw, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		return nil, nil, fmt.Errorf("read manifest: %w", err)
	}
	var m manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, nil, fmt.Errorf("parse manifest: %w", err)
	}
	if len(m.GoldenTypes) == 0 {
		return nil, nil, fmt.Errorf("manifest has no golden_types")
	}

	entries, err := os.ReadDir(filepath.Join(dir, "docs"))
	if err != nil {
		return nil, nil, fmt.Errorf("read docs dir: %w", err)
	}
	var docs []doc
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, "docs", e.Name()))
		if err != nil {
			return nil, nil, fmt.Errorf("read %s: %w", e.Name(), err)
		}
		docs = append(docs, doc{name: e.Name(), content: string(b)})
	}
	if len(docs) == 0 {
		return nil, nil, fmt.Errorf("corpus has no docs/*.md")
	}
	sort.Slice(docs, func(i, j int) bool { return docs[i].name < docs[j].name })
	return &m, docs, nil
}

// ─── Scoring ────────────────────────────────────────────────────────────────

type typeScore struct {
	Precision float64 `json:"precision"`
	Recall    float64 `json:"recall"`
	TruePos   int     `json:"true_positive"`
	FalsePos  int     `json:"false_positive"`
	FalseNeg  int     `json:"false_negative"`
	Golden    int     `json:"golden_count"`
	Found     int     `json:"discovered_count"`
}

// typeNameAliases maps a normalized discovered type name onto the normalized
// canonical catalog name it should be scored against, so a correct-but-synonymous
// discovery still counts as a hit (e.g. a discovered "Decision" toward the golden
// "DecisionRecord"). Keys and values are already normalized.
var typeNameAliases = map[string]string{
	"decision":                "decisionrecord",
	"energystoragetechnology": "storagesystem",
	"actionitem":              "task",
}

// normalizeTypeName lowercases and strips every non-alphanumeric character so
// "Decision Record", "decision_record" and "DecisionRecord" compare equal.
func normalizeTypeName(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// scoreTypes computes precision/recall of discovered type names against golden,
// normalizing both sides and applying typeNameAliases so a synonymous discovery
// counts toward its canonical golden type.
func scoreTypes(golden, discovered []string) typeScore {
	gold := map[string]bool{}
	for _, t := range golden {
		if n := normalizeTypeName(t); n != "" {
			gold[n] = true
		}
	}
	seen := map[string]bool{}    // normalized discovered names already considered
	matched := map[string]bool{} // canonical golden names already hit
	var tp, fp int
	for _, t := range discovered {
		n := normalizeTypeName(t)
		if n == "" || seen[n] {
			continue
		}
		seen[n] = true
		if c, ok := typeNameAliases[n]; ok {
			n = c
		}
		switch {
		case gold[n] && !matched[n]:
			matched[n] = true
			tp++
		case gold[n]:
			// A second discovered name resolving to an already-matched golden
			// type is a duplicate, not a new hit.
			fp++
		default:
			fp++
		}
	}
	fn := 0
	for t := range gold {
		if !matched[t] {
			fn++
		}
	}
	total := tp + fp
	var precision, recall float64
	if total > 0 {
		precision = float64(tp) / float64(total)
	}
	if len(gold) > 0 {
		recall = float64(tp) / float64(len(gold))
	}
	return typeScore{
		Precision: precision,
		Recall:    recall,
		TruePos:   tp,
		FalsePos:  fp,
		FalseNeg:  fn,
		Golden:    len(gold),
		Found:     total,
	}
}

// ─── HTTP helpers ───────────────────────────────────────────────────────────

func decodeData(resp *http.Response, into any) error {
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(b))
	}
	var envelope struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(b, &envelope); err == nil && len(envelope.Data) > 0 {
		if err := json.Unmarshal(envelope.Data, into); err != nil {
			return fmt.Errorf("decode data: %w", err)
		}
		return nil
	}
	if err := json.Unmarshal(b, into); err != nil {
		return fmt.Errorf("decode body: %w", err)
	}
	return nil
}

// ─── Pipeline steps ─────────────────────────────────────────────────────────

func setModeAuto(ctx context.Context, host, apiKey, projectID string) error {
	body, _ := json.Marshal(map[string]string{"mode": "auto"})
	req, err := http.NewRequestWithContext(ctx, "PUT", host+"/api/projects/"+projectID+"/pipeline", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("set mode=auto failed (HTTP %d): %s", resp.StatusCode, string(b))
	}
	return nil
}

func createDocument(ctx context.Context, host, apiKey, projectID, filename, content string) error {
	body, _ := json.Marshal(map[string]string{"filename": filename, "content": content})
	req, err := http.NewRequestWithContext(ctx, "POST", host+"/api/documents", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("X-Project-ID", projectID)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("create document %s failed (HTTP %d): %s", filename, resp.StatusCode, string(b))
	}
	return nil
}

func startDiscovery(ctx context.Context, host, apiKey, projectID string, limit int) (string, error) {
	body, _ := json.Marshal(map[string]any{"scope": "project", "limit": limit})
	req, err := http.NewRequestWithContext(ctx, "POST", host+"/api/discovery-jobs/projects/"+projectID+"/start", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("X-Project-ID", projectID)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("start discovery failed (HTTP %d): %s", resp.StatusCode, string(b))
	}
	var out struct {
		JobID string `json:"job_id"`
	}
	if err := decodeData(resp, &out); err != nil {
		return "", err
	}
	if out.JobID == "" {
		return "", fmt.Errorf("discovery start returned no job_id")
	}
	return out.JobID, nil
}

// waitDiscovery polls the job until it is terminal, returning the discovered
// type names. It returns an error on failure or timeout.
func waitDiscovery(ctx context.Context, host, apiKey, projectID, jobID string, timeout time.Duration) ([]string, error) {
	deadline := time.Now().Add(timeout)
	for {
		req, err := http.NewRequestWithContext(ctx, "GET", host+"/api/discovery-jobs/"+jobID, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+apiKey)
		req.Header.Set("X-Project-ID", projectID)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, err
		}
		var job struct {
			Status          string           `json:"status"`
			DiscoveredTypes []discoveredType `json:"discovered_types"`
			ErrorMessage    *string          `json:"error_message"`
		}
		err = decodeData(resp, &job)
		if err != nil {
			return nil, err
		}
		switch job.Status {
		case "completed":
			types := make([]string, 0, len(job.DiscoveredTypes))
			for _, t := range job.DiscoveredTypes {
				if t.TypeName != "" {
					types = append(types, t.TypeName)
				}
			}
			return types, nil
		case "failed":
			msg := "unknown"
			if job.ErrorMessage != nil {
				msg = *job.ErrorMessage
			}
			return nil, fmt.Errorf("discovery job failed: %s", msg)
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("timed out waiting for discovery job %s (status %s)", jobID, job.Status)
		}
		time.Sleep(3 * time.Second)
	}
}

type discoveredType struct {
	TypeName string `json:"type_name"`
}

// ─── Main ───────────────────────────────────────────────────────────────────

type runRecord struct {
	Version      string    `json:"version"`
	Timestamp    time.Time `json:"timestamp"`
	Host         string    `json:"host"`
	ProjectID    string    `json:"project_id"`
	Corpus       string    `json:"corpus"`
	GoldenTypes  int       `json:"golden_types"`
	DocsUploaded int       `json:"docs_uploaded"`
	Score        typeScore `json:"score"`
	Passed       bool      `json:"passed"`
}

func appendLog(path string, rec runRecord) error {
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.Marshal(rec)
	if err != nil {
		return err
	}
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
	corpusDir := flag.String("corpus-dir", "apps/server/testdata/synth-corpus", "Corpus directory with manifest.json + docs/*.md")
	minPrecision := flag.Float64("min-type-precision", 0.50, "Minimum type precision threshold")
	minRecall := flag.Float64("min-type-recall", 0.60, "Minimum type recall threshold")
	pollTimeout := flag.Int("poll-timeout", 180, "Seconds to wait for the discovery job")
	discoveryLimit := flag.Int("discovery-limit", 20, "Max docs per discovery run")
	logFile := flag.String("log-file", "docs/tests/pipeline_eval_log.jsonl", "JSONL run record path")
	flag.Parse()

	if *host == "" || *apiKey == "" || *projectID == "" {
		log.Fatal("--host, --api-key and --project-id are required")
	}

	m, docs, err := loadCorpus(*corpusDir)
	if err != nil {
		log.Fatalf("load corpus: %v", err)
	}
	log.Printf("corpus %q: %d golden types, %d docs", m.Name, len(m.GoldenTypes), len(docs))

	ctx := context.Background()

	if err := setModeAuto(ctx, *host, *apiKey, *projectID); err != nil {
		log.Fatalf("set mode=auto: %v", err)
	}

	for _, d := range docs {
		if err := createDocument(ctx, *host, *apiKey, *projectID, d.name, d.content); err != nil {
			log.Fatalf("upload %s: %v", d.name, err)
		}
	}
	log.Printf("uploaded %d docs", len(docs))

	jobID, err := startDiscovery(ctx, *host, *apiKey, *projectID, *discoveryLimit)
	if err != nil {
		log.Fatalf("start discovery: %v", err)
	}
	log.Printf("discovery job %s started", jobID)

	discovered, err := waitDiscovery(ctx, *host, *apiKey, *projectID, jobID, time.Duration(*pollTimeout)*time.Second)
	if err != nil {
		log.Fatalf("wait discovery: %v", err)
	}
	log.Printf("discovered types: %s", strings.Join(discovered, ", "))

	score := scoreTypes(m.GoldenTypes, discovered)
	log.Printf("score: precision=%.2f recall=%.2f (tp=%d fp=%d fn=%d)",
		score.Precision, score.Recall, score.TruePos, score.FalsePos, score.FalseNeg)

	passed := score.Precision >= *minPrecision && score.Recall >= *minRecall
	rec := runRecord{
		Version:      evalVersion,
		Timestamp:    time.Now().UTC(),
		Host:         *host,
		ProjectID:    *projectID,
		Corpus:       m.Name,
		GoldenTypes:  len(m.GoldenTypes),
		DocsUploaded: len(docs),
		Score:        score,
		Passed:       passed,
	}
	if err := appendLog(*logFile, rec); err != nil {
		log.Printf("warning: append log: %v", err)
	}

	if !passed {
		fmt.Printf("FAIL: precision %.2f < %.2f or recall %.2f < %.2f\n",
			score.Precision, *minPrecision, score.Recall, *minRecall)
		os.Exit(1)
	}
	fmt.Printf("PASS: precision %.2f recall %.2f\n", score.Precision, score.Recall)
}
