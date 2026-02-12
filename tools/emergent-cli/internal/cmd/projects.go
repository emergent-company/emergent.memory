package cmd

import (
	"context"
	"fmt"

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
		fmt.Printf("%d. %s\n", i+1, p.Name)
		fmt.Printf("   ID: %s\n", p.ID)
		if p.KBPurpose != nil && *p.KBPurpose != "" {
			fmt.Printf("   KB Purpose: %s\n", *p.KBPurpose)
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

	project, err := c.SDK.Projects.Get(context.Background(), projectID)
	if err != nil {
		return fmt.Errorf("failed to get project: %w", err)
	}

	fmt.Printf("Project: %s\n", project.Name)
	fmt.Printf("  ID:          %s\n", project.ID)
	fmt.Printf("  Org ID:      %s\n", project.OrgID)
	if project.KBPurpose != nil && *project.KBPurpose != "" {
		fmt.Printf("  KB Purpose:  %s\n", *project.KBPurpose)
	}

	return nil
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
	fmt.Printf("  ID:   %s\n", project.ID)
	fmt.Printf("  Name: %s\n", project.Name)

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
