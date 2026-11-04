// Package rerank provides vendor reranking adapters.
package rerank

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"time"
)

// API identifies a rerank vendor.
type API string

const (
	APICohere       API = "cohere"
	APIDashScope    API = "dashscope"
	APIVolcengine   API = "volcengine"
	APITencentLKEAP API = "tencent-lkeap"
)

// Result is the relevance of one input document, keyed by its original index.
type Result struct {
	Index int     `json:"index"`
	Score float64 `json:"score"`
}

// Reranker scores documents against a query. Returned results are sorted by
// score DESC (best first), Index refers to the position in the input documents
// slice, and the list is truncated to Config.TopN when TopN > 0.
type Reranker interface {
	Rerank(ctx context.Context, query string, documents []string) ([]Result, error)
}

// Config configures a rerank adapter.
type Config struct {
	API        API
	BaseURL    string            // vendor base URL; empty -> adapter default
	APIKey     string            // bearer credential
	Model      string            // rerank model name
	TopN       int               // 0 => return all (use len(documents))
	Headers    map[string]string // extra headers to attach (from provider ExtraHeaders)
	SecretID   string            // Tencent LKEAP (fallback: APIKey)
	SecretKey  string            // Tencent LKEAP secret key
	Region     string            // Tencent LKEAP region; default "ap-guangzhou"
	HTTPClient *http.Client      // optional; default http.Client{Timeout: 30s}
}

// New dispatches on cfg.API and returns the adapter. Unknown API -> error.
func New(cfg Config) (Reranker, error) {
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}

	switch cfg.API {
	case APICohere:
		base := cfg.BaseURL
		if base == "" {
			base = cohereDefaultBase
		}
		return &cohereReranker{
			baseURL: base,
			apiKey:  cfg.APIKey,
			model:   cfg.Model,
			topN:    cfg.TopN,
			headers: cfg.Headers,
			client:  client,
		}, nil
	case APIDashScope:
		base := cfg.BaseURL
		if base == "" {
			base = dashscopeDefaultBase
		}
		return &dashscopeReranker{
			baseURL: base,
			apiKey:  cfg.APIKey,
			model:   cfg.Model,
			topN:    cfg.TopN,
			headers: cfg.Headers,
			client:  client,
		}, nil
	case APIVolcengine:
		base := cfg.BaseURL
		if base == "" {
			base = volcengineDefaultBase
		}
		return &volcengineReranker{
			baseURL: base,
			apiKey:  cfg.APIKey,
			model:   cfg.Model,
			topN:    cfg.TopN,
			headers: cfg.Headers,
			client:  client,
		}, nil
	case APITencentLKEAP:
		secretID := cfg.SecretID
		if secretID == "" {
			secretID = cfg.APIKey
		}
		base := cfg.BaseURL
		if base == "" {
			base = tencentDefaultBase
		}
		return &tencentReranker{
			baseURL:   base,
			secretID:  secretID,
			secretKey: cfg.SecretKey,
			region:    cfg.Region,
			model:     cfg.Model,
			topN:      cfg.TopN,
			headers:   cfg.Headers,
			client:    client,
		}, nil
	default:
		return nil, fmt.Errorf("rerank: unknown API %q", cfg.API)
	}
}

// finalize sorts results by score DESC and truncates to topN (when > 0).
func finalize(results []Result, topN int) []Result {
	sort.Slice(results, func(i, j int) bool {
		if results[i].Score == results[j].Score {
			return results[i].Index < results[j].Index
		}
		return results[i].Score > results[j].Score
	})
	if topN > 0 && len(results) > topN {
		results = results[:topN]
	}
	return results
}

// postJSON issues a POST request with a JSON body and decodes the JSON
// response into out (when non-nil). Non-2xx responses become an error
// including status and body.
func postJSON(ctx context.Context, client *http.Client, url string, headers map[string]string, body, out any) error {
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(body); err != nil {
		return fmt.Errorf("rerank: encode request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, &buf)
	if err != nil {
		return fmt.Errorf("rerank: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("rerank: %s: %w", url, err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("rerank: read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("rerank: %s: status %d: %s", url, resp.StatusCode, string(data))
	}

	if out != nil {
		if err := json.Unmarshal(data, out); err != nil {
			return fmt.Errorf("rerank: decode response: %w", err)
		}
	}
	return nil
}
