package rerank

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	tencentDefaultBase  = "https://lkeap.tencentcloudapi.com/"
	tencentHost         = "lkeap.tencentcloudapi.com"
	tencentService      = "lkeap"
	tencentAction       = "RunRerank"
	tencentVersion      = "2024-05-22"
	tencentDefaultModel = "lke-reranker-base"
)

type tencentReranker struct {
	baseURL   string
	secretID  string
	secretKey string
	region    string
	model     string
	topN      int
	headers   map[string]string
	client    *http.Client
}

type tencentRequest struct {
	Query string   `json:"Query"`
	Docs  []string `json:"Docs"`
	Model string   `json:"Model"`
}

type tencentResponse struct {
	Response struct {
		ScoreList []float64     `json:"ScoreList"`
		Error     *tencentError `json:"Error"`
	} `json:"Response"`
}

type tencentError struct {
	Code    string `json:"Code"`
	Message string `json:"Message"`
}

func (r *tencentReranker) Rerank(ctx context.Context, query string, documents []string) ([]Result, error) {
	model := r.model
	if model == "" {
		model = tencentDefaultModel
	}

	region := r.region
	if region == "" {
		region = "ap-guangzhou"
	}

	payload, err := json.Marshal(tencentRequest{Query: query, Docs: documents, Model: model})
	if err != nil {
		return nil, fmt.Errorf("rerank: encode tencent request: %w", err)
	}

	timestamp := time.Now().UTC().Unix()
	date := time.Unix(timestamp, 0).UTC().Format("2006-01-02")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.baseURL, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("rerank: build tencent request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Host", tencentHost)
	req.Header.Set("X-TC-Action", tencentAction)
	req.Header.Set("X-TC-Version", tencentVersion)
	req.Header.Set("X-TC-Timestamp", strconv.FormatInt(timestamp, 10))
	req.Header.Set("X-TC-Region", region)
	for k, v := range r.headers {
		req.Header.Set(k, v)
	}
	req.Header.Set("Authorization", tc3Authorization(r.secretID, r.secretKey, date, timestamp, region, payload))

	resp, err := r.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("rerank: %s: %w", r.baseURL, err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("rerank: read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("rerank: %s: status %d: %s", r.baseURL, resp.StatusCode, string(data))
	}

	var tr tencentResponse
	if err := json.Unmarshal(data, &tr); err != nil {
		return nil, fmt.Errorf("rerank: decode response: %w", err)
	}

	if tr.Response.Error != nil {
		return nil, fmt.Errorf("rerank: tencent error %s: %s", tr.Response.Error.Code, tr.Response.Error.Message)
	}

	results := make([]Result, 0, len(tr.Response.ScoreList))
	for i, score := range tr.Response.ScoreList {
		results = append(results, Result{Index: i, Score: score})
	}
	return finalize(results, r.topN), nil
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func hmacSHA256(key, data []byte) []byte {
	h := hmac.New(sha256.New, key)
	h.Write(data)
	return h.Sum(nil)
}

// signedHeaders are the headers included in the TC3 signature. content-type and
// host are required; x-tc-action is the optional third signed header.
const signedHeaders = "content-type;host;x-tc-action"

// tc3CanonicalRequest builds the TC3 canonical request. TC3 canonicalisation
// lowercases signed header values, so the signed x-tc-action value is the
// lowercased action even though the wire header keeps its canonical casing
// (X-TC-Action: RunRerank). Signing the un-lowercased value fails validation.
func tc3CanonicalRequest(payload []byte) string {
	canonicalHeaders := "content-type:application/json\n" +
		"host:" + tencentHost + "\n" +
		"x-tc-action:" + strings.ToLower(tencentAction) + "\n"

	return strings.Join([]string{
		http.MethodPost,
		"/",
		"",
		canonicalHeaders,
		signedHeaders,
		sha256Hex(payload),
	}, "\n")
}

// tc3Authorization builds the TC3-HMAC-SHA256 Authorization header for the
// TencentCloud LKEAP API.
func tc3Authorization(secretID, secretKey, date string, timestamp int64, region string, payload []byte) string {
	canonicalRequest := tc3CanonicalRequest(payload)

	credentialScope := date + "/" + tencentService + "/tc3_request"

	stringToSign := strings.Join([]string{
		"TC3-HMAC-SHA256",
		strconv.FormatInt(timestamp, 10),
		credentialScope,
		sha256Hex([]byte(canonicalRequest)),
	}, "\n")

	secretDate := hmacSHA256([]byte("TC3"+secretKey), []byte(date))
	secretService := hmacSHA256(secretDate, []byte(tencentService))
	secretSigning := hmacSHA256(secretService, []byte("tc3_request"))
	signature := hex.EncodeToString(hmacSHA256(secretSigning, []byte(stringToSign)))

	return "TC3-HMAC-SHA256 Credential=" + secretID + "/" + credentialScope +
		", SignedHeaders=" + signedHeaders +
		", Signature=" + signature
}
