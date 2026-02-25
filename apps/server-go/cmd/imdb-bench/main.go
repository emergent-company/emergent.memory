// imdb-bench: End-to-end benchmark for IMDb graph seeding.
//
// Phases:
//   1. Delete existing project (if PROJECT_ID set) — async on server, returns immediately
//   2. Create a fresh project
//   3. Seed N titles (SEED_LIMIT, default 100) with precise per-step timing
//   4. Verify final object + relationship counts
//   5. Print full timing report and append to persistent log
//
// Usage:
//   # Fresh benchmark (creates new project, deletes old if PROJECT_ID set):
//   PROJECT_ID=<old-id> go run ./cmd/imdb-bench/
//
//   # Skip delete, just create new project and seed:
//   SKIP_DELETE=true go run ./cmd/imdb-bench/
//
//   # Fire delete only, then exit:
//   PROJECT_ID=<id> DELETE_ONLY=true go run ./cmd/imdb-bench/
//
// Environment:
//   SERVER_URL    - target server  (default http://mcj-emergent:3002)
//   API_KEY       - API key        (default embedded)
//   PROJECT_ID    - project to delete before creating fresh one
//   SEED_LIMIT    - titles to seed (default 100)
//   SKIP_DELETE   - skip delete phase even if PROJECT_ID is set
//   DELETE_ONLY   - fire delete for PROJECT_ID then exit
//   POLL_TIMEOUT  - seconds to poll for project disappearance after delete (default 10)
//   LOG_FILE      - path to append JSONL run log (default docs/tests/imdb_bench_log.jsonl)
//
// Each run appends one JSON line to LOG_FILE containing full env + timing data
// for later comparison across server versions and DB configurations.

package main

import (
	"bufio"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/emergent-company/emergent/apps/server-go/pkg/sdk"
	"github.com/emergent-company/emergent/apps/server-go/pkg/sdk/graph"
	"github.com/emergent-company/emergent/apps/server-go/pkg/sdk/projects"
)

// BenchVersion is bumped manually when the benchmark logic itself changes,
// so results from different script versions can be distinguished in the log.
const BenchVersion = "1.1.0"

// ─── Config ──────────────────────────────────────────────────────────────────

const (
	defaultServerURL = "http://mcj-emergent:3002"
	defaultAPIKey    = "emt_ec70233facfa29385abfef9bff015df72f08f7205be51f3034b42bf1484d0ec1"
	defaultOrgID     = "dcba78f0-fc40-414a-a24d-f9c32b762f15"
	batchSize        = 100
	numWorkers       = 20
)

// ─── Timing + environment capture ─────────────────────────────────────────────

type Step struct {
	Name     string        `json:"name"`
	Duration time.Duration `json:"duration_ms"`
	Detail   string        `json:"detail,omitempty"`
}

// BenchEnv captures everything needed to reproduce and compare a run.
type BenchEnv struct {
	BenchVersion  string `json:"bench_version"`  // version of this script
	ServerVersion string `json:"server_version"` // from /health
	ServerURL     string `json:"server_url"`
	ProjectID     string `json:"project_id"`
	OrgID         string `json:"org_id"`
	SeedLimit     int    `json:"seed_limit"`
	GitCommit     string `json:"git_commit"` // HEAD sha of this repo
	GoVersion     string `json:"go_version"`
	GOOS          string `json:"goos"`
	GOARCH        string `json:"goarch"`
	Hostname      string `json:"hostname"`
}

type BenchReport struct {
	Env       BenchEnv  `json:"env"`
	StartedAt time.Time `json:"started_at"`
	Steps     []Step    `json:"steps"`
	Objects   int       `json:"objects_count"`
	Relations int       `json:"relations_count"`
	RelErrors int64     `json:"rel_errors"`
}

func (r *BenchReport) Begin(name string) func(detail ...string) {
	start := time.Now()
	return func(detail ...string) {
		d := ""
		if len(detail) > 0 {
			d = detail[0]
		}
		elapsed := time.Since(start)
		r.Steps = append(r.Steps, Step{Name: name, Duration: elapsed, Detail: d})
		log.Printf("  [%s] done in %v  %s", name, elapsed.Round(time.Millisecond), d)
	}
}

func (r *BenchReport) TotalDuration() time.Duration {
	var total time.Duration
	for _, s := range r.Steps {
		total += s.Duration
	}
	return total
}

func (r *BenchReport) Print() {
	env := r.Env
	fmt.Println()
	fmt.Println("╔══════════════════════════════════════════════════════════════╗")
	fmt.Println("║              IMDb Seeder Benchmark — Results                 ║")
	fmt.Println("╠══════════════════════════════════════════════════════════════╣")
	fmt.Printf("║  bench_version  %-43s║\n", env.BenchVersion)
	fmt.Printf("║  server_version %-43s║\n", env.ServerVersion)
	fmt.Printf("║  server_url     %-43s║\n", env.ServerURL)
	fmt.Printf("║  project_id     %-43s║\n", env.ProjectID)
	fmt.Printf("║  git_commit     %-43s║\n", env.GitCommit)
	fmt.Printf("║  hostname       %-43s║\n", env.Hostname)
	fmt.Printf("║  seed_limit     %-43d║\n", env.SeedLimit)
	fmt.Println("╠══════════════════════════════════════════════════════════════╣")
	for _, s := range r.Steps {
		detail := ""
		if s.Detail != "" {
			detail = "  (" + s.Detail + ")"
		}
		fmt.Printf("║  %-35s  %8v%s\n", s.Name, s.Duration.Round(time.Millisecond), detail)
	}
	fmt.Println("╠══════════════════════════════════════════════════════════════╣")
	fmt.Printf("║  %-35s  %8v\n", "TOTAL", r.TotalDuration().Round(time.Millisecond))
	fmt.Println("╚══════════════════════════════════════════════════════════════╝")
}

// appendRunLog appends one JSON line per run to a persistent log file.
// This lets you diff runs across server versions and DB configs over time.
func appendRunLog(logFile string, report *BenchReport) {
	if err := os.MkdirAll(filepath.Dir(logFile), 0755); err != nil {
		log.Printf("WARNING: cannot create log dir: %v", err)
		return
	}
	f, err := os.OpenFile(logFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		log.Printf("WARNING: cannot open log file %s: %v", logFile, err)
		return
	}
	defer f.Close()

	// Serialise durations as milliseconds for easy comparison
	type stepJSON struct {
		Name       string  `json:"name"`
		DurationMs float64 `json:"duration_ms"`
		Detail     string  `json:"detail,omitempty"`
	}
	type runJSON struct {
		BenchEnv
		StartedAt string     `json:"started_at"`
		Steps     []stepJSON `json:"steps"`
		TotalMs   float64    `json:"total_ms"`
		Objects   int        `json:"objects_count"`
		Relations int        `json:"relations_count"`
		RelErrors int64      `json:"rel_errors"`
	}

	var steps []stepJSON
	for _, s := range report.Steps {
		steps = append(steps, stepJSON{
			Name:       s.Name,
			DurationMs: float64(s.Duration.Milliseconds()),
			Detail:     s.Detail,
		})
	}

	entry := runJSON{
		BenchEnv:  report.Env,
		StartedAt: report.StartedAt.Format(time.RFC3339),
		Steps:     steps,
		TotalMs:   float64(report.TotalDuration().Milliseconds()),
		Objects:   report.Objects,
		Relations: report.Relations,
		RelErrors: report.RelErrors,
	}

	data, err := json.Marshal(entry)
	if err != nil {
		log.Printf("WARNING: cannot marshal run entry: %v", err)
		return
	}
	f.Write(data)
	f.Write([]byte("\n"))
	log.Printf("Run appended to log: %s", logFile)
}

// ─── Environment detection ────────────────────────────────────────────────────

func fetchServerVersion(serverURL string) string {
	type healthResp struct {
		Version string `json:"version"`
	}
	resp, err := http.Get(serverURL + "/health")
	if err != nil {
		return "unknown"
	}
	defer resp.Body.Close()
	var h healthResp
	if err := json.NewDecoder(resp.Body).Decode(&h); err != nil || h.Version == "" {
		return "unknown"
	}
	return h.Version
}

func gitCommit() string {
	out, err := exec.Command("git", "rev-parse", "--short", "HEAD").Output()
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(out))
}

func hostname() string {
	h, err := os.Hostname()
	if err != nil {
		return "unknown"
	}
	return h
}

// ─── Main ─────────────────────────────────────────────────────────────────────

func main() {
	serverURL := getenv("SERVER_URL", defaultServerURL)
	apiKey := getenv("API_KEY", defaultAPIKey)
	existingProjectID := os.Getenv("PROJECT_ID")
	skipDelete := os.Getenv("SKIP_DELETE") == "true"
	deleteOnly := os.Getenv("DELETE_ONLY") == "true"
	seedLimit := 100
	if l := os.Getenv("SEED_LIMIT"); l != "" {
		if n, err := strconv.Atoi(l); err == nil {
			seedLimit = n
		}
	}
	pollTimeout := 10 * time.Second
	if t := os.Getenv("POLL_TIMEOUT"); t != "" {
		if n, err := strconv.Atoi(t); err == nil {
			pollTimeout = time.Duration(n) * time.Second
		}
	}
	logFile := getenv("LOG_FILE", "docs/tests/imdb_bench_log.jsonl")

	// SDK client (project-agnostic for create/delete operations)
	baseClient, err := sdk.New(sdk.Config{
		ServerURL:  serverURL,
		HTTPClient: &http.Client{Timeout: 5 * time.Minute},
		Auth:       sdk.AuthConfig{Mode: "apikey", APIKey: apiKey},
	})
	if err != nil {
		log.Fatalf("SDK init failed: %v", err)
	}

	ctx := context.Background()

	// Collect environment metadata upfront
	env := BenchEnv{
		BenchVersion:  BenchVersion,
		ServerVersion: fetchServerVersion(serverURL),
		ServerURL:     serverURL,
		OrgID:         defaultOrgID,
		SeedLimit:     seedLimit,
		GitCommit:     gitCommit(),
		GoVersion:     runtime.Version(),
		GOOS:          runtime.GOOS,
		GOARCH:        runtime.GOARCH,
		Hostname:      hostname(),
	}

	report := &BenchReport{Env: env, StartedAt: time.Now()}

	log.Printf("IMDb Benchmark v%s — server=%s (%s)  git=%s  seed=%d",
		BenchVersion, serverURL, env.ServerVersion, env.GitCommit, seedLimit)

	// ── Phase 0: Delete existing project (async on server) ────────────────────
	if existingProjectID != "" && !skipDelete {
		log.Printf("Phase 0: Deleting project %s (async) ...", existingProjectID)
		done0 := report.Begin("delete_project")
		if err := deleteProjectAsync(ctx, baseClient, serverURL, existingProjectID, pollTimeout); err != nil {
			log.Fatalf("Delete failed: %v", err)
		}
		done0(fmt.Sprintf("project=%s", existingProjectID))
	}

	if deleteOnly {
		log.Println("DELETE_ONLY mode — done.")
		report.Print()
		return
	}

	// ── Phase 1: Create project ───────────────────────────────────────────────
	log.Println("Phase 1: Creating fresh project ...")
	done1 := report.Begin("create_project")
	proj, err := baseClient.Projects.Create(ctx, &projects.CreateProjectRequest{
		Name:  fmt.Sprintf("IMDb Bench %s", time.Now().Format("2006-01-02T15:04:05")),
		OrgID: defaultOrgID,
	})
	if err != nil {
		log.Fatalf("Create project failed: %v", err)
	}
	projectID := proj.ID
	report.Env.ProjectID = projectID
	log.Printf("  Created project: %s (%s)", proj.Name, projectID)
	done1(fmt.Sprintf("id=%s", projectID))

	// SDK client scoped to the new project
	client, err := sdk.New(sdk.Config{
		ServerURL:  serverURL,
		ProjectID:  projectID,
		HTTPClient: &http.Client{Timeout: 5 * time.Minute},
		Auth:       sdk.AuthConfig{Mode: "apikey", APIKey: apiKey},
	})
	if err != nil {
		log.Fatalf("SDK re-init failed: %v", err)
	}

	// ── Phase 2: Load IMDb data ────────────────────────────────────────────────
	log.Printf("Phase 2: Loading IMDb data (limit=%d titles) ...", seedLimit)
	done2 := report.Begin("load_imdb_data")

	filteredRatings, filteredVotes := getFilteredTitleIDs(seedLimit)
	titles, titleGenres := getTitleMetadata(filteredRatings, filteredVotes)
	episodes, seasons := getEpisodes(titles)
	roles, targetPersonIDs, characterNames := getPrincipals(titles)
	crewRoles, crewTargetIDs := getCrew(titles)
	for id := range crewTargetIDs {
		targetPersonIDs[id] = true
	}
	akas := getAKAs(titles)
	people := getPeopleMetadata(targetPersonIDs)

	titleAKAs := make(map[string][]string)
	for _, a := range akas {
		titleAKAs[a.TitleID] = append(titleAKAs[a.TitleID], a.LocalizedTitle)
	}

	estObjects := estimateObjectCount(titles, episodes, seasons, people, titleGenres, characterNames)
	estRels := estimateRelationshipCount(roles, crewRoles, titleGenres, people, episodes, seasons)
	done2(fmt.Sprintf("titles=%d people=%d est_objects=%d est_rels=%d",
		len(titles), len(people), estObjects, estRels))

	// ── Phase 3: Ingest objects ────────────────────────────────────────────────
	log.Printf("Phase 3: Ingesting objects (~%d) ...", estObjects)
	done3 := report.Begin("ingest_objects")
	idMap := ingestObjects(ctx, client.Graph, titles, episodes, seasons, people, titleGenres, characterNames, titleAKAs)
	done3(fmt.Sprintf("mapped=%d", len(idMap)))

	// ── Phase 3b: Verify object count via API ─────────────────────────────────
	done3b := report.Begin("count_objects_api")
	objCount, _ := client.Graph.CountObjects(ctx, &graph.CountObjectsOptions{})
	objCountWithDel, _ := client.Graph.CountObjects(ctx, &graph.CountObjectsOptions{IncludeDeleted: true})
	report.Objects = objCount
	done3b(fmt.Sprintf("live=%d incl_deleted=%d", objCount, objCountWithDel))

	// ── Phase 4: Ingest relationships ─────────────────────────────────────────
	log.Printf("Phase 4: Ingesting relationships (~%d) ...", estRels)
	done4 := report.Begin("ingest_relationships")
	relSucceeded, relFailed := ingestRelationships(ctx, client.Graph, roles, crewRoles, titleGenres, titles, episodes, seasons, people, idMap)
	report.RelErrors = relFailed
	done4(fmt.Sprintf("succeeded=%d failed=%d", relSucceeded, relFailed))

	// ── Phase 4b: Verify relationship count via API ───────────────────────────
	done4b := report.Begin("count_relationships_api")
	relCount := -1
	if relResp, err := client.Graph.ListRelationships(ctx, &graph.ListRelationshipsOptions{Limit: 1}); err == nil {
		relCount = relResp.Total
	}
	report.Relations = relCount
	done4b(fmt.Sprintf("total=%d", relCount))

	// ── Summary + persistent log ──────────────────────────────────────────────
	fmt.Println()
	log.Printf("Project: %s", projectID)
	log.Printf("Objects: %d live  Relationships: %d  Rel errors: %d", objCount, relCount, relFailed)

	report.Print()
	appendRunLog(logFile, report)
}

// ─── Delete + Hard-Delete Verification ───────────────────────────────────────

func deleteProjectAsync(ctx context.Context, client *sdk.Client, serverURL, projectID string, pollTimeout time.Duration) error {
	// 1. Count objects + relationships BEFORE delete (baseline numbers for report)
	log.Printf("  Pre-delete: counting objects/relationships in project %s ...", projectID)

	projClient, err := sdk.New(sdk.Config{
		ServerURL:  serverURL,
		ProjectID:  projectID,
		HTTPClient: &http.Client{Timeout: 2 * time.Minute},
		Auth:       sdk.AuthConfig{Mode: "apikey", APIKey: defaultAPIKey},
	})
	if err != nil {
		return fmt.Errorf("failed to create project client: %w", err)
	}

	objsBefore, _ := projClient.Graph.CountObjects(ctx, &graph.CountObjectsOptions{})
	relsBefore := -1
	if relResp, err := projClient.Graph.ListRelationships(ctx, &graph.ListRelationshipsOptions{Limit: 1}); err == nil {
		relsBefore = relResp.Total
	}
	log.Printf("  Before delete: objects=%d  relationships=%d", objsBefore, relsBefore)

	// 2. Fire DELETE — server now returns 202 immediately and cascades in background.
	//    We don't wait for cascade to finish; we proceed to create the new project right away.
	deleteStart := time.Now()
	log.Printf("  Sending DELETE (async server-side) ...")
	if err := client.Projects.Delete(ctx, projectID); err != nil {
		return fmt.Errorf("delete request failed: %w", err)
	}
	log.Printf("  DELETE accepted in %v — cascade running in background on server", time.Since(deleteStart).Round(time.Millisecond))
	log.Printf("  (server will delete %d objects + %d relationships asynchronously)", objsBefore, relsBefore)

	// 3. Optional: poll briefly to confirm project is no longer visible (not required)
	if pollTimeout > 0 {
		deadline := time.Now().Add(pollTimeout)
		for time.Now().Before(deadline) {
			time.Sleep(2 * time.Second)
			_, err := client.Projects.Get(ctx, projectID, nil)
			if err != nil && (strings.Contains(err.Error(), "404") || strings.Contains(err.Error(), "not_found")) {
				log.Printf("  Project %s confirmed gone from API (%v after delete)", projectID, time.Since(deleteStart).Round(time.Second))
				return nil
			}
		}
		log.Printf("  Note: project may still appear in API while background cascade runs (this is expected)")
	}

	return nil
}

// ─── IMDb data loading (reused from seed-imdb) ────────────────────────────────

func streamIMDBFile(url string) (io.ReadCloser, *gzip.Reader, error) {
	filename := url[strings.LastIndex(url, "/")+1:]
	cacheDir := "/tmp/imdb_data"
	os.MkdirAll(cacheDir, 0755)
	localPath := filepath.Join(cacheDir, filename)

	if _, err := os.Stat(localPath); os.IsNotExist(err) {
		log.Printf("  Downloading %s...", url)
		resp, err := http.Get(url)
		if err != nil {
			return nil, nil, err
		}
		outFile, _ := os.Create(localPath)
		io.Copy(outFile, resp.Body)
		outFile.Close()
		resp.Body.Close()
	}
	f, _ := os.Open(localPath)
	reader, _ := gzip.NewReader(f)
	return f, reader, nil
}

func getFilteredTitleIDs(limit int) (map[string]float64, map[string]int) {
	closer, reader, _ := streamIMDBFile("https://datasets.imdbws.com/title.ratings.tsv.gz")
	defer closer.Close()
	defer reader.Close()

	scanner := bufio.NewScanner(reader)
	scanner.Scan()
	ratings, votes := make(map[string]float64), make(map[string]int)

	for scanner.Scan() {
		parts := strings.Split(scanner.Text(), "\t")
		if len(parts) != 3 {
			continue
		}
		if v, _ := strconv.Atoi(parts[2]); v > 5000 {
			r, _ := strconv.ParseFloat(parts[1], 64)
			ratings[parts[0]], votes[parts[0]] = r, v
			if limit > 0 && len(ratings) >= limit {
				break
			}
		}
	}
	return ratings, votes
}

type Title struct {
	ID, Name, OriginalName, Type string
	StartYear, EndYear, Runtime  int
	Rating                       float64
	Votes                        int
	IsAdult                      bool
}

func getTitleMetadata(ratings map[string]float64, votes map[string]int) (map[string]Title, map[string][]string) {
	closer, reader, _ := streamIMDBFile("https://datasets.imdbws.com/title.basics.tsv.gz")
	defer closer.Close()
	defer reader.Close()

	scanner := bufio.NewScanner(reader)
	scanner.Scan()
	titles := make(map[string]Title)
	genres := make(map[string][]string)
	validTypes := map[string]bool{"movie": true, "tvSeries": true, "tvMiniSeries": true, "videoGame": true, "tvEpisode": true, "tvMovie": true, "short": true}

	for scanner.Scan() {
		parts := strings.Split(scanner.Text(), "\t")
		if len(parts) < 9 || !validTypes[parts[1]] {
			continue
		}
		if rating, ok := ratings[parts[0]]; ok {
			sy, _ := strconv.Atoi(parts[5])
			ey, _ := strconv.Atoi(parts[6])
			rt, _ := strconv.Atoi(parts[7])
			titles[parts[0]] = Title{
				ID: parts[0], Name: parts[2], OriginalName: parts[3], Type: parts[1],
				StartYear: sy, EndYear: ey, Runtime: rt, Rating: rating, Votes: votes[parts[0]], IsAdult: parts[4] == "1",
			}
			if parts[8] != "\\N" {
				genres[parts[0]] = strings.Split(parts[8], ",")
			}
		}
	}
	return titles, genres
}

type Episode struct {
	ID, ParentID           string
	SeasonNumber, EpNumber int
}

func getEpisodes(titles map[string]Title) (map[string]Episode, map[string]string) {
	closer, reader, _ := streamIMDBFile("https://datasets.imdbws.com/title.episode.tsv.gz")
	defer closer.Close()
	defer reader.Close()

	scanner := bufio.NewScanner(reader)
	scanner.Scan()
	episodes := make(map[string]Episode)
	seasons := make(map[string]string)

	for scanner.Scan() {
		parts := strings.Split(scanner.Text(), "\t")
		if len(parts) < 4 {
			continue
		}
		if _, ok := titles[parts[0]]; !ok {
			continue
		}
		sn, _ := strconv.Atoi(parts[2])
		en, _ := strconv.Atoi(parts[3])
		episodes[parts[0]] = Episode{ID: parts[0], ParentID: parts[1], SeasonNumber: sn, EpNumber: en}
		if sn > 0 {
			seasons[fmt.Sprintf("%s_s%d", parts[1], sn)] = parts[1]
		}
	}
	return episodes, seasons
}

type Role struct {
	TitleID, PersonID, Category, Job string
	Characters                       []string
	Ordering                         int
}

func getPrincipals(titles map[string]Title) ([]Role, map[string]bool, map[string]bool) {
	closer, reader, _ := streamIMDBFile("https://datasets.imdbws.com/title.principals.tsv.gz")
	defer closer.Close()
	defer reader.Close()

	scanner := bufio.NewScanner(reader)
	scanner.Scan()
	var roles []Role
	peopleIDs := make(map[string]bool)
	characters := make(map[string]bool)

	for scanner.Scan() {
		parts := strings.Split(scanner.Text(), "\t")
		if len(parts) < 6 {
			continue
		}
		if _, ok := titles[parts[0]]; !ok {
			continue
		}
		ordering, _ := strconv.Atoi(parts[1])
		job := parts[4]
		if job == "\\N" {
			job = ""
		}
		peopleIDs[parts[2]] = true
		var chars []string
		if parts[5] != "\\N" {
			json.Unmarshal([]byte(parts[5]), &chars)
			for _, c := range chars {
				characters[c] = true
			}
		}
		roles = append(roles, Role{TitleID: parts[0], PersonID: parts[2], Category: parts[3], Job: job, Characters: chars, Ordering: ordering})
	}
	return roles, peopleIDs, characters
}

type CrewRel struct{ TitleID, PersonID, Role string }

func getCrew(titles map[string]Title) ([]CrewRel, map[string]bool) {
	closer, reader, _ := streamIMDBFile("https://datasets.imdbws.com/title.crew.tsv.gz")
	defer closer.Close()
	defer reader.Close()

	scanner := bufio.NewScanner(reader)
	scanner.Scan()
	var crew []CrewRel
	peopleIDs := make(map[string]bool)

	for scanner.Scan() {
		parts := strings.Split(scanner.Text(), "\t")
		if len(parts) < 3 {
			continue
		}
		if _, ok := titles[parts[0]]; !ok {
			continue
		}
		if parts[1] != "\\N" {
			for _, dID := range strings.Split(parts[1], ",") {
				peopleIDs[dID] = true
				crew = append(crew, CrewRel{parts[0], dID, "DIRECTED"})
			}
		}
		if parts[2] != "\\N" {
			for _, wID := range strings.Split(parts[2], ",") {
				peopleIDs[wID] = true
				crew = append(crew, CrewRel{parts[0], wID, "WROTE"})
			}
		}
	}
	return crew, peopleIDs
}

type AKA struct {
	TitleID, LocalizedTitle string
}

func getAKAs(titles map[string]Title) []AKA {
	closer, reader, _ := streamIMDBFile("https://datasets.imdbws.com/title.akas.tsv.gz")
	defer closer.Close()
	defer reader.Close()

	scanner := bufio.NewScanner(reader)
	scanner.Scan()
	var akas []AKA

	for scanner.Scan() {
		parts := strings.Split(scanner.Text(), "\t")
		if len(parts) < 8 {
			continue
		}
		if _, ok := titles[parts[0]]; !ok {
			continue
		}
		akas = append(akas, AKA{TitleID: parts[0], LocalizedTitle: parts[2]})
	}
	return akas
}

type Person struct {
	ID, Name              string
	Birth, Death          int
	Professions, KnownFor []string
}

func getPeopleMetadata(targetIDs map[string]bool) map[string]Person {
	closer, reader, _ := streamIMDBFile("https://datasets.imdbws.com/name.basics.tsv.gz")
	defer closer.Close()
	defer reader.Close()

	scanner := bufio.NewScanner(reader)
	scanner.Scan()
	people := make(map[string]Person)

	for scanner.Scan() {
		parts := strings.Split(scanner.Text(), "\t")
		if len(parts) < 6 || !targetIDs[parts[0]] {
			continue
		}
		b, _ := strconv.Atoi(parts[2])
		d, _ := strconv.Atoi(parts[3])
		var profs, kf []string
		if parts[4] != "\\N" {
			profs = strings.Split(parts[4], ",")
		}
		if parts[5] != "\\N" {
			kf = strings.Split(parts[5], ",")
		}
		people[parts[0]] = Person{parts[0], parts[1], b, d, profs, kf}
	}
	return people
}

// ─── Object/Relationship count estimators ─────────────────────────────────────

func estimateObjectCount(titles map[string]Title, episodes map[string]Episode, seasons map[string]string, people map[string]Person, titleGenres map[string][]string, characterNames map[string]bool) int {
	genres := make(map[string]bool)
	for _, gs := range titleGenres {
		for _, g := range gs {
			genres[g] = true
		}
	}
	profs := make(map[string]bool)
	for _, p := range people {
		for _, pr := range p.Professions {
			profs[pr] = true
		}
	}
	return len(titles) + len(episodes) + len(seasons) + len(people) + len(genres) + len(profs) + len(characterNames)
}

func estimateRelationshipCount(roles []Role, crewRoles []CrewRel, titleGenres map[string][]string, people map[string]Person, episodes map[string]Episode, seasons map[string]string) int {
	genreRels := 0
	for _, gs := range titleGenres {
		genreRels += len(gs)
	}
	profRels := 0
	kfRels := 0
	for _, p := range people {
		profRels += len(p.Professions)
		kfRels += len(p.KnownFor)
	}
	charRels := 0
	for _, r := range roles {
		charRels += len(r.Characters) * 2 // PLAYED + APPEARS_IN
	}
	return len(roles) + len(crewRoles) + genreRels + profRels + kfRels + charRels + len(episodes)*2 + len(seasons)
}

// ─── Object ingestion ─────────────────────────────────────────────────────────

func ingestObjects(ctx context.Context, client *graph.Client, titles map[string]Title, episodes map[string]Episode, seasons map[string]string, people map[string]Person, titleGenres map[string][]string, characterNames map[string]bool, titleAKAs map[string][]string) map[string]string {
	var items []graph.CreateObjectRequest

	// Genres
	uniqueGenres := make(map[string]bool)
	for _, gs := range titleGenres {
		for _, g := range gs {
			uniqueGenres[g] = true
		}
	}
	for g := range uniqueGenres {
		k := "genre_" + g
		items = append(items, graph.CreateObjectRequest{Type: "Genre", Key: &k, Properties: map[string]any{"name": g}})
	}

	// Professions
	uniqueProfs := make(map[string]bool)
	for _, p := range people {
		for _, pr := range p.Professions {
			uniqueProfs[pr] = true
		}
	}
	for prof := range uniqueProfs {
		k := "prof_" + prof
		items = append(items, graph.CreateObjectRequest{Type: "Profession", Key: &k, Properties: map[string]any{"name": strings.ReplaceAll(prof, "_", " ")}})
	}

	// Characters
	for char := range characterNames {
		k := "char_" + char
		items = append(items, graph.CreateObjectRequest{Type: "Character", Key: &k, Properties: map[string]any{"name": char}})
	}

	// Seasons
	for sKey, parentID := range seasons {
		k := sKey
		parts := strings.Split(sKey, "_s")
		sn, _ := strconv.Atoi(parts[1])
		items = append(items, graph.CreateObjectRequest{Type: "Season", Key: &k, Properties: map[string]any{"season_number": sn, "parent_series_id": parentID}})
	}

	// Titles
	for _, t := range titles {
		k := t.ID
		nodeType := titleNodeType(t.Type)
		props := titleProps(t)
		if akaList, ok := titleAKAs[t.ID]; ok && len(akaList) > 0 {
			props["aka_titles"] = akaList
		}
		items = append(items, graph.CreateObjectRequest{Type: nodeType, Key: &k, Properties: props})
	}

	// People
	for _, p := range people {
		k := p.ID
		props := map[string]any{"name": p.Name}
		if p.Birth > 0 {
			props["birth_year"] = p.Birth
			props["birth_decade"] = fmt.Sprintf("%ds", (p.Birth/10)*10)
		}
		if p.Death > 0 {
			props["death_year"] = p.Death
		}
		items = append(items, graph.CreateObjectRequest{Type: "Person", Key: &k, Properties: props})
	}

	log.Printf("  Uploading %d objects in batches of %d with %d workers ...", len(items), batchSize, numWorkers)
	return bulkUploadObjects(ctx, client, items)
}

func titleNodeType(t string) string {
	switch t {
	case "tvSeries":
		return "TVSeries"
	case "tvMiniSeries":
		return "TVMiniSeries"
	case "videoGame":
		return "VideoGame"
	case "tvMovie", "tvSpecial":
		return "TVMovie"
	case "tvEpisode":
		return "TVEpisode"
	case "short":
		return "ShortFilm"
	default:
		return "Movie"
	}
}

func titleProps(t Title) map[string]any {
	props := map[string]any{
		"title":          t.Name,
		"name":           t.Name,
		"original_title": t.OriginalName,
		"release_year":   t.StartYear,
		"runtime_mins":   t.Runtime,
		"rating":         t.Rating,
		"votes":          t.Votes,
		"is_adult":       t.IsAdult,
	}
	if t.EndYear > 0 {
		props["end_year"] = t.EndYear
	}
	switch {
	case t.Rating >= 9.0:
		props["rating_tier"] = "Masterpiece"
	case t.Rating >= 8.0:
		props["rating_tier"] = "Excellent"
	case t.Rating >= 7.0:
		props["rating_tier"] = "Good"
	case t.Rating >= 5.0:
		props["rating_tier"] = "Average"
	default:
		props["rating_tier"] = "Poor"
	}
	if t.Runtime > 0 {
		if t.Runtime < 40 {
			props["duration_category"] = "Short"
		} else if t.Runtime > 140 {
			props["duration_category"] = "Epic"
		} else {
			props["duration_category"] = "FeatureLength"
		}
	}
	if t.StartYear > 1800 {
		props["release_decade"] = fmt.Sprintf("%ds", (t.StartYear/10)*10)
	}
	return props
}

// ─── Relationship ingestion ────────────────────────────────────────────────────

func ingestRelationships(ctx context.Context, client *graph.Client, roles []Role, crewRoles []CrewRel, titleGenres map[string][]string, titles map[string]Title, episodes map[string]Episode, seasons map[string]string, people map[string]Person, idMap map[string]string) (succeeded, failed int64) {
	var items []graph.CreateRelationshipRequest

	for _, r := range roles {
		src, ok1 := idMap[r.PersonID]
		dst, ok2 := idMap[r.TitleID]
		if !ok1 || !ok2 {
			continue
		}
		relType := mapCategory(r.Category)
		if relType == "" {
			continue
		}
		props := map[string]any{}
		if r.Job != "" {
			props["job"] = r.Job
		}
		if r.Ordering > 0 {
			props["billing_order"] = r.Ordering
		}
		items = append(items, graph.CreateRelationshipRequest{Type: relType, SrcID: src, DstID: dst, Properties: props})
		for _, char := range r.Characters {
			if charDst, ok3 := idMap["char_"+char]; ok3 {
				items = append(items, graph.CreateRelationshipRequest{Type: "PLAYED", SrcID: src, DstID: charDst, Properties: map[string]any{}})
				items = append(items, graph.CreateRelationshipRequest{Type: "APPEARS_IN", SrcID: charDst, DstID: dst, Properties: map[string]any{}})
			}
		}
	}

	for _, cr := range crewRoles {
		if src, ok1 := idMap[cr.PersonID]; ok1 {
			if dst, ok2 := idMap[cr.TitleID]; ok2 {
				items = append(items, graph.CreateRelationshipRequest{Type: cr.Role, SrcID: src, DstID: dst, Properties: map[string]any{}})
			}
		}
	}

	for tID := range titles {
		src, ok1 := idMap[tID]
		if !ok1 {
			continue
		}
		for _, g := range titleGenres[tID] {
			if dst, ok2 := idMap["genre_"+g]; ok2 {
				items = append(items, graph.CreateRelationshipRequest{Type: "IN_GENRE", SrcID: src, DstID: dst, Properties: map[string]any{}})
			}
		}
	}

	for pID, p := range people {
		src, ok1 := idMap[pID]
		if !ok1 {
			continue
		}
		for _, prof := range p.Professions {
			if dst, ok2 := idMap["prof_"+prof]; ok2 {
				items = append(items, graph.CreateRelationshipRequest{Type: "HAS_PROFESSION", SrcID: src, DstID: dst, Properties: map[string]any{}})
			}
		}
		for _, kfID := range p.KnownFor {
			if dst, ok2 := idMap[kfID]; ok2 {
				items = append(items, graph.CreateRelationshipRequest{Type: "KNOWN_FOR", SrcID: src, DstID: dst, Properties: map[string]any{}})
			}
		}
	}

	for epID, ep := range episodes {
		epSrc, ok1 := idMap[epID]
		seriesDst, ok2 := idMap[ep.ParentID]
		if ok1 && ok2 {
			items = append(items, graph.CreateRelationshipRequest{Type: "EPISODE_OF", SrcID: epSrc, DstID: seriesDst, Properties: map[string]any{"season_number": ep.SeasonNumber, "episode_number": ep.EpNumber}})
			if ep.SeasonNumber > 0 {
				if seasonDst, ok3 := idMap[fmt.Sprintf("%s_s%d", ep.ParentID, ep.SeasonNumber)]; ok3 {
					items = append(items, graph.CreateRelationshipRequest{Type: "IN_SEASON", SrcID: epSrc, DstID: seasonDst, Properties: map[string]any{}})
				}
			}
		}
	}
	for sKey, seriesID := range seasons {
		if sSrc, ok1 := idMap[sKey]; ok1 {
			if seriesDst, ok2 := idMap[seriesID]; ok2 {
				items = append(items, graph.CreateRelationshipRequest{Type: "SEASON_OF", SrcID: sSrc, DstID: seriesDst, Properties: map[string]any{}})
			}
		}
	}

	log.Printf("  Uploading %d relationships in batches of %d with %d workers ...", len(items), batchSize, numWorkers)
	return bulkUploadRelationships(ctx, client, items)
}

func mapCategory(cat string) string {
	switch strings.ToUpper(cat) {
	case "ACTOR", "ACTRESS", "SELF":
		return "ACTED_IN"
	case "COMPOSER":
		return "COMPOSED_MUSIC_FOR"
	case "PRODUCTION_DESIGNER":
		return "DESIGNED_PRODUCTION_FOR"
	case "ARCHIVE_FOOTAGE", "ARCHIVE_SOUND":
		return "ARCHIVE_APPEARANCE_IN"
	case "DIRECTOR":
		return "DIRECTED"
	case "WRITER":
		return "WROTE"
	case "PRODUCER":
		return "PRODUCED"
	case "EDITOR":
		return "EDITED"
	case "CINEMATOGRAPHER":
		return "CINEMATOGRAPHER_ON"
	case "CASTING_DIRECTOR":
		return "CAST_FOR"
	default:
		return "" // skip unknown
	}
}

// ─── Bulk upload helpers ──────────────────────────────────────────────────────

func bulkUploadObjects(ctx context.Context, client *graph.Client, items []graph.CreateObjectRequest) map[string]string {
	type batchResult struct {
		batch []graph.CreateObjectRequest
		res   *graph.BulkCreateObjectsResponse
	}

	batches := make(chan []graph.CreateObjectRequest, numWorkers*2)
	results := make(chan batchResult, numWorkers*2)

	var wg sync.WaitGroup
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for batch := range batches {
				res, err := client.BulkCreateObjects(ctx, &graph.BulkCreateObjectsRequest{Items: batch})
				if err != nil {
					time.Sleep(500 * time.Millisecond)
					res, _ = client.BulkCreateObjects(ctx, &graph.BulkCreateObjectsRequest{Items: batch})
				}
				results <- batchResult{batch, res}
			}
		}()
	}
	go func() { wg.Wait(); close(results) }()

	go func() {
		for i := 0; i < len(items); i += batchSize {
			end := i + batchSize
			if end > len(items) {
				end = len(items)
			}
			batches <- items[i:end]
		}
		close(batches)
	}()

	idMap := make(map[string]string)
	var mu sync.Mutex
	var uploaded, conflicts, failed atomic.Int64

	type missingKey struct{ objType, key string }
	var missingKeys []missingKey
	var missingMu sync.Mutex

	for br := range results {
		if br.res != nil {
			mu.Lock()
			for idx, result := range br.res.Results {
				key := br.batch[idx].Key
				if key == nil {
					continue
				}
				if result.Object != nil {
					id := result.Object.EntityID
					if id == "" {
						id = result.Object.CanonicalID
					}
					if id == "" {
						id = result.Object.ID
					}
					idMap[*key] = id
					uploaded.Add(1)
				} else if result.Error != nil && strings.Contains(*result.Error, "conflict") {
					missingMu.Lock()
					missingKeys = append(missingKeys, missingKey{br.batch[idx].Type, *key})
					missingMu.Unlock()
					conflicts.Add(1)
				} else if result.Error != nil {
					log.Printf("  [objects] error key=%s: %s", *key, *result.Error)
					failed.Add(1)
				}
			}
			mu.Unlock()
		}
	}

	if len(missingKeys) > 0 {
		log.Printf("  Resolving %d conflicting keys ...", len(missingKeys))
		sem := make(chan struct{}, numWorkers)
		var resolveWg sync.WaitGroup
		for _, mk := range missingKeys {
			sem <- struct{}{}
			resolveWg.Add(1)
			go func(objType, key string) {
				defer resolveWg.Done()
				defer func() { <-sem }()
				resp, err := client.ListObjects(ctx, &graph.ListObjectsOptions{Type: objType, Key: key, Limit: 1})
				if err == nil && resp != nil && len(resp.Items) > 0 {
					obj := resp.Items[0]
					id := obj.EntityID
					if id == "" {
						id = obj.CanonicalID
					}
					if id == "" {
						id = obj.ID
					}
					mu.Lock()
					idMap[key] = id
					mu.Unlock()
				}
			}(mk.objType, mk.key)
		}
		resolveWg.Wait()
	}

	log.Printf("  Objects: %d new, %d conflict-resolved, %d errors, %d mapped",
		uploaded.Load(), conflicts.Load(), failed.Load(), len(idMap))
	return idMap
}

func bulkUploadRelationships(ctx context.Context, client *graph.Client, items []graph.CreateRelationshipRequest) (int64, int64) {
	batches := make(chan []graph.CreateRelationshipRequest, numWorkers*2)

	var wg sync.WaitGroup
	var succeeded, failed atomic.Int64

	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for batch := range batches {
				res, err := client.BulkCreateRelationships(ctx, &graph.BulkCreateRelationshipsRequest{Items: batch})
				if err != nil {
					time.Sleep(500 * time.Millisecond)
					res, err = client.BulkCreateRelationships(ctx, &graph.BulkCreateRelationshipsRequest{Items: batch})
				}
				if res != nil {
					succeeded.Add(int64(res.Success))
					failed.Add(int64(res.Failed))
					for _, r := range res.Results {
						if !r.Success && r.Error != nil {
							log.Printf("  [rels] item error: %s", *r.Error)
						}
					}
				} else if err != nil {
					log.Printf("  [rels] batch failed permanently: %v", err)
					failed.Add(int64(len(batch)))
				}
			}
		}()
	}

	go func() {
		for i := 0; i < len(items); i += batchSize {
			end := i + batchSize
			if end > len(items) {
				end = len(items)
			}
			batches <- items[i:end]
		}
		close(batches)
	}()

	wg.Wait()
	log.Printf("  Relationships: %d succeeded, %d failed", succeeded.Load(), failed.Load())
	return succeeded.Load(), failed.Load()
}

// ─── Utilities ────────────────────────────────────────────────────────────────

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
