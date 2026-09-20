//go:build benchmark

// Package experiments_test — memory_benchmark_test.go
//
// Benchmark suite for the Active Memory Management feature.
// Run with: go test -v -tags=benchmark -run TestMemoryBenchmark ./tests/experiments/
//
// This suite measures 8 metrics (M1–M8) and writes a JSON results file.
// Run BEFORE implementation to capture baseline, then AFTER to prove improvement.
//
// Results are written to:
//
//	/tmp/memory_benchmark_results/YYYY-MM-DD_<phase>.json
//
// Required environment variables:
//
//	MEMORY_TEST_SERVER   — URL of the Memory server
//	MEMORY_TEST_TOKEN    — API key for the Memory server
//	MEMORY_BENCHMARK_PHASE — "baseline" (default) or "post-implementation"
package experiments_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	framework "github.com/emergent-company/runlog"
)

// ─────────────────────────────────────────────────────────────────────────────
// Constants
// ─────────────────────────────────────────────────────────────────────────────

const (
	benchMCPProtocolVersion = "2025-06-18"
	benchCorpusSize         = 50  // memories to seed per test user
	benchQueryCount         = 40  // queries in the query set
	benchContradictions     = 20  // contradiction test pairs
	benchTemporalPairs      = 10  // temporal resolution pairs
	benchRecallCallCount    = 100 // recall calls for M7 latency
	benchSaveCallCount      = 50  // save calls per path for M8 latency
	benchRecallLimit        = 10  // top-K for recall calls
)

// Success gates from benchmark-design.md §7.
const (
	gateM1CoreHitRate     = 1.00 // 100%
	gateM2DedupPrecision  = 0.85 // 85%
	gateM3RecallPrecision = 0.10 // baseline + 10%
	gateM4RecencyRanking  = 0.90 // 90%
	gateM5TokenEfficiency = 0.40 // ≤ 40% of baseline
	gateM6TemporalAcc     = 0.90 // 90%
	gateM7P95AddedMs      = 50.0 // ≤ baseline + 50ms
	gateM8MergeP95Ms      = 600.0
)

// ─────────────────────────────────────────────────────────────────────────────
// Test corpus — inline definitions
// ─────────────────────────────────────────────────────────────────────────────

// benchMemory is a single corpus entry.
type benchMemory struct {
	Content    string  `json:"content"`
	Category   string  `json:"category"` // preference|pattern|correction|fact|instruction|convention
	Source     string  `json:"source"`   // explicit|inferred|corrected
	Confidence float64 `json:"confidence"`
	EventTime  string  `json:"event_time,omitempty"` // ISO 8601, optional
	ID         string  `json:"-"`                    // populated after seeding
}

// benchQuery is a query with expected relevant memory indices (into corpus).
type benchQuery struct {
	Query          string   `json:"query"`
	Intent         string   `json:"intent"` // PREFERENCE|CHRONOLOGICAL|FACTUAL|INSTRUCTIONAL
	RelevantTopics []string `json:"relevant_topics"`
}

// benchContradictionPair is a pair of memories with a known resolution action.
type benchContradictionPair struct {
	MemoryA     string `json:"memory_a"`
	MemoryB     string `json:"memory_b"`
	EventTimeA  string `json:"event_time_a,omitempty"`
	EventTimeB  string `json:"event_time_b,omitempty"`
	GroundTruth string `json:"ground_truth"` // ADD|UPDATE|DELETE_OLD_ADD_NEW|NOOP
	Rationale   string `json:"rationale"`
}

// benchTemporalPair tests that the more recent event_time ranks higher.
type benchTemporalPair struct {
	Query      string `json:"query"`
	RecentMem  string `json:"recent_memory"`
	StaleMem   string `json:"stale_memory"`
	RecentTime string `json:"recent_time"` // ISO 8601, newer
	StaleTime  string `json:"stale_time"`  // ISO 8601, older
}

// corpusMemories returns the fixed 50-memory benchmark corpus.
func corpusMemories() []benchMemory {
	recent := time.Now().UTC().AddDate(0, 0, -30).Format(time.RFC3339)
	stale := time.Now().UTC().AddDate(0, 0, -365).Format(time.RFC3339)

	return []benchMemory{
		// ── Preferences (10) ─────────────────────────────────────────────
		{Content: "User prefers TypeScript over JavaScript for all new projects", Category: "preference", Source: "explicit", Confidence: 0.9},
		{Content: "User uses single quotes in all code — never double quotes", Category: "preference", Source: "explicit", Confidence: 0.9},
		{Content: "User prefers functional programming style in TypeScript", Category: "preference", Source: "inferred", Confidence: 0.8},
		{Content: "User dislikes ORMs and prefers raw SQL with typed structs", Category: "preference", Source: "explicit", Confidence: 0.85},
		{Content: "User prefers dark mode in all IDEs and terminals", Category: "preference", Source: "inferred", Confidence: 0.7},
		{Content: "User prefers tabs over spaces for indentation", Category: "preference", Source: "explicit", Confidence: 0.9},
		{Content: "User prefers short, focused functions under 30 lines", Category: "preference", Source: "inferred", Confidence: 0.75},
		{Content: "User dislikes verbose comments — code should be self-documenting", Category: "preference", Source: "inferred", Confidence: 0.7},
		{Content: "User prefers explicit return types on all exported functions", Category: "preference", Source: "explicit", Confidence: 0.85},
		{Content: "User prefers monorepo structure with pnpm workspaces", Category: "preference", Source: "inferred", Confidence: 0.75},
		// ── Patterns (8) ─────────────────────────────────────────────────
		{Content: "User consistently writes unit tests before implementation (TDD)", Category: "pattern", Source: "inferred", Confidence: 0.8},
		{Content: "User always requests code review before merging to main", Category: "pattern", Source: "inferred", Confidence: 0.85},
		{Content: "User opens small, focused PRs rather than large feature branches", Category: "pattern", Source: "inferred", Confidence: 0.8},
		{Content: "User annotates TODOs with their GitHub username", Category: "pattern", Source: "inferred", Confidence: 0.7},
		{Content: "User runs golangci-lint before every commit", Category: "pattern", Source: "inferred", Confidence: 0.85},
		{Content: "User writes integration tests for all external API calls", Category: "pattern", Source: "inferred", Confidence: 0.8},
		{Content: "User prefers synchronous code paths for latency-sensitive paths", Category: "pattern", Source: "inferred", Confidence: 0.75},
		{Content: "User always adds context propagation to internal service calls", Category: "pattern", Source: "explicit", Confidence: 0.85},
		// ── Corrections (5) ──────────────────────────────────────────────
		{Content: "User requires apperror.Error for all API errors — never plain errors", Category: "correction", Source: "corrected", Confidence: 0.95},
		{Content: "Use context.WithTimeout — never bare context.Background in production paths", Category: "correction", Source: "corrected", Confidence: 0.95},
		{Content: "All DB queries must use FOR UPDATE SKIP LOCKED when dequeuing", Category: "correction", Source: "corrected", Confidence: 0.9},
		{Content: "Never use global state in handlers — always inject dependencies", Category: "correction", Source: "corrected", Confidence: 0.9},
		{Content: "Migrations must be idempotent — use IF NOT EXISTS everywhere", Category: "correction", Source: "corrected", Confidence: 0.9},
		// ── Facts (10) ───────────────────────────────────────────────────
		{Content: "Project uses PostgreSQL 16 with pgvector for embedding storage", Category: "fact", Source: "explicit", Confidence: 0.95},
		{Content: "API server runs on port 8080 in development", Category: "fact", Source: "explicit", Confidence: 0.9},
		{Content: "Project uses Go 1.22 with generics", Category: "fact", Source: "explicit", Confidence: 0.9},
		{Content: "Frontend is built with Next.js 15 and React 19", Category: "fact", Source: "explicit", Confidence: 0.85},
		{Content: "CI runs on GitHub Actions with Go cache enabled", Category: "fact", Source: "inferred", Confidence: 0.85, EventTime: recent},
		{Content: "User moved from React to Vue as preferred frontend framework", Category: "fact", Source: "explicit", Confidence: 0.85, EventTime: recent},
		{Content: "User previously used React as preferred frontend framework", Category: "fact", Source: "inferred", Confidence: 0.7, EventTime: stale},
		{Content: "Project uses pnpm 9 as the package manager", Category: "fact", Source: "explicit", Confidence: 0.85},
		{Content: "Redis is used for session caching with 24h TTL", Category: "fact", Source: "explicit", Confidence: 0.85},
		{Content: "Production runs on Kubernetes with 3 replicas", Category: "fact", Source: "inferred", Confidence: 0.8},
		// ── Instructions (7) ─────────────────────────────────────────────
		{Content: "Always run golangci-lint before suggesting any Go code changes", Category: "instruction", Source: "explicit", Confidence: 0.95},
		{Content: "All new endpoints must include OpenAPI annotations", Category: "instruction", Source: "explicit", Confidence: 0.9},
		{Content: "Use structured logging with slog — never fmt.Println in production code", Category: "instruction", Source: "explicit", Confidence: 0.95},
		{Content: "Wrap all database calls with the observability middleware", Category: "instruction", Source: "explicit", Confidence: 0.9},
		{Content: "Security headers must be set on all HTTP responses", Category: "instruction", Source: "explicit", Confidence: 0.9},
		{Content: "Never log PII — mask email and phone fields before logging", Category: "instruction", Source: "explicit", Confidence: 0.95},
		{Content: "All new features require a rollback plan in the PR description", Category: "instruction", Source: "explicit", Confidence: 0.85},
		// ── Conventions (10) ─────────────────────────────────────────────
		{Content: "Database service pattern: never access DB directly from HTTP handlers", Category: "convention", Source: "inferred", Confidence: 0.9},
		{Content: "Repository pattern used for all data access — no raw DB in domain layer", Category: "convention", Source: "inferred", Confidence: 0.9},
		{Content: "Error messages use lowercase and no trailing punctuation", Category: "convention", Source: "inferred", Confidence: 0.8},
		{Content: "Table names use snake_case, singular nouns", Category: "convention", Source: "inferred", Confidence: 0.85},
		{Content: "Go files in domain/ are named service.go, handler.go, repository.go", Category: "convention", Source: "inferred", Confidence: 0.85},
		{Content: "Environment variables are SCREAMING_SNAKE_CASE with APP_ prefix", Category: "convention", Source: "inferred", Confidence: 0.8},
		{Content: "All domain types are defined in domain/<name>/types.go", Category: "convention", Source: "inferred", Confidence: 0.85},
		{Content: "Integration tests use TestMain to set up a real DB instance", Category: "convention", Source: "inferred", Confidence: 0.85},
		{Content: "Feature flags are defined in config/features.go with bool fields", Category: "convention", Source: "inferred", Confidence: 0.8},
		{Content: "HTTP middleware chain: auth → rate-limit → logging → handler", Category: "convention", Source: "inferred", Confidence: 0.85},
	}
}

// querySet returns the fixed 40-query benchmark query set.
func querySet() []benchQuery {
	return []benchQuery{
		// PREFERENCE (10)
		{Query: "What coding style does the user prefer?", Intent: "PREFERENCE", RelevantTopics: []string{"functional", "TypeScript", "single quotes"}},
		{Query: "What database does the user prefer?", Intent: "PREFERENCE", RelevantTopics: []string{"SQL", "ORM", "PostgreSQL"}},
		{Query: "What editor settings does the user prefer?", Intent: "PREFERENCE", RelevantTopics: []string{"dark mode", "tabs", "spaces"}},
		{Query: "How long does the user prefer functions to be?", Intent: "PREFERENCE", RelevantTopics: []string{"function", "lines", "focused"}},
		{Query: "What is the user's opinion on comments in code?", Intent: "PREFERENCE", RelevantTopics: []string{"comment", "self-documenting"}},
		{Query: "What type system preferences does the user have?", Intent: "PREFERENCE", RelevantTopics: []string{"TypeScript", "explicit", "return types"}},
		{Query: "Does the user prefer monorepo or polyrepo?", Intent: "PREFERENCE", RelevantTopics: []string{"monorepo", "pnpm"}},
		{Query: "What is the user's preferred code review process?", Intent: "PREFERENCE", RelevantTopics: []string{"review", "PR", "merge"}},
		{Query: "How does the user prefer to structure PRs?", Intent: "PREFERENCE", RelevantTopics: []string{"small", "focused", "PR"}},
		{Query: "Does the user prefer sync or async code paths?", Intent: "PREFERENCE", RelevantTopics: []string{"synchronous", "async", "latency"}},
		// CHRONOLOGICAL (10)
		{Query: "What framework was the user using before switching?", Intent: "CHRONOLOGICAL", RelevantTopics: []string{"React", "Vue", "switched"}},
		{Query: "What is the user's current frontend framework?", Intent: "CHRONOLOGICAL", RelevantTopics: []string{"Vue", "frontend", "framework"}},
		{Query: "What CI system was recently set up?", Intent: "CHRONOLOGICAL", RelevantTopics: []string{"CI", "GitHub Actions"}},
		{Query: "What changed in the user's setup in the last few months?", Intent: "CHRONOLOGICAL", RelevantTopics: []string{"React", "Vue", "changed"}},
		{Query: "What are the most recently added project facts?", Intent: "CHRONOLOGICAL", RelevantTopics: []string{"recent", "fact"}},
		{Query: "Has the user's framework preference changed recently?", Intent: "CHRONOLOGICAL", RelevantTopics: []string{"Vue", "React", "preference"}},
		{Query: "When did the user switch from React?", Intent: "CHRONOLOGICAL", RelevantTopics: []string{"switched", "React", "Vue"}},
		{Query: "What project infrastructure changes happened lately?", Intent: "CHRONOLOGICAL", RelevantTopics: []string{"CI", "GitHub", "recent"}},
		{Query: "What framework preference did the user have a year ago?", Intent: "CHRONOLOGICAL", RelevantTopics: []string{"React", "stale", "old"}},
		{Query: "What is the user currently using for frontend development?", Intent: "CHRONOLOGICAL", RelevantTopics: []string{"Vue", "frontend", "current"}},
		// FACTUAL (10)
		{Query: "What port does the API run on?", Intent: "FACTUAL", RelevantTopics: []string{"8080", "port"}},
		{Query: "What database version is the project using?", Intent: "FACTUAL", RelevantTopics: []string{"PostgreSQL", "16", "pgvector"}},
		{Query: "What Go version does the project use?", Intent: "FACTUAL", RelevantTopics: []string{"Go", "1.22", "generics"}},
		{Query: "What package manager does the project use?", Intent: "FACTUAL", RelevantTopics: []string{"pnpm", "package manager"}},
		{Query: "What caching solution is in use?", Intent: "FACTUAL", RelevantTopics: []string{"Redis", "cache", "session"}},
		{Query: "How is the production environment set up?", Intent: "FACTUAL", RelevantTopics: []string{"Kubernetes", "replicas", "production"}},
		{Query: "What frontend framework version is in use?", Intent: "FACTUAL", RelevantTopics: []string{"Next.js", "React", "frontend"}},
		{Query: "Where does the app store embeddings?", Intent: "FACTUAL", RelevantTopics: []string{"pgvector", "PostgreSQL", "embeddings"}},
		{Query: "What CI/CD platform does the project use?", Intent: "FACTUAL", RelevantTopics: []string{"GitHub Actions", "CI"}},
		{Query: "What session TTL is configured for Redis?", Intent: "FACTUAL", RelevantTopics: []string{"Redis", "TTL", "session"}},
		// INSTRUCTIONAL (10)
		{Query: "How should I handle errors in this codebase?", Intent: "INSTRUCTIONAL", RelevantTopics: []string{"apperror", "error"}},
		{Query: "What linting checks should I run?", Intent: "INSTRUCTIONAL", RelevantTopics: []string{"golangci-lint", "lint"}},
		{Query: "How should I write log messages?", Intent: "INSTRUCTIONAL", RelevantTopics: []string{"slog", "structured", "logging"}},
		{Query: "What do I need to do before merging a PR?", Intent: "INSTRUCTIONAL", RelevantTopics: []string{"code review", "rollback", "PR"}},
		{Query: "How should I handle context in service calls?", Intent: "INSTRUCTIONAL", RelevantTopics: []string{"context", "WithTimeout"}},
		{Query: "What security requirements apply to HTTP responses?", Intent: "INSTRUCTIONAL", RelevantTopics: []string{"security headers", "HTTP"}},
		{Query: "How should I add observability to DB calls?", Intent: "INSTRUCTIONAL", RelevantTopics: []string{"observability", "middleware", "DB"}},
		{Query: "What API documentation format is required?", Intent: "INSTRUCTIONAL", RelevantTopics: []string{"OpenAPI", "annotation"}},
		{Query: "Are there any data privacy rules I need to follow?", Intent: "INSTRUCTIONAL", RelevantTopics: []string{"PII", "log", "mask"}},
		{Query: "How should new features be rolled out?", Intent: "INSTRUCTIONAL", RelevantTopics: []string{"rollback", "plan", "feature flag"}},
	}
}

// contradictionPairs returns the 20 fixed contradiction test cases.
func contradictionPairs() []benchContradictionPair {
	return []benchContradictionPair{
		// Contradictions (5) → DELETE_OLD_ADD_NEW
		{MemoryA: "User lives in New York", MemoryB: "User relocated to London from New York", EventTimeA: "2023-01-01T00:00:00Z", EventTimeB: "2026-01-01T00:00:00Z", GroundTruth: "DELETE_OLD_ADD_NEW", Rationale: "Clear location contradiction; newer event supersedes"},
		{MemoryA: "User's primary editor is Vim", MemoryB: "User switched to VS Code as primary editor", EventTimeA: "2022-06-01T00:00:00Z", EventTimeB: "2025-03-01T00:00:00Z", GroundTruth: "DELETE_OLD_ADD_NEW", Rationale: "Tool switch contradiction; newer preference supersedes"},
		{MemoryA: "User prefers React for frontend development", MemoryB: "User switched from React to Vue as preferred frontend framework", EventTimeA: "2024-01-01T00:00:00Z", EventTimeB: "2025-06-01T00:00:00Z", GroundTruth: "DELETE_OLD_ADD_NEW", Rationale: "Framework switch; newer supersedes older"},
		{MemoryA: "User works at Acme Corp", MemoryB: "User left Acme Corp and joined WidgetCo", EventTimeA: "2023-06-01T00:00:00Z", EventTimeB: "2025-12-01T00:00:00Z", GroundTruth: "DELETE_OLD_ADD_NEW", Rationale: "Employer contradiction; newer supersedes"},
		{MemoryA: "User prefers Python for scripting tasks", MemoryB: "User has moved to Go for all scripting and automation", EventTimeA: "2023-01-01T00:00:00Z", EventTimeB: "2025-10-01T00:00:00Z", GroundTruth: "DELETE_OLD_ADD_NEW", Rationale: "Language preference contradiction; newer supersedes"},
		// Updates (5) → UPDATE (additive, same subject, new detail added)
		{MemoryA: "User prefers Python", MemoryB: "User prefers Python, especially using type hints and dataclasses", GroundTruth: "UPDATE", Rationale: "Superset of original — UPDATE with expanded detail"},
		{MemoryA: "Project uses PostgreSQL", MemoryB: "Project uses PostgreSQL 16 with pgvector extension for embeddings", GroundTruth: "UPDATE", Rationale: "More specific version and extension detail — UPDATE"},
		{MemoryA: "User writes unit tests", MemoryB: "User writes unit tests before implementation using the TDD approach", GroundTruth: "UPDATE", Rationale: "More detail added — UPDATE"},
		{MemoryA: "API runs on port 8080", MemoryB: "API runs on port 8080 in development; 443 in production behind nginx", GroundTruth: "UPDATE", Rationale: "Additional context added — UPDATE"},
		{MemoryA: "User uses GitHub for version control", MemoryB: "User uses GitHub for version control with protected main branch and required PR reviews", GroundTruth: "UPDATE", Rationale: "Policy detail added — UPDATE"},
		// Distinct (5) → ADD (genuinely different facts)
		{MemoryA: "User prefers tabs over spaces", MemoryB: "User prefers dark mode in all IDEs", GroundTruth: "ADD", Rationale: "Independent preferences — both can be true simultaneously"},
		{MemoryA: "User uses Go for backend services", MemoryB: "User uses TypeScript for frontend", GroundTruth: "ADD", Rationale: "Different language domains — not contradictory"},
		{MemoryA: "Project uses Redis for caching", MemoryB: "Project uses PostgreSQL for persistent storage", GroundTruth: "ADD", Rationale: "Different storage layers — both active"},
		{MemoryA: "User writes TDD tests before implementation", MemoryB: "User requires code review before merging to main", GroundTruth: "ADD", Rationale: "Independent workflow practices"},
		{MemoryA: "API requires authentication on all endpoints", MemoryB: "API uses structured JSON error responses with apperror.Error", GroundTruth: "ADD", Rationale: "Independent API requirements"},
		// Redundant (5) → NOOP (semantically equivalent)
		{MemoryA: "User prefers functional programming style in TypeScript", MemoryB: "User likes functional programming style for TypeScript code", GroundTruth: "NOOP", Rationale: "Semantically equivalent restatement — discard duplicate"},
		{MemoryA: "Always use structured logging with slog", MemoryB: "Use slog for all logging — never fmt.Println in production", GroundTruth: "NOOP", Rationale: "Same instruction, different wording — discard"},
		{MemoryA: "Never use global state in handlers", MemoryB: "Avoid global variables in HTTP handlers; inject dependencies instead", GroundTruth: "NOOP", Rationale: "Same correction — discard duplicate"},
		{MemoryA: "Project uses pnpm as package manager", MemoryB: "pnpm is the package manager for this project", GroundTruth: "NOOP", Rationale: "Identical fact — discard"},
		{MemoryA: "User opens small, focused PRs", MemoryB: "User prefers creating small pull requests focused on one change", GroundTruth: "NOOP", Rationale: "Same pattern — discard duplicate"},
	}
}

// temporalTestPairs returns the 10 temporal ranking test pairs.
func temporalTestPairs() []benchTemporalPair {
	now := time.Now().UTC()
	recent := now.AddDate(0, 0, -30).Format(time.RFC3339)
	stale := now.AddDate(0, 0, -365).Format(time.RFC3339)

	return []benchTemporalPair{
		{Query: "What is the user's current frontend framework preference?", RecentMem: "TEMPORAL_RECENT_A: User switched to Vue as primary frontend framework", StaleMem: "TEMPORAL_STALE_A: User uses React as primary frontend framework", RecentTime: recent, StaleTime: stale},
		{Query: "Where does the user live?", RecentMem: "TEMPORAL_RECENT_B: User is based in London", StaleMem: "TEMPORAL_STALE_B: User is based in New York", RecentTime: recent, StaleTime: stale},
		{Query: "What is the user's primary code editor?", RecentMem: "TEMPORAL_RECENT_C: User's primary editor is VS Code with Vim keybindings", StaleMem: "TEMPORAL_STALE_C: User's primary editor is Vim", RecentTime: recent, StaleTime: stale},
		{Query: "What package manager does the user prefer?", RecentMem: "TEMPORAL_RECENT_D: User's preferred package manager is pnpm", StaleMem: "TEMPORAL_STALE_D: User's preferred package manager is npm", RecentTime: recent, StaleTime: stale},
		{Query: "What scripting language does the user prefer?", RecentMem: "TEMPORAL_RECENT_E: User prefers Go for all scripting tasks", StaleMem: "TEMPORAL_STALE_E: User prefers Python for scripting tasks", RecentTime: recent, StaleTime: stale},
		{Query: "What CSS framework does the user prefer?", RecentMem: "TEMPORAL_RECENT_F: User prefers Tailwind CSS for styling", StaleMem: "TEMPORAL_STALE_F: User prefers Bootstrap for CSS", RecentTime: recent, StaleTime: stale},
		{Query: "What testing framework does the user use?", RecentMem: "TEMPORAL_RECENT_G: User uses Vitest for frontend testing", StaleMem: "TEMPORAL_STALE_G: User uses Jest for frontend testing", RecentTime: recent, StaleTime: stale},
		{Query: "What deployment platform does the project use?", RecentMem: "TEMPORAL_RECENT_H: Project deploys on Kubernetes", StaleMem: "TEMPORAL_STALE_H: Project deploys on bare VMs", RecentTime: recent, StaleTime: stale},
		{Query: "What is the user's current role?", RecentMem: "TEMPORAL_RECENT_I: User is a senior software engineer", StaleMem: "TEMPORAL_STALE_I: User is a mid-level software engineer", RecentTime: recent, StaleTime: stale},
		{Query: "What is the project's primary language?", RecentMem: "TEMPORAL_RECENT_J: Project primary language is Go with TypeScript frontend", StaleMem: "TEMPORAL_STALE_J: Project primary language is Python", RecentTime: recent, StaleTime: stale},
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// MCP session helper (local — avoids importing cli_test package)
// ─────────────────────────────────────────────────────────────────────────────

type benchMCPSession struct {
	t         *testing.T
	url       string
	token     string
	projectID string
	sessionID string
}

func newBenchMCPSession(t *testing.T, srv, token, projectID string) *benchMCPSession {
	t.Helper()
	s := &benchMCPSession{
		t:         t,
		url:       srv + "/api/mcp/rpc",
		token:     token,
		projectID: projectID,
	}

	initBody, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "initialize",
		"params": map[string]any{
			"protocolVersion": benchMCPProtocolVersion,
			"clientInfo":      map[string]any{"name": "e2e-benchmark", "version": "1.0"},
			"capabilities":    map[string]any{},
			"project_id":      projectID,
		},
	})
	resp := framework.DoMCPJSON(t, s.url, token, projectID, "", benchMCPProtocolVersion, initBody)
	s.sessionID = resp.Header.Get("Mcp-Session-Id")
	body := framework.ReadBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("benchmark MCP initialize: want 200, got %d — %s", resp.StatusCode, body)
	}

	notifyBody, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "method": "notifications/initialized", "params": map[string]any{},
	})
	nr := framework.DoMCPJSON(t, s.url, token, projectID, s.sessionID, benchMCPProtocolVersion, notifyBody)
	nr.Body.Close()
	return s
}

func (s *benchMCPSession) call(toolName string, args map[string]any) (string, error) {
	body, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 2, "method": "tools/call",
		"params": map[string]any{"name": toolName, "arguments": args},
	})
	resp := framework.DoMCPJSON(s.t, s.url, s.token, s.projectID, s.sessionID, benchMCPProtocolVersion, body)
	raw := framework.ReadBody(s.t, resp)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("MCP %s: HTTP %d: %s", toolName, resp.StatusCode, raw)
	}
	var rpcResp struct {
		Result *struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
			IsError bool `json:"isError"`
		} `json:"result"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(raw), &rpcResp); err != nil {
		return "", fmt.Errorf("decode MCP %s response: %v", toolName, err)
	}
	if rpcResp.Error != nil {
		return "", fmt.Errorf("MCP %s error: %s", toolName, rpcResp.Error.Message)
	}
	if rpcResp.Result != nil && rpcResp.Result.IsError {
		text := ""
		if len(rpcResp.Result.Content) > 0 {
			text = rpcResp.Result.Content[0].Text
		}
		return "", fmt.Errorf("MCP %s tool error: %s", toolName, text)
	}
	if rpcResp.Result == nil || len(rpcResp.Result.Content) == 0 {
		return "", nil
	}
	return rpcResp.Result.Content[0].Text, nil
}

func (s *benchMCPSession) mustCall(toolName string, args map[string]any) string {
	s.t.Helper()
	result, err := s.call(toolName, args)
	if err != nil {
		s.t.Fatalf("MCP mustCall %s: %v", toolName, err)
	}
	return result
}

// timedCall invokes a tool and returns (result, latencyMs, error).
func (s *benchMCPSession) timedCall(toolName string, args map[string]any) (string, float64, error) {
	start := time.Now()
	result, err := s.call(toolName, args)
	return result, float64(time.Since(start).Milliseconds()), err
}

// ─────────────────────────────────────────────────────────────────────────────
// Corpus seeding
// ─────────────────────────────────────────────────────────────────────────────

// seedCorpus saves all 50 corpus memories and populates their IDs.
// Returns the seeded memories with IDs set.
func seedCorpus(t *testing.T, mcp *benchMCPSession, corpus []benchMemory) []benchMemory {
	t.Helper()
	seeded := make([]benchMemory, len(corpus))
	copy(seeded, corpus)

	for i, m := range seeded {
		args := map[string]any{
			"content":    m.Content,
			"category":   m.Category,
			"source":     m.Source,
			"confidence": m.Confidence,
		}
		if m.EventTime != "" {
			args["event_time"] = m.EventTime
		}
		result, err := mcp.call("save_memory", args)
		if err != nil {
			t.Logf("warn: seed memory %d failed: %v", i, err)
			continue
		}
		seeded[i].ID = parseJSONField(result, "id")
	}
	t.Logf("seeded %d corpus memories", len(seeded))
	return seeded
}

// ─────────────────────────────────────────────────────────────────────────────
// Benchmark results
// ─────────────────────────────────────────────────────────────────────────────

// benchResults holds all metric measurements.
type benchResults struct {
	RunDate  string            `json:"run_date"`
	Phase    string            `json:"phase"`
	GitHead  string            `json:"git_commit,omitempty"`
	Metrics  map[string]any    `json:"metrics"`
	Targets  map[string]any    `json:"targets"`
	Baseline map[string]any    `json:"baseline,omitempty"`
	Gates    map[string]string `json:"gate_results,omitempty"`
}

func newBenchResults(phase string) *benchResults {
	return &benchResults{
		RunDate: time.Now().UTC().Format("2006-01-02"),
		Phase:   phase,
		Metrics: make(map[string]any),
		Targets: map[string]any{
			"M1_core_hit_rate":          gateM1CoreHitRate,
			"M2_dedup_precision":        gateM2DedupPrecision,
			"M3_recall_precision_delta": gateM3RecallPrecision,
			"M4_recency_ranking":        gateM4RecencyRanking,
			"M5_token_efficiency":       gateM5TokenEfficiency,
			"M6_temporal_accuracy":      gateM6TemporalAcc,
			"M7_p95_added_ms":           gateM7P95AddedMs,
			"M8_merge_p95_ms":           gateM8MergeP95Ms,
		},
	}
}

func (r *benchResults) set(key string, value any) {
	r.Metrics[key] = value
}

func (r *benchResults) write(t *testing.T) {
	t.Helper()
	dir := "/tmp/memory_benchmark_results"
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Logf("warn: could not create results dir %s: %v", dir, err)
		return
	}
	phase := r.Phase
	if phase == "" {
		phase = "unknown"
	}
	filename := fmt.Sprintf("%s_%s.json", r.RunDate, phase)
	path := filepath.Join(dir, filename)

	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		t.Logf("warn: marshal results: %v", err)
		return
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Logf("warn: write results to %s: %v", path, err)
		return
	}
	t.Logf("benchmark results written to: %s", path)

	// Also print to log for CI visibility.
	t.Logf("=== BENCHMARK RESULTS (%s) ===\n%s", r.Phase, string(data))
}

// loadBaseline loads the most recent baseline JSON from the results dir, if any.
func loadBaseline(t *testing.T) map[string]any {
	t.Helper()
	dir := "/tmp/memory_benchmark_results"
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	// Find most recent baseline file.
	var baselineFile string
	for _, e := range entries {
		if !e.IsDir() && strings.Contains(e.Name(), "baseline") {
			// Files are YYYY-MM-DD_baseline.json — lexicographic order gives most recent last.
			baselineFile = filepath.Join(dir, e.Name())
		}
	}
	if baselineFile == "" {
		return nil
	}
	data, err := os.ReadFile(baselineFile)
	if err != nil {
		t.Logf("warn: could not read baseline file %s: %v", baselineFile, err)
		return nil
	}
	var result benchResults
	if err := json.Unmarshal(data, &result); err != nil {
		t.Logf("warn: could not parse baseline file: %v", err)
		return nil
	}
	t.Logf("loaded baseline from: %s", baselineFile)
	return result.Metrics
}

// ─────────────────────────────────────────────────────────────────────────────
// Percentile helper
// ─────────────────────────────────────────────────────────────────────────────

func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(math.Ceil(float64(len(sorted))*p/100)) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}

// ─────────────────────────────────────────────────────────────────────────────
// Schema installation
// ─────────────────────────────────────────────────────────────────────────────

func installBenchmarkSchema(t *testing.T, home, projectID string) {
	t.Helper()
	schemasOut := mustRunCLIInDirWithHome(t, "", home, "schemas", "list", "--output", "json", "--project", projectID)

	var schemas []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	_ = json.Unmarshal([]byte(schemasOut), &schemas)

	var schemaID string
	for _, s := range schemas {
		if strings.Contains(strings.ToLower(s.Name), "agent-memory") {
			schemaID = s.ID
			break
		}
	}
	if schemaID == "" {
		for _, line := range strings.Split(schemasOut, "\n") {
			if strings.Contains(line, "agent-memory") {
				parts := strings.Fields(line)
				if len(parts) > 0 {
					schemaID = parts[0]
					break
				}
			}
		}
	}
	if schemaID == "" {
		t.Skip("agent-memory schema pack not found on server — skipping memory benchmark")
	}

	mustRunCLIInDirWithHome(t, "", home, "schemas", "install", schemaID, "--project", projectID)
	t.Logf("installed agent-memory schema")
}

// ─────────────────────────────────────────────────────────────────────────────
// Main benchmark test
// ─────────────────────────────────────────────────────────────────────────────

func TestMemoryBenchmark(t *testing.T) {
	skipIfServerDown(t, nil)

	srv := serverURL()
	token := e2eTestToken()
	home := t.TempDir()

	phase := os.Getenv("MEMORY_BENCHMARK_PHASE")
	if phase == "" {
		phase = "baseline"
	}
	t.Logf("=== Memory Benchmark: phase=%s ===", phase)

	// Set up CLI authentication in the temp home directory.
	setupCLIAuth(t, home)

	// Create isolated benchmark project.
	projectName := fmt.Sprintf("benchmark-%s-%d", phase, time.Now().UnixMilli())
	projectID := benchCreateProject(t, home, srv, projectName)
	t.Cleanup(func() { benchDeleteProject(t, home, projectID, projectName) })

	installBenchmarkSchema(t, home, projectID)

	// Seed MCP session and corpus.
	mcp := newBenchMCPSession(t, srv, token, projectID)
	corpus := seedCorpus(t, mcp, corpusMemories())

	results := newBenchResults(phase)

	// Load baseline for comparison (post-implementation only).
	var baseline map[string]any
	if phase != "baseline" {
		baseline = loadBaseline(t)
		results.Baseline = baseline
	}

	// ── Run each metric ──────────────────────────────────────────────────────

	t.Run("M1_CoreMemoryHitRate", func(t *testing.T) {
		measureM1(t, mcp, results, corpus)
	})

	t.Run("M2_DedupPrecision", func(t *testing.T) {
		measureM2(t, srv, token, projectID, home, results)
	})

	t.Run("M3_RecallPrecision", func(t *testing.T) {
		measureM3(t, mcp, results)
	})

	t.Run("M4_RecencyRanking", func(t *testing.T) {
		measureM4(t, srv, token, projectID, home, results)
	})

	t.Run("M5_TokenEfficiency", func(t *testing.T) {
		measureM5(t, mcp, results)
	})

	t.Run("M6_TemporalAccuracy", func(t *testing.T) {
		measureM6(t, srv, token, projectID, home, results)
	})

	t.Run("M7_RecallLatency", func(t *testing.T) {
		measureM7(t, mcp, results)
	})

	t.Run("M8_SaveLatency", func(t *testing.T) {
		measureM8(t, srv, token, projectID, home, results)
	})

	// Write results JSON.
	results.write(t)

	// Assert gates (post-implementation only).
	if phase != "baseline" && baseline != nil {
		assertGates(t, results, baseline)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// M1 — Core Memory Hit Rate
// ─────────────────────────────────────────────────────────────────────────────

func measureM1(t *testing.T, mcp *benchMCPSession, results *benchResults, corpus []benchMemory) {
	t.Helper()

	// Find an instruction-category memory to promote.
	var memID string
	for _, m := range corpus {
		if m.Category == "instruction" && m.ID != "" {
			memID = m.ID
			break
		}
	}
	if memID == "" {
		t.Log("M1: no instruction memory with ID found — recording 0.0")
		results.set("M1_core_hit_rate", 0.0)
		return
	}

	// Attempt to promote to core tier.
	_, promoteErr := mcp.call("promote_to_core", map[string]any{"memory_id": memID})
	if promoteErr != nil {
		// promote_to_core not implemented yet (baseline expected).
		t.Logf("M1: promote_to_core unavailable (%v) — baseline=0.0", promoteErr)
		results.set("M1_core_hit_rate", 0.0)
		results.set("M1_note", "promote_to_core tool not implemented")
		return
	}

	// Verify the memory appears in list with tier=core.
	listOut := mcp.mustCall("manage_memory", map[string]any{"action": "list", "limit": 50})
	if strings.Contains(strings.ToLower(listOut), "core") {
		t.Log("M1: core tier visible in memory list — hit rate = 1.0 (injection requires chat session test)")
		results.set("M1_core_hit_rate", 1.0)
	} else {
		t.Log("M1: core tier NOT visible in memory list — hit rate = 0.0")
		results.set("M1_core_hit_rate", 0.0)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// M2 — Dedup Precision
// ─────────────────────────────────────────────────────────────────────────────

func measureM2(t *testing.T, srv, token, projectID, home string, results *benchResults) {
	t.Helper()

	pairs := contradictionPairs()
	correct := 0

	// Use a fresh project for each contradiction pair so memories don't pollute each other.
	// For speed, use the same project but clear between groups.
	// For the baseline, we measure threshold-only dedup behavior:
	// threshold handles NOOP well but fails on contradictions and merges.

	for _, pair := range pairs {
		m2proj := fmt.Sprintf("m2-dedup-%d", time.Now().UnixNano())
		projID := benchCreateProject(t, home, srv, m2proj)
		mcp := newBenchMCPSession(t, srv, token, projID)
		installBenchmarkSchema(t, home, projID)

		// Save memory A first.
		_, err := mcp.call("save_memory", map[string]any{
			"content":  pair.MemoryA,
			"category": "fact",
			"source":   "explicit",
		})
		if err != nil {
			benchDeleteProject(t, home, projID, m2proj)
			continue
		}

		// Small wait for indexing.
		time.Sleep(200 * time.Millisecond)

		// Save memory B — this is where dedup/merge should fire.
		args := map[string]any{
			"content":  pair.MemoryB,
			"category": "fact",
			"source":   "explicit",
		}
		if pair.EventTimeB != "" {
			args["event_time"] = pair.EventTimeB
		}
		_, _ = mcp.call("save_memory", args)

		time.Sleep(200 * time.Millisecond)

		// Check resulting state against ground truth.
		listOut := mcp.mustCall("manage_memory", map[string]any{"action": "list", "limit": 20})
		countA := strings.Count(listOut, pair.MemoryA[:min(30, len(pair.MemoryA))])
		countB := strings.Count(listOut, pair.MemoryB[:min(30, len(pair.MemoryB))])

		var detectedAction string
		switch {
		case countA == 0 && countB > 0:
			detectedAction = "DELETE_OLD_ADD_NEW"
		case countA > 0 && countB > 0 && (countA+countB) > 2:
			detectedAction = "ADD"
		case countA > 0 && countB == 0:
			detectedAction = "NOOP"
		default:
			detectedAction = "UPDATE" // merged into one entry
		}

		if detectedAction == pair.GroundTruth {
			correct++
		}
		t.Logf("M2: pair %q → detected=%s want=%s", pair.MemoryA[:min(30, len(pair.MemoryA))], detectedAction, pair.GroundTruth)
		benchDeleteProject(t, home, projID, m2proj)
	}

	precision := float64(correct) / float64(len(pairs))
	t.Logf("M2 dedup precision: %d/%d = %.2f", correct, len(pairs), precision)
	results.set("M2_dedup_precision", precision)
	results.set("M2_correct", correct)
	results.set("M2_total", len(pairs))
}

// ─────────────────────────────────────────────────────────────────────────────
// M3 — Recall Precision@K
// ─────────────────────────────────────────────────────────────────────────────

func measureM3(t *testing.T, mcp *benchMCPSession, results *benchResults) {
	t.Helper()

	queries := querySet()
	precisionPerIntent := map[string][]float64{}
	var allPrecisions []float64

	for _, q := range queries {
		recallOut, err := mcp.call("recall_memories", map[string]any{
			"query": q.Query,
			"limit": benchRecallLimit,
		})
		if err != nil {
			t.Logf("M3: recall failed for query %q: %v", q.Query, err)
			continue
		}

		// Score relevance: check how many relevant topics appear in the top results.
		// This is a simple proxy for LLM-as-judge (which requires an actual LLM call).
		// For each topic, check if it appears in the recall output.
		hits := 0
		for _, topic := range q.RelevantTopics {
			if strings.Contains(strings.ToLower(recallOut), strings.ToLower(topic)) {
				hits++
			}
		}
		topicPrecision := float64(hits) / float64(len(q.RelevantTopics))
		precisionPerIntent[q.Intent] = append(precisionPerIntent[q.Intent], topicPrecision)
		allPrecisions = append(allPrecisions, topicPrecision)
	}

	// Compute averages.
	total := 0.0
	for _, p := range allPrecisions {
		total += p
	}
	avg := 0.0
	if len(allPrecisions) > 0 {
		avg = total / float64(len(allPrecisions))
	}

	results.set("M3_recall_precision_avg", avg)
	for intent, precisions := range precisionPerIntent {
		sum := 0.0
		for _, p := range precisions {
			sum += p
		}
		intentAvg := sum / float64(len(precisions))
		results.set("M3_recall_precision_"+strings.ToLower(intent), intentAvg)
		t.Logf("M3 precision [%s]: %.2f", intent, intentAvg)
	}
	t.Logf("M3 recall precision avg: %.2f", avg)
}

// ─────────────────────────────────────────────────────────────────────────────
// M4 — Recency Ranking Score
// ─────────────────────────────────────────────────────────────────────────────

func measureM4(t *testing.T, srv, token, projectID, home string, results *benchResults) {
	t.Helper()

	pairs := temporalTestPairs()
	correct := 0

	for i, pair := range pairs {
		// Use a fresh project per pair for isolation.
		projName := fmt.Sprintf("m4-recency-%d-%d", i, time.Now().UnixNano())
		projID := benchCreateProject(t, home, srv, projName)
		mcp := newBenchMCPSession(t, srv, token, projID)
		installBenchmarkSchema(t, home, projID)

		// Save stale memory first.
		_, _ = mcp.call("save_memory", map[string]any{
			"content":    pair.StaleMem,
			"category":   "fact",
			"source":     "inferred",
			"event_time": pair.StaleTime,
		})
		// Save recent memory.
		_, _ = mcp.call("save_memory", map[string]any{
			"content":    pair.RecentMem,
			"category":   "fact",
			"source":     "explicit",
			"event_time": pair.RecentTime,
		})
		time.Sleep(200 * time.Millisecond)

		recallOut, err := mcp.call("recall_memories", map[string]any{
			"query": pair.Query,
			"limit": 5,
		})
		if err != nil {
			benchDeleteProject(t, home, projID, projName)
			continue
		}

		// Extract sentinel token names to check ranking by position in output.
		// TEMPORAL_RECENT_X should appear before TEMPORAL_STALE_X.
		recentSentinel := pair.RecentMem[:strings.Index(pair.RecentMem, ":")+8]
		staleSentinel := pair.StaleMem[:strings.Index(pair.StaleMem, ":")+8]

		recentIdx := strings.Index(recallOut, recentSentinel)
		staleIdx := strings.Index(recallOut, staleSentinel)

		if recentIdx != -1 && (staleIdx == -1 || recentIdx < staleIdx) {
			correct++
			t.Logf("M4 pair %d: PASS — recent ranked above stale", i)
		} else {
			t.Logf("M4 pair %d: FAIL — recent at %d, stale at %d in output", i, recentIdx, staleIdx)
		}

		benchDeleteProject(t, home, projID, projName)
	}

	score := float64(correct) / float64(len(pairs))
	t.Logf("M4 recency ranking: %d/%d = %.2f", correct, len(pairs), score)
	results.set("M4_recency_ranking", score)
	results.set("M4_correct", correct)
	results.set("M4_total", len(pairs))
}

// ─────────────────────────────────────────────────────────────────────────────
// M5 — Token Efficiency (raw count proxy)
// ─────────────────────────────────────────────────────────────────────────────

func measureM5(t *testing.T, mcp *benchMCPSession, results *benchResults) {
	t.Helper()

	// Recall all memories for a broad query — count total characters as token proxy.
	// (1 token ≈ 4 chars for English text)
	recallOut := mcp.mustCall("recall_memories", map[string]any{
		"query": "tell me everything about user preferences, facts, and instructions",
		"limit": 20,
	})

	charCount := len(recallOut)
	approxTokens := charCount / 4
	t.Logf("M5 raw recall output: %d chars ≈ %d tokens", charCount, approxTokens)

	results.set("M5_raw_recall_chars", charCount)
	results.set("M5_approx_tokens", approxTokens)
	// M5 target (token efficiency ratio) is computed post-implementation vs baseline.
	// For baseline, we just record the raw count.
	results.set("M5_token_efficiency", 1.0) // baseline = 1.0 by definition
}

// ─────────────────────────────────────────────────────────────────────────────
// M6 — Temporal Accuracy
// ─────────────────────────────────────────────────────────────────────────────

func measureM6(t *testing.T, srv, token, projectID, home string, results *benchResults) {
	t.Helper()

	// Reuse the temporal pairs but this time measure dedup/merge resolution accuracy.
	pairs := temporalTestPairs()
	correct := 0

	for i, pair := range pairs {
		projName := fmt.Sprintf("m6-temporal-%d-%d", i, time.Now().UnixNano())
		projID := benchCreateProject(t, home, srv, projName)
		mcp := newBenchMCPSession(t, srv, token, projID)
		installBenchmarkSchema(t, home, projID)

		// Save stale memory first.
		_, _ = mcp.call("save_memory", map[string]any{
			"content":    pair.StaleMem,
			"category":   "fact",
			"source":     "inferred",
			"event_time": pair.StaleTime,
		})
		time.Sleep(200 * time.Millisecond)
		// Save recent memory — LLM merge should prefer this.
		_, _ = mcp.call("save_memory", map[string]any{
			"content":    pair.RecentMem,
			"category":   "fact",
			"source":     "explicit",
			"event_time": pair.RecentTime,
		})
		time.Sleep(300 * time.Millisecond)

		// Recall and check that the recent version is the primary result.
		recallOut, err := mcp.call("recall_memories", map[string]any{
			"query": pair.Query,
			"limit": 3,
		})
		if err != nil {
			benchDeleteProject(t, home, projID, projName)
			continue
		}

		recentIdx := strings.Index(recallOut, "TEMPORAL_RECENT")
		staleIdx := strings.Index(recallOut, "TEMPORAL_STALE")

		if recentIdx != -1 && (staleIdx == -1 || recentIdx < staleIdx) {
			correct++
			t.Logf("M6 pair %d: PASS — recent version returned first", i)
		} else {
			t.Logf("M6 pair %d: FAIL — stale version at %d, recent at %d", i, staleIdx, recentIdx)
		}

		benchDeleteProject(t, home, projID, projName)
	}

	accuracy := float64(correct) / float64(len(pairs))
	t.Logf("M6 temporal accuracy: %d/%d = %.2f", correct, len(pairs), accuracy)
	results.set("M6_temporal_accuracy", accuracy)
	results.set("M6_correct", correct)
	results.set("M6_total", len(pairs))
}

// ─────────────────────────────────────────────────────────────────────────────
// M7 — Recall Latency (p50, p95)
// ─────────────────────────────────────────────────────────────────────────────

func measureM7(t *testing.T, mcp *benchMCPSession, results *benchResults) {
	t.Helper()

	queries := querySet()
	var latencies []float64

	for i := 0; i < benchRecallCallCount; i++ {
		q := queries[i%len(queries)]
		_, latencyMs, err := mcp.timedCall("recall_memories", map[string]any{
			"query": q.Query,
			"limit": benchRecallLimit,
		})
		if err != nil {
			t.Logf("M7: recall call %d failed: %v", i, err)
			continue
		}
		latencies = append(latencies, latencyMs)
	}

	if len(latencies) == 0 {
		t.Log("M7: no successful recall calls")
		return
	}

	sort.Float64s(latencies)
	p50 := percentile(latencies, 50)
	p95 := percentile(latencies, 95)
	p99 := percentile(latencies, 99)

	t.Logf("M7 recall latency (n=%d): p50=%.1fms p95=%.1fms p99=%.1fms", len(latencies), p50, p95, p99)
	results.set("M7_latency_p50_ms", p50)
	results.set("M7_latency_p95_ms", p95)
	results.set("M7_latency_p99_ms", p99)
	results.set("M7_sample_count", len(latencies))
}

// ─────────────────────────────────────────────────────────────────────────────
// M8 — save_memory Latency: fast path vs merge path
// ─────────────────────────────────────────────────────────────────────────────

func measureM8(t *testing.T, srv, token, projectID, home string, results *benchResults) {
	t.Helper()

	// Fast path: save memories with unique content that has no near-duplicates.
	mcp := newBenchMCPSession(t, srv, token, projectID)

	var fastLatencies []float64
	for i := 0; i < benchSaveCallCount; i++ {
		content := fmt.Sprintf("BENCHMARK_FAST_PATH_M8_%d: unique fact about item %d that has no duplicates in the corpus", i, i)
		_, latencyMs, err := mcp.timedCall("save_memory", map[string]any{
			"content":  content,
			"category": "fact",
			"source":   "inferred",
		})
		if err != nil {
			t.Logf("M8 fast path call %d failed: %v", i, err)
			continue
		}
		fastLatencies = append(fastLatencies, latencyMs)
	}

	// Merge path: save memories similar to corpus entries (should trigger LLM merge call).
	// Use a fresh project so we start clean.
	mergeProjectName := fmt.Sprintf("m8-merge-%d", time.Now().UnixNano())
	mergeProjID := benchCreateProject(t, home, srv, mergeProjectName)
	installBenchmarkSchema(t, home, mergeProjID)
	mergeMCP := newBenchMCPSession(t, srv, token, mergeProjID)

	// Seed a known memory first.
	_, _ = mergeMCP.call("save_memory", map[string]any{
		"content":  "User prefers TypeScript over JavaScript for all new projects",
		"category": "preference",
		"source":   "explicit",
	})
	time.Sleep(300 * time.Millisecond)

	var mergeLatencies []float64
	for i := 0; i < benchSaveCallCount; i++ {
		// Each call is similar to the seeded memory — should trigger the merge path.
		content := fmt.Sprintf("User strongly prefers TypeScript over JavaScript variant %d", i)
		_, latencyMs, err := mergeMCP.timedCall("save_memory", map[string]any{
			"content":  content,
			"category": "preference",
			"source":   "inferred",
		})
		if err != nil {
			t.Logf("M8 merge path call %d failed: %v", i, err)
			continue
		}
		mergeLatencies = append(mergeLatencies, latencyMs)
	}
	benchDeleteProject(t, home, mergeProjID, mergeProjectName)

	// Compute percentiles.
	if len(fastLatencies) > 0 {
		sort.Float64s(fastLatencies)
		p50 := percentile(fastLatencies, 50)
		p95 := percentile(fastLatencies, 95)
		t.Logf("M8 fast path latency (n=%d): p50=%.1fms p95=%.1fms", len(fastLatencies), p50, p95)
		results.set("M8_save_fast_path_p50_ms", p50)
		results.set("M8_save_fast_path_p95_ms", p95)
	}

	if len(mergeLatencies) > 0 {
		sort.Float64s(mergeLatencies)
		p50 := percentile(mergeLatencies, 50)
		p95 := percentile(mergeLatencies, 95)
		t.Logf("M8 merge path latency (n=%d): p50=%.1fms p95=%.1fms", len(mergeLatencies), p50, p95)
		results.set("M8_save_merge_path_p50_ms", p50)
		results.set("M8_save_merge_path_p95_ms", p95)
	} else {
		t.Log("M8: no merge path latencies — LLM merge not yet implemented (baseline)")
		results.set("M8_save_merge_path_p95_ms", nil)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Gate assertions (post-implementation)
// ─────────────────────────────────────────────────────────────────────────────

func assertGates(t *testing.T, results *benchResults, baseline map[string]any) {
	t.Helper()
	gates := make(map[string]string)

	check := func(name string, got, want float64, op string) {
		var pass bool
		switch op {
		case ">=":
			pass = got >= want
		case "<=":
			pass = got <= want
		case "==":
			pass = math.Abs(got-want) < 0.001
		}
		status := "PASS"
		if !pass {
			status = "FAIL"
			t.Errorf("gate %s: got %.3f, want %s %.3f", name, got, op, want)
		}
		gates[name] = fmt.Sprintf("%s (got=%.3f, want%s%.3f)", status, got, op, want)
	}

	floatMetric := func(key string) (float64, bool) {
		v, ok := results.Metrics[key]
		if !ok {
			return 0, false
		}
		switch x := v.(type) {
		case float64:
			return x, true
		case int:
			return float64(x), true
		case json.Number:
			f, err := x.Float64()
			return f, err == nil
		}
		return 0, false
	}

	baselineFloat := func(key string) (float64, bool) {
		v, ok := baseline[key]
		if !ok {
			return 0, false
		}
		switch x := v.(type) {
		case float64:
			return x, true
		case json.Number:
			f, err := x.Float64()
			return f, err == nil
		}
		return 0, false
	}

	if m1, ok := floatMetric("M1_core_hit_rate"); ok {
		check("M1_core_hit_rate", m1, gateM1CoreHitRate, ">=")
	}
	if m2, ok := floatMetric("M2_dedup_precision"); ok {
		check("M2_dedup_precision", m2, gateM2DedupPrecision, ">=")
	}
	if m4, ok := floatMetric("M4_recency_ranking"); ok {
		check("M4_recency_ranking", m4, gateM4RecencyRanking, ">=")
	}
	if m5, ok := floatMetric("M5_token_efficiency"); ok {
		check("M5_token_efficiency", m5, gateM5TokenEfficiency, "<=")
	}
	if m6, ok := floatMetric("M6_temporal_accuracy"); ok {
		check("M6_temporal_accuracy", m6, gateM6TemporalAcc, ">=")
	}

	// M3: baseline-relative assertion.
	if m3, ok := floatMetric("M3_recall_precision_avg"); ok {
		if bm3, ok := baselineFloat("M3_recall_precision_avg"); ok {
			check("M3_recall_precision_delta", m3, bm3+gateM3RecallPrecision, ">=")
		}
	}

	// M7: baseline-relative latency assertion.
	if m7, ok := floatMetric("M7_latency_p95_ms"); ok {
		if bm7, ok := baselineFloat("M7_latency_p95_ms"); ok {
			check("M7_p95_latency", m7, bm7+gateM7P95AddedMs, "<=")
		}
	}

	// M8: merge path p95 absolute gate.
	if m8, ok := floatMetric("M8_save_merge_path_p95_ms"); ok {
		check("M8_merge_path_p95", m8, gateM8MergeP95Ms, "<=")
	}

	results.Gates = gates

	t.Logf("=== Gate Results ===")
	for gate, status := range gates {
		t.Logf("  %s: %s", gate, status)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────────────────────────────────────

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// postBenchJSON sends a POST JSON request for benchmark use (no framework logging).
func postBenchJSON(t *testing.T, method, url, token string, body []byte) *http.Response {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), method, url, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request %s %s: %v", method, url, err)
	}
	return resp
}

// benchCreateProject creates a new project using the CLI with the given home dir.
func benchCreateProject(t *testing.T, home, srv, name string) string {
	t.Helper()
	args := append([]string{"projects", "create", "--name", name}, projectCreateOrgArgs()...)
	out := mustRunCLIInDirWithHome(t, "", home, args...)
	id := parseProjectID(out)
	if id == "" {
		t.Fatalf("benchCreateProject: could not parse project ID from: %s", out)
	}
	return id
}

// benchDeleteProject deletes a project via CLI. Does not call t.Fatal — safe for cleanup.
func benchDeleteProject(t *testing.T, home, projectID, name string) {
	t.Helper()
	deleteProjectViaExec(t, home, projectID, name)
}
