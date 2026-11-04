package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/emergent-company/emergent.memory/apps/cli/internal/client"
	"github.com/emergent-company/emergent.memory/apps/cli/internal/config"
	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/search"
)

func main() {
	// Configuration — no deployment coordinates are baked in.
	serverURL := os.Getenv("MEMORY_SERVER_URL")
	projectID := os.Getenv("MEMORY_PROJECT_ID")
	orgID := os.Getenv("MEMORY_ORG_ID")
	if serverURL == "" || projectID == "" {
		log.Fatal("MEMORY_SERVER_URL and MEMORY_PROJECT_ID must be set")
	}

	// Load credentials from config
	configPath := os.Getenv("HOME") + "/.memory/config.json"
	configData, err := os.ReadFile(configPath)
	if err != nil {
		log.Fatalf("Failed to read config: %v", err)
	}

	var creds struct {
		ServerURL   string `json:"server_url"`
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(configData, &creds); err != nil {
		log.Fatalf("Failed to parse config: %v", err)
	}

	// Create client
	c, err := client.New(&config.Config{
		ServerURL: serverURL,
		APIKey:    creds.AccessToken,
		OrgID:     orgID,
		ProjectID: projectID,
	})
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}

	// Test query
	query := "who directed fight club and what are the other movies of this director"
	fmt.Printf("Testing query: %s\n", query)
	fmt.Printf("Project: %s\n", projectID)
	fmt.Printf("Server: %s\n\n", serverURL)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	start := time.Now()
	response, err := c.SDK.Search.Search(ctx, &search.SearchRequest{
		Query:          query,
		FusionStrategy: "weighted",
		ResultTypes:    "both",
		Limit:          5,
		IncludeDebug:   true,
	})
	elapsed := time.Since(start)

	if err != nil {
		log.Fatalf("Query failed after %v: %v", elapsed, err)
	}

	fmt.Printf("Query completed in %v\n", elapsed)
	fmt.Printf("Total results: %d\n\n", response.Total)

	// Print results
	for i, result := range response.Results {
		fmt.Printf("Result %d:\n", i+1)
		resultJSON, _ := json.MarshalIndent(result, "  ", "  ")
		fmt.Printf("  %s\n\n", resultJSON)
	}

	if response.Total == 0 {
		fmt.Println("⚠️  WARNING: Query returned zero results!")
		fmt.Println("This might indicate:")
		fmt.Println("  - Embeddings not generated for graph objects")
		fmt.Println("  - Search parameters too restrictive")
		fmt.Println("  - Data not indexed properly")
	} else {
		fmt.Printf("✅ Success! Found %d results\n", response.Total)
	}
}
