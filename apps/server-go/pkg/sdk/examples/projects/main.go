// Example: Managing projects
//
// This example demonstrates:
// - Listing projects
// - Creating a new project
// - Updating project settings
// - Deleting a project

package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/emergent/emergent-core/pkg/sdk"
	"github.com/emergent/emergent-core/pkg/sdk/projects"
)

func main() {
	apiKey := os.Getenv("EMERGENT_API_KEY")
	if apiKey == "" {
		log.Fatal("EMERGENT_API_KEY environment variable required")
	}

	serverURL := os.Getenv("EMERGENT_SERVER_URL")
	if serverURL == "" {
		serverURL = "http://localhost:3002"
	}

	client, err := sdk.New(sdk.Config{
		ServerURL: serverURL,
		Auth: sdk.AuthConfig{
			Mode:   "apikey",
			APIKey: apiKey,
		},
		OrgID: os.Getenv("EMERGENT_ORG_ID"),
	})
	if err != nil {
		log.Fatalf("Failed to create SDK client: %v", err)
	}

	ctx := context.Background()

	fmt.Println("=== Listing Projects ===")
	projectList, err := client.Projects.List(ctx, &projects.ListOptions{
		Limit: 10,
	})
	if err != nil {
		log.Fatalf("Failed to list projects: %v", err)
	}

	for _, p := range projectList {
		fmt.Printf("- %s (ID: %s)\n", p.Name, p.ID)
	}

	fmt.Println("\n=== Creating New Project ===")
	newProject, err := client.Projects.Create(ctx, &projects.CreateProjectRequest{
		Name:  "SDK Example",
		OrgID: os.Getenv("EMERGENT_ORG_ID"),
	})
	if err != nil {
		log.Fatalf("Failed to create project: %v", err)
	}

	fmt.Printf("Created project: %s (ID: %s)\n", newProject.Name, newProject.ID)

	fmt.Println("\n=== Updating Project ===")
	newName := "SDK Example (Updated)"
	updated, err := client.Projects.Update(ctx, newProject.ID, &projects.UpdateProjectRequest{
		Name: &newName,
	})
	if err != nil {
		log.Fatalf("Failed to update project: %v", err)
	}

	fmt.Printf("Updated project name to: %s\n", updated.Name)

	fmt.Println("\n=== Deleting Project ===")
	if err := client.Projects.Delete(ctx, newProject.ID); err != nil {
		log.Fatalf("Failed to delete project: %v", err)
	}

	fmt.Println("Project deleted successfully")
}
