package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/emergent-company/emergent/apps/server-go/pkg/sdk/search"
	"github.com/emergent-company/emergent/tools/emergent-cli/internal/client"
	"github.com/emergent-company/emergent/tools/emergent-cli/internal/config"
)

func main() {
	// Configuration for mcj-emergent server
	serverURL := "https://api.dev.emergent-company.ai"
	projectID := "dfe2febb-1971-4325-8f97-c816c6609f6d" // IMDB project
	orgID := "c9bfa6d1-dc9f-4c3b-ac37-7a0411a0beba"

	// Load credentials from config
	configPath := os.Getenv("HOME") + "/.emergent/config.json"
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
	fmt.Printf("Project: IMDB (%s)\n", projectID)
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
