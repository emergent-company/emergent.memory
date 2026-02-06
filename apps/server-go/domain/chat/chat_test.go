package chat

import (
	"strings"
	"testing"
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

// Helper function
func strPtr(s string) *string {
	return &s
}
