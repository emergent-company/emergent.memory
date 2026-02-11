package chunks_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/emergent/emergent-core/pkg/sdk"
	"github.com/emergent/emergent-core/pkg/sdk/chunks"
	"github.com/emergent/emergent-core/pkg/sdk/testutil"
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
					"id":          "chunk_123",
					"document_id": "doc_456",
					"content":     "Sample chunk content",
					"position":    0,
					"created_at":  "2026-02-11T10:00:00Z",
				},
			},
			"meta": map[string]string{},
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
}

func TestChunksListWithFilters(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	mock.On("GET", "/api/chunks", func(w http.ResponseWriter, r *http.Request) {
		if docID := r.URL.Query().Get("document_id"); docID != "doc_456" {
			t.Errorf("expected document_id=doc_456, got %s", docID)
		}
		if limit := r.URL.Query().Get("limit"); limit != "100" {
			t.Errorf("expected limit=100, got %s", limit)
		}
		if cursor := r.URL.Query().Get("cursor"); cursor != "cursor_abc" {
			t.Errorf("expected cursor=cursor_abc, got %s", cursor)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"data":[],"meta":{}}`))
	})

	client, _ := sdk.New(sdk.Config{
		ServerURL: mock.URL,
		Auth:      sdk.AuthConfig{Mode: "apikey", APIKey: "test_key"},
	})

	_, err := client.Chunks.List(context.Background(), &chunks.ListOptions{
		DocumentID: "doc_456",
		Limit:      100,
		Cursor:     "cursor_abc",
	})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
}
