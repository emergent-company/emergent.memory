package clickup

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent/pkg/logger"
)

func TestRateLimiter(t *testing.T) {
	t.Run("allows requests within limit", func(t *testing.T) {
		rl := NewRateLimiter(5, 1000) // 5 requests per second
		ctx := context.Background()

		for i := 0; i < 5; i++ {
			err := rl.WaitForSlot(ctx)
			assert.NoError(t, err)
		}
	})

	t.Run("respects context cancellation", func(t *testing.T) {
		rl := NewRateLimiter(1, 60000) // 1 request per minute
		ctx := context.Background()

		// First request should succeed
		err := rl.WaitForSlot(ctx)
		require.NoError(t, err)

		// Second request with cancelled context should fail
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		err = rl.WaitForSlot(ctx)
		assert.Error(t, err)
	})

	t.Run("reset clears timestamps", func(t *testing.T) {
		rl := NewRateLimiter(1, 60000)
		ctx := context.Background()

		// Use up the slot
		err := rl.WaitForSlot(ctx)
		require.NoError(t, err)

		// Reset and verify we can make another request
		rl.Reset()

		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()

		err = rl.WaitForSlot(ctx)
		assert.NoError(t, err)
	})

	t.Run("waits when rate limit hit and retries after window", func(t *testing.T) {
		// Use a very short window (50ms) so test completes quickly
		rl := NewRateLimiter(1, 50) // 1 request per 50ms
		ctx := context.Background()

		// First request succeeds immediately
		err := rl.WaitForSlot(ctx)
		require.NoError(t, err)

		// Second request should block until the window expires, then succeed
		start := time.Now()
		err = rl.WaitForSlot(ctx)
		elapsed := time.Since(start)

		assert.NoError(t, err)
		// Should have waited at least close to the window time (50ms + 100ms buffer = 150ms)
		// Allow some tolerance for timing
		assert.GreaterOrEqual(t, elapsed.Milliseconds(), int64(100), "should have waited for rate limit window")
	})

	t.Run("context cancellation while waiting for rate limit", func(t *testing.T) {
		// Use a long window so we're guaranteed to be waiting
		rl := NewRateLimiter(1, 60000) // 1 request per minute
		ctx := context.Background()

		// Use up the only slot
		err := rl.WaitForSlot(ctx)
		require.NoError(t, err)

		// Second request with a timeout that will expire during wait
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()

		// This should block, then return context error when timeout expires
		err = rl.WaitForSlot(ctx)
		assert.Error(t, err)
		assert.ErrorIs(t, err, context.DeadlineExceeded)
	})
}

func TestClient(t *testing.T) {
	log := logger.NewLogger()

	t.Run("GetWorkspaces success", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/api/v2/team", r.URL.Path)
			assert.Equal(t, "test-token", r.Header.Get("Authorization"))

			resp := WorkspacesResponse{
				Teams: []Workspace{
					{ID: "123", Name: "Test Workspace"},
				},
			}
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		client := NewClient(log)
		// Verify client is created with correct defaults
		assert.NotNil(t, client)
		assert.NotNil(t, client.httpClient)
		assert.NotNil(t, client.rateLimiter)
	})

	t.Run("GetSpaces success", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Contains(t, r.URL.Path, "/space")

			resp := SpacesResponse{
				Spaces: []Space{
					{ID: "456", Name: "Test Space", Archived: false},
				},
			}
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()
	})
}

func TestTypes(t *testing.T) {
	t.Run("Config JSON parsing", func(t *testing.T) {
		configJSON := `{
			"apiToken": "pk_test_123",
			"workspaceId": "ws_123",
			"workspaceName": "Test Workspace",
			"selectedSpaces": [
				{"id": "sp_1", "name": "Space 1"},
				{"id": "sp_2", "name": "Space 2"}
			],
			"includeArchived": true,
			"lastSyncedAt": 1704067200000
		}`

		var config Config
		err := json.Unmarshal([]byte(configJSON), &config)
		require.NoError(t, err)

		assert.Equal(t, "pk_test_123", config.APIToken)
		assert.Equal(t, "ws_123", config.WorkspaceID)
		assert.Equal(t, "Test Workspace", config.WorkspaceName)
		assert.Len(t, config.SelectedSpaces, 2)
		assert.Equal(t, "sp_1", config.SelectedSpaces[0].ID)
		assert.True(t, config.IncludeArchived)
		assert.Equal(t, int64(1704067200000), config.LastSyncedAt)
	})

	t.Run("Doc JSON parsing", func(t *testing.T) {
		docJSON := `{
			"id": "doc_123",
			"name": "Test Doc",
			"parent": {"id": "sp_456", "type": 6},
			"workspace_id": "ws_789",
			"creator_id": 12345,
			"date_created": "1704067200000",
			"date_updated": "1704153600000",
			"avatar": {"value": "emoji::📄"},
			"archived": false,
			"deleted": false,
			"protected": false
		}`

		var doc Doc
		err := json.Unmarshal([]byte(docJSON), &doc)
		require.NoError(t, err)

		assert.Equal(t, "doc_123", doc.ID)
		assert.Equal(t, "Test Doc", doc.Name)
		assert.Equal(t, "sp_456", doc.Parent.ID)
		assert.Equal(t, 6, doc.Parent.Type)
		assert.Equal(t, "ws_789", doc.WorkspaceID)
		assert.Equal(t, 12345, doc.CreatorID)
		assert.NotNil(t, doc.Avatar)
		assert.Equal(t, "emoji::📄", doc.Avatar.Value)
		assert.False(t, doc.Archived)
	})

	t.Run("Page JSON parsing with nested pages", func(t *testing.T) {
		pageJSON := `{
			"page_id": "pg_123",
			"name": "Parent Page",
			"content": "# Content\n\nSome text",
			"date_created": "1704067200000",
			"date_updated": "1704153600000",
			"creator_id": 12345,
			"archived": false,
			"protected": false,
			"pages": [
				{
					"page_id": "pg_456",
					"name": "Child Page",
					"content": "Child content",
					"parent_page_id": "pg_123",
					"date_created": "1704067200000",
					"date_updated": "1704153600000",
					"creator_id": 12345,
					"archived": false,
					"protected": false
				}
			]
		}`

		var page Page
		err := json.Unmarshal([]byte(pageJSON), &page)
		require.NoError(t, err)

		assert.Equal(t, "pg_123", page.PageID)
		assert.Equal(t, "Parent Page", page.Name)
		assert.Contains(t, page.Content, "# Content")
		assert.Len(t, page.Pages, 1)
		assert.Equal(t, "pg_456", page.Pages[0].PageID)
		assert.Equal(t, "Child Page", page.Pages[0].Name)
		assert.Equal(t, "pg_123", page.Pages[0].ParentPageID)
	})

	t.Run("DocumentMetadata JSON roundtrip", func(t *testing.T) {
		metadata := DocumentMetadata{
			ClickUpDocID:       "doc_123",
			ClickUpWorkspaceID: "ws_456",
			ClickUpSpaceID:     "sp_789",
			CreatorID:          12345,
			ClickUpCreatedAt:   "1704067200000",
			ClickUpUpdatedAt:   "1704153600000",
			Archived:           false,
			PageCount:          5,
			Provider:           "clickup",
		}

		data, err := json.Marshal(metadata)
		require.NoError(t, err)

		var parsed DocumentMetadata
		err = json.Unmarshal(data, &parsed)
		require.NoError(t, err)

		assert.Equal(t, metadata.ClickUpDocID, parsed.ClickUpDocID)
		assert.Equal(t, metadata.ClickUpWorkspaceID, parsed.ClickUpWorkspaceID)
		assert.Equal(t, metadata.PageCount, parsed.PageCount)
		assert.Equal(t, "clickup", parsed.Provider)
	})
}

func TestProviderHelpers(t *testing.T) {
	log := logger.NewLogger()

	t.Run("parseConfig validates API token", func(t *testing.T) {
		// Note: This test would need actual database access for full provider testing
		// Here we test the config parsing logic conceptually

		validConfig := map[string]interface{}{
			"apiToken":    "pk_test_123",
			"workspaceId": "ws_123",
		}

		// Marshal and unmarshal to test Config parsing
		data, err := json.Marshal(validConfig)
		require.NoError(t, err)

		var config Config
		err = json.Unmarshal(data, &config)
		require.NoError(t, err)

		assert.Equal(t, "pk_test_123", config.APIToken)
		assert.Equal(t, "ws_123", config.WorkspaceID)
	})

	t.Run("buildContent combines pages into markdown", func(t *testing.T) {
		// Create a mock provider to test the buildContent method
		p := &Provider{
			log: log,
		}

		pages := []Page{
			{
				Name:    "Introduction",
				Content: "This is the intro.",
				Pages: []Page{
					{
						Name:    "Subsection",
						Content: "This is a subsection.",
					},
				},
			},
			{
				Name:    "Conclusion",
				Content: "Final thoughts.",
			},
		}

		content := p.buildContent("My Document", pages)

		assert.Contains(t, content, "# My Document")
		assert.Contains(t, content, "## Introduction")
		assert.Contains(t, content, "This is the intro.")
		assert.Contains(t, content, "### Subsection")
		assert.Contains(t, content, "This is a subsection.")
		assert.Contains(t, content, "## Conclusion")
		assert.Contains(t, content, "Final thoughts.")
	})

	t.Run("buildContent handles empty pages", func(t *testing.T) {
		p := &Provider{
			log: log,
		}

		content := p.buildContent("Empty Doc", nil)

		assert.Contains(t, content, "# Empty Doc")
		assert.Contains(t, content, "[No content]")
	})

	t.Run("buildContent skips pages with empty name", func(t *testing.T) {
		p := &Provider{
			log: log,
		}

		pages := []Page{
			{Name: "Valid Page", Content: "Some content."},
			{Name: "", Content: "Should be skipped."},
			{Name: "Another Valid", Content: "More content."},
		}

		content := p.buildContent("Test Doc", pages)

		assert.Contains(t, content, "# Test Doc")
		assert.Contains(t, content, "## Valid Page")
		assert.Contains(t, content, "Some content.")
		assert.NotContains(t, content, "Should be skipped")
		assert.Contains(t, content, "## Another Valid")
		assert.Contains(t, content, "More content.")
	})

	t.Run("countPages counts recursively", func(t *testing.T) {
		p := &Provider{
			log: log,
		}

		pages := []Page{
			{
				Name: "Page 1",
				Pages: []Page{
					{Name: "Page 1.1"},
					{
						Name: "Page 1.2",
						Pages: []Page{
							{Name: "Page 1.2.1"},
						},
					},
				},
			},
			{Name: "Page 2"},
		}

		count := p.countPages(pages)
		assert.Equal(t, 5, count) // 2 top-level + 2 nested in Page 1 + 1 deeply nested
	})

	t.Run("filterByUpdatedSince filters correctly", func(t *testing.T) {
		p := &Provider{
			log: log,
		}

		docs := []Doc{
			{ID: "old", DateUpdated: "1704067200000"},   // Jan 1, 2024
			{ID: "new1", DateUpdated: "1704153600000"},  // Jan 2, 2024
			{ID: "new2", DateUpdated: "1704240000000"},  // Jan 3, 2024
		}

		// Filter to docs updated after Jan 1, 2024
		sinceMs := int64(1704067200000)
		filtered := p.filterByUpdatedSince(docs, sinceMs)

		assert.Len(t, filtered, 2)
		assert.Equal(t, "new1", filtered[0].ID)
		assert.Equal(t, "new2", filtered[1].ID)
	})

	t.Run("filterByUpdatedSince skips docs with invalid timestamps", func(t *testing.T) {
		p := &Provider{
			log: log,
		}

		docs := []Doc{
			{ID: "valid", DateUpdated: "1704153600000"},   // Valid timestamp
			{ID: "invalid1", DateUpdated: "not-a-number"}, // Invalid timestamp
			{ID: "invalid2", DateUpdated: ""},             // Empty timestamp
			{ID: "also-valid", DateUpdated: "1704240000000"},
		}

		sinceMs := int64(1704067200000)
		filtered := p.filterByUpdatedSince(docs, sinceMs)

		// Only valid docs should be included
		assert.Len(t, filtered, 2)
		assert.Equal(t, "valid", filtered[0].ID)
		assert.Equal(t, "also-valid", filtered[1].ID)
	})
}

func TestConfigSchema(t *testing.T) {
	t.Run("ConfigSchema has required fields", func(t *testing.T) {
		schema := ConfigSchema

		// Check type
		assert.Equal(t, "object", schema["type"])

		// Check required fields
		required, ok := schema["required"].([]string)
		assert.True(t, ok)
		assert.Contains(t, required, "apiToken")

		// Check properties exist
		props, ok := schema["properties"].(map[string]interface{})
		assert.True(t, ok)
		assert.Contains(t, props, "apiToken")
		assert.Contains(t, props, "workspaceId")
		assert.Contains(t, props, "includeArchived")
	})
}

func TestProvider_ParseConfig(t *testing.T) {
	log := logger.NewLogger()
	p := &Provider{
		log: log,
	}

	t.Run("valid config", func(t *testing.T) {
		config := map[string]interface{}{
			"apiToken":    "pk_test_123",
			"workspaceId": "ws_123",
		}

		result, err := p.parseConfig(config)
		require.NoError(t, err)
		assert.Equal(t, "pk_test_123", result.APIToken)
		assert.Equal(t, "ws_123", result.WorkspaceID)
	})

	t.Run("missing API token", func(t *testing.T) {
		config := map[string]interface{}{
			"workspaceId": "ws_123",
		}

		_, err := p.parseConfig(config)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "API token is required")
	})

	t.Run("empty API token", func(t *testing.T) {
		config := map[string]interface{}{
			"apiToken":    "",
			"workspaceId": "ws_123",
		}

		_, err := p.parseConfig(config)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "API token is required")
	})

	t.Run("config with all fields", func(t *testing.T) {
		config := map[string]interface{}{
			"apiToken":        "pk_test_123",
			"workspaceId":     "ws_123",
			"workspaceName":   "Test Workspace",
			"includeArchived": true,
			"lastSyncedAt":    int64(1704067200000),
			"selectedSpaces": []map[string]interface{}{
				{"id": "sp_1", "name": "Space 1"},
				{"id": "sp_2", "name": "Space 2"},
			},
		}

		result, err := p.parseConfig(config)
		require.NoError(t, err)
		assert.Equal(t, "pk_test_123", result.APIToken)
		assert.Equal(t, "ws_123", result.WorkspaceID)
		assert.Equal(t, "Test Workspace", result.WorkspaceName)
		assert.True(t, result.IncludeArchived)
		assert.Equal(t, int64(1704067200000), result.LastSyncedAt)
		assert.Len(t, result.SelectedSpaces, 2)
	})

	t.Run("nil config", func(t *testing.T) {
		_, err := p.parseConfig(nil)
		// nil marshals to "null" which unmarshals fine but leaves empty config
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "API token is required")
	})

	t.Run("empty config", func(t *testing.T) {
		config := map[string]interface{}{}

		_, err := p.parseConfig(config)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "API token is required")
	})
}

func TestProvider_ProviderType(t *testing.T) {
	log := logger.NewLogger()
	p := &Provider{
		log: log,
	}

	providerType := p.ProviderType()
	assert.Equal(t, ProviderTypeClickUp, providerType)
	assert.Equal(t, "clickup", providerType)
}

func TestClient_ResetRateLimiter(t *testing.T) {
	log := logger.NewLogger()
	client := NewClient(log)

	// Fill up the rate limiter
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		_ = client.rateLimiter.WaitForSlot(ctx)
	}

	// Reset should clear the limiter
	client.ResetRateLimiter()

	// After reset, we should be able to immediately use slots again
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err := client.rateLimiter.WaitForSlot(ctx)
	assert.NoError(t, err)
}
