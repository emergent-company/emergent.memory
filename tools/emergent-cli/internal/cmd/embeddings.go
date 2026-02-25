package cmd

// embeddings.go — "emergent embeddings" command group
//
// Subcommands:
//   emergent embeddings status   — show pause/run state of all embedding workers
//   emergent embeddings pause    — pause all embedding workers
//   emergent embeddings resume   — resume all embedding workers
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

	"github.com/emergent-company/emergent/tools/emergent-cli/internal/config"
	"github.com/spf13/cobra"
)

// ─── flags ────────────────────────────────────────────────────────────────────

var embeddingsFlags struct {
	server     string
	configPath string
}

// ─── parent command ───────────────────────────────────────────────────────────

var embeddingsCmd = &cobra.Command{
	Use:   "embeddings",
	Short: "Manage embedding workers",
	Long: `Inspect and control the embedding workers running in the Emergent server.

Useful for benchmarking: pause all workers before a bench run so embeddings
don't interfere with write throughput, then resume afterwards.

Examples:
  emergent embeddings status            Show current worker state
  emergent embeddings pause             Pause all embedding workers
  emergent embeddings resume            Resume all embedding workers
  emergent embeddings pause --server http://mcj-emergent:3002`,
}

func init() {
	embeddingsCmd.PersistentFlags().StringVar(&embeddingsFlags.server, "server", "", "Emergent server URL (overrides config)")
	embeddingsCmd.PersistentFlags().StringVar(&embeddingsFlags.configPath, "config-path", "", "path to Emergent config.yaml")

	embeddingsCmd.AddCommand(embeddingsStatusCmd)
	embeddingsCmd.AddCommand(embeddingsPauseCmd)
	embeddingsCmd.AddCommand(embeddingsResumeCmd)
	embeddingsCmd.AddCommand(embeddingsConfigCmd)

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
		if v := os.Getenv("EMERGENT_SERVER_URL"); v != "" {
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

// ─── config subcommand ────────────────────────────────────────────────────────

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
  emergent embeddings config                                  Show current config
  emergent embeddings config --batch 200 --concurrency 200   Max throughput
  emergent embeddings config --stale-minutes 60              Raise stale threshold
  emergent embeddings config --batch 10 --concurrency 10     Throttle down`,
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
