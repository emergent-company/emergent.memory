// Example: Basic SDK usage with API key authentication
//
// This example demonstrates:
// - Creating an SDK client with API key
// - Setting organization and project context
// - Basic error handling

package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/emergent-company/emergent/apps/server-go/pkg/sdk"
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
		OrgID:     os.Getenv("EMERGENT_ORG_ID"),
		ProjectID: os.Getenv("EMERGENT_PROJECT_ID"),
	})
	if err != nil {
		log.Fatalf("Failed to create SDK client: %v", err)
	}

	ctx := context.Background()

	health, err := client.Health.Health(ctx)
	if err != nil {
		log.Fatalf("Health check failed: %v", err)
	}

	fmt.Printf("Server Status: %s\n", health.Status)
	fmt.Printf("Server Version: %s\n", health.Version)
	fmt.Printf("Server Uptime: %s\n", health.Uptime)
	fmt.Println("✓ SDK connection successful!")
}
