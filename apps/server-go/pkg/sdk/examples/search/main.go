// Example: Searching documents
//
// This example demonstrates:
// - Performing hybrid search (lexical + semantic)
// - Processing search results
// - Handling different search strategies

package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/emergent/emergent-core/pkg/sdk"
	"github.com/emergent/emergent-core/pkg/sdk/search"
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

	query := "machine learning"
	if len(os.Args) > 1 {
		query = os.Args[1]
	}

	fmt.Printf("Searching for: %q\n\n", query)

	results, err := client.Search.Search(ctx, &search.SearchRequest{
		Query:    query,
		Strategy: "hybrid",
		Limit:    10,
	})
	if err != nil {
		log.Fatalf("Search failed: %v", err)
	}

	fmt.Printf("Found %d results\n\n", results.Total)

	for i, result := range results.Results {
		fmt.Printf("%d. Score: %.4f\n", i+1, result.Score)
		fmt.Printf("   Document: %s\n", result.DocumentID)
		fmt.Printf("   Chunk: %s\n", result.ChunkID)
		fmt.Printf("   Preview: %s\n\n", truncate(result.Content, 100))
	}
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
