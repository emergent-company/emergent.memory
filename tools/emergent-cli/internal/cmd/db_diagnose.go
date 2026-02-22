package cmd

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// ANSI color codes (inline — cmd package has no shared color constants)
const (
	diagReset  = "\033[0m"
	diagBold   = "\033[1m"
	diagGreen  = "\033[0;32m"
	diagRed    = "\033[0;31m"
	diagYellow = "\033[0;33m"
	diagCyan   = "\033[0;36m"
)

// ── flags ─────────────────────────────────────────────────────────────────────

var dbDiagnoseFlags struct {
	verbose    bool
	slowMS     int
	installDir string
	dsn        string
}

// ── command definition ────────────────────────────────────────────────────────

var dbDiagnoseCmd = &cobra.Command{
	Use:   "diagnose",
	Short: "Analyse query performance via EXPLAIN ANALYZE",
	Long: `Run EXPLAIN (ANALYZE, BUFFERS) on the most critical query patterns in
Emergent and report slow plans, missing index usage, bad row estimates,
sequential scans on large tables, table bloat, and unused indexes.

Connection priority (first found wins):
  1. --dsn flag
  2. EMERGENT_DATABASE_URL / DATABASE_URL environment variable
  3. Standalone install ~/.emergent/config/.env.local (auto-detected)

Examples:
  emergent db diagnose
  emergent db diagnose --verbose
  emergent db diagnose --slow 100
  emergent db diagnose --dsn "postgres://user:pass@localhost:5432/emergent?sslmode=disable"`,
	RunE: runDbDiagnose,
}

func init() {
	dbDiagnoseCmd.Flags().BoolVarP(&dbDiagnoseFlags.verbose, "verbose", "v", false, "print full EXPLAIN output for every query")
	dbDiagnoseCmd.Flags().IntVar(&dbDiagnoseFlags.slowMS, "slow", 200, "flag queries that take longer than this many milliseconds")
	dbDiagnoseCmd.Flags().StringVar(&dbDiagnoseFlags.installDir, "dir", "", "standalone install directory (default: ~/.emergent)")
	dbDiagnoseCmd.Flags().StringVar(&dbDiagnoseFlags.dsn, "dsn", "", "PostgreSQL connection string (overrides auto-detection)")
}

// ── result type ───────────────────────────────────────────────────────────────

type diagResult struct {
	name     string
	status   string // "pass", "warn", "fail", "skip"
	message  string
	planText string
}

// ── entry point ───────────────────────────────────────────────────────────────

func runDbDiagnose(_ *cobra.Command, _ []string) error {
	fmt.Printf("\n%s%sEmergent Database Diagnostics%s\n", diagBold, diagCyan, diagReset)
	fmt.Println("═══════════════════════════════════════════════════════")
	fmt.Println()

	dsn, err := resolveDiagDSN()
	if err != nil || dsn == "" {
		fmt.Printf("%s✗ Could not find a PostgreSQL connection string%s\n\n", diagRed, diagReset)
		fmt.Println("Set one via --dsn or the EMERGENT_DATABASE_URL environment variable.")
		return fmt.Errorf("no connection string")
	}

	// Quick ping to fail fast with a clear error
	fmt.Print("Connecting to PostgreSQL... ")
	if out, err := psql(dsn, "SELECT 1"); err != nil {
		fmt.Println("FAILED")
		fmt.Printf("%s%s%s\n", diagRed, out, diagReset)
		return fmt.Errorf("connection failed: %w", err)
	}
	fmt.Printf("OK  %s(%s)%s\n\n", diagCyan, maskDiagDSN(dsn), diagReset)

	// ── Run checks ──────────────────────────────────────────────────────────────
	var results []diagResult

	results = append(results, checkDiagVersion(dsn))
	results = append(results, checkDiagSharedBuffers(dsn))
	results = append(results, checkDiagWalSize(dsn))
	results = append(results, checkDiagTableStats(dsn))
	results = append(results, checkDiagDeadTuples(dsn))
	results = append(results, checkDiagUnusedIndexes(dsn))
	results = append(results, diagExplain(dsn, "graph_objects: HEAD lookup by (project_id,type,key)",
		`SELECT id, canonical_id, type, key, properties
		 FROM kb.graph_objects
		 WHERE project_id = '00000000-0000-0000-0000-000000000001'
		   AND type = 'Person'
		   AND key = '__explain_probe__'
		   AND supersedes_id IS NULL
		   AND deleted_at IS NULL
		   AND branch_id IS NULL
		 LIMIT 1`))
	results = append(results, diagExplain(dsn, "graph_objects: list HEAD objects for project",
		`SELECT id, type, key
		 FROM kb.graph_objects
		 WHERE project_id = '00000000-0000-0000-0000-000000000001'
		   AND supersedes_id IS NULL
		   AND deleted_at IS NULL
		   AND branch_id IS NULL
		 ORDER BY created_at DESC
		 LIMIT 50`))
	results = append(results, diagExplain(dsn, "graph_objects: full-text search (FTS)",
		`SELECT id, type, key, ts_rank(fts, query) AS rank
		 FROM kb.graph_objects, to_tsquery('simple', 'test') query
		 WHERE project_id = '00000000-0000-0000-0000-000000000001'
		   AND fts @@ query
		   AND supersedes_id IS NULL
		   AND deleted_at IS NULL
		 ORDER BY rank DESC
		 LIMIT 20`))
	results = append(results, diagExplain(dsn, "graph_relationships: lookup by src_id",
		`SELECT id, dst_id, type, properties
		 FROM kb.graph_relationships
		 WHERE src_id = '00000000-0000-0000-0000-000000000001'
		   AND supersedes_id IS NULL
		   AND deleted_at IS NULL`))
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
	results = append(results, diagExplain(dsn, "chunks: list by document_id",
		`SELECT id, chunk_index, text
		 FROM kb.chunks
		 WHERE document_id = '00000000-0000-0000-0000-000000000001'
		 ORDER BY chunk_index`))
	results = append(results, diagExplain(dsn, "agent_runs: list by agent_id + status",
		`SELECT id, status, started_at
		 FROM kb.agent_runs
		 WHERE agent_id = '00000000-0000-0000-0000-000000000001'
		   AND status = 'running'
		 ORDER BY started_at DESC
		 LIMIT 20`))

	// ── Print summary ────────────────────────────────────────────────────────────
	printDiagSummary(results)

	for _, r := range results {
		if r.status == "fail" {
			return fmt.Errorf("one or more checks failed — see report above")
		}
	}
	return nil
}

// ── connection helpers ────────────────────────────────────────────────────────

func resolveDiagDSN() (string, error) {
	if dbDiagnoseFlags.dsn != "" {
		return dbDiagnoseFlags.dsn, nil
	}
	for _, env := range []string{"EMERGENT_DATABASE_URL", "DATABASE_URL"} {
		if v := os.Getenv(env); v != "" {
			return v, nil
		}
	}
	return readDiagDSNFromEnvLocal(), nil
}

func readDiagDSNFromEnvLocal() string {
	dir := dbDiagnoseFlags.installDir
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".emergent")
	}
	data, err := os.ReadFile(filepath.Join(dir, "config", ".env.local"))
	if err != nil {
		return ""
	}
	vals := map[string]string{
		"POSTGRES_USER":     "emergent",
		"POSTGRES_PASSWORD": "",
		"POSTGRES_DB":       "emergent",
		"POSTGRES_PORT":     "5432",
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#") || !strings.Contains(line, "=") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if _, ok := vals[parts[0]]; ok && len(parts) == 2 {
			vals[parts[0]] = parts[1]
		}
	}
	if vals["POSTGRES_PASSWORD"] == "" {
		return ""
	}
	return fmt.Sprintf("postgres://%s:%s@localhost:%s/%s?sslmode=disable",
		vals["POSTGRES_USER"], vals["POSTGRES_PASSWORD"],
		vals["POSTGRES_PORT"], vals["POSTGRES_DB"])
}

func maskDiagDSN(dsn string) string {
	if idx := strings.Index(dsn, "://"); idx >= 0 {
		rest := dsn[idx+3:]
		if at := strings.LastIndex(rest, "@"); at >= 0 {
			creds := rest[:at]
			if colon := strings.Index(creds, ":"); colon >= 0 {
				return dsn[:idx+3] + creds[:colon+1] + "***" + rest[at:]
			}
		}
	}
	return dsn
}

// psql shells out to the `psql` binary and returns combined stdout+stderr.
// This avoids adding a postgres driver dependency to the CLI module.
func psql(dsn, query string) (string, error) {
	cmd := exec.Command("psql", dsn, "--no-psqlrc", "--tuples-only", "--no-align", "-c", query)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	return strings.TrimSpace(buf.String()), err
}

// psqlRaw runs a query and returns every output line as a slice.
func psqlRaw(dsn, query string) ([]string, error) {
	out, err := psql(dsn, query)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", out, err)
	}
	var lines []string
	for _, l := range strings.Split(out, "\n") {
		l = strings.TrimRight(l, " ")
		if l != "" {
			lines = append(lines, l)
		}
	}
	return lines, nil
}

// ── system checks ─────────────────────────────────────────────────────────────

func checkDiagVersion(dsn string) diagResult {
	fmt.Print("Checking PostgreSQL version...                          ")
	out, err := psql(dsn, "SELECT version()")
	if err != nil {
		fmt.Println("FAILED")
		return diagResult{name: "PostgreSQL version", status: "fail", message: out}
	}
	major := 0
	fmt.Sscanf(out, "PostgreSQL %d", &major)
	if major < 17 {
		fmt.Println("WARN")
		return diagResult{name: "PostgreSQL version", status: "warn",
			message: fmt.Sprintf("%s\n  → Upgrade to PostgreSQL 17 for best performance (run: emergent upgrade)", strings.SplitN(out, " on ", 2)[0])}
	}
	fmt.Println("OK")
	return diagResult{name: "PostgreSQL version", status: "pass", message: strings.SplitN(out, " on ", 2)[0]}
}

func checkDiagSharedBuffers(dsn string) diagResult {
	fmt.Print("Checking shared_buffers...                              ")
	out, err := psql(dsn, "SHOW shared_buffers")
	if err != nil {
		fmt.Println("FAILED")
		return diagResult{name: "shared_buffers", status: "fail", message: out}
	}
	if out == "128MB" {
		fmt.Println("WARN")
		return diagResult{name: "shared_buffers", status: "warn",
			message: fmt.Sprintf("%s — default value, tune to 25%% of RAM (emergent upgrade applies this automatically)", out)}
	}
	fmt.Println("OK")
	return diagResult{name: "shared_buffers", status: "pass", message: out}
}

func checkDiagWalSize(dsn string) diagResult {
	fmt.Print("Checking max_wal_size...                                ")
	out, err := psql(dsn, "SHOW max_wal_size")
	if err != nil {
		fmt.Println("FAILED")
		return diagResult{name: "max_wal_size", status: "fail", message: out}
	}
	// Warn if still at default 1GB
	if out == "1GB" {
		fmt.Println("WARN")
		return diagResult{name: "max_wal_size", status: "warn",
			message: fmt.Sprintf("%s — default value causes frequent checkpoints during bulk writes (tune to 4-16 GB)", out)}
	}
	fmt.Println("OK")
	return diagResult{name: "max_wal_size", status: "pass", message: out}
}

func checkDiagTableStats(dsn string) diagResult {
	fmt.Print("Checking table sizes and planner statistics...          ")
	lines, err := psqlRaw(dsn, `
		SELECT
			c.relname||'|'||pg_size_pretty(pg_total_relation_size(c.oid))||'|'||
			COALESCE(s.n_live_tup,0)||'|'||
			CASE WHEN c.reltuples > 100 AND s.n_live_tup > 0
				THEN round(abs(c.reltuples - s.n_live_tup)/GREATEST(c.reltuples,1)*100)
				ELSE 0
			END
		FROM pg_class c
		JOIN pg_namespace n ON n.oid = c.relnamespace
		LEFT JOIN pg_stat_user_tables s ON s.relname = c.relname AND s.schemaname = n.nspname
		WHERE n.nspname = 'kb' AND c.relkind = 'r'
		ORDER BY pg_total_relation_size(c.oid) DESC
		LIMIT 15`)
	if err != nil {
		fmt.Println("FAILED")
		return diagResult{name: "Table stats", status: "fail", message: err.Error()}
	}

	var stale, details []string
	for _, line := range lines {
		parts := strings.Split(line, "|")
		if len(parts) < 4 {
			continue
		}
		table, size, live, errPct := parts[0], parts[1], parts[2], parts[3]
		details = append(details, fmt.Sprintf("  %-42s %10s  %s rows", table, size, live))
		if pct, _ := strconv.Atoi(strings.TrimSpace(errPct)); pct > 20 {
			liveN, _ := strconv.Atoi(strings.TrimSpace(live))
			if liveN > 500 {
				stale = append(stale, fmt.Sprintf("%s (estimate off by %d%%)", table, pct))
			}
		}
	}

	if len(stale) > 0 {
		fmt.Println("WARN")
		return diagResult{
			name:     "Table stats / row estimates",
			status:   "warn",
			message:  fmt.Sprintf("Stale statistics (run ANALYZE): %s", strings.Join(stale, ", ")),
			planText: strings.Join(details, "\n"),
		}
	}
	fmt.Println("OK")
	return diagResult{
		name:     "Table stats / row estimates",
		status:   "pass",
		message:  "All row estimates within 20% of actual",
		planText: strings.Join(details, "\n"),
	}
}

func checkDiagDeadTuples(dsn string) diagResult {
	fmt.Print("Checking dead tuples (table bloat)...                  ")
	lines, err := psqlRaw(dsn, `
		SELECT relname||'|'||n_dead_tup||'|'||
			CASE WHEN n_live_tup > 0 THEN round(n_dead_tup::numeric/n_live_tup*100,1) ELSE 0 END
		FROM pg_stat_user_tables
		WHERE schemaname = 'kb' AND n_dead_tup > 1000
		ORDER BY n_dead_tup DESC
		LIMIT 10`)
	if err != nil {
		fmt.Println("FAILED")
		return diagResult{name: "Dead tuples", status: "fail", message: err.Error()}
	}

	var bloated []string
	for _, line := range lines {
		parts := strings.Split(line, "|")
		if len(parts) < 3 {
			continue
		}
		pct, _ := strconv.ParseFloat(strings.TrimSpace(parts[2]), 64)
		if pct > 10 {
			bloated = append(bloated, fmt.Sprintf("%s (%.0f%% dead)", parts[0], pct))
		}
	}
	if len(bloated) > 0 {
		fmt.Println("WARN")
		return diagResult{name: "Dead tuples / bloat", status: "warn",
			message: fmt.Sprintf("High bloat — run VACUUM ANALYZE: %s", strings.Join(bloated, "; "))}
	}
	fmt.Println("OK")
	return diagResult{name: "Dead tuples / bloat", status: "pass", message: "No significant bloat detected"}
}

func checkDiagUnusedIndexes(dsn string) diagResult {
	fmt.Print("Checking for unused indexes...                          ")
	lines, err := psqlRaw(dsn, `
		SELECT indexrelname||'|'||relname||'|'||pg_size_pretty(pg_relation_size(indexrelid))||'|'||idx_scan
		FROM pg_stat_user_indexes
		JOIN pg_index USING (indexrelid)
		WHERE schemaname = 'kb'
		  AND NOT indisunique
		  AND idx_scan < 10
		  AND pg_relation_size(indexrelid) > 65536
		ORDER BY pg_relation_size(indexrelid) DESC
		LIMIT 10`)
	if err != nil {
		fmt.Println("FAILED")
		return diagResult{name: "Unused indexes", status: "fail", message: err.Error()}
	}

	var unused []string
	for _, line := range lines {
		parts := strings.Split(line, "|")
		if len(parts) < 4 {
			continue
		}
		unused = append(unused, fmt.Sprintf("  %s on %s (%s, %s scans)", parts[0], parts[1], parts[2], parts[3]))
	}
	if len(unused) > 0 {
		fmt.Println("WARN")
		return diagResult{name: "Unused indexes", status: "warn",
			message: fmt.Sprintf("%d non-unique indexes with <10 scans:\n%s", len(unused), strings.Join(unused, "\n"))}
	}
	fmt.Println("OK")
	return diagResult{name: "Unused indexes", status: "pass", message: "All non-unique indexes are actively used"}
}

// ── EXPLAIN checks ────────────────────────────────────────────────────────────

func diagExplain(dsn, name, query string) diagResult {
	label := fmt.Sprintf("EXPLAIN: %-44s", name)
	fmt.Printf("%-56s", label)

	start := time.Now()
	lines, err := psqlRaw(dsn, "EXPLAIN (ANALYZE, BUFFERS, FORMAT TEXT) "+query)
	elapsed := time.Since(start)
	_ = elapsed

	if err != nil {
		fmt.Println("FAILED")
		return diagResult{name: "EXPLAIN " + name, status: "fail", message: strings.Join(lines, "\n")}
	}

	plan := strings.Join(lines, "\n")

	// Parse "Execution Time: X.XXX ms"
	actualMS := 0.0
	for i := len(lines) - 1; i >= 0 && i >= len(lines)-5; i-- {
		if strings.HasPrefix(lines[i], "Execution Time:") {
			fmt.Sscanf(lines[i], "Execution Time: %f ms", &actualMS)
			break
		}
	}

	// Detect issues
	seqScanLines := []string{}
	for _, l := range lines {
		trimmed := strings.TrimSpace(l)
		if strings.HasPrefix(trimmed, "Seq Scan on kb.") || strings.HasPrefix(trimmed, "Seq Scan on ") {
			seqScanLines = append(seqScanLines, "  "+trimmed)
		}
	}

	isSlow := actualMS > float64(dbDiagnoseFlags.slowMS)
	hasSeqScan := len(seqScanLines) > 0

	var issues []string
	if isSlow {
		issues = append(issues, fmt.Sprintf("slow: %.1f ms (threshold: %d ms)", actualMS, dbDiagnoseFlags.slowMS))
	}
	if hasSeqScan {
		issues = append(issues, "sequential scan — possible missing index:\n"+strings.Join(seqScanLines, "\n"))
	}

	status := "pass"
	if len(issues) > 0 {
		status = "warn"
		fmt.Printf("WARN (%.1f ms)\n", actualMS)
	} else {
		fmt.Printf("OK   (%.1f ms)\n", actualMS)
	}

	msg := fmt.Sprintf("%.1f ms", actualMS)
	if len(issues) > 0 {
		msg = strings.Join(issues, "\n  ")
	}

	return diagResult{
		name:     "EXPLAIN " + name,
		status:   status,
		message:  msg,
		planText: plan,
	}
}

// ── summary printer ───────────────────────────────────────────────────────────

func printDiagSummary(results []diagResult) {
	passed, warned, failed := 0, 0, 0
	for _, r := range results {
		switch r.status {
		case "pass":
			passed++
		case "warn":
			warned++
		case "fail":
			failed++
		}
	}

	fmt.Println()
	fmt.Printf("%s%s━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━%s\n", diagBold, diagCyan, diagReset)
	fmt.Printf("%s%s  Report%s\n", diagBold, diagCyan, diagReset)
	fmt.Printf("%s%s━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━%s\n", diagBold, diagCyan, diagReset)
	fmt.Println()

	for _, r := range results {
		icon, col := "✓", diagGreen
		switch r.status {
		case "warn":
			icon, col = "⚠", diagYellow
		case "fail":
			icon, col = "✗", diagRed
		}
		fmt.Printf("%s%s %s%s\n", col, icon, r.name, diagReset)
		if r.message != "" {
			for _, line := range strings.Split(r.message, "\n") {
				fmt.Printf("    %s\n", line)
			}
		}

		// Show table size breakdown if verbose or non-pass
		if r.planText != "" && (dbDiagnoseFlags.verbose || r.status != "pass") {
			fmt.Println()
			fmt.Printf("    %sDetail:%s\n", diagCyan, diagReset)
			for _, line := range strings.Split(r.planText, "\n") {
				fmt.Printf("    %s\n", line)
			}
		}
		fmt.Println()
	}

	fmt.Printf("Checks: %s%d passed%s", diagGreen, passed, diagReset)
	if warned > 0 {
		fmt.Printf(", %s%d warnings%s", diagYellow, warned, diagReset)
	}
	if failed > 0 {
		fmt.Printf(", %s%d failed%s", diagRed, failed, diagReset)
	}
	fmt.Println()
	fmt.Println()

	if warned > 0 || failed > 0 {
		fmt.Printf("%sTip:%s Use --verbose to see the full EXPLAIN plan for every query.\n\n", diagYellow, diagReset)
	}
}
