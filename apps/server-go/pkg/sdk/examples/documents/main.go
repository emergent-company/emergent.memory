// Example: Working with documents and chunks
//
// This example demonstrates:
// - Listing documents with pagination
// - Fetching a specific document
// - Listing chunks for a document

package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/emergent/emergent-core/pkg/sdk"
	"github.com/emergent/emergent-core/pkg/sdk/chunks"
	"github.com/emergent/emergent-core/pkg/sdk/documents"
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

	fmt.Println("=== Listing Documents ===")
	docs, err := client.Documents.List(ctx, &documents.ListOptions{
		Limit: 10,
	})
	if err != nil {
		log.Fatalf("Failed to list documents: %v", err)
	}

	fmt.Printf("Found %d documents\n\n", len(docs.Data))

	if len(docs.Data) == 0 {
		fmt.Println("No documents found. Upload some documents first!")
		return
	}

	for _, doc := range docs.Data {
		fmt.Printf("- %s (ID: %s)\n", doc.Title, doc.ID)
		fmt.Printf("  Type: %s, Source: %s\n", doc.ContentType, doc.SourceType)
		fmt.Printf("  Created: %s\n\n", doc.CreatedAt.Format("2006-01-02 15:04:05"))
	}

	firstDoc := docs.Data[0]

	fmt.Printf("=== Document Details: %s ===\n", firstDoc.Title)
	fullDoc, err := client.Documents.Get(ctx, firstDoc.ID)
	if err != nil {
		log.Fatalf("Failed to get document: %v", err)
	}

	fmt.Printf("Title: %s\n", fullDoc.Title)
	fmt.Printf("Source URL: %s\n", fullDoc.SourceURL)
	fmt.Printf("Content Type: %s\n", fullDoc.ContentType)

	fmt.Println("\n=== Listing Chunks ===")
	docChunks, err := client.Chunks.List(ctx, &chunks.ListOptions{
		DocumentID: firstDoc.ID,
		Limit:      5,
	})
	if err != nil {
		log.Fatalf("Failed to list chunks: %v", err)
	}

	fmt.Printf("Found %d chunks for this document\n\n", len(docChunks.Data))

	for i, chunk := range docChunks.Data {
		fmt.Printf("%d. Position: %d\n", i+1, chunk.Position)
		fmt.Printf("   Preview: %s\n\n", truncate(chunk.Content, 80))
	}
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
