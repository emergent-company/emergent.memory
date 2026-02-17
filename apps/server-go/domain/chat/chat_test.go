package chat

import (
	"strings"
	"testing"

	"github.com/emergent-company/emergent/domain/search"
)

func TestValidateCreateConversationRequest(t *testing.T) {
	tests := []struct {
		name    string
		req     *CreateConversationRequest
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid request",
			req: &CreateConversationRequest{
				Title:   "Test Conversation",
				Message: "Hello, world!",
			},
			wantErr: false,
		},
		{
			name: "empty title",
			req: &CreateConversationRequest{
				Title:   "",
				Message: "Hello",
			},
			wantErr: true,
			errMsg:  "title is required",
		},
		{
			name: "title too long",
			req: &CreateConversationRequest{
				Title:   strings.Repeat("a", 513),
				Message: "Hello",
			},
			wantErr: true,
			errMsg:  "title must be at most 512 characters",
		},
		{
			name: "empty message",
			req: &CreateConversationRequest{
				Title:   "Test",
				Message: "",
			},
			wantErr: true,
			errMsg:  "message is required",
		},
		{
			name: "message too long",
			req: &CreateConversationRequest{
				Title:   "Test",
				Message: strings.Repeat("a", 100001),
			},
			wantErr: true,
			errMsg:  "message must be at most 100000 characters",
		},
		{
			name: "valid canonicalId",
			req: &CreateConversationRequest{
				Title:       "Test",
				Message:     "Hello",
				CanonicalID: strPtr("550e8400-e29b-41d4-a716-446655440000"),
			},
			wantErr: false,
		},
		{
			name: "invalid canonicalId",
			req: &CreateConversationRequest{
				Title:       "Test",
				Message:     "Hello",
				CanonicalID: strPtr("not-a-uuid"),
			},
			wantErr: true,
			errMsg:  "invalid canonicalId format",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateCreateConversationRequest(tt.req)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				} else if !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("error = %q, want to contain %q", err.Error(), tt.errMsg)
				}
			} else if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestValidateUpdateConversationRequest(t *testing.T) {
	tests := []struct {
		name    string
		req     *UpdateConversationRequest
		wantErr bool
		errMsg  string
	}{
		{
			name:    "empty request (valid)",
			req:     &UpdateConversationRequest{},
			wantErr: false,
		},
		{
			name: "valid title",
			req: &UpdateConversationRequest{
				Title: strPtr("New Title"),
			},
			wantErr: false,
		},
		{
			name: "title too long",
			req: &UpdateConversationRequest{
				Title: strPtr(strings.Repeat("a", 513)),
			},
			wantErr: true,
			errMsg:  "title must be at most 512 characters",
		},
		{
			name: "valid draftText",
			req: &UpdateConversationRequest{
				DraftText: strPtr("Some draft"),
			},
			wantErr: false,
		},
		{
			name: "draftText too long",
			req: &UpdateConversationRequest{
				DraftText: strPtr(strings.Repeat("a", 100001)),
			},
			wantErr: true,
			errMsg:  "draftText must be at most 100000 characters",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateUpdateConversationRequest(tt.req)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				} else if !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("error = %q, want to contain %q", err.Error(), tt.errMsg)
				}
			} else if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestValidateAddMessageRequest(t *testing.T) {
	tests := []struct {
		name    string
		req     *AddMessageRequest
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid user message",
			req: &AddMessageRequest{
				Role:    RoleUser,
				Content: "Hello",
			},
			wantErr: false,
		},
		{
			name: "valid assistant message",
			req: &AddMessageRequest{
				Role:    RoleAssistant,
				Content: "Hello",
			},
			wantErr: false,
		},
		{
			name: "valid system message",
			req: &AddMessageRequest{
				Role:    RoleSystem,
				Content: "Hello",
			},
			wantErr: false,
		},
		{
			name: "invalid role",
			req: &AddMessageRequest{
				Role:    "invalid",
				Content: "Hello",
			},
			wantErr: true,
			errMsg:  "role must be one of",
		},
		{
			name: "empty role",
			req: &AddMessageRequest{
				Role:    "",
				Content: "Hello",
			},
			wantErr: true,
			errMsg:  "role must be one of",
		},
		{
			name: "empty content",
			req: &AddMessageRequest{
				Role:    RoleUser,
				Content: "",
			},
			wantErr: true,
			errMsg:  "content is required",
		},
		{
			name: "content too long",
			req: &AddMessageRequest{
				Role:    RoleUser,
				Content: strings.Repeat("a", 100001),
			},
			wantErr: true,
			errMsg:  "content must be at most 100000 characters",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateAddMessageRequest(tt.req)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				} else if !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("error = %q, want to contain %q", err.Error(), tt.errMsg)
				}
			} else if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestValidateStreamRequest(t *testing.T) {
	tests := []struct {
		name    string
		req     *StreamRequest
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid request",
			req: &StreamRequest{
				Message: "Hello",
			},
			wantErr: false,
		},
		{
			name: "empty message",
			req: &StreamRequest{
				Message: "",
			},
			wantErr: true,
			errMsg:  "message is required",
		},
		{
			name: "whitespace only message",
			req: &StreamRequest{
				Message: "   ",
			},
			wantErr: true,
			errMsg:  "message is required",
		},
		{
			name: "message too long",
			req: &StreamRequest{
				Message: strings.Repeat("a", 100001),
			},
			wantErr: true,
			errMsg:  "message must be at most 100000 characters",
		},
		{
			name: "valid conversationId",
			req: &StreamRequest{
				Message:        "Hello",
				ConversationID: strPtr("550e8400-e29b-41d4-a716-446655440000"),
			},
			wantErr: false,
		},
		{
			name: "invalid conversationId",
			req: &StreamRequest{
				Message:        "Hello",
				ConversationID: strPtr("not-a-uuid"),
			},
			wantErr: true,
			errMsg:  "invalid conversationId format",
		},
		{
			name: "valid canonicalId",
			req: &StreamRequest{
				Message:     "Hello",
				CanonicalID: strPtr("550e8400-e29b-41d4-a716-446655440000"),
			},
			wantErr: false,
		},
		{
			name: "invalid canonicalId",
			req: &StreamRequest{
				Message:     "Hello",
				CanonicalID: strPtr("not-a-uuid"),
			},
			wantErr: true,
			errMsg:  "invalid canonicalId format",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateStreamRequest(tt.req)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				} else if !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("error = %q, want to contain %q", err.Error(), tt.errMsg)
				}
			} else if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestFormatSearchContext(t *testing.T) {
	h := &Handler{} // formatSearchContext doesn't use any handler fields

	t.Run("empty results returns empty string", func(t *testing.T) {
		result := h.formatSearchContext(nil)
		if result != "" {
			t.Errorf("expected empty string, got %q", result)
		}
		result = h.formatSearchContext([]search.UnifiedSearchResultItem{})
		if result != "" {
			t.Errorf("expected empty string for empty slice, got %q", result)
		}
	})

	t.Run("relationship item formatted as triplet text", func(t *testing.T) {
		items := []search.UnifiedSearchResultItem{
			{
				Type:        search.ItemTypeRelationship,
				TripletText: "Elon Musk founded Tesla",
				Score:       0.95,
			},
		}
		result := h.formatSearchContext(items)
		expected := "- Elon Musk founded Tesla"
		if result != expected {
			t.Errorf("got %q, want %q", result, expected)
		}
	})

	t.Run("multiple relationships each on own line", func(t *testing.T) {
		items := []search.UnifiedSearchResultItem{
			{
				Type:        search.ItemTypeRelationship,
				TripletText: "Elon Musk founded Tesla",
			},
			{
				Type:        search.ItemTypeRelationship,
				TripletText: "Tesla manufactures Model 3",
			},
		}
		result := h.formatSearchContext(items)
		if !strings.Contains(result, "- Elon Musk founded Tesla") {
			t.Error("missing first relationship triplet text")
		}
		if !strings.Contains(result, "- Tesla manufactures Model 3") {
			t.Error("missing second relationship triplet text")
		}
		lines := strings.Split(result, "\n")
		if len(lines) != 2 {
			t.Errorf("expected 2 lines, got %d: %q", len(lines), result)
		}
	})

	t.Run("mixed results: graph, relationship, and text", func(t *testing.T) {
		items := []search.UnifiedSearchResultItem{
			{
				Type:       search.ItemTypeGraph,
				ObjectType: "Person",
				Key:        "Elon Musk",
				Fields:     map[string]any{"role": "CEO"},
			},
			{
				Type:        search.ItemTypeRelationship,
				TripletText: "Elon Musk founded SpaceX",
			},
			{
				Type:    search.ItemTypeText,
				Snippet: "SpaceX was founded in 2002.",
			},
		}
		result := h.formatSearchContext(items)
		lines := strings.Split(result, "\n")
		if len(lines) != 3 {
			t.Errorf("expected 3 lines, got %d: %q", len(lines), result)
		}

		// Graph line should have bold object type and key
		if !strings.Contains(lines[0], "**Person**") {
			t.Errorf("graph line missing bold ObjectType: %q", lines[0])
		}
		if !strings.Contains(lines[0], "Elon Musk") {
			t.Errorf("graph line missing key: %q", lines[0])
		}
		if !strings.Contains(lines[0], "role=CEO") {
			t.Errorf("graph line missing field: %q", lines[0])
		}

		// Relationship line should be just the triplet text
		if lines[1] != "- Elon Musk founded SpaceX" {
			t.Errorf("relationship line = %q, want %q", lines[1], "- Elon Musk founded SpaceX")
		}

		// Text line should contain snippet
		if !strings.Contains(lines[2], "SpaceX was founded in 2002.") {
			t.Errorf("text line missing snippet: %q", lines[2])
		}
	})

	t.Run("text snippet truncated at 300 chars", func(t *testing.T) {
		longSnippet := strings.Repeat("x", 400)
		items := []search.UnifiedSearchResultItem{
			{
				Type:    search.ItemTypeText,
				Snippet: longSnippet,
			},
		}
		result := h.formatSearchContext(items)
		// Should be "- " + 300 chars + "…"
		if len(result) > 310 {
			t.Errorf("text snippet not truncated, len=%d", len(result))
		}
		if !strings.HasSuffix(result, "…") {
			t.Error("truncated snippet should end with ellipsis")
		}
	})

	t.Run("relationship with empty triplet text", func(t *testing.T) {
		items := []search.UnifiedSearchResultItem{
			{
				Type:        search.ItemTypeRelationship,
				TripletText: "",
			},
		}
		result := h.formatSearchContext(items)
		if result != "- " {
			t.Errorf("expected %q, got %q", "- ", result)
		}
	})
}

// Helper function
func strPtr(s string) *string {
	return &s
}
