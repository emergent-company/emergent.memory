package chunks_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/emergent-company/emergent/apps/server-go/pkg/sdk"
	"github.com/emergent-company/emergent/apps/server-go/pkg/sdk/chunks"
	"github.com/emergent-company/emergent/apps/server-go/pkg/sdk/testutil"
)

func TestChunksList(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	mock.On("GET", "/api/chunks", func(w http.ResponseWriter, r *http.Request) {
		testutil.AssertHeader(t, r, "X-API-Key", "test_key")

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		testutil.JSONResponse(t, w, map[string]interface{}{
			"data": []map[string]interface{}{
				{
					"id":            "chunk_123",
					"documentId":    "doc_456",
					"documentTitle": "test.pdf",
					"index":         0,
					"size":          150,
					"hasEmbedding":  true,
					"text":          "Sample chunk content",
					"createdAt":     "2026-02-11T10:00:00Z",
				},
			},
			"totalCount": 1,
		})
	})

	client, _ := sdk.New(sdk.Config{
		ServerURL: mock.URL,
		Auth:      sdk.AuthConfig{Mode: "apikey", APIKey: "test_key"},
	})

	result, err := client.Chunks.List(context.Background(), nil)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}

	if len(result.Data) != 1 {
		t.Errorf("expected 1 chunk, got %d", len(result.Data))
	}

	if result.Data[0].ID != "chunk_123" {
		t.Errorf("expected chunk ID chunk_123, got %s", result.Data[0].ID)
	}

	if result.TotalCount != 1 {
		t.Errorf("expected totalCount 1, got %d", result.TotalCount)
	}
}

func TestChunksListByDocument(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	mock.On("GET", "/api/chunks", func(w http.ResponseWriter, r *http.Request) {
		if docID := r.URL.Query().Get("documentId"); docID != "doc_456" {
			t.Errorf("expected documentId=doc_456, got %s", docID)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		testutil.JSONResponse(t, w, map[string]interface{}{
			"data":       []interface{}{},
			"totalCount": 0,
		})
	})

	client, _ := sdk.New(sdk.Config{
		ServerURL: mock.URL,
		Auth:      sdk.AuthConfig{Mode: "apikey", APIKey: "test_key"},
	})

	_, err := client.Chunks.List(context.Background(), &chunks.ListOptions{
		DocumentID: "doc_456",
	})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
}

func TestChunksDelete(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	mock.On("DELETE", "/api/chunks/chunk_123", func(w http.ResponseWriter, r *http.Request) {
		testutil.AssertHeader(t, r, "X-API-Key", "test_key")
		w.WriteHeader(http.StatusNoContent)
	})

	client, _ := sdk.New(sdk.Config{
		ServerURL: mock.URL,
		Auth:      sdk.AuthConfig{Mode: "apikey", APIKey: "test_key"},
	})

	err := client.Chunks.Delete(context.Background(), "chunk_123")
	if err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
}

func TestChunksBulkDelete(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	mock.On("DELETE", "/api/chunks", func(w http.ResponseWriter, r *http.Request) {
		testutil.AssertHeader(t, r, "X-API-Key", "test_key")
		testutil.AssertHeader(t, r, "Content-Type", "application/json")

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		testutil.JSONResponse(t, w, map[string]interface{}{
			"totalRequested": 2,
			"totalDeleted":   2,
			"totalFailed":    0,
			"results": []map[string]interface{}{
				{"id": "chunk_1", "success": true},
				{"id": "chunk_2", "success": true},
			},
		})
	})

	client, _ := sdk.New(sdk.Config{
		ServerURL: mock.URL,
		Auth:      sdk.AuthConfig{Mode: "apikey", APIKey: "test_key"},
	})

	result, err := client.Chunks.BulkDelete(context.Background(), []string{"chunk_1", "chunk_2"})
	if err != nil {
		t.Fatalf("BulkDelete() error = %v", err)
	}

	if result.TotalDeleted != 2 {
		t.Errorf("expected 2 deleted, got %d", result.TotalDeleted)
	}
}

func TestChunksDeleteByDocument(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	mock.On("DELETE", "/api/chunks/by-document/doc_456", func(w http.ResponseWriter, r *http.Request) {
		testutil.AssertHeader(t, r, "X-API-Key", "test_key")

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		testutil.JSONResponse(t, w, map[string]interface{}{
			"documentId":    "doc_456",
			"chunksDeleted": 5,
			"success":       true,
		})
	})

	client, _ := sdk.New(sdk.Config{
		ServerURL: mock.URL,
		Auth:      sdk.AuthConfig{Mode: "apikey", APIKey: "test_key"},
	})

	result, err := client.Chunks.DeleteByDocument(context.Background(), "doc_456")
	if err != nil {
		t.Fatalf("DeleteByDocument() error = %v", err)
	}

	if result.ChunksDeleted != 5 {
		t.Errorf("expected 5 chunks deleted, got %d", result.ChunksDeleted)
	}
}

func TestChunksBulkDeleteByDocuments(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	mock.On("DELETE", "/api/chunks/by-documents", func(w http.ResponseWriter, r *http.Request) {
		testutil.AssertHeader(t, r, "X-API-Key", "test_key")
		testutil.AssertHeader(t, r, "Content-Type", "application/json")

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		testutil.JSONResponse(t, w, map[string]interface{}{
			"totalDocuments": 2,
			"totalChunks":    10,
			"results": []map[string]interface{}{
				{"documentId": "doc_1", "chunksDeleted": 5, "success": true},
				{"documentId": "doc_2", "chunksDeleted": 5, "success": true},
			},
		})
	})

	client, _ := sdk.New(sdk.Config{
		ServerURL: mock.URL,
		Auth:      sdk.AuthConfig{Mode: "apikey", APIKey: "test_key"},
	})

	result, err := client.Chunks.BulkDeleteByDocuments(context.Background(), []string{"doc_1", "doc_2"})
	if err != nil {
		t.Fatalf("BulkDeleteByDocuments() error = %v", err)
	}

	if result.TotalChunks != 10 {
		t.Errorf("expected 10 total chunks, got %d", result.TotalChunks)
	}
}
