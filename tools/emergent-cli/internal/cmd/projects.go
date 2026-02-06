package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/emergent-company/emergent/tools/emergent-cli/internal/client"
	"github.com/emergent-company/emergent/tools/emergent-cli/internal/config"
	"github.com/spf13/cobra"
)

var projectsCmd = &cobra.Command{
	Use:   "projects",
	Short: "Manage projects",
	Long:  "Commands for managing projects in the Emergent platform",
}

var listProjectsCmd = &cobra.Command{
	Use:   "list",
	Short: "List all projects",
	Long:  "List all projects you have access to",
	RunE:  runListProjects,
}

type Project struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

func runListProjects(cmd *cobra.Command, args []string) error {
	configPath, _ := cmd.Flags().GetString("config")
	if configPath == "" {
		configPath = config.DiscoverPath("")
	}

	cfg, err := config.LoadWithEnv(configPath)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	if cfg.ServerURL == "" {
		return fmt.Errorf("no server URL configured. Set EMERGENT_SERVER_URL or run: emergent-cli config set-server <url>")
	}

	c := client.New(cfg)

	resp, err := c.Get("/api/projects")
	if err != nil {
		return fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("request failed with status %d: %s", resp.StatusCode, string(body))
	}

	var projects []Project
	if err := json.NewDecoder(resp.Body).Decode(&projects); err != nil {
		return fmt.Errorf("failed to decode response: %w", err)
	}

	if len(projects) == 0 {
		fmt.Println("No projects found.")
		return nil
	}

	fmt.Printf("Found %d project(s):\n\n", len(projects))
	for i, p := range projects {
		fmt.Printf("%d. %s\n", i+1, p.Name)
		fmt.Printf("   ID: %s\n", p.ID)
		if p.Description != "" {
			fmt.Printf("   Description: %s\n", p.Description)
		}
		fmt.Println()
	}

	return nil
}

func init() {
	projectsCmd.AddCommand(listProjectsCmd)
	rootCmd.AddCommand(projectsCmd)
}
