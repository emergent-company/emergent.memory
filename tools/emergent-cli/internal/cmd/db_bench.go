package cmd

// db_bench.go — "emergent db bench" command
//
// Runs a full end-to-end write benchmark against the Emergent HTTP API using
// real IMDb data, then runs db diagnose EXPLAIN checks on the now-populated DB.
//
// Phases:
//   1. (optional) Delete previous bench project                   --project-id
//   2. Create a fresh project
//   3. Download/cache IMDb TSV files to /tmp/imdb_data/
//   4. Seed N titles with parallel workers                         --seed / --workers / --batch
//   5. Verify object + relationship counts via API
//   6. Run EXPLAIN checks (db diagnose) against the live DB        --dsn / auto-detect
//   7. Print combined timing + EXPLAIN report
//   8. (optional) Delete the bench project                         --cleanup
//   9. Append JSONL result to log file                             --log
//
// Connection for EXPLAIN checks uses the same DSN resolution as "db diagnose".
// Connection for the API uses --server or the config file / EMERGENT_SERVER_URL.

import (
	"bufio"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
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
	sdkgraph "github.com/emergent-company/emergent/apps/server-go/pkg/sdk/graph"
	"github.com/emergent-company/emergent/apps/server-go/pkg/sdk/projects"
	"github.com/emergent-company/emergent/tools/emergent-cli/internal/config"
	"github.com/spf13/cobra"
)

// ─── bench version ────────────────────────────────────────────────────────────

const benchVersion = "1.0.0"

// ─── flags ────────────────────────────────────────────────────────────────────

var dbBenchFlags struct {
	seed          int
	offset        int
	workers       int
	batch         int
	cleanup       bool
	logFile       string
	dsn           string
	server        string
	projectID     string
	appendProject string
	skipDelete    bool
	slowMS        int
	verbose       bool
	configPath    string
}

// ─── command definition ───────────────────────────────────────────────────────

var dbBenchCmd = &cobra.Command{
	Use:   "bench",
	Short: "Benchmark write throughput using real IMDb data, then run EXPLAIN checks",
	Long: `Run an end-to-end write benchmark against the Emergent HTTP API using real
IMDb datasets (downloaded and cached to /tmp/imdb_data/). After seeding,
EXPLAIN ANALYZE checks are run against the live database so you get
meaningful query timing on real data.

Phases:
  1. (optional) Delete a previous bench project  --project-id
  2. Create a fresh project
  3. Download / cache IMDb TSV files
  4. Seed N titles with parallel workers         --seed, --workers, --batch
  5. Verify object + relationship counts via API
  6. Run EXPLAIN checks via psql                 --dsn or auto-detect
  7. Print combined timing + EXPLAIN report
  8. (optional) Delete the bench project         --cleanup
  9. Append JSONL result to log                  --log

Connection for EXPLAIN checks (first match wins):
  1. --dsn flag
  2. EMERGENT_DATABASE_URL / DATABASE_URL env var
  3. ~/.emergent/config/.env.local (auto-detect)

Connection for the API (first match wins):
  1. --server flag
  2. EMERGENT_SERVER_URL env var
  3. ~/.emergent/config.yaml server_url field

Examples:
  emergent db bench
  emergent db bench --seed 500 --workers 20 --cleanup
  emergent db bench --dsn "postgres://u:p@localhost:5432/emergent?sslmode=disable"
  emergent db bench --server http://localhost:3002 --seed 1000 --log /tmp/bench.jsonl`,
	RunE: runDbBench,
}

func init() {
	dbBenchCmd.Flags().IntVar(&dbBenchFlags.seed, "seed", 100, "number of IMDb titles to seed (the chunk size)")
	dbBenchCmd.Flags().IntVar(&dbBenchFlags.offset, "offset", 0, "skip the first N qualifying IMDb titles (for chunked seeding)")
	dbBenchCmd.Flags().IntVar(&dbBenchFlags.workers, "workers", 20, "number of parallel upload workers")
	dbBenchCmd.Flags().IntVar(&dbBenchFlags.batch, "batch", 100, "batch size for bulk API calls")
	dbBenchCmd.Flags().BoolVar(&dbBenchFlags.cleanup, "cleanup", false, "delete the bench project after the run")
	dbBenchCmd.Flags().StringVar(&dbBenchFlags.logFile, "log", "", "JSONL log file to append results to (default: ~/.emergent/bench_log.jsonl)")
	dbBenchCmd.Flags().StringVar(&dbBenchFlags.dsn, "dsn", "", "PostgreSQL DSN for EXPLAIN checks (overrides auto-detect)")
	dbBenchCmd.Flags().StringVar(&dbBenchFlags.server, "server", "", "Emergent server URL (overrides config)")
	dbBenchCmd.Flags().StringVar(&dbBenchFlags.projectID, "project-id", "", "delete this project ID before creating a new bench project")
	dbBenchCmd.Flags().StringVar(&dbBenchFlags.appendProject, "append-project", "", "append to existing project ID instead of creating a new one")
	dbBenchCmd.Flags().BoolVar(&dbBenchFlags.skipDelete, "skip-delete", false, "skip deleting --project-id even if set")
	dbBenchCmd.Flags().IntVar(&dbBenchFlags.slowMS, "slow", 200, "flag EXPLAIN queries slower than this many ms")
	dbBenchCmd.Flags().BoolVarP(&dbBenchFlags.verbose, "verbose", "v", false, "print full EXPLAIN output for every query")
	dbBenchCmd.Flags().StringVar(&dbBenchFlags.configPath, "config-path", "", "path to Emergent config.yaml (default: ~/.emergent/config.yaml)")
}

// ─── timing report ────────────────────────────────────────────────────────────

type benchStep struct {
	Name     string        `json:"name"`
	Duration time.Duration `json:"duration"`
	Detail   string        `json:"detail,omitempty"`
}

type benchReport struct {
	BenchVersion  string    `json:"bench_version"`
	ServerVersion string    `json:"server_version"`
	ServerURL     string    `json:"server_url"`
	ProjectID     string    `json:"project_id"`
	SeedLimit     int       `json:"seed_limit"`
	SeedOffset    int       `json:"seed_offset"`
	GitCommit     string    `json:"git_commit"`
	GoVersion     string    `json:"go_version"`
	GOOS          string    `json:"goos"`
	GOARCH        string    `json:"goarch"`
	Hostname      string    `json:"hostname"`
	StartedAt     time.Time `json:"started_at"`
	Steps         []benchStep
	Objects       int   `json:"objects_count"`
	Relations     int   `json:"relations_count"`
	RelErrors     int64 `json:"rel_errors"`
}

func (r *benchReport) begin(name string) func(detail ...string) {
	start := time.Now()
	return func(detail ...string) {
		d := ""
		if len(detail) > 0 {
			d = detail[0]
		}
		elapsed := time.Since(start)
		r.Steps = append(r.Steps, benchStep{Name: name, Duration: elapsed, Detail: d})
		fmt.Printf("  [%s] done in %v", name, elapsed.Round(time.Millisecond))
		if d != "" {
			fmt.Printf("  (%s)", d)
		}
		fmt.Println()
	}
}

func (r *benchReport) totalDuration() time.Duration {
	var total time.Duration
	for _, s := range r.Steps {
		total += s.Duration
	}
	return total
}

func (r *benchReport) print() {
	fmt.Println()
	fmt.Println("╔══════════════════════════════════════════════════════════════╗")
	fmt.Println("║           Emergent db bench — Results                        ║")
	fmt.Println("╠══════════════════════════════════════════════════════════════╣")
	fmt.Printf("║  bench_version  %-43s║\n", r.BenchVersion)
	fmt.Printf("║  server_version %-43s║\n", r.ServerVersion)
	fmt.Printf("║  server_url     %-43s║\n", r.ServerURL)
	fmt.Printf("║  project_id     %-43s║\n", r.ProjectID)
	fmt.Printf("║  git_commit     %-43s║\n", r.GitCommit)
	fmt.Printf("║  hostname       %-43s║\n", r.Hostname)
	fmt.Printf("║  seed_limit     %-43d║\n", r.SeedLimit)
	fmt.Println("╠══════════════════════════════════════════════════════════════╣")
	for _, s := range r.Steps {
		detail := ""
		if s.Detail != "" {
			detail = "  (" + s.Detail + ")"
		}
		fmt.Printf("║  %-35s  %8v%s\n", s.Name, s.Duration.Round(time.Millisecond), detail)
	}
	fmt.Println("╠══════════════════════════════════════════════════════════════╣")
	fmt.Printf("║  %-35s  %8v\n", "TOTAL", r.totalDuration().Round(time.Millisecond))
	fmt.Println("╚══════════════════════════════════════════════════════════════╝")
}

// ─── entry point ──────────────────────────────────────────────────────────────

func runDbBench(_ *cobra.Command, _ []string) error {
	ctx := context.Background()

	// ── Resolve server URL ────────────────────────────────────────────────────
	svrURL := dbBenchFlags.server
	if svrURL == "" {
		if v := os.Getenv("EMERGENT_SERVER_URL"); v != "" {
			svrURL = v
		}
	}
	if svrURL == "" {
		cfgPath := config.DiscoverPath(dbBenchFlags.configPath)
		if cfg, err := config.LoadWithEnv(cfgPath); err == nil && cfg.ServerURL != "" {
			svrURL = cfg.ServerURL
		}
	}
	if svrURL == "" {
		svrURL = "http://localhost:3002"
	}

	// ── Resolve API key + org ID ──────────────────────────────────────────────
	apiKey := os.Getenv("EMERGENT_API_KEY")
	orgID := os.Getenv("EMERGENT_ORG_ID")
	if apiKey == "" || orgID == "" {
		cfgPath := config.DiscoverPath(dbBenchFlags.configPath)
		if cfg, err := config.LoadWithEnv(cfgPath); err == nil {
			if apiKey == "" {
				apiKey = cfg.APIKey
			}
			if orgID == "" {
				orgID = cfg.OrgID
			}
		}
	}

	// ── Resolve log file path ─────────────────────────────────────────────────
	logFile := dbBenchFlags.logFile
	if logFile == "" {
		home, _ := os.UserHomeDir()
		logFile = filepath.Join(home, ".emergent", "bench_log.jsonl")
	}

	// ── Collect environment metadata ──────────────────────────────────────────
	report := &benchReport{
		BenchVersion:  benchVersion,
		ServerVersion: benchFetchServerVersion(svrURL),
		ServerURL:     svrURL,
		SeedLimit:     dbBenchFlags.seed,
		SeedOffset:    dbBenchFlags.offset,
		GitCommit:     benchGitCommit(),
		GoVersion:     runtime.Version(),
		GOOS:          runtime.GOOS,
		GOARCH:        runtime.GOARCH,
		Hostname:      benchHostname(),
		StartedAt:     time.Now(),
	}

	fmt.Printf("\n%s%sEmergent db bench v%s%s\n", diagBold, diagCyan, benchVersion, diagReset)
	fmt.Printf("server=%s (%s)  git=%s  seed=%d  offset=%d\n\n",
		svrURL, report.ServerVersion, report.GitCommit, dbBenchFlags.seed, dbBenchFlags.offset)

	// ── SDK base client (project-agnostic) ────────────────────────────────────
	baseClient, err := benchNewSDK(svrURL, "", apiKey)
	if err != nil {
		return fmt.Errorf("SDK init failed: %w", err)
	}

	// ── Phase 0: Delete previous bench project (optional, async) ──────────────
	if dbBenchFlags.projectID != "" && !dbBenchFlags.skipDelete {
		fmt.Printf("Phase 0: Deleting project %s ...\n", dbBenchFlags.projectID)
		done0 := report.begin("delete_prev_project")
		if err := benchDeleteProject(ctx, baseClient, dbBenchFlags.projectID); err != nil {
			return fmt.Errorf("delete failed: %w", err)
		}
		done0(fmt.Sprintf("project=%s", dbBenchFlags.projectID))
	}

	// ── Phase 1: Create project (or reuse existing) ───────────────────────────
	var projectID string
	if dbBenchFlags.appendProject != "" {
		// Append mode: write into existing project, skip creation
		projectID = dbBenchFlags.appendProject
		report.ProjectID = projectID
		fmt.Printf("Phase 1: Appending to existing project %s ...\n", projectID)
	} else {
		fmt.Println("Phase 1: Creating fresh benchmark project ...")
		done1 := report.begin("create_project")
		proj, err := baseClient.Projects.Create(ctx, &projects.CreateProjectRequest{
			Name:  fmt.Sprintf("Bench %s", time.Now().Format("2006-01-02T15:04:05")),
			OrgID: orgID,
		})
		if err != nil {
			return fmt.Errorf("create project failed: %w", err)
		}
		projectID = proj.ID
		report.ProjectID = projectID
		fmt.Printf("  Created project: %s (%s)\n", proj.Name, projectID)
		done1(fmt.Sprintf("id=%s", projectID))
	}

	// Project-scoped client
	projClient, err := benchNewSDK(svrURL, projectID, apiKey)
	if err != nil {
		return fmt.Errorf("SDK re-init failed: %w", err)
	}

	// ── Phase 2: Load IMDb data ───────────────────────────────────────────────
	fmt.Printf("Phase 2: Loading IMDb data (limit=%d offset=%d titles) ...\n", dbBenchFlags.seed, dbBenchFlags.offset)
	done2 := report.begin("load_imdb_data")

	var (
		titles         map[string]benchTitle
		titleGenres    map[string][]string
		episodes       map[string]benchEpisode
		seasons        map[string]string
		roles          []benchRole
		crewRoles      []benchCrewRel
		characterNames map[string]bool
		titleAKAs      map[string][]string
		people         map[string]benchPerson
	)

	if cached := benchLoadParsedCache(dbBenchFlags.seed, dbBenchFlags.offset); cached != nil {
		fmt.Printf("  Using parsed cache for seed=%d offset=%d\n", dbBenchFlags.seed, dbBenchFlags.offset)
		titles, titleGenres = cached.Titles, cached.TitleGenres
		episodes, seasons = cached.Episodes, cached.Seasons
		roles, crewRoles = cached.Roles, cached.CrewRoles
		characterNames, titleAKAs = cached.CharNames, cached.TitleAKAs
		people = cached.People
	} else {
		filteredRatings, filteredVotes := benchGetFilteredTitleIDs(dbBenchFlags.seed, dbBenchFlags.offset)
		titles, titleGenres = benchGetTitleMetadata(filteredRatings, filteredVotes)
		episodes, seasons = benchGetEpisodes(titles)
		roles, targetPersonIDs, charNames := benchGetPrincipals(titles)
		crewRoles, crewTargetIDs := benchGetCrew(titles)
		for id := range crewTargetIDs {
			targetPersonIDs[id] = true
		}
		akas := benchGetAKAs(titles)
		people = benchGetPeopleMetadata(targetPersonIDs)
		characterNames = charNames
		titleAKAs = make(map[string][]string)
		for _, a := range akas {
			titleAKAs[a.titleID] = append(titleAKAs[a.titleID], a.localizedTitle)
		}
		benchSaveParsedCache(&benchParsedCache{
			Seed: dbBenchFlags.seed, Offset: dbBenchFlags.offset, Titles: titles, TitleGenres: titleGenres,
			Episodes: episodes, Seasons: seasons, Roles: roles, CrewRoles: crewRoles,
			CharNames: characterNames, TitleAKAs: titleAKAs, People: people,
		})
	}

	estObjects := benchEstimateObjectCount(titles, episodes, seasons, people, titleGenres, characterNames)
	estRels := benchEstimateRelCount(roles, crewRoles, titleGenres, people, episodes, seasons)
	done2(fmt.Sprintf("titles=%d people=%d est_objects=%d est_rels=%d",
		len(titles), len(people), estObjects, estRels))

	// ── Phase 3: Ingest objects ───────────────────────────────────────────────
	fmt.Printf("Phase 3: Ingesting objects (~%d) ...\\n", estObjects)
	done3 := report.begin("ingest_objects")
	objBatch := dbBenchFlags.batch
	if objBatch > 200 {
		fmt.Printf("  Note: server max batch size for objects is 200; capping --batch from %d to 200\\\n", objBatch)
		objBatch = 200
	}
	idMap := benchIngestObjects(ctx, projClient.Graph, titles, episodes, seasons, people, titleGenres, characterNames, titleAKAs, objBatch, dbBenchFlags.workers)
	done3(fmt.Sprintf("mapped=%d", len(idMap)))

	// ── Phase 3b: Count objects ───────────────────────────────────────────────
	done3b := report.begin("count_objects_api")
	objCount, _ := projClient.Graph.CountObjects(ctx, &sdkgraph.CountObjectsOptions{})
	report.Objects = objCount
	done3b(fmt.Sprintf("live=%d", objCount))

	// ── Phase 4: Ingest relationships ─────────────────────────────────────────
	fmt.Printf("Phase 4: Ingesting relationships (~%d) ...\\n", estRels)
	done4 := report.begin("ingest_relationships")
	relBatch := dbBenchFlags.batch
	if relBatch > 200 {
		fmt.Printf("  Note: server max batch size for relationships is 200; capping --batch from %d to 200\\n", relBatch)
		relBatch = 200
	}
	relSucceeded, relFailed := benchIngestRelationships(ctx, projClient.Graph, roles, crewRoles, titleGenres, titles, episodes, seasons, people, idMap, relBatch, dbBenchFlags.workers)
	report.RelErrors = relFailed
	done4(fmt.Sprintf("succeeded=%d failed=%d", relSucceeded, relFailed))

	// ── Phase 4b: Count relationships ────────────────────────────────────────
	done4b := report.begin("count_relationships_api")
	relCount := -1
	if relResp, err := projClient.Graph.ListRelationships(ctx, &sdkgraph.ListRelationshipsOptions{Limit: 1}); err == nil {
		relCount = relResp.Total
	}
	report.Relations = relCount
	done4b(fmt.Sprintf("total=%d", relCount))

	// Print timing report now so it appears above the EXPLAIN section
	report.print()

	// ── Phase 5: EXPLAIN checks (db diagnose) ─────────────────────────────────
	dsn, _ := resolveBenchDSN()
	if dsn != "" {
		fmt.Printf("\n%s%sPhase 5: EXPLAIN checks on live data%s\n", diagBold, diagCyan, diagReset)
		fmt.Println("═══════════════════════════════════════════════════════")
		fmt.Println()

		// Use the real project UUID so EXPLAIN hits rows that actually exist
		explainResults := runBenchExplain(dsn, projectID)
		printDiagSummary(explainResults)
	} else {
		fmt.Printf("\n%sPhase 5: EXPLAIN checks skipped — no DSN found%s\n", diagYellow, diagReset)
		fmt.Println("  Set --dsn or EMERGENT_DATABASE_URL to enable EXPLAIN analysis.")
	}

	// ── Phase 6: Cleanup (optional) ──────────────────────────────────────────
	if dbBenchFlags.cleanup {
		fmt.Printf("\nPhase 6: Cleaning up project %s ...\n", projectID)
		doneClean := report.begin("cleanup_project")
		if err := benchDeleteProject(ctx, baseClient, projectID); err != nil {
			fmt.Printf("%sWARN: cleanup failed: %v%s\n", diagYellow, err, diagReset)
		}
		doneClean()
	}

	// ── Append JSONL log ──────────────────────────────────────────────────────
	benchAppendLog(logFile, report)

	fmt.Printf("\n%sObjects: %d  Relationships: %d  Errors: %d%s\n",
		diagGreen, objCount, relCount, relFailed, diagReset)
	fmt.Printf("Project: %s\n", projectID)

	return nil
}

// ─── EXPLAIN checks adapted from db_diagnose.go ───────────────────────────────
//
// Uses a real project_id so the planner sees actual data.

func runBenchExplain(dsn, projectID string) []diagResult {
	// Re-use the slowMS and verbose flags from dbBenchFlags
	// (mirror dbDiagnoseFlags so printDiagSummary uses the right threshold)
	origSlowMS := dbDiagnoseFlags.slowMS
	origVerbose := dbDiagnoseFlags.verbose
	dbDiagnoseFlags.slowMS = dbBenchFlags.slowMS
	dbDiagnoseFlags.verbose = dbBenchFlags.verbose
	defer func() {
		dbDiagnoseFlags.slowMS = origSlowMS
		dbDiagnoseFlags.verbose = origVerbose
	}()

	var results []diagResult
	results = append(results, checkDiagVersion(dsn))
	results = append(results, checkDiagSharedBuffers(dsn))
	results = append(results, checkDiagWalSize(dsn))
	results = append(results, checkDiagTableStats(dsn))
	results = append(results, checkDiagDeadTuples(dsn))
	results = append(results, checkDiagUnusedIndexes(dsn))

	pid := projectID
	results = append(results, diagExplain(dsn, "graph_objects: HEAD lookup by (project,type,key)",
		fmt.Sprintf(`SELECT id, canonical_id, type, key, properties
		 FROM kb.graph_objects
		 WHERE project_id = '%s'
		   AND type = 'Movie'
		   AND key = '__explain_probe__'
		   AND supersedes_id IS NULL
		   AND deleted_at IS NULL
		   AND branch_id IS NULL
		 LIMIT 1`, pid)))
	results = append(results, diagExplain(dsn, "graph_objects: list HEAD objects for project",
		fmt.Sprintf(`SELECT id, type, key
		 FROM kb.graph_objects
		 WHERE project_id = '%s'
		   AND supersedes_id IS NULL
		   AND deleted_at IS NULL
		   AND branch_id IS NULL
		 ORDER BY created_at DESC
		 LIMIT 50`, pid)))
	results = append(results, diagExplain(dsn, "graph_objects: full-text search (FTS)",
		fmt.Sprintf(`SELECT id, type, key, ts_rank(fts, query) AS rank
		 FROM kb.graph_objects, to_tsquery('simple', 'movie') query
		 WHERE project_id = '%s'
		   AND fts @@ query
		   AND supersedes_id IS NULL
		   AND deleted_at IS NULL
		 ORDER BY rank DESC
		 LIMIT 20`, pid)))
	results = append(results, diagExplain(dsn, "graph_relationships: lookup by src_id",
		fmt.Sprintf(`SELECT r.id, r.dst_id, r.type, r.properties
		 FROM kb.graph_relationships r
		 JOIN kb.graph_objects o ON o.id = r.src_id
		 WHERE o.project_id = '%s'
		   AND r.supersedes_id IS NULL
		   AND r.deleted_at IS NULL
		 LIMIT 50`, pid)))
	results = append(results, diagExplain(dsn, "chunk_embedding_jobs: pending dequeue",
		`SELECT id, chunk_id
		 FROM kb.chunk_embedding_jobs
		 WHERE status = 'pending'
		 ORDER BY scheduled_at ASC NULLS FIRST, priority DESC
		 LIMIT 10`))
	results = append(results, diagExplain(dsn, "graph_embedding_jobs: pending dequeue",
		`SELECT id, object_id
		 FROM kb.graph_embedding_jobs
		 WHERE status = 'pending'
		 ORDER BY created_at ASC
		 LIMIT 10`))
	return results
}

// resolveBenchDSN uses the same priority as db diagnose but reads from
// dbBenchFlags.dsn first.
func resolveBenchDSN() (string, error) {
	if dbBenchFlags.dsn != "" {
		return dbBenchFlags.dsn, nil
	}
	// Re-use diagnose's resolver (it checks env vars + .env.local)
	// Temporarily inject into dbDiagnoseFlags so resolveDiagDSN works.
	return resolveDiagDSN()
}

// ─── SDK helpers ──────────────────────────────────────────────────────────────

func benchNewSDK(serverURL, projectID, apiKey string) (*sdk.Client, error) {
	authCfg := sdk.AuthConfig{Mode: "apikey", APIKey: apiKey}
	if apiKey == "" {
		// Fall back to no-auth (works for local dev without auth middleware)
		authCfg = sdk.AuthConfig{Mode: "none"}
	}
	return sdk.New(sdk.Config{
		ServerURL:  serverURL,
		ProjectID:  projectID,
		HTTPClient: &http.Client{Timeout: 10 * time.Minute},
		Auth:       authCfg,
	})
}

func benchDeleteProject(ctx context.Context, client *sdk.Client, projectID string) error {
	return client.Projects.Delete(ctx, projectID)
}

// ─── Environment helpers ──────────────────────────────────────────────────────

func benchFetchServerVersion(serverURL string) string {
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

func benchGitCommit() string {
	out, err := exec.Command("git", "rev-parse", "--short", "HEAD").Output()
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(out))
}

func benchHostname() string {
	h, err := os.Hostname()
	if err != nil {
		return "unknown"
	}
	return h
}

// ─── IMDb data types ──────────────────────────────────────────────────────────

type benchTitle struct {
	ID, Name, OriginalName, Type string
	StartYear, EndYear, Runtime  int
	Rating                       float64
	Votes                        int
	IsAdult                      bool
}

type benchEpisode struct {
	ID, ParentID           string
	SeasonNumber, EpNumber int
}

type benchRole struct {
	TitleID, PersonID, Category, Job string
	Characters                       []string
	Ordering                         int
}

type benchCrewRel struct{ TitleID, PersonID, Role string }

type benchAKA struct{ titleID, localizedTitle string }

type benchPerson struct {
	ID, Name              string
	Birth, Death          int
	Professions, KnownFor []string
}

// ─── parsed-data cache ────────────────────────────────────────────────────────

type benchParsedCache struct {
	Seed        int                     `json:"seed"`
	Offset      int                     `json:"offset"`
	Titles      map[string]benchTitle   `json:"titles"`
	TitleGenres map[string][]string     `json:"title_genres"`
	Episodes    map[string]benchEpisode `json:"episodes"`
	Seasons     map[string]string       `json:"seasons"`
	Roles       []benchRole             `json:"roles"`
	CrewRoles   []benchCrewRel          `json:"crew_roles"`
	CharNames   map[string]bool         `json:"char_names"`
	TitleAKAs   map[string][]string     `json:"title_akas"`
	People      map[string]benchPerson  `json:"people"`
}

func benchParsedCachePath(seed, offset int) string {
	if offset == 0 {
		return fmt.Sprintf("/tmp/imdb_data/parsed_%d.json.gz", seed)
	}
	return fmt.Sprintf("/tmp/imdb_data/parsed_%d_off%d.json.gz", seed, offset)
}

func benchLoadParsedCache(seed, offset int) *benchParsedCache {
	path := benchParsedCachePath(seed, offset)
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return nil
	}
	defer gz.Close()
	var c benchParsedCache
	if err := json.NewDecoder(gz).Decode(&c); err != nil {
		return nil
	}
	if c.Seed != seed || c.Offset != offset {
		return nil
	}
	return &c
}

func benchSaveParsedCache(c *benchParsedCache) {
	os.MkdirAll("/tmp/imdb_data", 0755)
	path := benchParsedCachePath(c.Seed, c.Offset)
	f, err := os.Create(path)
	if err != nil {
		return
	}
	defer f.Close()
	gz := gzip.NewWriter(f)
	defer gz.Close()
	json.NewEncoder(gz).Encode(c) //nolint:errcheck
}

// ─── IMDb download + parse ────────────────────────────────────────────────────

func benchStreamIMDB(url string) (io.ReadCloser, *gzip.Reader, error) {
	filename := url[strings.LastIndex(url, "/")+1:]
	cacheDir := "/tmp/imdb_data"
	os.MkdirAll(cacheDir, 0755)
	localPath := filepath.Join(cacheDir, filename)

	if _, err := os.Stat(localPath); os.IsNotExist(err) {
		fmt.Printf("  Downloading %s ...\n", url)
		resp, err := http.Get(url)
		if err != nil {
			return nil, nil, err
		}
		outFile, _ := os.Create(localPath)
		io.Copy(outFile, resp.Body)
		outFile.Close()
		resp.Body.Close()
	} else {
		fmt.Printf("  Using cached %s\n", filename)
	}
	f, _ := os.Open(localPath)
	reader, _ := gzip.NewReader(f)
	return f, reader, nil
}

func benchGetFilteredTitleIDs(limit, offset int) (map[string]float64, map[string]int) {
	closer, reader, _ := benchStreamIMDB("https://datasets.imdbws.com/title.ratings.tsv.gz")
	defer closer.Close()
	defer reader.Close()

	scanner := bufio.NewScanner(reader)
	scanner.Scan() // header
	ratings, votes := make(map[string]float64), make(map[string]int)
	skipped := 0

	for scanner.Scan() {
		parts := strings.Split(scanner.Text(), "\t")
		if len(parts) != 3 {
			continue
		}
		if v, _ := strconv.Atoi(parts[2]); v > 5000 {
			if skipped < offset {
				skipped++
				continue
			}
			r, _ := strconv.ParseFloat(parts[1], 64)
			ratings[parts[0]], votes[parts[0]] = r, v
			if limit > 0 && len(ratings) >= limit {
				break
			}
		}
	}
	return ratings, votes
}

func benchGetTitleMetadata(ratings map[string]float64, votes map[string]int) (map[string]benchTitle, map[string][]string) {
	closer, reader, _ := benchStreamIMDB("https://datasets.imdbws.com/title.basics.tsv.gz")
	defer closer.Close()
	defer reader.Close()

	scanner := bufio.NewScanner(reader)
	scanner.Scan()
	titles := make(map[string]benchTitle)
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
			titles[parts[0]] = benchTitle{
				ID: parts[0], Name: parts[2], OriginalName: parts[3], Type: parts[1],
				StartYear: sy, EndYear: ey, Runtime: rt,
				Rating: rating, Votes: votes[parts[0]], IsAdult: parts[4] == "1",
			}
			if parts[8] != "\\N" {
				genres[parts[0]] = strings.Split(parts[8], ",")
			}
		}
	}
	return titles, genres
}

func benchGetEpisodes(titles map[string]benchTitle) (map[string]benchEpisode, map[string]string) {
	closer, reader, _ := benchStreamIMDB("https://datasets.imdbws.com/title.episode.tsv.gz")
	defer closer.Close()
	defer reader.Close()

	scanner := bufio.NewScanner(reader)
	scanner.Scan()
	episodes := make(map[string]benchEpisode)
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
		episodes[parts[0]] = benchEpisode{ID: parts[0], ParentID: parts[1], SeasonNumber: sn, EpNumber: en}
		if sn > 0 {
			seasons[fmt.Sprintf("%s_s%d", parts[1], sn)] = parts[1]
		}
	}
	return episodes, seasons
}

func benchGetPrincipals(titles map[string]benchTitle) ([]benchRole, map[string]bool, map[string]bool) {
	closer, reader, _ := benchStreamIMDB("https://datasets.imdbws.com/title.principals.tsv.gz")
	defer closer.Close()
	defer reader.Close()

	scanner := bufio.NewScanner(reader)
	scanner.Scan()
	var roles []benchRole
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
		roles = append(roles, benchRole{
			TitleID: parts[0], PersonID: parts[2], Category: parts[3],
			Job: job, Characters: chars, Ordering: ordering,
		})
	}
	return roles, peopleIDs, characters
}

func benchGetCrew(titles map[string]benchTitle) ([]benchCrewRel, map[string]bool) {
	closer, reader, _ := benchStreamIMDB("https://datasets.imdbws.com/title.crew.tsv.gz")
	defer closer.Close()
	defer reader.Close()

	scanner := bufio.NewScanner(reader)
	scanner.Scan()
	var crew []benchCrewRel
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
				crew = append(crew, benchCrewRel{parts[0], dID, "DIRECTED"})
			}
		}
		if parts[2] != "\\N" {
			for _, wID := range strings.Split(parts[2], ",") {
				peopleIDs[wID] = true
				crew = append(crew, benchCrewRel{parts[0], wID, "WROTE"})
			}
		}
	}
	return crew, peopleIDs
}

func benchGetAKAs(titles map[string]benchTitle) []benchAKA {
	closer, reader, _ := benchStreamIMDB("https://datasets.imdbws.com/title.akas.tsv.gz")
	defer closer.Close()
	defer reader.Close()

	scanner := bufio.NewScanner(reader)
	scanner.Scan()
	var akas []benchAKA

	for scanner.Scan() {
		parts := strings.Split(scanner.Text(), "\t")
		if len(parts) < 8 {
			continue
		}
		if _, ok := titles[parts[0]]; !ok {
			continue
		}
		akas = append(akas, benchAKA{titleID: parts[0], localizedTitle: parts[2]})
	}
	return akas
}

func benchGetPeopleMetadata(targetIDs map[string]bool) map[string]benchPerson {
	closer, reader, _ := benchStreamIMDB("https://datasets.imdbws.com/name.basics.tsv.gz")
	defer closer.Close()
	defer reader.Close()

	scanner := bufio.NewScanner(reader)
	scanner.Scan()
	people := make(map[string]benchPerson)

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
		people[parts[0]] = benchPerson{parts[0], parts[1], b, d, profs, kf}
	}
	return people
}

// ─── Count estimators ─────────────────────────────────────────────────────────

func benchEstimateObjectCount(titles map[string]benchTitle, episodes map[string]benchEpisode, seasons map[string]string, people map[string]benchPerson, titleGenres map[string][]string, characterNames map[string]bool) int {
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

func benchEstimateRelCount(roles []benchRole, crewRoles []benchCrewRel, titleGenres map[string][]string, people map[string]benchPerson, episodes map[string]benchEpisode, seasons map[string]string) int {
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
		charRels += len(r.Characters) * 2
	}
	return len(roles) + len(crewRoles) + genreRels + profRels + kfRels + charRels + len(episodes)*2 + len(seasons)
}

// ─── Object ingestion ─────────────────────────────────────────────────────────

func benchTitleNodeType(t string) string {
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

func benchTitleProps(t benchTitle) map[string]any {
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

func benchIngestObjects(ctx context.Context, client *sdkgraph.Client, titles map[string]benchTitle, episodes map[string]benchEpisode, seasons map[string]string, people map[string]benchPerson, titleGenres map[string][]string, characterNames map[string]bool, titleAKAs map[string][]string, batchSz, nWorkers int) map[string]string {
	var items []sdkgraph.CreateObjectRequest

	// Genres
	uniqueGenres := make(map[string]bool)
	for _, gs := range titleGenres {
		for _, g := range gs {
			uniqueGenres[g] = true
		}
	}
	for g := range uniqueGenres {
		k := "genre_" + g
		items = append(items, sdkgraph.CreateObjectRequest{Type: "Genre", Key: &k, Properties: map[string]any{"name": g}})
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
		items = append(items, sdkgraph.CreateObjectRequest{Type: "Profession", Key: &k, Properties: map[string]any{"name": strings.ReplaceAll(prof, "_", " ")}})
	}

	// Characters
	for char := range characterNames {
		k := "char_" + char
		items = append(items, sdkgraph.CreateObjectRequest{Type: "Character", Key: &k, Properties: map[string]any{"name": char}})
	}

	// Seasons
	for sKey, parentID := range seasons {
		k := sKey
		parts := strings.Split(sKey, "_s")
		sn, _ := strconv.Atoi(parts[1])
		items = append(items, sdkgraph.CreateObjectRequest{Type: "Season", Key: &k, Properties: map[string]any{"season_number": sn, "parent_series_id": parentID}})
	}

	// Titles
	for _, t := range titles {
		k := t.ID
		nodeType := benchTitleNodeType(t.Type)
		props := benchTitleProps(t)
		if akaList, ok := titleAKAs[t.ID]; ok && len(akaList) > 0 {
			props["aka_titles"] = akaList
		}
		items = append(items, sdkgraph.CreateObjectRequest{Type: nodeType, Key: &k, Properties: props})
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
		items = append(items, sdkgraph.CreateObjectRequest{Type: "Person", Key: &k, Properties: props})
	}

	fmt.Printf("  Uploading %d objects in batches of %d with %d workers ...\n", len(items), batchSz, nWorkers)
	return benchBulkUploadObjects(ctx, client, items, batchSz, nWorkers)
}

func benchMapCategory(cat string) string {
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
		return ""
	}
}

// ─── Relationship ingestion ───────────────────────────────────────────────────

func benchIngestRelationships(ctx context.Context, client *sdkgraph.Client, roles []benchRole, crewRoles []benchCrewRel, titleGenres map[string][]string, titles map[string]benchTitle, episodes map[string]benchEpisode, seasons map[string]string, people map[string]benchPerson, idMap map[string]string, batchSz, nWorkers int) (int64, int64) {
	var items []sdkgraph.CreateRelationshipRequest

	for _, r := range roles {
		src, ok1 := idMap[r.PersonID]
		dst, ok2 := idMap[r.TitleID]
		if !ok1 || !ok2 {
			continue
		}
		relType := benchMapCategory(r.Category)
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
		items = append(items, sdkgraph.CreateRelationshipRequest{Type: relType, SrcID: src, DstID: dst, Properties: props})
		for _, char := range r.Characters {
			if charDst, ok3 := idMap["char_"+char]; ok3 {
				items = append(items, sdkgraph.CreateRelationshipRequest{Type: "PLAYED", SrcID: src, DstID: charDst, Properties: map[string]any{}})
				items = append(items, sdkgraph.CreateRelationshipRequest{Type: "APPEARS_IN", SrcID: charDst, DstID: dst, Properties: map[string]any{}})
			}
		}
	}

	for _, cr := range crewRoles {
		if src, ok1 := idMap[cr.PersonID]; ok1 {
			if dst, ok2 := idMap[cr.TitleID]; ok2 {
				items = append(items, sdkgraph.CreateRelationshipRequest{Type: cr.Role, SrcID: src, DstID: dst, Properties: map[string]any{}})
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
				items = append(items, sdkgraph.CreateRelationshipRequest{Type: "IN_GENRE", SrcID: src, DstID: dst, Properties: map[string]any{}})
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
				items = append(items, sdkgraph.CreateRelationshipRequest{Type: "HAS_PROFESSION", SrcID: src, DstID: dst, Properties: map[string]any{}})
			}
		}
		for _, kfID := range p.KnownFor {
			if dst, ok2 := idMap[kfID]; ok2 {
				items = append(items, sdkgraph.CreateRelationshipRequest{Type: "KNOWN_FOR", SrcID: src, DstID: dst, Properties: map[string]any{}})
			}
		}
	}

	for epID, ep := range episodes {
		epSrc, ok1 := idMap[epID]
		seriesDst, ok2 := idMap[ep.ParentID]
		if ok1 && ok2 {
			items = append(items, sdkgraph.CreateRelationshipRequest{Type: "EPISODE_OF", SrcID: epSrc, DstID: seriesDst, Properties: map[string]any{"season_number": ep.SeasonNumber, "episode_number": ep.EpNumber}})
			if ep.SeasonNumber > 0 {
				if seasonDst, ok3 := idMap[fmt.Sprintf("%s_s%d", ep.ParentID, ep.SeasonNumber)]; ok3 {
					items = append(items, sdkgraph.CreateRelationshipRequest{Type: "IN_SEASON", SrcID: epSrc, DstID: seasonDst, Properties: map[string]any{}})
				}
			}
		}
	}
	for sKey, seriesID := range seasons {
		if sSrc, ok1 := idMap[sKey]; ok1 {
			if seriesDst, ok2 := idMap[seriesID]; ok2 {
				items = append(items, sdkgraph.CreateRelationshipRequest{Type: "SEASON_OF", SrcID: sSrc, DstID: seriesDst, Properties: map[string]any{}})
			}
		}
	}

	fmt.Printf("  Uploading %d relationships in batches of %d with %d workers ...\n", len(items), batchSz, nWorkers)
	return benchBulkUploadRelationships(ctx, client, items, batchSz, nWorkers)
}

// ─── Bulk upload helpers ──────────────────────────────────────────────────────

func benchBulkUploadObjects(ctx context.Context, client *sdkgraph.Client, items []sdkgraph.CreateObjectRequest, batchSz, nWorkers int) map[string]string {
	type batchResult struct {
		batch []sdkgraph.CreateObjectRequest
		res   *sdkgraph.BulkCreateObjectsResponse
	}

	batches := make(chan []sdkgraph.CreateObjectRequest, nWorkers*2)
	results := make(chan batchResult, nWorkers*2)

	var wg sync.WaitGroup
	for i := 0; i < nWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for batch := range batches {
				res, err := client.BulkCreateObjects(ctx, &sdkgraph.BulkCreateObjectsRequest{Items: batch})
				if err != nil {
					time.Sleep(500 * time.Millisecond)
					fmt.Printf("Error: %v\n", err)
					res, _ = client.BulkCreateObjects(ctx, &sdkgraph.BulkCreateObjectsRequest{Items: batch})
				}
				results <- batchResult{batch, res}
			}
		}()
	}
	go func() { wg.Wait(); close(results) }()

	go func() {
		for i := 0; i < len(items); i += batchSz {
			end := i + batchSz
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
					failed.Add(1)
				}
			}
			mu.Unlock()
		}
	}

	if len(missingKeys) > 0 {
		fmt.Printf("  Resolving %d conflicting keys ...\n", len(missingKeys))
		sem := make(chan struct{}, nWorkers)
		var resolveWg sync.WaitGroup
		for _, mk := range missingKeys {
			sem <- struct{}{}
			resolveWg.Add(1)
			go func(objType, key string) {
				defer resolveWg.Done()
				defer func() { <-sem }()
				resp, err := client.ListObjects(ctx, &sdkgraph.ListObjectsOptions{Type: objType, Key: key, Limit: 1})
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

	fmt.Printf("  Objects: %d new, %d conflict-resolved, %d errors, %d mapped\n",
		uploaded.Load(), conflicts.Load(), failed.Load(), len(idMap))
	return idMap
}

func benchBulkUploadRelationships(ctx context.Context, client *sdkgraph.Client, items []sdkgraph.CreateRelationshipRequest, batchSz, nWorkers int) (int64, int64) {
	batches := make(chan []sdkgraph.CreateRelationshipRequest, nWorkers*2)

	var wg sync.WaitGroup
	var succeeded, failed atomic.Int64

	for i := 0; i < nWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for batch := range batches {
				res, err := client.BulkCreateRelationships(ctx, &sdkgraph.BulkCreateRelationshipsRequest{Items: batch})
				if err != nil {
					time.Sleep(500 * time.Millisecond)
					res, err = client.BulkCreateRelationships(ctx, &sdkgraph.BulkCreateRelationshipsRequest{Items: batch})
				}
				if res != nil {
					succeeded.Add(int64(res.Success))
					failed.Add(int64(res.Failed))
				} else if err != nil {
					fmt.Printf("Rel batch failed: %v\n", err)
					failed.Add(int64(len(batch)))
				}
			}
		}()
	}

	go func() {
		for i := 0; i < len(items); i += batchSz {
			end := i + batchSz
			if end > len(items) {
				end = len(items)
			}
			batches <- items[i:end]
		}
		close(batches)
	}()

	wg.Wait()
	fmt.Printf("  Relationships: %d succeeded, %d failed\n", succeeded.Load(), failed.Load())
	return succeeded.Load(), failed.Load()
}

// ─── JSONL log ────────────────────────────────────────────────────────────────

func benchAppendLog(logFile string, report *benchReport) {
	if err := os.MkdirAll(filepath.Dir(logFile), 0755); err != nil {
		fmt.Printf("%sWARN: cannot create log dir: %v%s\n", diagYellow, err, diagReset)
		return
	}
	f, err := os.OpenFile(logFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		fmt.Printf("%sWARN: cannot open log file %s: %v%s\n", diagYellow, logFile, err, diagReset)
		return
	}
	defer f.Close()

	type stepJSON struct {
		Name       string  `json:"name"`
		DurationMs float64 `json:"duration_ms"`
		Detail     string  `json:"detail,omitempty"`
	}
	type runJSON struct {
		BenchVersion  string     `json:"bench_version"`
		ServerVersion string     `json:"server_version"`
		ServerURL     string     `json:"server_url"`
		ProjectID     string     `json:"project_id"`
		SeedLimit     int        `json:"seed_limit"`
		GitCommit     string     `json:"git_commit"`
		GoVersion     string     `json:"go_version"`
		GOOS          string     `json:"goos"`
		GOARCH        string     `json:"goarch"`
		Hostname      string     `json:"hostname"`
		StartedAt     string     `json:"started_at"`
		Steps         []stepJSON `json:"steps"`
		TotalMs       float64    `json:"total_ms"`
		Objects       int        `json:"objects_count"`
		Relations     int        `json:"relations_count"`
		RelErrors     int64      `json:"rel_errors"`
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
		BenchVersion:  report.BenchVersion,
		ServerVersion: report.ServerVersion,
		ServerURL:     report.ServerURL,
		ProjectID:     report.ProjectID,
		SeedLimit:     report.SeedLimit,
		GitCommit:     report.GitCommit,
		GoVersion:     report.GoVersion,
		GOOS:          report.GOOS,
		GOARCH:        report.GOARCH,
		Hostname:      report.Hostname,
		StartedAt:     report.StartedAt.Format(time.RFC3339),
		Steps:         steps,
		TotalMs:       float64(report.totalDuration().Milliseconds()),
		Objects:       report.Objects,
		Relations:     report.Relations,
		RelErrors:     report.RelErrors,
	}

	data, err := json.Marshal(entry)
	if err != nil {
		fmt.Printf("%sWARN: cannot marshal log entry: %v%s\n", diagYellow, err, diagReset)
		return
	}
	f.Write(data)
	f.Write([]byte("\n"))
	fmt.Printf("Run appended to log: %s\n", logFile)
}
