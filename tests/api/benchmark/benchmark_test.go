package benchmark

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/emergent/api-tests/client"
	"github.com/emergent/api-tests/testutil"
	"github.com/stretchr/testify/require"
)

type BenchmarkConfig struct {
	Concurrent int
	Iterations int
}

type EndpointResult struct {
	Endpoint     string  `json:"endpoint"`
	Method       string  `json:"method"`
	TotalReqs    int     `json:"total_requests"`
	SuccessReqs  int     `json:"success_requests"`
	FailedReqs   int     `json:"failed_requests"`
	MinMs        float64 `json:"min_ms"`
	MaxMs        float64 `json:"max_ms"`
	AvgMs        float64 `json:"avg_ms"`
	P50Ms        float64 `json:"p50_ms"`
	P95Ms        float64 `json:"p95_ms"`
	P99Ms        float64 `json:"p99_ms"`
	TotalMs      float64 `json:"total_ms"`
	RequestsPerS float64 `json:"requests_per_second"`
}

type BenchmarkResults struct {
	ServerType  string           `json:"server_type"`
	BaseURL     string           `json:"base_url"`
	Concurrent  int              `json:"concurrent_workers"`
	Iterations  int              `json:"iterations_per_worker"`
	Timestamp   time.Time        `json:"timestamp"`
	Endpoints   []EndpointResult `json:"endpoints"`
	TotalMs     float64          `json:"total_benchmark_ms"`
	TotalReqs   int              `json:"total_requests"`
	SuccessRate float64          `json:"success_rate_percent"`
}

func loadConfig() BenchmarkConfig {
	concurrent := 10
	iterations := 50

	if v := os.Getenv("BENCH_CONCURRENT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			concurrent = n
		}
	}
	if v := os.Getenv("BENCH_ITERATIONS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			iterations = n
		}
	}

	return BenchmarkConfig{Concurrent: concurrent, Iterations: iterations}
}

func runEndpointBenchmark(
	apiClient *client.Client,
	cfg BenchmarkConfig,
	method, path string,
	body interface{},
	opts ...client.Option,
) EndpointResult {
	var (
		successReqs int64
		failedReqs  int64
		wg          sync.WaitGroup
	)

	apiClient.ResetMetrics()
	start := time.Now()

	for w := 0; w < cfg.Concurrent; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < cfg.Iterations; i++ {
				var resp *client.Response
				var err error

				switch method {
				case "GET":
					resp, err = apiClient.GET(path, opts...)
				case "POST":
					resp, err = apiClient.POST(path, body, opts...)
				case "DELETE":
					resp, err = apiClient.DELETE(path, opts...)
				default:
					resp, err = apiClient.GET(path, opts...)
				}

				if err != nil || resp == nil || !resp.IsSuccess() {
					atomic.AddInt64(&failedReqs, 1)
				} else {
					atomic.AddInt64(&successReqs, 1)
				}
			}
		}()
	}

	wg.Wait()
	totalTime := time.Since(start)

	metrics := apiClient.Metrics()
	summary := metrics.Summary()

	return EndpointResult{
		Endpoint:     path,
		Method:       method,
		TotalReqs:    summary.TotalRequests,
		SuccessReqs:  int(successReqs),
		FailedReqs:   int(failedReqs),
		MinMs:        float64(summary.MinDuration.Microseconds()) / 1000,
		MaxMs:        float64(summary.MaxDuration.Microseconds()) / 1000,
		AvgMs:        float64(summary.AvgDuration.Microseconds()) / 1000,
		P50Ms:        float64(summary.P50Duration.Microseconds()) / 1000,
		P95Ms:        float64(summary.P95Duration.Microseconds()) / 1000,
		P99Ms:        float64(summary.P99Duration.Microseconds()) / 1000,
		TotalMs:      float64(totalTime.Milliseconds()),
		RequestsPerS: float64(summary.TotalRequests) / totalTime.Seconds(),
	}
}

func setupBenchmarkFixtures(ctx context.Context, db *testutil.DB) (userID, orgID, projectID string, err error) {
	userID = "00000000-0000-0000-0000-000000000001"
	orgID = "00000000-0000-0000-0000-000000000200"
	projectID = "00000000-0000-0000-0000-000000000100"

	var exists bool
	err = db.NewRaw(`SELECT EXISTS(SELECT 1 FROM core.user_profiles WHERE id = ?)`, userID).Scan(ctx, &exists)
	if err != nil {
		return
	}
	if !exists {
		_, err = db.NewRaw(`
			INSERT INTO core.user_profiles (id, zitadel_user_id, first_name, last_name, created_at, updated_at)
			VALUES (?, 'test-admin-user', 'Bench', 'User', NOW(), NOW())
		`, userID).Exec(ctx)
		if err != nil {
			return
		}
	}

	err = db.NewRaw(`SELECT EXISTS(SELECT 1 FROM kb.orgs WHERE id = ?)`, orgID).Scan(ctx, &exists)
	if err != nil {
		return
	}
	if !exists {
		_, err = db.NewRaw(`
			INSERT INTO kb.orgs (id, name, created_at, updated_at)
			VALUES (?, 'Benchmark Org', NOW(), NOW())
		`, orgID).Exec(ctx)
		if err != nil {
			return
		}
	}

	err = db.NewRaw(`SELECT EXISTS(SELECT 1 FROM kb.projects WHERE id = ?)`, projectID).Scan(ctx, &exists)
	if err != nil {
		return
	}
	if !exists {
		_, err = db.NewRaw(`
			INSERT INTO kb.projects (id, name, organization_id, created_at, updated_at)
			VALUES (?, 'Benchmark Project', ?, NOW(), NOW())
		`, projectID, orgID).Exec(ctx)
		if err != nil {
			return
		}
	}

	err = db.NewRaw(`SELECT EXISTS(SELECT 1 FROM kb.project_memberships WHERE project_id = ? AND user_id = ?)`, projectID, userID).Scan(ctx, &exists)
	if err != nil {
		return
	}
	if !exists {
		_, err = db.NewRaw(`
			INSERT INTO kb.project_memberships (project_id, user_id, role, created_at)
			VALUES (?, ?, 'owner', NOW())
		`, projectID, userID).Exec(ctx)
		if err != nil {
			return
		}
	}

	err = db.NewRaw(`SELECT EXISTS(SELECT 1 FROM kb.organization_memberships WHERE organization_id = ? AND user_id = ?)`, orgID, userID).Scan(ctx, &exists)
	if err != nil {
		return
	}
	if !exists {
		_, err = db.NewRaw(`
			INSERT INTO kb.organization_memberships (organization_id, user_id, role, created_at)
			VALUES (?, ?, 'admin', NOW())
		`, orgID, userID).Exec(ctx)
	}

	return
}

func TestPerformanceBenchmark(t *testing.T) {
	ctx := context.Background()
	cfg := loadConfig()
	testCfg := testutil.LoadConfig()

	apiClient := client.New(client.Config{
		BaseURL:    testCfg.BaseURL,
		ServerType: testCfg.ServerType,
	})
	tokens := apiClient.Tokens()

	resp, err := apiClient.GET("/health")
	require.NoError(t, err, "Failed to connect to server - is it running?")
	require.True(t, resp.IsSuccess(), "Server health check failed: %s", resp.BodyString())

	db, err := testutil.ConnectDB(testCfg)
	require.NoError(t, err, "Failed to connect to database")
	defer db.Close()

	_, _, projectID, err := setupBenchmarkFixtures(ctx, db)
	require.NoError(t, err, "Failed to setup fixtures")

	t.Logf("\n========================================")
	t.Logf("PERFORMANCE BENCHMARK: %s", testCfg.ServerType)
	t.Logf("URL: %s | Workers: %d | Iterations: %d", testCfg.BaseURL, cfg.Concurrent, cfg.Iterations)
	t.Logf("========================================\n")

	results := BenchmarkResults{
		ServerType: string(testCfg.ServerType),
		BaseURL:    testCfg.BaseURL,
		Concurrent: cfg.Concurrent,
		Iterations: cfg.Iterations,
		Timestamp:  time.Now(),
		Endpoints:  []EndpointResult{},
	}

	benchStart := time.Now()

	type endpointDef struct {
		name   string
		method string
		path   string
		body   interface{}
		opts   []client.Option
	}

	// Server-specific endpoint definitions
	// Go server: /api/v2/* prefix, /healthz, /ready
	// NestJS server: no prefix, /health only
	var endpoints []endpointDef

	if testCfg.ServerType == "go" {
		endpoints = []endpointDef{
			{"Health", "GET", "/health", nil, nil},
			{"Healthz", "GET", "/healthz", nil, nil},
			{"Ready", "GET", "/ready", nil, nil},
			{"List Documents", "GET", "/api/v2/documents", nil, []client.Option{
				client.WithAuth(tokens.Admin()),
				client.WithProjectID(projectID),
				client.WithQuery("limit", "10"),
			}},
			{"List Projects", "GET", "/api/projects", nil, []client.Option{
				client.WithAuth(tokens.Admin()),
			}},
		}
	} else {
		// NestJS server endpoints
		endpoints = []endpointDef{
			{"Health", "GET", "/health", nil, nil},
			{"List Documents", "GET", "/documents", nil, []client.Option{
				client.WithAuth(tokens.Admin()),
				client.WithProjectID(projectID),
				client.WithQuery("limit", "10"),
			}},
			{"List Projects", "GET", "/projects", nil, []client.Option{
				client.WithAuth(tokens.Admin()),
			}},
		}
	}

	totalReqs := 0
	totalSuccess := 0

	for _, ep := range endpoints {
		t.Logf("%-20s %s %s", ep.name, ep.method, ep.path)

		result := runEndpointBenchmark(apiClient, cfg, ep.method, ep.path, ep.body, ep.opts...)
		results.Endpoints = append(results.Endpoints, result)

		totalReqs += result.TotalReqs
		totalSuccess += result.SuccessReqs

		t.Logf("  reqs=%d ok=%d fail=%d | avg=%.2fms p50=%.2fms p95=%.2fms p99=%.2fms | %.0f req/s\n",
			result.TotalReqs, result.SuccessReqs, result.FailedReqs,
			result.AvgMs, result.P50Ms, result.P95Ms, result.P99Ms, result.RequestsPerS)
	}

	results.TotalMs = float64(time.Since(benchStart).Milliseconds())
	results.TotalReqs = totalReqs
	if totalReqs > 0 {
		results.SuccessRate = float64(totalSuccess) / float64(totalReqs) * 100
	}

	t.Logf("\n========================================")
	t.Logf("TOTAL: %dms | %d reqs | %.1f%% success", int(results.TotalMs), results.TotalReqs, results.SuccessRate)
	t.Logf("========================================\n")

	jsonOutput, _ := json.MarshalIndent(results, "", "  ")
	outputFile := fmt.Sprintf("benchmark_%s_%s.json", testCfg.ServerType, time.Now().Format("20060102_150405"))
	if err := os.WriteFile(outputFile, jsonOutput, 0644); err == nil {
		t.Logf("Results: %s", outputFile)
	}
}
