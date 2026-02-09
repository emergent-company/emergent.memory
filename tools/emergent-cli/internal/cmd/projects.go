package cmd

import (
	"bytes"
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

var getProjectCmd = &cobra.Command{
	Use:   "get [id]",
	Short: "Get project details",
	Long:  "Get details for a specific project by ID",
	Args:  cobra.ExactArgs(1),
	RunE:  runGetProject,
}

var createProjectCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a new project",
	Long:  "Create a new project in the Emergent platform",
	RunE:  runCreateProject,
}

var (
	projectName        string
	projectDescription string
	projectOrgID       string
)

type Project struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	KBPurpose   string `json:"kb_purpose,omitempty"`
	CreatedAt   string `json:"created_at,omitempty"`
	UpdatedAt   string `json:"updated_at,omitempty"`
}

func getClient(cmd *cobra.Command) (*client.Client, error) {
	configPath, _ := cmd.Flags().GetString("config")
	if configPath == "" {
		configPath = config.DiscoverPath("")
	}

	cfg, err := config.LoadWithEnv(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	if cfg.ServerURL == "" {
		return nil, fmt.Errorf("no server URL configured. Set EMERGENT_SERVER_URL or run: emergent config set-server <url>")
	}

	return client.New(cfg), nil
}

func runListProjects(cmd *cobra.Command, args []string) error {
	c, err := getClient(cmd)
	if err != nil {
		return err
	}

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

func runGetProject(cmd *cobra.Command, args []string) error {
	projectID := args[0]

	c, err := getClient(cmd)
	if err != nil {
		return err
	}

	resp, err := c.Get("/api/projects/" + projectID)
	if err != nil {
		return fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf("project not found: %s", projectID)
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("request failed with status %d: %s", resp.StatusCode, string(body))
	}

	var project Project
	if err := json.NewDecoder(resp.Body).Decode(&project); err != nil {
		return fmt.Errorf("failed to decode response: %w", err)
	}

	fmt.Printf("Project: %s\n", project.Name)
	fmt.Printf("  ID:          %s\n", project.ID)
	if project.Description != "" {
		fmt.Printf("  Description: %s\n", project.Description)
	}
	if project.KBPurpose != "" {
		fmt.Printf("  KB Purpose:  %s\n", project.KBPurpose)
	}
	if project.CreatedAt != "" {
		fmt.Printf("  Created:     %s\n", project.CreatedAt)
	}
	if project.UpdatedAt != "" {
		fmt.Printf("  Updated:     %s\n", project.UpdatedAt)
	}

	return nil
}

type Organization struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func runCreateProject(cmd *cobra.Command, args []string) error {
	if projectName == "" {
		return fmt.Errorf("project name is required. Use --name flag")
	}

	c, err := getClient(cmd)
	if err != nil {
		return err
	}

	orgID := projectOrgID
	if orgID == "" {
		orgs, err := fetchOrganizations(c)
		if err != nil {
			return fmt.Errorf("failed to fetch organizations: %w", err)
		}
		if len(orgs) == 0 {
			return fmt.Errorf("no organizations found. Create an organization first or specify --org-id")
		}
		orgID = orgs[0].ID
	}

	payload := map[string]string{
		"name":  projectName,
		"orgId": orgID,
	}
	if projectDescription != "" {
		payload["description"] = projectDescription
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to encode request: %w", err)
	}

	resp, err := c.Post("/api/projects", "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("request failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	var project Project
	if err := json.NewDecoder(resp.Body).Decode(&project); err != nil {
		return fmt.Errorf("failed to decode response: %w", err)
	}

	fmt.Println("Project created successfully!")
	fmt.Printf("  ID:   %s\n", project.ID)
	fmt.Printf("  Name: %s\n", project.Name)
	if project.Description != "" {
		fmt.Printf("  Description: %s\n", project.Description)
	}

	return nil
}

func fetchOrganizations(c *client.Client) ([]Organization, error) {
	resp, err := c.Get("/api/orgs")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("request failed with status %d: %s", resp.StatusCode, string(body))
	}

	var orgs []Organization
	if err := json.NewDecoder(resp.Body).Decode(&orgs); err != nil {
		return nil, err
	}

	return orgs, nil
}

func init() {
	createProjectCmd.Flags().StringVar(&projectName, "name", "", "Project name (required)")
	createProjectCmd.Flags().StringVar(&projectDescription, "description", "", "Project description")
	createProjectCmd.Flags().StringVar(&projectOrgID, "org-id", "", "Organization ID (auto-detected if not specified)")
	_ = createProjectCmd.MarkFlagRequired("name")

	projectsCmd.AddCommand(listProjectsCmd)
	projectsCmd.AddCommand(getProjectCmd)
	projectsCmd.AddCommand(createProjectCmd)
	rootCmd.AddCommand(projectsCmd)
}
