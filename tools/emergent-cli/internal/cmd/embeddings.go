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

type embeddingWorkerStatus struct {
	Running bool `json:"running"`
	Paused  bool `json:"paused"`
}

type embeddingStatusResponse struct {
	Objects       embeddingWorkerStatus `json:"objects"`
	Relationships embeddingWorkerStatus `json:"relationships"`
	Sweep         embeddingWorkerStatus `json:"sweep"`
}

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

func printEmbeddingStatus(result map[string]any) {
	// Try to extract the nested "status" key (present in pause/resume responses)
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
