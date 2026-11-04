package rerank

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

const volcengineDefaultBase = "https://api-knowledgebase.mlp.cn-beijing.volces.com"

const volcengineRerankPath = "/api/knowledge/service/rerank"

type volcengineReranker struct {
	baseURL string
	apiKey  string
	model   string
	topN    int
	headers map[string]string
	client  *http.Client
}

type volcengineRequest struct {
	RerankModel string           `json:"rerank_model"`
	Datas       []volcengineData `json:"datas"`
	TopK        int              `json:"top_k,omitempty"`
}

type volcengineData struct {
	Query   string `json:"query"`
	Content string `json:"content"`
}

// volcengineResponse accepts both response shapes:
//
//	{"code":0,"data":{"scores":[0.12,0.82]},"message":""}
//	{"data":[0.12,0.82]}
type volcengineResponse struct {
	Code    int              `json:"code"`
	Message string           `json:"message"`
	Data    volcengineScores `json:"data"`
}

// volcengineScores decodes either a flat array of scores or an object with a
// "scores" array. Scores are positional: element i is the score of document i.
type volcengineScores struct {
	Scores []float64
}

func (s *volcengineScores) UnmarshalJSON(b []byte) error {
	var arr []float64
	if err := json.Unmarshal(b, &arr); err == nil {
		s.Scores = arr
		return nil
	}

	var obj struct {
		Scores []float64 `json:"scores"`
	}
	if err := json.Unmarshal(b, &obj); err != nil {
		return fmt.Errorf("rerank: decode volcengine data: %w", err)
	}
	s.Scores = obj.Scores
	return nil
}

func (r *volcengineReranker) endpoint() string {
	if strings.HasSuffix(r.baseURL, volcengineRerankPath) {
		return r.baseURL
	}
	return strings.TrimRight(r.baseURL, "/") + volcengineRerankPath
}

func (r *volcengineReranker) Rerank(ctx context.Context, query string, documents []string) ([]Result, error) {
	datas := make([]volcengineData, len(documents))
	for i, doc := range documents {
		datas[i] = volcengineData{Query: query, Content: doc}
	}

	headers := map[string]string{"Authorization": "Bearer " + r.apiKey}
	for k, v := range r.headers {
		headers[k] = v
	}

	var resp volcengineResponse
	if err := postJSON(ctx, r.client, r.endpoint(), headers, volcengineRequest{
		RerankModel: r.model,
		Datas:       datas,
		TopK:        r.topN,
	}, &resp); err != nil {
		return nil, err
	}

	if resp.Code != 0 {
		return nil, fmt.Errorf("rerank: volcengine code %d: %s", resp.Code, resp.Message)
	}

	results := make([]Result, 0, len(resp.Data.Scores))
	for i, score := range resp.Data.Scores {
		results = append(results, Result{Index: i, Score: score})
	}
	return finalize(results, r.topN), nil
}
