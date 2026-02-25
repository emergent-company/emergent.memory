package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"
)

type ApiClient struct {
	client    *http.Client
	baseURL   string
	apiKey    string
	projectID string
	dryRun    bool
}

func NewApiClient(baseURL, apiKey, projectID string, dryRun bool) *ApiClient {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.MaxIdleConns = 200
	t.MaxConnsPerHost = 200
	t.MaxIdleConnsPerHost = 200

	return &ApiClient{
		client: &http.Client{
			Transport: t,
			Timeout:   5 * time.Minute,
		},
		baseURL:   baseURL,
		apiKey:    apiKey,
		projectID: projectID,
		dryRun:    dryRun,
	}
}

func (a *ApiClient) doRequest(ctx context.Context, method, path string, body any, out any) error {
	var reqBodyBytes []byte
	var err error
	if body != nil {
		reqBodyBytes, err = json.Marshal(body)
		if err != nil {
			return err
		}
	}

	maxRetries := 5
	for attempt := 0; attempt < maxRetries; attempt++ {
		var reqBody io.Reader
		if reqBodyBytes != nil {
			reqBody = bytes.NewReader(reqBodyBytes)
		}

		req, err := http.NewRequestWithContext(ctx, method, a.baseURL+path, reqBody)
		if err != nil {
			return err
		}

		req.Header.Set("Content-Type", "application/json")
		if a.apiKey != "" {
			req.Header.Set("Authorization", "Bearer "+a.apiKey)
		}
		if a.projectID != "" {
			req.Header.Set("X-Project-ID", a.projectID)
		}

		resp, err := a.client.Do(req)
		if err != nil {
			log.Printf("Network error on %s %s (attempt %d/%d): %v", method, path, attempt+1, maxRetries, err)
			time.Sleep(time.Duration((attempt+1)*2) * time.Second)
			continue
		}

		if resp.StatusCode >= 500 || resp.StatusCode == 429 {
			b, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			log.Printf("Server error %d on %s %s (attempt %d/%d): %s", resp.StatusCode, method, path, attempt+1, maxRetries, string(b))
			time.Sleep(time.Duration((attempt+1)*2) * time.Second)
			continue
		}

		defer resp.Body.Close()
		if resp.StatusCode >= 300 {
			b, _ := io.ReadAll(resp.Body)
			return fmt.Errorf("API error %s %s: %d %s", method, path, resp.StatusCode, string(b))
		}

		if out != nil {
			if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
				return err
			}
		}
		return nil
	}
	return fmt.Errorf("failed after %d attempts", maxRetries)
}

func (a *ApiClient) BulkCreateObjects(ctx context.Context, items []CreateGraphObjectRequest) (*BulkCreateObjectsResponse, error) {
	if a.dryRun {
		for i, item := range items {
			key := ""
			if item.Key != nil {
				key = *item.Key
			}
			log.Printf("[DRY RUN] Would bulk create object %d: %s key=%s", i, item.Type, key)
		}
		res := &BulkCreateObjectsResponse{Success: len(items)}
		for i := range items {
			res.Results = append(res.Results, BulkCreateObjectResult{
				Index:   i,
				Success: true,
				Object:  &GraphObjectResponse{ID: fmt.Sprintf("dry_run_id_%d", i), CanonicalID: fmt.Sprintf("dry_run_canonical_%d", i)},
			})
		}
		return res, nil
	}

	req := BulkCreateObjectsRequest{Items: items}
	var res BulkCreateObjectsResponse
	err := a.doRequest(ctx, "POST", "/api/graph/objects/bulk", req, &res)
	return &res, err
}

func (a *ApiClient) UpsertObject(ctx context.Context, item CreateGraphObjectRequest) (*GraphObjectResponse, error) {
	if a.dryRun {
		key := ""
		if item.Key != nil {
			key = *item.Key
		}
		log.Printf("[DRY RUN] Would upsert object: %s key=%s", item.Type, key)
		return &GraphObjectResponse{ID: "dry_run_id", CanonicalID: "dry_run_canonical"}, nil
	}

	var res GraphObjectResponse
	err := a.doRequest(ctx, "PUT", "/api/graph/objects/upsert", item, &res)
	return &res, err
}

func (a *ApiClient) PatchObject(ctx context.Context, id string, item PatchGraphObjectRequest) (*GraphObjectResponse, error) {
	if a.dryRun {
		log.Printf("[DRY RUN] Would patch object: %s", id)
		return &GraphObjectResponse{ID: "dry_run_id", CanonicalID: "dry_run_canonical"}, nil
	}

	var res GraphObjectResponse
	err := a.doRequest(ctx, "PATCH", "/api/graph/objects/"+id, item, &res)
	return &res, err
}

func (a *ApiClient) BulkCreateRelationships(ctx context.Context, items []CreateGraphRelationshipRequest) (*BulkCreateRelationshipsResponse, error) {
	if a.dryRun {
		for i, item := range items {
			log.Printf("[DRY RUN] Would bulk create relationship %d: %s %s -> %s", i, item.Type, item.SrcID, item.DstID)
		}
		res := &BulkCreateRelationshipsResponse{Success: len(items)}
		for i := range items {
			res.Results = append(res.Results, BulkCreateRelationshipResult{Index: i, Success: true})
		}
		return res, nil
	}

	req := BulkCreateRelationshipsRequest{Items: items}
	var res BulkCreateRelationshipsResponse
	err := a.doRequest(ctx, "POST", "/api/graph/relationships/bulk", req, &res)
	return &res, err
}
