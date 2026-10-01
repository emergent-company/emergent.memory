package chat_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk"
	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/chat"
	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/testutil"
)

func newChatClient(t *testing.T, mock *testutil.MockServer) *sdk.Client {
	t.Helper()
	client, err := sdk.New(sdk.Config{
		ServerURL: mock.URL,
		Auth:      sdk.AuthConfig{Mode: "apikey", APIKey: "test_key"},
	})
	if err != nil {
		t.Fatalf("sdk.New() error = %v", err)
	}
	return client
}

// TestListConversationsIncludeArchived asserts the includeArchived query
// parameter is forwarded when the option is set.
func TestListConversationsIncludeArchived(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	mock.On("GET", "/api/chat/conversations", func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("includeArchived"); got != "true" {
			t.Errorf("expected includeArchived=true, got %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		testutil.JSONResponse(t, w, map[string]interface{}{
			"conversations": []interface{}{},
			"total":         0,
		})
	})

	client := newChatClient(t, mock)

	_, err := client.Chat.ListConversations(context.Background(), &chat.ListConversationsOptions{
		IncludeArchived: true,
	})
	if err != nil {
		t.Fatalf("ListConversations() error = %v", err)
	}
}

// TestListConversationsExcludeArchivedByDefault asserts the includeArchived
// query parameter is absent when the option is not set, preserving the
// server's default (archived excluded) behaviour.
func TestListConversationsExcludeArchivedByDefault(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	mock.On("GET", "/api/chat/conversations", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := r.URL.Query()["includeArchived"]; ok {
			t.Errorf("includeArchived must not be sent by default, got %q", r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		testutil.JSONResponse(t, w, map[string]interface{}{
			"conversations": []interface{}{},
			"total":         0,
		})
	})

	client := newChatClient(t, mock)

	_, err := client.Chat.ListConversations(context.Background(), &chat.ListConversationsOptions{})
	if err != nil {
		t.Fatalf("ListConversations() error = %v", err)
	}
}
