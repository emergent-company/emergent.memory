package cmd

// embeddings.go — "memory embeddings" command group
//
// Subcommands:
//   memory embeddings status   — show pause/run state of all embedding workers
//   memory embeddings pause    — pause all embedding workers
//   memory embeddings resume   — resume all embedding workers
//
// The commands hit the internal server-side endpoints:
//   GET  /api/embeddings/status
//   POST /api/embeddings/pause
//   POST /api/embeddings/resume
//
// No auth required (same pattern as /api/diagnostics).

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/emergent-company/emergent.memory/tools/cli/internal/config"
	"github.com/spf13/cobra"
)

// ─── flags ────────────────────────────────────────────────────────────────────

var embeddingsFlags struct {
	server     string
	configPath string
}

// ─── parent command ───────────────────────────────────────────────────────────

var embeddingsCmd = &cobra.Command{
	Use:     "embeddings",
	Short:   "Manage embedding workers",
	GroupID: "knowledge",
	Long: `Inspect and control the embedding workers running in the Memory server.

Useful for benchmarking: pause all workers before a bench run so embeddings
don't interfere with write throughput, then resume afterwards.

Examples:
  memory embeddings status            Show current worker state
  memory embeddings pause             Pause all embedding workers
  memory embeddings resume            Resume all embedding workers
  memory embeddings pause --server http://your-server:3002`,
}

func init() {
	embeddingsCmd.PersistentFlags().StringVar(&embeddingsFlags.server, "server", "", "Memory server URL (overrides config)")
	embeddingsCmd.PersistentFlags().StringVar(&embeddingsFlags.configPath, "config-path", "", "path to Memory config.yaml")

	embeddingsCmd.AddCommand(embeddingsStatusCmd)
	embeddingsCmd.AddCommand(embeddingsPauseCmd)
	embeddingsCmd.AddCommand(embeddingsResumeCmd)
	embeddingsCmd.AddCommand(embeddingsConfigCmd)
	embeddingsCmd.AddCommand(embeddingsProgressCmd)
	embeddingsCmd.AddCommand(embeddingsClearCmd)

	embeddingsConfigCmd.Flags().IntVar(&embeddingsConfigFlags.batchSize, "batch", 0, "Number of jobs to dequeue per poll (0 = no change)")
	embeddingsConfigCmd.Flags().IntVar(&embeddingsConfigFlags.concurrency, "concurrency", 0, "Number of jobs processed concurrently per poll (0 = no change)")
	embeddingsConfigCmd.Flags().IntVar(&embeddingsConfigFlags.intervalMs, "interval-ms", 0, "Polling interval in milliseconds (0 = no change)")
	embeddingsConfigCmd.Flags().IntVar(&embeddingsConfigFlags.staleMinutes, "stale-minutes", 0, "Minutes before a processing job is marked stale (0 = no change)")

	rootCmd.AddCommand(embeddingsCmd)
}

// ─── resolve server URL (same logic as db bench) ──────────────────────────────

func resolveEmbeddingsServerURL() string {
	u := embeddingsFlags.server
	if u == "" {
		if v := os.Getenv("MEMORY_SERVER_URL"); v != "" {
			u = v
		}
	}
	if u == "" {
		cfgPath := config.DiscoverPath(embeddingsFlags.configPath)
		if cfg, err := config.LoadWithEnv(cfgPath); err == nil && cfg.ServerURL != "" {
			u = cfg.ServerURL
		}
	}
	if u == "" {
		u = "http://localhost:3002"
	}
	return strings.TrimRight(u, "/")
}

// ─── shared HTTP helper ───────────────────────────────────────────────────────

func embeddingsDoRequest(method, path string) (map[string]any, error) {
	svrURL := resolveEmbeddingsServerURL()
	url := svrURL + path

	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request to %s failed: %w", url, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("server returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var result map[string]any
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}
	return result, nil
}

func embeddingsDoRequestJSON(method, path string, payload map[string]any) (map[string]any, error) {
	svrURL := resolveEmbeddingsServerURL()
	url := svrURL + path

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequest(method, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request to %s failed: %w", url, err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("server returned %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	var result map[string]any
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}
	return result, nil
}

func printEmbeddingStatus(result map[string]any) {
	// Try to extract the nested "status" key (present in pause/resume/config responses)
	if nested, ok := result["status"]; ok {
		if nestedMap, ok := nested.(map[string]any); ok {
			result = nestedMap
		}
	}

	printWorker := func(name string, w map[string]any) {
		running, _ := w["running"].(bool)
		paused, _ := w["paused"].(bool)

		state := "running"
		if !running {
			state = "stopped"
		} else if paused {
			state = "paused"
		}

		symbol := "●"
		switch state {
		case "paused":
			symbol = "⏸"
		case "stopped":
			symbol = "○"
		}
		fmt.Printf("  %s  %-15s %s\n", symbol, name, state)
	}

	fmt.Println()
	fmt.Println("Embedding workers:")
	for _, name := range []string{"objects", "relationships", "sweep"} {
		if w, ok := result[name].(map[string]any); ok {
			printWorker(name, w)
		}
	}

	if cfg, ok := result["config"].(map[string]any); ok {
		fmt.Println()
		fmt.Println("Config:")
		if v, ok := cfg["batch_size"]; ok {
			fmt.Printf("  batch_size:    %v\n", v)
		}
		if v, ok := cfg["concurrency"]; ok {
			fmt.Printf("  concurrency:   %v\n", v)
		}
		if v, ok := cfg["interval_ms"]; ok {
			fmt.Printf("  interval_ms:   %v\n", v)
		}
		if v, ok := cfg["stale_minutes"]; ok {
			fmt.Printf("  stale_minutes: %v\n", v)
		}
	}
	fmt.Println()
}

// ─── status subcommand ────────────────────────────────────────────────────────

var embeddingsStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show pause/run state of all embedding workers",
	Long: `Show the current state of all embedding workers.

Prints a worker state table for the objects, relationships, and sweep workers.
Each worker is shown with a symbol indicating its state: running (●), paused (⏸),
or stopped (○). Also displays the current worker Config: batch_size, concurrency,
interval_ms, and stale_minutes.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		result, err := embeddingsDoRequest(http.MethodGet, "/api/embeddings/status")
		if err != nil {
			return err
		}
		printEmbeddingStatus(result)
		return nil
	},
}

// ─── pause subcommand ─────────────────────────────────────────────────────────

var embeddingsPauseCmd = &cobra.Command{
	Use:   "pause",
	Short: "Pause all embedding workers (object, relationship, sweep)",
	Long: `Pause all embedding workers (objects, relationships, and sweep).

Prints a confirmation message from the server, then displays the updated worker
state table showing each worker's status symbol (running ●, paused ⏸, stopped ○)
and the current Config (batch_size, concurrency, interval_ms, stale_minutes).`,
	RunE: func(cmd *cobra.Command, args []string) error {
		result, err := embeddingsDoRequest(http.MethodPost, "/api/embeddings/pause")
		if err != nil {
			return err
		}
		if msg, ok := result["message"].(string); ok {
			fmt.Println(msg)
		}
		printEmbeddingStatus(result)
		return nil
	},
}

// ─── resume subcommand ────────────────────────────────────────────────────────

var embeddingsResumeCmd = &cobra.Command{
	Use:   "resume",
	Short: "Resume all embedding workers",
	Long: `Resume all paused embedding workers (objects, relationships, and sweep).

Prints a confirmation message from the server, then displays the updated worker
state table showing each worker's status symbol (running ●, paused ⏸, stopped ○)
and the current Config (batch_size, concurrency, interval_ms, stale_minutes).`,
	RunE: func(cmd *cobra.Command, args []string) error {
		result, err := embeddingsDoRequest(http.MethodPost, "/api/embeddings/resume")
		if err != nil {
			return err
		}
		if msg, ok := result["message"].(string); ok {
			fmt.Println(msg)
		}
		printEmbeddingStatus(result)
		return nil
	},
}

// ─── progress subcommand ──────────────────────────────────────────────────────

var embeddingsProgressCmd = &cobra.Command{
	Use:   "progress",
	Short: "Show embedding job queue progress (pending, processing, completed, failed)",
	Long: `Show embedding job queue statistics for all queues.

Displays counts of pending, processing, completed, failed, and dead-letter jobs
for both the graph object and graph relationship embedding queues.

Examples:
  memory embeddings progress
  memory embeddings progress --server http://your-server:3002`,
	RunE: func(cmd *cobra.Command, args []string) error {
		result, err := embeddingsDoRequest(http.MethodGet, "/api/embeddings/progress")
		if err != nil {
			return err
		}
		printEmbeddingProgress(result)
		return nil
	},
}

func printEmbeddingProgress(result map[string]any) {
	printQueue := func(name string, q map[string]any) {
		pending, _ := q["pending"].(float64)
		processing, _ := q["processing"].(float64)
		completed, _ := q["completed"].(float64)
		failed, _ := q["failed"].(float64)
		deadLetter, _ := q["deadLetter"].(float64)
		total := pending + processing + completed + failed + deadLetter
		var pct float64
		if total > 0 {
			pct = completed / total * 100
		}
		fmt.Printf("  %-16s  pending=%-6.0f  processing=%-6.0f  completed=%-6.0f  failed=%-6.0f  dead_letter=%-6.0f  (%.1f%%)\n",
			name, pending, processing, completed, failed, deadLetter, pct)
	}

	fmt.Println()
	fmt.Println("Embedding queue progress:")
	for _, name := range []string{"objects", "relationships"} {
		if q, ok := result[name].(map[string]any); ok {
			printQueue(name, q)
		}
	}
	fmt.Println()
}

// ─── clear subcommand ─────────────────────────────────────────────────────────

var embeddingsClearCmd = &cobra.Command{
	Use:   "clear",
	Short: "Delete all pending and processing embedding jobs from both queues",
	Long: `Delete all pending and processing jobs from the object and relationship
embedding queues. Useful when the queue is stuck or polluted.

Examples:
  memory embeddings clear
  memory embeddings clear --server http://your-server:3002`,
	RunE: func(cmd *cobra.Command, args []string) error {
		result, err := embeddingsDoRequest(http.MethodDelete, "/api/embeddings/queue")
		if err != nil {
			return err
		}
		objN, _ := result["objects_cleared"].(float64)
		relN, _ := result["relationships_cleared"].(float64)
		fmt.Printf("Cleared %.0f object jobs and %.0f relationship jobs.\n", objN, relN)
		return nil
	},
}

var embeddingsConfigFlags struct {
	batchSize    int
	concurrency  int
	intervalMs   int
	staleMinutes int
}

var embeddingsConfigCmd = &cobra.Command{
	Use:   "config",
	Short: "Get or set embedding worker config (batch, concurrency, stale-minutes)",
	Long: `Get or update embedding worker configuration at runtime without restarting.

All flags are optional — omit a flag to leave that value unchanged.
With no flags, shows the current configuration.

Examples:
  memory embeddings config                                  Show current config
  memory embeddings config --batch 200 --concurrency 200   Max throughput
  memory embeddings config --stale-minutes 60              Raise stale threshold
  memory embeddings config --batch 10 --concurrency 10     Throttle down`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// If no flags set, just show status
		if !cmd.Flags().Changed("batch") &&
			!cmd.Flags().Changed("concurrency") &&
			!cmd.Flags().Changed("interval-ms") &&
			!cmd.Flags().Changed("stale-minutes") {
			result, err := embeddingsDoRequest(http.MethodGet, "/api/embeddings/status")
			if err != nil {
				return err
			}
			printEmbeddingStatus(result)
			return nil
		}

		// Build PATCH body with only changed fields
		body := map[string]any{}
		if cmd.Flags().Changed("batch") {
			body["batch_size"] = embeddingsConfigFlags.batchSize
		}
		if cmd.Flags().Changed("concurrency") {
			body["concurrency"] = embeddingsConfigFlags.concurrency
		}
		if cmd.Flags().Changed("interval-ms") {
			body["interval_ms"] = embeddingsConfigFlags.intervalMs
		}
		if cmd.Flags().Changed("stale-minutes") {
			body["stale_minutes"] = embeddingsConfigFlags.staleMinutes
		}

		result, err := embeddingsDoRequestJSON(http.MethodPatch, "/api/embeddings/config", body)
		if err != nil {
			return err
		}
		if msg, ok := result["message"].(string); ok {
			fmt.Println(msg)
		}
		printEmbeddingStatus(result)
		return nil
	},
}
