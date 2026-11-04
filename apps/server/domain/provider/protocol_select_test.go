package provider

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/pkg/adk"
)

// TestProviderDefinition_ProtocolForBaseURL is the data-driven selection
// contract: a vendor whose default is openai-chat is promoted to
// openai-responses only for a declared first-party host; every relay/proxy host
// keeps the default.
func TestProviderDefinition_ProtocolForBaseURL(t *testing.T) {
	openai := definitionFor(ProviderOpenAI)
	if openai == nil {
		t.Fatal("ProviderOpenAI definition missing")
	}
	if openai.Protocol != ProtocolOpenAIChat {
		t.Fatalf("ProviderOpenAI default protocol = %q, want %q", openai.Protocol, ProtocolOpenAIChat)
	}

	cases := []struct {
		name    string
		def     *ProviderDefinition
		baseURL string
		want    Protocol
	}{
		{"first-party openai", openai, "https://api.openai.com/v1", ProtocolOpenAIResponses},
		{"first-party openai trailing slash", openai, "https://api.openai.com/v1/", ProtocolOpenAIResponses},
		{"relay host keeps chat", openai, "https://my-relay.example.com/v1", ProtocolOpenAIChat},
		{"openrouter keeps chat", openai, "https://openrouter.ai/api/v1", ProtocolOpenAIChat},
		{"localhost keeps chat", openai, "http://localhost:11434/v1", ProtocolOpenAIChat},
		{"lookalike host keeps chat", openai, "https://api.openai.com.evil.example/v1", ProtocolOpenAIChat},
		{"empty base url keeps chat", openai, "", ProtocolOpenAIChat},
		{"deepseek never responses", definitionFor(ProviderDeepSeek), "https://api.deepseek.com/v1", ProtocolOpenAIChat},
		{"nil definition", nil, "https://api.openai.com/v1", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.def.ProtocolForBaseURL(tc.baseURL); got != tc.want {
				t.Errorf("ProtocolForBaseURL(%q) = %q, want %q", tc.baseURL, got, tc.want)
			}
		})
	}
}

// TestEnvAndDBProtocolSelection_Agree pins the env/DB protocol parity contract:
// for the same resolved base URL both paths select the same protocol. A
// non-OpenAI openai-chat vendor (OpenRouter) whose base URL is overridden to a
// first-party OpenAI host must NOT be promoted to Responses, because its
// definition does not declare that host.
func TestEnvAndDBProtocolSelection_Agree(t *testing.T) {
	registry := NewRegistry()
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

	t.Run("non-openai vendor at overridden openai host keeps chat", func(t *testing.T) {
		f := adk.NewModelFactory(&config.LLMConfig{}, logger, nil, nil, nil)
		f.WithEnvVendorResolver(NewEnvVendorAdapter(registry))
		f.WithEnvLookup(func(k string) string {
			switch k {
			case "OPENROUTER_API_KEY":
				return "rk"
			case "OPENROUTER_BASE_URL":
				return "https://api.openai.com/v1"
			}
			return ""
		})

		def, err := registry.Get(ProviderOpenRouter)
		if err != nil {
			t.Fatalf("registry.Get(openrouter) error = %v", err)
		}
		wantDB := def.ProtocolForBaseURL("https://api.openai.com/v1")
		if wantDB != ProtocolOpenAIChat {
			t.Fatalf("precondition: openrouter DB protocol = %q, want %q", wantDB, ProtocolOpenAIChat)
		}

		cfg, err := f.ResolveEnvVendor(string(ProviderOpenRouter), "openai/gpt-4o")
		if err != nil {
			t.Fatalf("ResolveEnvVendor() error = %v", err)
		}
		if cfg.Protocol != string(wantDB) {
			t.Errorf("env protocol = %q, DB protocol = %q — env and DB diverge", cfg.Protocol, wantDB)
		}
		if cfg.Protocol == string(ProtocolOpenAIResponses) {
			t.Errorf("non-OpenAI openai-chat vendor promoted to Responses by an overridden host")
		}
	})

	t.Run("openai vendor at first-party host is responses on both paths", func(t *testing.T) {
		f := adk.NewModelFactory(&config.LLMConfig{
			OpenAIAPIKey:  "ok",
			OpenAIBaseURL: "https://api.openai.com/v1",
		}, logger, nil, nil, nil)

		def, err := registry.Get(ProviderOpenAI)
		if err != nil {
			t.Fatalf("registry.Get(openai) error = %v", err)
		}
		wantDB := def.ProtocolForBaseURL("https://api.openai.com/v1")
		if wantDB != ProtocolOpenAIResponses {
			t.Fatalf("precondition: openai first-party DB protocol = %q, want %q", wantDB, ProtocolOpenAIResponses)
		}

		cfg, err := f.ResolveEnvVendor(string(ProviderOpenAI), "gpt-4o")
		if err != nil {
			t.Fatalf("ResolveEnvVendor() error = %v", err)
		}
		if cfg.Protocol != string(wantDB) {
			t.Errorf("env protocol = %q, DB protocol = %q — env and DB diverge", cfg.Protocol, wantDB)
		}
	})
}

// TestOpenAIResponsesSelection_ResolvedCredential proves the selection reaches
// a resolved credential: the first-party base URL yields openai-responses and a
// relay base URL yields openai-chat, through applyDefinition.
func TestOpenAIResponsesSelection_ResolvedCredential(t *testing.T) {
	s := newBaseURLTestService()

	firstParty := s.buildTempResolvedCred(ProviderOpenAI, UpsertProviderConfigRequest{
		APIKey:  "sk-test",
		BaseURL: "https://api.openai.com/v1",
	})
	if firstParty.Protocol != ProtocolOpenAIResponses {
		t.Errorf("first-party cred Protocol = %q, want %q", firstParty.Protocol, ProtocolOpenAIResponses)
	}

	relay := s.buildTempResolvedCred(ProviderOpenAI, UpsertProviderConfigRequest{
		APIKey:  "sk-test",
		BaseURL: "https://relay.example.com/v1",
	})
	if relay.Protocol != ProtocolOpenAIChat {
		t.Errorf("relay cred Protocol = %q, want %q", relay.Protocol, ProtocolOpenAIChat)
	}
}

// TestGenerateContentForModel_ResponsesProtocol verifies the catalog
// test-connection path POSTs to /responses and decodes a Responses output array
// when the effective protocol is openai-responses.
func TestGenerateContentForModel_ResponsesProtocol(t *testing.T) {
	var (
		gotPath string
		gotBody map[string]any
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"hi there"}]}]}`)
	}))
	defer srv.Close()

	s := newBaseURLTestService()
	cred := s.buildTempResolvedCred(ProviderOpenAI, UpsertProviderConfigRequest{
		APIKey:          "sk-test",
		BaseURL:         srv.URL + "/v1",
		GenerativeModel: "gpt-5",
	})
	// The httptest host is a relay, so force the Responses protocol to exercise
	// the wire path the first-party host would take.
	cred.Protocol = ProtocolOpenAIResponses

	reply, err := s.catalog.generateContentForModel(context.Background(), ProviderOpenAI, cred, "gpt-5")
	if err != nil {
		t.Fatalf("generateContentForModel() error = %v", err)
	}
	if reply != "hi there" {
		t.Errorf("reply = %q, want %q", reply, "hi there")
	}
	if gotPath != "/v1/responses" {
		t.Errorf("path = %q, want /v1/responses", gotPath)
	}
	if gotBody["max_output_tokens"] == nil {
		t.Error("responses request missing max_output_tokens")
	}
	if gotBody["input"] == nil {
		t.Error("responses request missing input")
	}
}

// TestGenerateContentForModel_RelayKeepsChat verifies a relay OpenAI credential
// still uses Chat Completions.
func TestGenerateContentForModel_RelayKeepsChat(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"hello"}}]}`)
	}))
	defer srv.Close()

	s := newBaseURLTestService()
	cred := s.buildTempResolvedCred(ProviderOpenAI, UpsertProviderConfigRequest{
		APIKey:          "sk-test",
		BaseURL:         srv.URL + "/v1",
		GenerativeModel: "gpt-4o",
	})
	if cred.Protocol != ProtocolOpenAIChat {
		t.Fatalf("relay cred Protocol = %q, want %q", cred.Protocol, ProtocolOpenAIChat)
	}

	reply, err := s.catalog.generateContentForModel(context.Background(), ProviderOpenAI, cred, "gpt-4o")
	if err != nil {
		t.Fatalf("generateContentForModel() error = %v", err)
	}
	if reply != "hello" {
		t.Errorf("reply = %q, want %q", reply, "hello")
	}
	if gotPath != "/v1/chat/completions" {
		t.Errorf("path = %q, want /v1/chat/completions", gotPath)
	}
}

// TestGenerateContentForModel_ResponsesReasoningNotInReply verifies the
// credential-test reply is built from message content, not concatenated with
// reasoning summaries (which are not the assistant's reply). It falls back to
// the reasoning summary only when the model returned no message text.
func TestGenerateContentForModel_ResponsesReasoningNotInReply(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"output":[
			{"type":"reasoning","summary":[{"type":"summary_text","text":"pondering"}]},
			{"type":"message","role":"assistant","content":[{"type":"output_text","text":"hi"}]}
		]}`)
	}))
	defer srv.Close()

	s := newBaseURLTestService()
	cred := s.buildTempResolvedCred(ProviderOpenAI, UpsertProviderConfigRequest{
		APIKey:          "sk-test",
		BaseURL:         srv.URL + "/v1",
		GenerativeModel: "gpt-5",
	})
	cred.Protocol = ProtocolOpenAIResponses

	reply, err := s.catalog.generateContentForModel(context.Background(), ProviderOpenAI, cred, "gpt-5")
	if err != nil {
		t.Fatalf("generateContentForModel() error = %v", err)
	}
	if reply != "hi" {
		t.Errorf("reply = %q, want %q (reasoning summary must not pollute reply)", reply, "hi")
	}

	// No message text: the reasoning summary is an acceptable fallback.
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"output":[{"type":"reasoning","summary":[{"type":"summary_text","text":"pondering"}]}]}`)
	}))
	defer srv2.Close()
	cred2 := s.buildTempResolvedCred(ProviderOpenAI, UpsertProviderConfigRequest{
		APIKey:          "sk-test",
		BaseURL:         srv2.URL + "/v1",
		GenerativeModel: "gpt-5",
	})
	cred2.Protocol = ProtocolOpenAIResponses
	reply2, err := s.catalog.generateContentForModel(context.Background(), ProviderOpenAI, cred2, "gpt-5")
	if err != nil {
		t.Fatalf("generateContentForModel() error 2 = %v", err)
	}
	if reply2 != "pondering" {
		t.Errorf("reply2 = %q, want reasoning fallback %q", reply2, "pondering")
	}
}
