package adk

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"google.golang.org/adk/model"
	"google.golang.org/genai"
)

// TestOpenAICompatibleModel_ExtraConfigApplied verifies that a vendor extra
// field declared as a request query parameter (Azure OpenAI api_version) reaches
// the request URL, and that extra headers (a secondary secret) are attached.
func TestOpenAICompatibleModel_ExtraConfigApplied(t *testing.T) {
	var gotAPIVersion, gotAPIKey, gotSecret string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAPIVersion = r.URL.Query().Get("api-version")
		gotAPIKey = r.Header.Get("api-key")
		gotSecret = r.Header.Get("X-Secret-Key")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(makeOpenAIResponse("hello")))
	}))
	defer srv.Close()

	m := NewOpenAICompatibleModelWithAuth(
		srv.URL, "azure-key", "gpt-4o", AuthAPIKeyHeader,
		map[string]string{"api_version": "2024-10-21"},
		map[string]string{"X-Secret-Key": "sk-secondary"},
	)
	req := &model.LLMRequest{
		Contents: []*genai.Content{{Role: "user", Parts: []*genai.Part{{Text: "hi"}}}},
	}
	if _, err := collectResponse(m.GenerateContent(context.Background(), req, false)); err != nil {
		t.Fatalf("GenerateContent() error = %v", err)
	}

	if gotAPIVersion != "2024-10-21" {
		t.Errorf("request api-version = %q, want 2024-10-21", gotAPIVersion)
	}
	if gotAPIKey != "azure-key" {
		t.Errorf("request api-key header = %q, want azure-key", gotAPIKey)
	}
	if gotSecret != "sk-secondary" {
		t.Errorf("request X-Secret-Key header = %q, want sk-secondary", gotSecret)
	}
}

// TestOpenAICompatibleModel_NoExtraConfig verifies the extra-config path is
// inert when no extras are set: the URL carries no api-version and no extra
// headers are attached.
func TestOpenAICompatibleModel_NoExtraConfig(t *testing.T) {
	var gotQuery, gotSecret string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		gotSecret = r.Header.Get("X-Secret-Key")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(makeOpenAIResponse("hello")))
	}))
	defer srv.Close()

	m := NewOpenAICompatibleModel(srv.URL, "", "llama3")
	req := &model.LLMRequest{
		Contents: []*genai.Content{{Role: "user", Parts: []*genai.Part{{Text: "hi"}}}},
	}
	if _, err := collectResponse(m.GenerateContent(context.Background(), req, false)); err != nil {
		t.Fatalf("GenerateContent() error = %v", err)
	}
	if gotQuery != "" {
		t.Errorf("request query = %q, want empty", gotQuery)
	}
	if gotSecret != "" {
		t.Errorf("request X-Secret-Key = %q, want empty", gotSecret)
	}
}

// TestAppendQueryParam pins the query-append helper's behaviour, including
// preserving an existing query string.
func TestAppendQueryParam(t *testing.T) {
	tests := []struct {
		url, key, value, want string
	}{
		{"https://x/chat/completions", "api-version", "2024-10-21", "https://x/chat/completions?api-version=2024-10-21"},
		{"https://x/chat/completions?a=b", "api-version", "2024-10-21", "https://x/chat/completions?a=b&api-version=2024-10-21"},
		{"https://x/chat/completions", "api-version", "", "https://x/chat/completions"},
	}
	for _, tc := range tests {
		if got := AppendQueryParam(tc.url, tc.key, tc.value); got != tc.want {
			t.Errorf("AppendQueryParam(%q, %q, %q) = %q, want %q", tc.url, tc.key, tc.value, got, tc.want)
		}
	}
}
