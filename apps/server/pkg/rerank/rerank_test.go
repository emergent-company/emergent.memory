package rerank

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func decodeBody(t *testing.T, r *http.Request) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.NewDecoder(r.Body).Decode(&m); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	return m
}

func TestCohereReranker(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/rerank" {
			t.Errorf("path = %q, want /rerank", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("Authorization = %q, want %q", got, "Bearer test-key")
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", got)
		}
		body := decodeBody(t, r)
		if body["model"] != "rerank-english-v3.0" {
			t.Errorf("model = %v, want rerank-english-v3.0", body["model"])
		}
		if body["query"] != "hello" {
			t.Errorf("query = %v, want hello", body["query"])
		}
		wantDocs := []any{"a", "b", "c", "d"}
		if !reflect.DeepEqual(body["documents"], wantDocs) {
			t.Errorf("documents = %v, want %v", body["documents"], wantDocs)
		}
		if body["top_n"] != float64(2) {
			t.Errorf("top_n = %v, want 2", body["top_n"])
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"results":[{"index":3,"relevance_score":0.4},{"index":0,"relevance_score":0.99},{"index":2,"relevance_score":0.5}]}`)
	}))
	defer srv.Close()

	rr, err := New(Config{API: APICohere, BaseURL: srv.URL, APIKey: "test-key", Model: "rerank-english-v3.0", TopN: 2})
	if err != nil {
		t.Fatal(err)
	}
	got, err := rr.Rerank(context.Background(), "hello", []string{"a", "b", "c", "d"})
	if err != nil {
		t.Fatal(err)
	}
	want := []Result{{Index: 0, Score: 0.99}, {Index: 2, Score: 0.5}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("results = %v, want %v", got, want)
	}
}

func TestCohereOmitTopN(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := decodeBody(t, r)
		if _, ok := body["top_n"]; ok {
			t.Errorf("top_n should be omitted when TopN=0, got %v", body["top_n"])
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"results":[]}`)
	}))
	defer srv.Close()

	rr, err := New(Config{API: APICohere, BaseURL: srv.URL, APIKey: "k", Model: "m"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rr.Rerank(context.Background(), "q", []string{"a"}); err != nil {
		t.Fatal(err)
	}
}

func TestCohereNon2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, `{"error":"boom"}`)
	}))
	defer srv.Close()

	rr, err := New(Config{API: APICohere, BaseURL: srv.URL, APIKey: "k", Model: "m"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = rr.Rerank(context.Background(), "q", []string{"a"})
	if err == nil || !strings.Contains(err.Error(), "500") {
		t.Errorf("expected error containing 500, got %v", err)
	}
}

func TestDashScopeReranker(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		wantPath := "/api/v1/services/rerank/text-rerank/text-rerank"
		if r.URL.Path != wantPath {
			t.Errorf("path = %q, want %q", r.URL.Path, wantPath)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer key" {
			t.Errorf("Authorization = %q, want %q", got, "Bearer key")
		}
		body := decodeBody(t, r)
		if body["model"] != "gte-rerank-v2" {
			t.Errorf("model = %v, want gte-rerank-v2", body["model"])
		}
		input := body["input"].(map[string]any)
		if input["query"] != "q" {
			t.Errorf("input.query = %v, want q", input["query"])
		}
		if !reflect.DeepEqual(input["documents"], []any{"x", "y"}) {
			t.Errorf("input.documents = %v", input["documents"])
		}
		params := body["parameters"].(map[string]any)
		if params["top_n"] != float64(2) {
			t.Errorf("parameters.top_n = %v, want 2", params["top_n"])
		}
		if rd, ok := params["return_documents"]; !ok || rd != false {
			t.Errorf("parameters.return_documents = %v, want false", rd)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"output":{"results":[{"index":1,"relevance_score":0.93},{"index":0,"relevance_score":0.5}]},"usage":{"total_tokens":10},"request_id":"r1"}`)
	}))
	defer srv.Close()

	rr, err := New(Config{API: APIDashScope, BaseURL: srv.URL, APIKey: "key", Model: "gte-rerank-v2", TopN: 2})
	if err != nil {
		t.Fatal(err)
	}
	got, err := rr.Rerank(context.Background(), "q", []string{"x", "y"})
	if err != nil {
		t.Fatal(err)
	}
	want := []Result{{Index: 1, Score: 0.93}, {Index: 0, Score: 0.5}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("results = %v, want %v", got, want)
	}
}

func TestVolcengineRerankerScoresObject(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/knowledge/service/rerank" {
			t.Errorf("path = %q", r.URL.Path)
		}
		body := decodeBody(t, r)
		if body["rerank_model"] != "doubao-rerank" {
			t.Errorf("rerank_model = %v", body["rerank_model"])
		}
		if body["top_k"] != float64(2) {
			t.Errorf("top_k = %v, want 2", body["top_k"])
		}
		datas := body["datas"].([]any)
		d0 := datas[0].(map[string]any)
		if d0["content"] != "doc A" || d0["query"] != "q" {
			t.Errorf("datas[0] = %v", d0)
		}
		d1 := datas[1].(map[string]any)
		if d1["content"] != "doc B" || d1["query"] != "q" {
			t.Errorf("datas[1] = %v", d1)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"code":0,"data":{"scores":[0.12,0.82]},"message":""}`)
	}))
	defer srv.Close()

	rr, err := New(Config{API: APIVolcengine, BaseURL: srv.URL, APIKey: "k", Model: "doubao-rerank", TopN: 2})
	if err != nil {
		t.Fatal(err)
	}
	got, err := rr.Rerank(context.Background(), "q", []string{"doc A", "doc B"})
	if err != nil {
		t.Fatal(err)
	}
	want := []Result{{Index: 1, Score: 0.82}, {Index: 0, Score: 0.12}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("results = %v, want %v", got, want)
	}
}

func TestVolcengineRerankerScoresArray(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":[0.82,0.12]}`)
	}))
	defer srv.Close()

	rr, err := New(Config{API: APIVolcengine, BaseURL: srv.URL, APIKey: "k", Model: "m", TopN: 2})
	if err != nil {
		t.Fatal(err)
	}
	got, err := rr.Rerank(context.Background(), "q", []string{"doc A", "doc B"})
	if err != nil {
		t.Fatal(err)
	}
	want := []Result{{Index: 0, Score: 0.82}, {Index: 1, Score: 0.12}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("results = %v, want %v", got, want)
	}
}

func TestTencentReranker(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if got := r.Header.Get("X-TC-Action"); got != "RunRerank" {
			t.Errorf("X-TC-Action = %q, want RunRerank", got)
		}
		if got := r.Header.Get("X-TC-Version"); got != "2024-05-22" {
			t.Errorf("X-TC-Version = %q", got)
		}
		if got := r.Header.Get("X-TC-Region"); got != "ap-guangzhou" {
			t.Errorf("X-TC-Region = %q", got)
		}
		if got := r.Header.Get("X-TC-Timestamp"); got == "" {
			t.Error("X-TC-Timestamp missing")
		}
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "TC3-HMAC-SHA256 Credential=") {
			t.Errorf("Authorization = %q, want TC3-HMAC-SHA256 Credential= prefix", auth)
		}
		if !strings.Contains(auth, "SignedHeaders=content-type;host;x-tc-action") {
			t.Errorf("Authorization missing signed headers: %q", auth)
		}
		body := decodeBody(t, r)
		if body["Query"] != "q" {
			t.Errorf("Query = %v, want q", body["Query"])
		}
		if !reflect.DeepEqual(body["Docs"], []any{"doc A", "doc B"}) {
			t.Errorf("Docs = %v", body["Docs"])
		}
		if body["Model"] != "lke-reranker-base" {
			t.Errorf("Model = %v, want lke-reranker-base", body["Model"])
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"Response":{"ScoreList":[0.2,0.9],"Usage":{"TotalTokens":123},"RequestId":"rid"}}`)
	}))
	defer srv.Close()

	rr, err := New(Config{API: APITencentLKEAP, BaseURL: srv.URL, SecretID: "sid", SecretKey: "skey", Region: "ap-guangzhou"})
	if err != nil {
		t.Fatal(err)
	}
	got, err := rr.Rerank(context.Background(), "q", []string{"doc A", "doc B"})
	if err != nil {
		t.Fatal(err)
	}
	want := []Result{{Index: 1, Score: 0.9}, {Index: 0, Score: 0.2}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("results = %v, want %v", got, want)
	}
}

func TestTencentRerankerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"Response":{"Error":{"Code":"InvalidParameter","Message":"bad param"}}}`)
	}))
	defer srv.Close()

	rr, err := New(Config{API: APITencentLKEAP, BaseURL: srv.URL, SecretID: "sid", SecretKey: "skey"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = rr.Rerank(context.Background(), "q", []string{"a"})
	if err == nil || !strings.Contains(err.Error(), "InvalidParameter") {
		t.Errorf("expected error containing InvalidParameter, got %v", err)
	}
}

func TestTencentAPIKeyFallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if !strings.Contains(auth, "Credential=apikey/") {
			t.Errorf("Authorization = %q, want SecretID fallback to APIKey", auth)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"Response":{"ScoreList":[0.5]}}`)
	}))
	defer srv.Close()

	rr, err := New(Config{API: APITencentLKEAP, BaseURL: srv.URL, APIKey: "apikey", SecretKey: "skey"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rr.Rerank(context.Background(), "q", []string{"a"}); err != nil {
		t.Fatal(err)
	}
}

// TestTencentCanonicalRequestLowercasesSignedAction guards the TC3 rule that
// signed header values are lowercased: the wire header keeps X-TC-Action:
// RunRerank, but the canonical request must sign x-tc-action:runrerank.
func TestTencentCanonicalRequestLowercasesSignedAction(t *testing.T) {
	canonical := tc3CanonicalRequest([]byte(`{"Query":"q"}`))
	if !strings.Contains(canonical, "x-tc-action:runrerank\n") {
		t.Errorf("canonical request must sign the lowercased action, got:\n%s", canonical)
	}
	if strings.Contains(canonical, "x-tc-action:RunRerank") {
		t.Errorf("canonical request must not sign the un-lowercased action, got:\n%s", canonical)
	}
}

// tc3Sha256Hex hashes with SHA-256 and hex-encodes the digest.
func tc3Sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// tc3SignRaw is an independent reimplementation of the Tencent Cloud TC3
// HMAC-SHA256 signing chain, taken from the official Signature v3 docs. It does
// NOT call the production helpers, so a mutation to the production key
// derivation (e.g. the "TC3" prefix) or canonicalisation cannot be masked by a
// shared code path. It is validated against the official documented vector in
// TestTC3SignatureChainMatchesOfficialTencentVector.
func tc3SignRaw(secretKey, date, service string, timestamp int64, canonicalRequest string) string {
	mac := func(key []byte, data string) []byte {
		h := hmac.New(sha256.New, key)
		h.Write([]byte(data))
		return h.Sum(nil)
	}
	credentialScope := date + "/" + service + "/tc3_request"
	stringToSign := strings.Join([]string{
		"TC3-HMAC-SHA256",
		strconv.FormatInt(timestamp, 10),
		credentialScope,
		tc3Sha256Hex([]byte(canonicalRequest)),
	}, "\n")
	secretDate := mac([]byte("TC3"+secretKey), date)
	secretService := mac(secretDate, service)
	secretSigning := mac(secretService, "tc3_request")
	return hex.EncodeToString(mac(secretSigning, stringToSign))
}

// TestTC3SignatureChainMatchesOfficialTencentVector anchors the independent TC3
// chain to the worked example published in Tencent Cloud's official Signature
// v3 documentation (cloud.tencent.com/document/api/213/30654): action
// DescribeInstances, service cvm, timestamp 1551113065, date 2019-02-25. Its
// documented final signature is asserted byte-for-byte. If this test fails the
// test-local reimplementation has drifted and the RunRerank vector below is no
// longer independently validated.
func TestTC3SignatureChainMatchesOfficialTencentVector(t *testing.T) {
	const (
		secretKey = "********************************" // 32 '*', official doc test key
		date      = "2019-02-25"
		service   = "cvm"
		timestamp = 1551113065
	)
	payload := []byte(`{"Limit": 1, "Filters": [{"Values": ["\u672a\u547d\u540d"], "Name": "instance-name"}]}`)
	canonical := "POST\n/\n\n" +
		"content-type:application/json; charset=utf-8\n" +
		"host:cvm.tencentcloudapi.com\n" +
		"x-tc-action:describeinstances\n\n" +
		"content-type;host;x-tc-action\n" +
		tc3Sha256Hex(payload)
	const want = "10b1a37a7301a02ca19a647ad722d5e43b4b3cff309d421d85b46093f6ab6c4f"
	if got := tc3SignRaw(secretKey, date, service, timestamp, canonical); got != want {
		t.Fatalf("TC3 chain drifted from official vector: got %s want %s", got, want)
	}
}

// TestTencentAuthorizationKnownGoodVector pins the exact TC3 canonical request,
// string-to-sign and Authorization header the production signer produces for a
// fixed RunRerank request. The expected signature is derived by the
// official-vector-anchored independent chain (tc3SignRaw) and passed through
// production to kill mutations of the "TC3" prefix, the lowercased signed
// action, the host, the service and the credential scope.
func TestTencentAuthorizationKnownGoodVector(t *testing.T) {
	const (
		secretID  = "AKID********************************" // AKID + 32 '*'
		secretKey = "********************************"     // 32 '*', official doc test key
		date      = "2019-02-25"
		timestamp = 1551113065
		region    = "ap-guangzhou"
	)
	payload := []byte(`{"Query":"q","Docs":["a"],"Model":"lke-reranker-base"}`)

	wantCanonical := "POST\n/\n\n" +
		"content-type:application/json\n" +
		"host:lkeap.tencentcloudapi.com\n" +
		"x-tc-action:runrerank\n\n" +
		"content-type;host;x-tc-action\n" +
		tc3Sha256Hex(payload)
	if got := tc3CanonicalRequest(payload); got != wantCanonical {
		t.Fatalf("canonical request mismatch:\n got %q\nwant %q", got, wantCanonical)
	}

	const wantSignature = "702462978ce10d4eade69bced0a559ee63f0ab01ca9a02aa6e6ed973909dfedd"
	if got := tc3SignRaw(secretKey, date, "lkeap", timestamp, wantCanonical); got != wantSignature {
		t.Fatalf("independent TC3 chain drifted: got %s want %s", got, wantSignature)
	}

	wantAuth := "TC3-HMAC-SHA256 Credential=" + secretID + "/" + date + "/lkeap/tc3_request" +
		", SignedHeaders=content-type;host;x-tc-action" +
		", Signature=" + wantSignature
	if got := tc3Authorization(secretID, secretKey, date, timestamp, region, payload); got != wantAuth {
		t.Fatalf("Authorization mismatch:\n got %s\nwant %s", got, wantAuth)
	}
}

func TestNewDispatch(t *testing.T) {
	for _, api := range []API{APICohere, APIDashScope, APIVolcengine, APITencentLKEAP} {
		rr, err := New(Config{API: api})
		if err != nil {
			t.Errorf("New(%q) error = %v", api, err)
		}
		if rr == nil {
			t.Errorf("New(%q) returned nil reranker", api)
		}
	}
	if _, err := New(Config{API: API("bogus")}); err == nil {
		t.Error("New(bogus) expected error, got nil")
	}
}

func TestFinalizeOrderingAndTopN(t *testing.T) {
	in := []Result{{Index: 0, Score: 0.1}, {Index: 1, Score: 0.9}, {Index: 2, Score: 0.5}, {Index: 3, Score: 0.7}}

	got := finalize(append([]Result(nil), in...), 2)
	want := []Result{{Index: 1, Score: 0.9}, {Index: 3, Score: 0.7}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("topN=2 = %v, want %v", got, want)
	}

	all := finalize(append([]Result(nil), in...), 0)
	wantAll := []Result{{Index: 1, Score: 0.9}, {Index: 3, Score: 0.7}, {Index: 2, Score: 0.5}, {Index: 0, Score: 0.1}}
	if !reflect.DeepEqual(all, wantAll) {
		t.Errorf("topN=0 = %v, want %v", all, wantAll)
	}
}

func TestNoopReranker(t *testing.T) {
	n := NoopReranker{TopN: 2}
	got, err := n.Rerank(context.Background(), "q", []string{"a", "b", "c"})
	if err != nil {
		t.Fatal(err)
	}
	want := []Result{{Index: 0, Score: 0}, {Index: 1, Score: 0}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("results = %v, want %v", got, want)
	}
}
