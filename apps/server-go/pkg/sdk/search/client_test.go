package search_test

import (
	"context"
	"io"
	"net/http"
	"testing"

	"github.com/emergent-company/emergent/apps/server-go/pkg/sdk"
	"github.com/emergent-company/emergent/apps/server-go/pkg/sdk/search"
	"github.com/emergent-company/emergent/apps/server-go/pkg/sdk/testutil"
)

func TestSearchHybrid(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	mock.On("POST", "/api/search/unified", func(w http.ResponseWriter, r *http.Request) {
		testutil.AssertHeader(t, r, "X-API-Key", "test_key")
		testutil.AssertHeader(t, r, "Content-Type", "application/json")

		body, _ := io.ReadAll(r.Body)
		if len(body) == 0 {
			t.Error("expected request body")
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		testutil.JSONResponse(t, w, map[string]interface{}{
			"data": map[string]interface{}{
				"results": []map[string]interface{}{
					{
						"document_id": "doc_123",
						"chunk_id":    "chunk_456",
						"content":     "Machine learning is a subset of AI",
						"score":       0.95,
					},
				},
				"total": 1,
			},
		})
	})

	client, _ := sdk.New(sdk.Config{
		ServerURL: mock.URL,
		Auth:      sdk.AuthConfig{Mode: "apikey", APIKey: "test_key"},
	})

	result, err := client.Search.Search(context.Background(), &search.SearchRequest{
		Query:    "machine learning",
		Strategy: "hybrid",
		Limit:    10,
	})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}

	if len(result.Results) != 1 {
		t.Errorf("expected 1 result, got %d", len(result.Results))
	}

	if result.Results[0].Score != 0.95 {
		t.Errorf("expected score 0.95, got %f", result.Results[0].Score)
	}
}

func TestSearchSemantic(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	mock.On("POST", "/api/search/unified", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		testutil.JSONResponse(t, w, map[string]interface{}{
			"data": map[string]interface{}{
				"results": []map[string]interface{}{},
				"total":   0,
			},
		})
	})

	client, _ := sdk.New(sdk.Config{
		ServerURL: mock.URL,
		Auth:      sdk.AuthConfig{Mode: "apikey", APIKey: "test_key"},
	})

	result, err := client.Search.Search(context.Background(), &search.SearchRequest{
		Query:    "neural networks",
		Strategy: "semantic",
		Limit:    20,
	})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}

	if result.Total != 0 {
		t.Errorf("expected total 0, got %d", result.Total)
	}
}

func TestSearchError(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	mock.On("POST", "/api/search/unified", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"code":"validation_error","message":"Query is required"}}`))
	})

	client, _ := sdk.New(sdk.Config{
		ServerURL: mock.URL,
		Auth:      sdk.AuthConfig{Mode: "apikey", APIKey: "test_key"},
	})

	_, err := client.Search.Search(context.Background(), &search.SearchRequest{
		Query: "",
	})
	if err == nil {
		t.Fatal("expected validation error")
	}
}
