package rerank

import (
	"context"
	"net/http"
	"strings"
)

const dashscopeDefaultBase = "https://dashscope.aliyuncs.com"

const dashscopeRerankPath = "/api/v1/services/rerank/text-rerank/text-rerank"

type dashscopeReranker struct {
	baseURL string
	apiKey  string
	model   string
	topN    int
	headers map[string]string
	client  *http.Client
}

type dashscopeRequest struct {
	Model      string              `json:"model"`
	Input      dashscopeInput      `json:"input"`
	Parameters dashscopeParameters `json:"parameters"`
}

type dashscopeInput struct {
	Query     string   `json:"query"`
	Documents []string `json:"documents"`
}

type dashscopeParameters struct {
	TopN            int  `json:"top_n,omitempty"`
	ReturnDocuments bool `json:"return_documents"`
}

type dashscopeResponse struct {
	Output struct {
		Results []dashscopeResult `json:"results"`
	} `json:"output"`
}

type dashscopeResult struct {
	Index          int     `json:"index"`
	RelevanceScore float64 `json:"relevance_score"`
}

func (r *dashscopeReranker) endpoint() string {
	if strings.Contains(r.baseURL, "/api/v1/services/rerank") {
		return r.baseURL
	}
	return strings.TrimRight(r.baseURL, "/") + dashscopeRerankPath
}

func (r *dashscopeReranker) Rerank(ctx context.Context, query string, documents []string) ([]Result, error) {
	headers := map[string]string{"Authorization": "Bearer " + r.apiKey}
	for k, v := range r.headers {
		headers[k] = v
	}

	var resp dashscopeResponse
	if err := postJSON(ctx, r.client, r.endpoint(), headers, dashscopeRequest{
		Model: r.model,
		Input: dashscopeInput{Query: query, Documents: documents},
		Parameters: dashscopeParameters{
			TopN:            r.topN,
			ReturnDocuments: false,
		},
	}, &resp); err != nil {
		return nil, err
	}

	results := make([]Result, 0, len(resp.Output.Results))
	for _, rr := range resp.Output.Results {
		results = append(results, Result{Index: rr.Index, Score: rr.RelevanceScore})
	}
	return finalize(results, r.topN), nil
}
