// Command e2e-suite is the unified Emergent end-to-end test suite runner.
//
// Usage:
//
//	e2e-suite [flags] <suite>
//
// Available suites: imdb, huma, niezatapialni, all
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/emergent-company/emergent/tools/e2e-suite/suite"
	"github.com/emergent-company/emergent/tools/e2e-suite/suites/huma"
	imdbsuite "github.com/emergent-company/emergent/tools/e2e-suite/suites/imdb"
	"github.com/emergent-company/emergent/tools/e2e-suite/suites/niezatapialni"
)

func main() {
	var (
		serverURL   = flag.String("server-url", "", "Server URL [$EMERGENT_SERVER_URL]")
		apiKey      = flag.String("api-key", "", "API key [$EMERGENT_API_KEY]")
		orgID       = flag.String("org-id", "", "Org ID [$EMERGENT_ORG_ID]")
		projectID   = flag.String("project-id", "", "Project ID [$EMERGENT_PROJECT_ID]")
		concurrency = flag.Int("concurrency", 0, "Worker count (default 4)")
		timeout     = flag.Duration("timeout", 0, "Suite timeout (default 30m)")
		dryRun      = flag.Bool("dry-run", false, "Show plan without executing")
		output      = flag.String("output", "", "Output format: text|json (default text)")
		envFile     = flag.String("env-file", ".env", ".env file path")
	)
	flag.Usage = usage
	flag.Parse()

	if flag.NArg() < 1 {
		usage()
		os.Exit(1)
	}
	suiteName := flag.Arg(0)

	// Load config from env (and .env file), then apply flag overrides
	cfg, err := suite.Load(*envFile)
	if err != nil {
		fatalf("loading config: %v", err)
	}

	if *serverURL != "" {
		cfg.ServerURL = *serverURL
	}
	if *apiKey != "" {
		cfg.APIKey = *apiKey
	}
	if *orgID != "" {
		cfg.OrgID = *orgID
	}
	if *projectID != "" {
		cfg.ProjectID = *projectID
	}
	if *concurrency > 0 {
		cfg.Concurrency = *concurrency
	}
	if *timeout > 0 {
		cfg.Timeout = *timeout
	}
	if *dryRun {
		cfg.DryRun = true
	}
	if *output != "" {
		cfg.OutputFormat = *output
	}

	// Validate config (skip for dry-run)
	if !cfg.DryRun {
		if err := cfg.Validate(); err != nil {
			fatalf("config error: %v", err)
		}
	}

	// Build list of suites to run
	suites, err := resolveSuites(suiteName)
	if err != nil {
		fatalf("%v", err)
	}

	// Print config summary
	fmt.Printf("Emergent E2E Suite\n")
	fmt.Printf("  Server:      %s\n", cfg.ServerURL)
	fmt.Printf("  Project:     %s\n", cfg.ProjectID)
	fmt.Printf("  Concurrency: %d\n", cfg.Concurrency)
	fmt.Printf("  Timeout:     %s\n", cfg.Timeout)
	fmt.Printf("  Dry-run:     %v\n", cfg.DryRun)
	fmt.Printf("  Suites:      %s\n\n", suiteName)

	ctx, cancel := context.WithTimeout(context.Background(), cfg.Timeout+5*time.Minute)
	defer cancel()

	runner := &suite.Runner{
		Suites: suites,
		Cfg:    cfg,
	}

	results := runner.Run(ctx)
	suite.PrintSummary(results, cfg.OutputFormat)

	// Exit non-zero if any suite had failures
	for _, r := range results {
		if r != nil && (r.Failed > 0 || r.Timeout > 0) {
			os.Exit(1)
		}
	}
}

func resolveSuites(name string) ([]suite.Suite, error) {
	all := []suite.Suite{
		&imdbsuite.Suite{},
		&huma.Suite{},
		&niezatapialni.Suite{},
	}

	switch name {
	case "all":
		return all, nil
	case "imdb":
		return []suite.Suite{&imdbsuite.Suite{}}, nil
	case "huma":
		return []suite.Suite{&huma.Suite{}}, nil
	case "niezatapialni":
		return []suite.Suite{&niezatapialni.Suite{}}, nil
	default:
		return nil, fmt.Errorf("unknown suite %q — valid options: imdb, huma, niezatapialni, all", name)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, `Usage: e2e-suite [flags] <suite>

Suites:
  imdb            Seed IMDB graph data and run agent queries
  huma            Upload HUMA docs from cache dir and verify extraction
  niezatapialni   Upload Niezatapialni MP3s and verify transcription
  all             Run all suites sequentially

Flags:
`)
	flag.PrintDefaults()
	fmt.Fprintf(os.Stderr, `
Environment variables (override with flags):
  EMERGENT_SERVER_URL   Server URL (default: http://mcj-emergent:3002)
  EMERGENT_API_KEY      API key
  EMERGENT_ORG_ID       Org ID
  EMERGENT_PROJECT_ID   Project ID

Suite-specific env vars:
  IMDB_AGENT_DEF_ID         Agent definition ID for IMDB queries (required)
  IMDB_MIN_VOTES            Min vote threshold (default: 20000)
  IMDB_DATASET_URL          Base URL for IMDB datasets
  HUMA_CACHE_DIR            Dir containing cached HUMA docs (default: /root/data)
  NIEZATAPIALNI_MP3_DIR     Dir containing MP3 files (default: tools/niezatapialni-scraper/all_mp3s)
`)
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "e2e-suite: "+format+"\n", args...)
	os.Exit(1)
}
