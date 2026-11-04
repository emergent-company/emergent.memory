// bridge_internal.go — Pure-Go implementation of all bridge operations.
//
// No CGO dependency. The functions here are called by the CGO exports in
// bridge.go and can be unit-tested on Linux without a C compiler.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	sdk "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk"
	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/chat"
	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/documents"
	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/graph"
	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/projects"
	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/search"
	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/tasks"
)

// ---------------------------------------------------------------------------
// JSON helpers
// ---------------------------------------------------------------------------

func goOKJSON(result any) string {
	b, _ := json.Marshal(envelope{Result: result})
	return string(b)
}

func goErrJSONStr(msg string) string {
	b, _ := json.Marshal(envelope{Error: msg})
	return string(b)
}

func goErrJSON(err error) string {
	if err == nil {
		return goErrJSONStr("unknown error")
	}
	return goErrJSONStr(err.Error())
}

// ---------------------------------------------------------------------------
// Client lifecycle
// ---------------------------------------------------------------------------

// goCreateClient parses configJSON, creates an SDK client, stores it in the
// registry, and returns the standard JSON envelope string.
func goCreateClient(configJSON string) string {
	var req CreateClientRequest
	if err := json.Unmarshal([]byte(configJSON), &req); err != nil {
		return goErrJSONStr(fmt.Sprintf("invalid config JSON: %s", err))
	}
	if req.ServerURL == "" {
		return goErrJSONStr("server_url is required")
	}
	if req.APIKey == "" {
		return goErrJSONStr("api_key is required")
	}
	if req.AuthMode == "" {
		req.AuthMode = "apikey"
	}

	client, err := sdk.New(sdk.Config{
		ServerURL: req.ServerURL,
		Auth: sdk.AuthConfig{
			Mode:   req.AuthMode,
			APIKey: req.APIKey,
		},
		OrgID:      req.OrgID,
		ProjectID:  req.ProjectID,
		HTTPClient: &http.Client{Timeout: 30 * time.Second},
	})
	if err != nil {
		return goErrJSON(fmt.Errorf("failed to create client: %w", err))
	}

	id := atomic.AddUint32(&nextID, 1) - 1
	clientsMu.Lock()
	clients[id] = client
	clientsMu.Unlock()

	bridgeLog(1, fmt.Sprintf("CreateClient: handle=%d server=%s", id, req.ServerURL))
	return goOKJSON(CreateClientResponse{Handle: id})
}

// goFreeClient tears down the client associated with id.
func goFreeClient(id uint32) {
	clientsMu.Lock()
	c, ok := clients[id]
	if ok {
		delete(clients, id)
	}
	clientsMu.Unlock()
	if ok && c != nil {
		c.Close()
	}
}

// ---------------------------------------------------------------------------
// SetContext
// ---------------------------------------------------------------------------

// SetContextRequest is the JSON payload for the SetContext call.
type SetContextRequest struct {
	OrgID     string `json:"org_id"`
	ProjectID string `json:"project_id"`
}

func goSetContext(id uint32, requestJSON string) string {
	clientsMu.RLock()
	client, ok := clients[id]
	clientsMu.RUnlock()
	if !ok {
		return goErrJSONStr(fmt.Sprintf("unknown client handle: %d", id))
	}

	var req SetContextRequest
	if err := json.Unmarshal([]byte(requestJSON), &req); err != nil {
		return goErrJSONStr(fmt.Sprintf("invalid request JSON: %s", err))
	}

	client.SetContext(req.OrgID, req.ProjectID)
	return goOKJSON(map[string]bool{"ok": true})
}

// ---------------------------------------------------------------------------
// Ping (synchronous POC)
// ---------------------------------------------------------------------------

func goPing(id uint32, requestJSON string) string {
	clientsMu.RLock()
	_, ok := clients[id]
	clientsMu.RUnlock()
	if !ok {
		return goErrJSONStr(fmt.Sprintf("unknown client handle: %d", id))
	}

	var req PingRequest
	if err := json.Unmarshal([]byte(requestJSON), &req); err != nil {
		return goErrJSONStr(fmt.Sprintf("invalid request JSON: %s", err))
	}

	return goOKJSON(PingResponse{
		Echo:      req.Message,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	})
}

// ---------------------------------------------------------------------------
// Async health check
// ---------------------------------------------------------------------------

// goHealthCheck fires an async health check for the given client handle.
// callback is invoked exactly once with the operation ID and result JSON.
// Returns the operation ID (0 if the handle is invalid).
func goHealthCheck(id uint32, callback func(opID uint64, result string)) uint64 {
	clientsMu.RLock()
	client, ok := clients[id]
	clientsMu.RUnlock()
	if !ok {
		go callback(0, goErrJSONStr(fmt.Sprintf("unknown client handle: %d", id)))
		return 0
	}

	ctx, cancel := context.WithCancel(context.Background())
	opID := registerCancel(cancel)

	go func() {
		defer deregisterCancel(opID)
		health, err := client.Health.Health(ctx)
		var result string
		if err != nil {
			result = goErrJSON(err)
		} else {
			result = goOKJSON(health)
		}
		callback(opID, result)
	}()

	return opID
}

// ---------------------------------------------------------------------------
// Async search
// ---------------------------------------------------------------------------

// SearchRequest is the JSON payload for the Search call.
type SearchRequest struct {
	Query          string `json:"query"`
	Limit          int    `json:"limit,omitempty"`
	ResultTypes    string `json:"result_types,omitempty"`    // graph, text, both
	FusionStrategy string `json:"fusion_strategy,omitempty"` // weighted, rrf, etc.
}

func goSearch(id uint32, requestJSON string, callback func(opID uint64, result string)) uint64 {
	clientsMu.RLock()
	client, ok := clients[id]
	clientsMu.RUnlock()
	if !ok {
		go callback(0, goErrJSONStr(fmt.Sprintf("unknown client handle: %d", id)))
		return 0
	}

	var req SearchRequest
	if err := json.Unmarshal([]byte(requestJSON), &req); err != nil {
		go callback(0, goErrJSONStr(fmt.Sprintf("invalid request JSON: %s", err)))
		return 0
	}
	if req.Limit <= 0 {
		req.Limit = 10
	}

	ctx, cancel := context.WithCancel(context.Background())
	opID := registerCancel(cancel)

	go func() {
		defer deregisterCancel(opID)
		sdkReq := &search.SearchRequest{
			Query:          req.Query,
			Limit:          req.Limit,
			ResultTypes:    req.ResultTypes,
			FusionStrategy: req.FusionStrategy,
		}
		results, err := client.Search.Search(ctx, sdkReq)
		var result string
		if err != nil {
			result = goErrJSON(err)
		} else {
			result = goOKJSON(results)
		}
		callback(opID, result)
	}()

	return opID
}

// ---------------------------------------------------------------------------
// Async chat
// ---------------------------------------------------------------------------

// ChatRequest is the JSON payload for the Chat call.
type ChatRequest struct {
	Message        string  `json:"message"`
	ConversationID *string `json:"conversation_id,omitempty"`
}

// ChatResponse is the JSON payload returned on a successful chat call.
// It collects all streamed tokens into a single response for simplicity.
type ChatResponse struct {
	ConversationID string `json:"conversation_id,omitempty"`
	Content        string `json:"content"`
}

func goChat(id uint32, requestJSON string, callback func(opID uint64, result string)) uint64 {
	clientsMu.RLock()
	client, ok := clients[id]
	clientsMu.RUnlock()
	if !ok {
		go callback(0, goErrJSONStr(fmt.Sprintf("unknown client handle: %d", id)))
		return 0
	}

	var req ChatRequest
	if err := json.Unmarshal([]byte(requestJSON), &req); err != nil {
		go callback(0, goErrJSONStr(fmt.Sprintf("invalid request JSON: %s", err)))
		return 0
	}

	ctx, cancel := context.WithCancel(context.Background())
	opID := registerCancel(cancel)

	go func() {
		defer deregisterCancel(opID)
		stream, err := client.Chat.StreamChat(ctx, &chat.StreamRequest{
			Message:        req.Message,
			ConversationID: req.ConversationID,
		})
		if err != nil {
			callback(opID, goErrJSON(err))
			return
		}
		defer stream.Close()

		var sb strings.Builder
		var conversationID string
		for event := range stream.Events() {
			switch event.Type {
			case "token":
				sb.WriteString(event.Token)
			case "meta":
				if event.ConversationID != "" {
					conversationID = event.ConversationID
				}
			case "error":
				callback(opID, goErrJSONStr(event.Error))
				return
			}
		}
		if err := stream.Err(); err != nil {
			callback(opID, goErrJSON(err))
			return
		}
		callback(opID, goOKJSON(ChatResponse{
			ConversationID: conversationID,
			Content:        sb.String(),
		}))
	}()

	return opID
}

// ---------------------------------------------------------------------------
// Async list documents
// ---------------------------------------------------------------------------

// ListDocumentsRequest is the JSON payload for the ListDocuments call.
type ListDocumentsRequest struct {
	Limit  int    `json:"limit,omitempty"`
	Cursor string `json:"cursor,omitempty"`
}

func goListDocuments(id uint32, requestJSON string, callback func(opID uint64, result string)) uint64 {
	clientsMu.RLock()
	client, ok := clients[id]
	clientsMu.RUnlock()
	if !ok {
		go callback(0, goErrJSONStr(fmt.Sprintf("unknown client handle: %d", id)))
		return 0
	}

	var req ListDocumentsRequest
	if err := json.Unmarshal([]byte(requestJSON), &req); err != nil {
		go callback(0, goErrJSONStr(fmt.Sprintf("invalid request JSON: %s", err)))
		return 0
	}
	if req.Limit <= 0 {
		req.Limit = 20
	}

	ctx, cancel := context.WithCancel(context.Background())
	opID := registerCancel(cancel)

	go func() {
		defer deregisterCancel(opID)
		docs, err := client.Documents.List(ctx, &documents.ListOptions{
			Limit:  req.Limit,
			Cursor: req.Cursor,
		})
		var result string
		if err != nil {
			result = goErrJSON(err)
		} else {
			result = goOKJSON(docs)
		}
		callback(opID, result)
	}()

	return opID
}

// ---------------------------------------------------------------------------
// Cancel (internal)
// ---------------------------------------------------------------------------

func goCancel(opID uint64) {
	if cancel := deregisterCancel(opID); cancel != nil {
		cancel()
		bridgeLog(1, fmt.Sprintf("CancelOperation: opID=%d cancelled", opID))
	}
}

// ---------------------------------------------------------------------------
// Async GetProjects
// ---------------------------------------------------------------------------

// GetProjectsRequest is the JSON payload for the GetProjects call.
type GetProjectsRequest struct {
	Limit        int  `json:"limit,omitempty"`
	IncludeStats bool `json:"include_stats,omitempty"`
}

func goGetProjects(id uint32, requestJSON string, callback func(opID uint64, result string)) uint64 {
	clientsMu.RLock()
	client, ok := clients[id]
	clientsMu.RUnlock()
	if !ok {
		go callback(0, goErrJSONStr(fmt.Sprintf("unknown client handle: %d", id)))
		return 0
	}

	var req GetProjectsRequest
	if err := json.Unmarshal([]byte(requestJSON), &req); err != nil {
		go callback(0, goErrJSONStr(fmt.Sprintf("invalid request JSON: %s", err)))
		return 0
	}
	if req.Limit <= 0 {
		req.Limit = 50
	}

	ctx, cancel := context.WithCancel(context.Background())
	opID := registerCancel(cancel)

	go func() {
		defer deregisterCancel(opID)
		projs, err := client.Projects.List(ctx, &projects.ListOptions{
			Limit:        req.Limit,
			IncludeStats: req.IncludeStats,
		})
		var result string
		if err != nil {
			result = goErrJSON(err)
		} else {
			result = goOKJSON(projs)
		}
		callback(opID, result)
	}()

	return opID
}

// ---------------------------------------------------------------------------
// Async SearchObjects (graph hybrid search)
// ---------------------------------------------------------------------------

// SearchObjectsRequest is the JSON payload for the SearchObjects call.
type SearchObjectsRequest struct {
	Query         string  `json:"query"`
	Limit         int     `json:"limit,omitempty"`
	LexicalWeight float32 `json:"lexical_weight,omitempty"`
	VectorWeight  float32 `json:"vector_weight,omitempty"`
}

func goSearchObjects(id uint32, requestJSON string, callback func(opID uint64, result string)) uint64 {
	clientsMu.RLock()
	client, ok := clients[id]
	clientsMu.RUnlock()
	if !ok {
		go callback(0, goErrJSONStr(fmt.Sprintf("unknown client handle: %d", id)))
		return 0
	}

	var req SearchObjectsRequest
	if err := json.Unmarshal([]byte(requestJSON), &req); err != nil {
		go callback(0, goErrJSONStr(fmt.Sprintf("invalid request JSON: %s", err)))
		return 0
	}
	if req.Query == "" {
		go callback(0, goErrJSONStr("query is required"))
		return 0
	}
	if req.Limit <= 0 {
		req.Limit = 10
	}

	ctx, cancel := context.WithCancel(context.Background())
	opID := registerCancel(cancel)

	go func() {
		defer deregisterCancel(opID)
		sdkReq := &graph.HybridSearchRequest{
			Query: req.Query,
			Limit: req.Limit,
		}
		if req.LexicalWeight != 0 {
			sdkReq.LexicalWeight = &req.LexicalWeight
		}
		if req.VectorWeight != 0 {
			sdkReq.VectorWeight = &req.VectorWeight
		}
		results, err := client.Graph.HybridSearch(ctx, sdkReq)
		var result string
		if err != nil {
			result = goErrJSON(err)
		} else {
			result = goOKJSON(results)
		}
		callback(opID, result)
	}()

	return opID
}

// ---------------------------------------------------------------------------
// Async GetProjectStats
// ---------------------------------------------------------------------------

// GetProjectStatsRequest is the JSON payload for the GetProjectStats call.
type GetProjectStatsRequest struct {
	ProjectID string `json:"project_id"`
}

func goGetProjectStats(id uint32, requestJSON string, callback func(opID uint64, result string)) uint64 {
	clientsMu.RLock()
	client, ok := clients[id]
	clientsMu.RUnlock()
	if !ok {
		go callback(0, goErrJSONStr(fmt.Sprintf("unknown client handle: %d", id)))
		return 0
	}

	var req GetProjectStatsRequest
	if err := json.Unmarshal([]byte(requestJSON), &req); err != nil {
		go callback(0, goErrJSONStr(fmt.Sprintf("invalid request JSON: %s", err)))
		return 0
	}
	if req.ProjectID == "" {
		go callback(0, goErrJSONStr("project_id is required"))
		return 0
	}

	ctx, cancel := context.WithCancel(context.Background())
	opID := registerCancel(cancel)

	go func() {
		defer deregisterCancel(opID)
		proj, err := client.Projects.Get(ctx, req.ProjectID, &projects.GetOptions{IncludeStats: true})
		var result string
		if err != nil {
			result = goErrJSON(err)
		} else {
			result = goOKJSON(proj)
		}
		callback(opID, result)
	}()

	return opID
}

// ---------------------------------------------------------------------------
// Async GetAccountStats (task counts across all projects)
// ---------------------------------------------------------------------------

func goGetAccountStats(id uint32, callback func(opID uint64, result string)) uint64 {
	clientsMu.RLock()
	client, ok := clients[id]
	clientsMu.RUnlock()
	if !ok {
		go callback(0, goErrJSONStr(fmt.Sprintf("unknown client handle: %d", id)))
		return 0
	}

	ctx, cancel := context.WithCancel(context.Background())
	opID := registerCancel(cancel)

	go func() {
		defer deregisterCancel(opID)
		counts, err := client.Tasks.GetAllCounts(ctx)
		var result string
		if err != nil {
			result = goErrJSON(err)
		} else {
			result = goOKJSON(counts)
		}
		callback(opID, result)
	}()

	return opID
}

// ---------------------------------------------------------------------------
// Async GetWorkers (active/queued tasks for a project)
// ---------------------------------------------------------------------------

// GetWorkersRequest is the JSON payload for the GetWorkers call.
type GetWorkersRequest struct {
	ProjectID string `json:"project_id"`
	Limit     int    `json:"limit,omitempty"`
}

func goGetWorkers(id uint32, requestJSON string, callback func(opID uint64, result string)) uint64 {
	clientsMu.RLock()
	client, ok := clients[id]
	clientsMu.RUnlock()
	if !ok {
		go callback(0, goErrJSONStr(fmt.Sprintf("unknown client handle: %d", id)))
		return 0
	}

	var req GetWorkersRequest
	if err := json.Unmarshal([]byte(requestJSON), &req); err != nil {
		go callback(0, goErrJSONStr(fmt.Sprintf("invalid request JSON: %s", err)))
		return 0
	}
	if req.Limit <= 0 {
		req.Limit = 50
	}

	ctx, cancel := context.WithCancel(context.Background())
	opID := registerCancel(cancel)

	go func() {
		defer deregisterCancel(opID)
		taskList, err := client.Tasks.List(ctx, &tasks.ListOptions{
			ProjectID: req.ProjectID,
			Limit:     req.Limit,
		})
		var result string
		if err != nil {
			result = goErrJSON(err)
		} else {
			result = goOKJSON(taskList)
		}
		callback(opID, result)
	}()

	return opID
}

// ---------------------------------------------------------------------------
// Async GetUserProfile
// ---------------------------------------------------------------------------

func goGetUserProfile(id uint32, callback func(opID uint64, result string)) uint64 {
	clientsMu.RLock()
	client, ok := clients[id]
	clientsMu.RUnlock()
	if !ok {
		go callback(0, goErrJSONStr(fmt.Sprintf("unknown client handle: %d", id)))
		return 0
	}

	ctx, cancel := context.WithCancel(context.Background())
	opID := registerCancel(cancel)

	go func() {
		defer deregisterCancel(opID)
		profile, err := client.Users.GetProfile(ctx)
		var result string
		if err != nil {
			result = goErrJSON(err)
		} else {
			result = goOKJSON(profile)
		}
		callback(opID, result)
	}()

	return opID
}
