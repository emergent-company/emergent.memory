package rerank

import (
	"context"
	"net/http"
	"strings"
)

const cohereDefaultBase = "https://api.cohere.com/v2"

type cohereReranker struct {
	baseURL string
	apiKey  string
	model   string
	topN    int
	headers map[string]string
	client  *http.Client
}

type cohereRequest struct {
	Model     string   `json:"model"`
	Query     string   `json:"query"`
	Documents []string `json:"documents"`
	TopN      int      `json:"top_n,omitempty"`
}

type cohereResponse struct {
	Results []cohereResult `json:"results"`
}

type cohereResult struct {
	Index          int     `json:"index"`
	RelevanceScore float64 `json:"relevance_score"`
}

func (r *cohereReranker) endpoint() string {
	if strings.HasSuffix(r.baseURL, "/rerank") {
		return r.baseURL
	}
	return strings.TrimRight(r.baseURL, "/") + "/rerank"
}

func (r *cohereReranker) Rerank(ctx context.Context, query string, documents []string) ([]Result, error) {
	headers := map[string]string{"Authorization": "Bearer " + r.apiKey}
	for k, v := range r.headers {
		headers[k] = v
	}

	var resp cohereResponse
	if err := postJSON(ctx, r.client, r.endpoint(), headers, cohereRequest{
		Model:     r.model,
		Query:     query,
		Documents: documents,
		TopN:      r.topN,
	}, &resp); err != nil {
		return nil, err
	}

	results := make([]Result, 0, len(resp.Results))
	for _, rr := range resp.Results {
		results = append(results, Result{Index: rr.Index, Score: rr.RelevanceScore})
	}
	return finalize(results, r.topN), nil
}
