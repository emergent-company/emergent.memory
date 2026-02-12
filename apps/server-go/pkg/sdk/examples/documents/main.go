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

	"github.com/emergent-company/emergent/apps/server-go/pkg/sdk"
	"github.com/emergent-company/emergent/apps/server-go/pkg/sdk/chunks"
	"github.com/emergent-company/emergent/apps/server-go/pkg/sdk/documents"
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
	result, err := client.Documents.List(ctx, &documents.ListOptions{
		Limit: 10,
	})
	if err != nil {
		log.Fatalf("Failed to list documents: %v", err)
	}

	fmt.Printf("Found %d documents (total: %d)\n\n", len(result.Documents), result.Total)

	if len(result.Documents) == 0 {
		fmt.Println("No documents found. Upload some documents first!")
		return
	}

	for _, doc := range result.Documents {
		name := doc.ID
		if doc.Filename != nil {
			name = *doc.Filename
		}
		fmt.Printf("- %s (ID: %s)\n", name, doc.ID)
		if doc.MimeType != nil {
			fmt.Printf("  Type: %s\n", *doc.MimeType)
		}
		fmt.Printf("  Created: %s\n\n", doc.CreatedAt.Format("2006-01-02 15:04:05"))
	}

	firstDoc := result.Documents[0]

	firstDocName := firstDoc.ID
	if firstDoc.Filename != nil {
		firstDocName = *firstDoc.Filename
	}
	fmt.Printf("=== Document Details: %s ===\n", firstDocName)
	fullDoc, err := client.Documents.Get(ctx, firstDoc.ID)
	if err != nil {
		log.Fatalf("Failed to get document: %v", err)
	}

	if fullDoc.Filename != nil {
		fmt.Printf("Filename: %s\n", *fullDoc.Filename)
	}
	if fullDoc.SourceURL != nil {
		fmt.Printf("Source URL: %s\n", *fullDoc.SourceURL)
	}
	if fullDoc.MimeType != nil {
		fmt.Printf("MIME Type: %s\n", *fullDoc.MimeType)
	}

	fmt.Println("\n=== Listing Chunks ===")
	docChunks, err := client.Chunks.List(ctx, &chunks.ListOptions{
		DocumentID: firstDoc.ID,
	})
	if err != nil {
		log.Fatalf("Failed to list chunks: %v", err)
	}

	fmt.Printf("Found %d chunks for this document (total: %d)\n\n", len(docChunks.Data), docChunks.TotalCount)

	for i, chunk := range docChunks.Data {
		fmt.Printf("%d. Index: %d, Size: %d chars\n", i+1, chunk.Index, chunk.Size)
		fmt.Printf("   Preview: %s\n\n", truncate(chunk.Text, 80))
	}
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
