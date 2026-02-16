package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/emergent-company/emergent/apps/server-go/pkg/sdk/projects"
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
	Use:   "get [name-or-id]",
	Short: "Get project details",
	Long:  "Get details for a specific project by name or ID",
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

	return client.New(cfg)
}

func runListProjects(cmd *cobra.Command, args []string) error {
	c, err := getClient(cmd)
	if err != nil {
		return err
	}

	projectList, err := c.SDK.Projects.List(context.Background(), nil)
	if err != nil {
		return fmt.Errorf("failed to list projects: %w", err)
	}

	if len(projectList) == 0 {
		fmt.Println("No projects found.")
		return nil
	}

	fmt.Printf("Found %d project(s):\n\n", len(projectList))
	for i, p := range projectList {
		fmt.Printf("%d. %s (%s)\n", i+1, p.Name, p.ID)
		if p.KBPurpose != nil && *p.KBPurpose != "" {
			fmt.Printf("   KB Purpose: %s\n", *p.KBPurpose)
		}
		fmt.Println()
	}

	return nil
}

func runGetProject(cmd *cobra.Command, args []string) error {
	c, err := getClient(cmd)
	if err != nil {
		return err
	}

	projectID, err := resolveProjectNameOrID(c, args[0])
	if err != nil {
		return err
	}

	project, err := c.SDK.Projects.Get(context.Background(), projectID)
	if err != nil {
		return fmt.Errorf("failed to get project: %w", err)
	}

	fmt.Printf("Project: %s (%s)\n", project.Name, project.ID)
	fmt.Printf("  Org ID:      %s\n", project.OrgID)
	if project.KBPurpose != nil && *project.KBPurpose != "" {
		fmt.Printf("  KB Purpose:  %s\n", *project.KBPurpose)
	}

	return nil
}

// isUUID checks if a string looks like a UUID
func isUUID(s string) bool {
	// Simple check: UUIDs are 36 chars with hyphens in specific positions
	if len(s) != 36 {
		return false
	}
	for i, c := range s {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if c != '-' {
				return false
			}
		} else if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}

// resolveProjectNameOrID resolves a project name or ID to a project ID.
// If the input looks like a UUID, it's returned as-is.
// Otherwise, it fetches all projects and finds a match by name (case-insensitive).
func resolveProjectNameOrID(c *client.Client, nameOrID string) (string, error) {
	if isUUID(nameOrID) {
		return nameOrID, nil
	}

	// Treat as a name — look up all projects and match
	projectList, err := c.SDK.Projects.List(context.Background(), nil)
	if err != nil {
		return "", fmt.Errorf("failed to list projects for name resolution: %w", err)
	}

	var matches []projects.Project
	for _, p := range projectList {
		if strings.EqualFold(p.Name, nameOrID) {
			matches = append(matches, p)
		}
	}

	switch len(matches) {
	case 0:
		return "", fmt.Errorf("no project found with name %q", nameOrID)
	case 1:
		return matches[0].ID, nil
	default:
		fmt.Printf("Multiple projects match %q:\n", nameOrID)
		for _, p := range matches {
			fmt.Printf("  - %s (%s)\n", p.Name, p.ID)
		}
		return "", fmt.Errorf("ambiguous project name %q — use the project ID instead", nameOrID)
	}
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
		orgs, err := c.SDK.Orgs.List(context.Background())
		if err != nil {
			return fmt.Errorf("failed to fetch organizations: %w", err)
		}
		if len(orgs) == 0 {
			return fmt.Errorf("no organizations found. Create an organization first or specify --org-id")
		}
		orgID = orgs[0].ID
	}

	req := &projects.CreateProjectRequest{
		Name:  projectName,
		OrgID: orgID,
	}

	project, err := c.SDK.Projects.Create(context.Background(), req)
	if err != nil {
		return fmt.Errorf("failed to create project: %w", err)
	}

	fmt.Println("Project created successfully!")
	fmt.Printf("  Name: %s (%s)\n", project.Name, project.ID)

	return nil
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
