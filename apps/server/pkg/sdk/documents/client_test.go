package documents_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk"
	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/documents"
	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/testutil"
)

func TestDocumentsList(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	fixtureDocuments := testutil.FixtureDocuments()

	mock.On("GET", "/api/documents", func(w http.ResponseWriter, r *http.Request) {
		testutil.AssertHeader(t, r, "X-API-Key", "test_key")
		testutil.AssertHeader(t, r, "X-Org-ID", "org_test")
		testutil.AssertHeader(t, r, "X-Project-ID", "proj_test")

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		testutil.JSONResponse(t, w, map[string]interface{}{
			"documents": []map[string]interface{}{
				{
					"id":        fixtureDocuments[0].ID,
					"filename":  fixtureDocuments[0].Filename,
					"mimeType":  fixtureDocuments[0].MimeType,
					"createdAt": fixtureDocuments[0].CreatedAt,
					"updatedAt": fixtureDocuments[0].UpdatedAt,
				},
			},
			"total":       1,
			"next_cursor": "cursor_123",
		})
	})

	client, _ := sdk.New(sdk.Config{
		ServerURL: mock.URL,
		Auth:      sdk.AuthConfig{Mode: "apikey", APIKey: "test_key"},
		OrgID:     "org_test",
		ProjectID: "proj_test",
	})

	result, err := client.Documents.List(context.Background(), nil)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}

	if len(result.Documents) != 1 {
		t.Errorf("expected 1 document, got %d", len(result.Documents))
	}

	if result.Documents[0].ID != fixtureDocuments[0].ID {
		t.Errorf("expected document ID %s, got %s", fixtureDocuments[0].ID, result.Documents[0].ID)
	}

	if result.NextCursor == nil || *result.NextCursor != "cursor_123" {
		t.Errorf("expected next_cursor=cursor_123, got %v", result.NextCursor)
	}

	if result.Total != 1 {
		t.Errorf("expected total=1, got %d", result.Total)
	}
}

func TestDocumentsListWithOptions(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	mock.On("GET", "/api/documents", func(w http.ResponseWriter, r *http.Request) {
		// Verify query parameters
		if limit := r.URL.Query().Get("limit"); limit != "50" {
			t.Errorf("expected limit=50, got %s", limit)
		}
		if cursor := r.URL.Query().Get("cursor"); cursor != "abc123" {
			t.Errorf("expected cursor=abc123, got %s", cursor)
		}

		testutil.AssertHeader(t, r, "X-API-Key", "test_key")

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"documents":[],"total":0}`))
	})

	client, _ := sdk.New(sdk.Config{
		ServerURL: mock.URL,
		Auth:      sdk.AuthConfig{Mode: "apikey", APIKey: "test_key"},
	})

	_, err := client.Documents.List(context.Background(), &documents.ListOptions{
		Limit:  50,
		Cursor: "abc123",
	})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
}

func TestDocumentsGet(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	fixtureDoc := testutil.FixtureDocuments()[0]

	mock.On("GET", "/api/documents/doc_123", func(w http.ResponseWriter, r *http.Request) {
		testutil.AssertHeader(t, r, "X-API-Key", "test_key")
		testutil.AssertHeader(t, r, "X-Org-ID", "org_test")
		testutil.AssertHeader(t, r, "X-Project-ID", "proj_test")

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		testutil.JSONResponse(t, w, map[string]interface{}{
			"id":        fixtureDoc.ID,
			"filename":  fixtureDoc.Filename,
			"mimeType":  fixtureDoc.MimeType,
			"createdAt": fixtureDoc.CreatedAt,
			"updatedAt": fixtureDoc.UpdatedAt,
		})
	})

	client, _ := sdk.New(sdk.Config{
		ServerURL: mock.URL,
		Auth:      sdk.AuthConfig{Mode: "apikey", APIKey: "test_key"},
		OrgID:     "org_test",
		ProjectID: "proj_test",
	})

	result, err := client.Documents.Get(context.Background(), "doc_123")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	if result.ID != fixtureDoc.ID {
		t.Errorf("expected document ID %s, got %s", fixtureDoc.ID, result.ID)
	}

	if result.Filename == nil || *result.Filename != *fixtureDoc.Filename {
		t.Errorf("expected filename %v, got %v", fixtureDoc.Filename, result.Filename)
	}
}

func TestDocumentsGetNotFound(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	mock.On("GET", "/api/documents/invalid", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":{"code":"not_found","message":"Document not found"}}`))
	})

	client, _ := sdk.New(sdk.Config{
		ServerURL: mock.URL,
		Auth:      sdk.AuthConfig{Mode: "apikey", APIKey: "test_key"},
	})

	_, err := client.Documents.Get(context.Background(), "invalid")
	if err == nil {
		t.Fatal("expected error for not found document")
	}
}

func TestDocumentsListUnauthorized(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	mock.On("GET", "/api/documents", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"code":"unauthorized","message":"Invalid API key"}}`))
	})

	client, _ := sdk.New(sdk.Config{
		ServerURL: mock.URL,
		Auth:      sdk.AuthConfig{Mode: "apikey", APIKey: "invalid_key"},
	})

	_, err := client.Documents.List(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for unauthorized request")
	}
}

func TestDocumentsListEmpty(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	mock.On("GET", "/api/documents", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"documents":[],"total":0}`))
	})

	client, _ := sdk.New(sdk.Config{
		ServerURL: mock.URL,
		Auth:      sdk.AuthConfig{Mode: "apikey", APIKey: "test_key"},
	})

	result, err := client.Documents.List(context.Background(), nil)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}

	if len(result.Documents) != 0 {
		t.Errorf("expected empty list, got %d documents", len(result.Documents))
	}
}

func TestDocumentsSetContext(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	mock.On("GET", "/api/documents", func(w http.ResponseWriter, r *http.Request) {
		testutil.AssertHeader(t, r, "X-Org-ID", "new_org")
		testutil.AssertHeader(t, r, "X-Project-ID", "new_proj")

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"documents":[],"total":0}`))
	})

	client, _ := sdk.New(sdk.Config{
		ServerURL: mock.URL,
		Auth:      sdk.AuthConfig{Mode: "apikey", APIKey: "test_key"},
		OrgID:     "old_org",
		ProjectID: "old_proj",
	})

	client.SetContext("new_org", "new_proj")

	_, err := client.Documents.List(context.Background(), nil)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
}

func TestDocumentsListFilters(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	mock.On("GET", "/api/documents", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		for key, want := range map[string]string{
			"limit":      "25",
			"cursor":     "cur_1",
			"status":     "ready",
			"q":          "report",
			"sort":       "updated",
			"order":      "asc",
			"sourceType": "drive",
		} {
			if got := q.Get(key); got != want {
				t.Errorf("query %s = %q, want %q", key, got, want)
			}
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"documents": [],
			"total": 137,
			"statusCounts": {"all":137,"processing":4,"ready":18,"extracted":112,"failed":3},
			"next_cursor": "cur_2"
		}`))
	})

	client, _ := sdk.New(sdk.Config{
		ServerURL: mock.URL,
		Auth:      sdk.AuthConfig{Mode: "apikey", APIKey: "test_key"},
	})

	result, err := client.Documents.List(context.Background(), &documents.ListOptions{
		Limit:      25,
		Cursor:     "cur_1",
		Status:     "ready",
		Query:      "report",
		Sort:       "updated",
		Order:      "asc",
		SourceType: "drive",
	})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if result.Total != 137 {
		t.Errorf("total = %d, want 137", result.Total)
	}
	if result.StatusCounts == nil {
		t.Fatal("statusCounts not decoded")
	}
	if result.StatusCounts.Processing != 4 || result.StatusCounts.Extracted != 112 || result.StatusCounts.Failed != 3 {
		t.Errorf("statusCounts = %+v", *result.StatusCounts)
	}
	if result.NextCursor == nil || *result.NextCursor != "cur_2" {
		t.Errorf("next_cursor = %v, want cur_2", result.NextCursor)
	}
}
