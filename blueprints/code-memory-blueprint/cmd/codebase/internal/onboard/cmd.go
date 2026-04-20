// Package onboardcmd provides `codebase onboard` — a non-interactive command
// that populates the Memory knowledge graph for a new project and prints a
// structured report. Designed to be invoked by an AI agent, not a human.
package onboardcmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	sdkgraph "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/graph"
	"github.com/emergent-company/emergent.memory/blueprints/code-memory-blueprint/cmd/codebase/internal/config"
	"github.com/spf13/cobra"
)

func NewCmd(flagProjectID *string, flagBranch *string) *cobra.Command {
	var (
		flagRepo   string
		flagDryRun bool
	)
	cwd, _ := os.Getwd()

	cmd := &cobra.Command{
		Use:   "onboard",
		Short: "Populate the graph for a new project (AI-oriented)",
		Long: `Populate the Memory knowledge graph for a new codebase project.

Designed to be called by an AI agent. Runs the full population sequence and
prints a structured report of what was created or already existed.

Sequence:
  1. sync routes      — create APIEndpoint objects from route files
  2. sync middleware  — wire Middleware → APIEndpoint relationships
  3. sync files       — create SourceFile objects
  4. seed exposes     — wire Service → exposes → APIEndpoint
  5. constitution     — create constitution-v1 with starter rules (if absent)
  6. skills install   — install the codebase skill into .opencode/skills/

Use --dry-run to preview without writing.
`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runOnboard(flagProjectID, flagBranch, flagRepo, flagDryRun)
		},
	}

	cmd.Flags().StringVar(&flagRepo, "repo", cwd, "Path to repository root")
	cmd.Flags().BoolVar(&flagDryRun, "dry-run", false, "Preview without writing")
	return cmd
}

type stepResult struct {
	Name    string
	Status  string // "ok", "skipped", "error"
	Detail  string
	Created int
}

func runOnboard(flagProjectID *string, flagBranch *string, repo string, dryRun bool) error {
	c, err := config.New(*flagProjectID, *flagBranch)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	exe, _ := os.Executable()

	// ── Snapshot before ───────────────────────────────────────────────────────
	before := graphSnapshot(ctx, c.Graph)

	var results []stepResult

	// ── Step 1: sync routes ───────────────────────────────────────────────────
	args := []string{"sync", "routes", "--repo", repo}
	if dryRun {
		args = append(args, "--dry-run")
	}
	r := runStep("sync routes", exe, args...)
	results = append(results, r)

	// ── Step 2: sync middleware ───────────────────────────────────────────────
	args = []string{"sync", "middleware", "--repo", repo}
	if dryRun {
		args = append(args, "--dry-run")
	}
	r = runStep("sync middleware", exe, args...)
	results = append(results, r)

	// ── Step 3: sync files ────────────────────────────────────────────────────
	args = []string{"sync", "files", "--repo", repo}
	if !dryRun {
		args = append(args, "--sync")
	}
	r = runStep("sync files", exe, args...)
	results = append(results, r)

	// ── Step 4: seed exposes ──────────────────────────────────────────────────
	args = []string{"seed", "exposes"}
	if dryRun {
		args = append(args, "--dry-run")
	}
	r = runStep("seed exposes", exe, args...)
	results = append(results, r)

	// ── Step 5: constitution ──────────────────────────────────────────────────
	constCount := countObjects(ctx, c.Graph, "Constitution")
	if constCount > 0 {
		results = append(results, stepResult{
			Name:   "constitution",
			Status: "skipped",
			Detail: "constitution-v1 already exists",
		})
	} else if !dryRun {
		if err := createConstitution(ctx, c.Graph); err != nil {
			results = append(results, stepResult{Name: "constitution", Status: "error", Detail: err.Error()})
		} else {
			results = append(results, stepResult{
				Name:    "constitution",
				Status:  "ok",
				Detail:  fmt.Sprintf("created constitution-v1 with %d starter rules", len(starterRules)),
				Created: 1 + len(starterRules),
			})
		}
	} else {
		results = append(results, stepResult{
			Name:   "constitution",
			Status: "ok",
			Detail: fmt.Sprintf("would create constitution-v1 with %d starter rules", len(starterRules)),
		})
	}

	// ── Step 6: install skill ─────────────────────────────────────────────────
	args = []string{"skills", "install"}
	r = runStep("skills install", exe, args...)
	results = append(results, r)

	// ── Snapshot after ────────────────────────────────────────────────────────
	after := graphSnapshot(ctx, c.Graph)

	// ── Report ────────────────────────────────────────────────────────────────
	printReport(results, before, after, dryRun)
	return nil
}

func runStep(name, exe string, args ...string) stepResult {
	cmd := exec.Command(exe, args...)
	cmd.Stdout = os.Stderr // progress to stderr, report to stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return stepResult{Name: name, Status: "error", Detail: err.Error()}
	}
	return stepResult{Name: name, Status: "ok"}
}

type snapshot struct {
	Endpoints int
	Files     int
	Services  int
	Domains   int
}

func graphSnapshot(ctx context.Context, gc *sdkgraph.Client) snapshot {
	return snapshot{
		Endpoints: countObjects(ctx, gc, "APIEndpoint"),
		Files:     countObjects(ctx, gc, "SourceFile"),
		Services:  countObjects(ctx, gc, "Service"),
		Domains:   countObjects(ctx, gc, "Domain"),
	}
}

func countObjects(ctx context.Context, gc *sdkgraph.Client, objType string) int {
	res, err := gc.ListObjects(ctx, &sdkgraph.ListObjectsOptions{Type: objType, Limit: 500})
	if err != nil {
		return 0
	}
	return len(res.Items)
}

func globFiles(repo, patterns string) []string {
	var files []string
	for _, p := range strings.Split(patterns, ",") {
		matches, _ := filepath.Glob(filepath.Join(repo, strings.TrimSpace(p)))
		files = append(files, matches...)
	}
	return files
}

func printReport(results []stepResult, before, after snapshot, dryRun bool) {
	tag := ""
	if dryRun {
		tag = " [DRY RUN]"
	}
	fmt.Printf("\n╔═ codebase onboard%s ═╗\n\n", tag)

	fmt.Println("Steps:")
	for _, r := range results {
		icon := "✓"
		if r.Status == "error" {
			icon = "✗"
		} else if r.Status == "skipped" {
			icon = "–"
		}
		detail := ""
		if r.Detail != "" {
			detail = "  (" + r.Detail + ")"
		}
		fmt.Printf("  %s  %-20s%s\n", icon, r.Name, detail)
	}

	fmt.Printf("\nGraph delta:\n")
	fmt.Printf("  Domains      : %d → %d\n", before.Domains, after.Domains)
	fmt.Printf("  Services     : %d → %d\n", before.Services, after.Services)
	fmt.Printf("  APIEndpoints : %d → %d\n", before.Endpoints, after.Endpoints)
	fmt.Printf("  SourceFiles  : %d → %d\n", before.Files, after.Files)

	fmt.Printf("\nNext steps for the AI agent:\n")
	fmt.Printf("  codebase check api              # audit endpoint quality\n")
	fmt.Printf("  codebase check coverage         # find untested domains\n")
	fmt.Printf("  codebase analyze tree           # explore domain structure\n")
	fmt.Printf("  codebase constitution rules     # view coding rules\n")
	fmt.Printf("  codebase constitution check     # run rule checks\n")
	fmt.Println()
}
