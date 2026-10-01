package main

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
)

// WorkItem is one board card: a board-enabled work object (graph object) joined
// to its latest run's execution status. Mirrors the server's WorkItemDTO.
type WorkItem struct {
	ProjectID   string `json:"projectId"`
	CanonicalID string `json:"canonicalId"`
	Type        string `json:"type"`
	Key         string `json:"key"`
	Status      string `json:"status"`
	Assignee    string `json:"assignee"`
	Version     int    `json:"version"`
	NeedsReview bool   `json:"needsReview"`
	UpdatedAt   string `json:"updatedAt"`

	LatestRunStatus       string `json:"latestRunStatus"`
	LatestRunFailureClass string `json:"latestRunFailureClass"`
	LatestRunID           string `json:"latestRunId"`
	RunCount              int    `json:"runCount"`

	Unroutable bool `json:"unroutable"`
}

// WorkItemRun is a compact run summary in a work-item detail response.
type WorkItemRun struct {
	ID           string         `json:"id"`
	Status       string         `json:"status"`
	StartedAt    string         `json:"startedAt"`
	CompletedAt  *string        `json:"completedAt"`
	ErrorMessage *string        `json:"errorMessage"`
	FailureClass *string        `json:"failureClass"`
	Summary      map[string]any `json:"summary"`
}

// WorkItemFeedback is one append-only rework round.
type WorkItemFeedback struct {
	Round     int    `json:"round"`
	Author    string `json:"author"`
	Text      string `json:"text"`
	RunID     string `json:"runId"`
	CreatedAt string `json:"createdAt"`
}

// WorkItemDetail is the card-drawer payload: the item joined to its feedback
// history and recent runs.
type WorkItemDetail struct {
	Item     *WorkItem          `json:"item"`
	Feedback []WorkItemFeedback `json:"feedback"`
	Runs     []WorkItemRun      `json:"runs"`
}

// WorkItemAction is the response of a human action (approve/retry/etc).
type WorkItemAction struct {
	Item      *WorkItemActionItem `json:"item"`
	Round     int                 `json:"round,omitempty"`
	Escalated bool                `json:"escalated,omitempty"`
	RunID     string              `json:"runId,omitempty"`
}

// WorkItemActionItem is the HEAD projection returned by a work-item action.
type WorkItemActionItem struct {
	ProjectID   string `json:"projectId"`
	CanonicalID string `json:"canonicalId"`
	Type        string `json:"type"`
	Key         string `json:"key"`
	Status      string `json:"status"`
	Assignee    string `json:"assignee"`
	Version     int    `json:"version"`
}

func (m *MemoryClient) workItemsPath(ctx context.Context) string {
	return "/api/projects/" + url.PathEscape(m.projectIDFor(ctx)) + "/work-items"
}

// ListWorkItems lists the board projection of work items. status empty = all
// statuses; typeName empty = all board-enabled types.
func (m *MemoryClient) ListWorkItems(ctx context.Context, status, typeName string, limit int) ([]WorkItem, error) {
	q := url.Values{}
	if status != "" {
		q.Set("status", status)
	}
	if typeName != "" {
		q.Set("type", typeName)
	}
	if limit <= 0 {
		limit = 200
	}
	q.Set("limit", strconv.Itoa(limit))
	var env successEnvelope[[]WorkItem]
	if err := m.do(ctx, http.MethodGet, m.workItemsPath(ctx)+"?"+q.Encode(), nil, &env); err != nil {
		return nil, err
	}
	if env.Data == nil {
		return []WorkItem{}, nil
	}
	return env.Data, nil
}

// GetWorkItem fetches one work item joined to its feedback and recent runs.
func (m *MemoryClient) GetWorkItem(ctx context.Context, canonicalID string) (*WorkItemDetail, error) {
	var env successEnvelope[WorkItemDetail]
	if err := m.do(ctx, http.MethodGet, m.workItemsPath(ctx)+"/"+url.PathEscape(canonicalID), nil, &env); err != nil {
		return nil, err
	}
	return &env.Data, nil
}

// ApproveWorkItem finalizes a work item in review.
func (m *MemoryClient) ApproveWorkItem(ctx context.Context, canonicalID string) (*WorkItemAction, error) {
	var env successEnvelope[WorkItemAction]
	if err := m.do(ctx, http.MethodPost, m.workItemsPath(ctx)+"/"+url.PathEscape(canonicalID)+"/approve", nil, &env); err != nil {
		return nil, err
	}
	return &env.Data, nil
}

// RequestChangesWorkItem returns a work item for rework with required feedback.
func (m *MemoryClient) RequestChangesWorkItem(ctx context.Context, canonicalID, feedback string) (*WorkItemAction, error) {
	var env successEnvelope[WorkItemAction]
	if err := m.do(ctx, http.MethodPost, m.workItemsPath(ctx)+"/"+url.PathEscape(canonicalID)+"/request-changes", map[string]string{"feedback": feedback}, &env); err != nil {
		return nil, err
	}
	return &env.Data, nil
}

// RetryWorkItem returns a blocked work item to ready and enqueues it.
func (m *MemoryClient) RetryWorkItem(ctx context.Context, canonicalID string) (*WorkItemAction, error) {
	var env successEnvelope[WorkItemAction]
	if err := m.do(ctx, http.MethodPost, m.workItemsPath(ctx)+"/"+url.PathEscape(canonicalID)+"/retry", nil, &env); err != nil {
		return nil, err
	}
	return &env.Data, nil
}

// ReassignWorkItem sets or clears the assignee of a work item.
func (m *MemoryClient) ReassignWorkItem(ctx context.Context, canonicalID, assignee string) (*WorkItemAction, error) {
	var env successEnvelope[WorkItemAction]
	if err := m.do(ctx, http.MethodPost, m.workItemsPath(ctx)+"/"+url.PathEscape(canonicalID)+"/reassign", map[string]string{"assignee": assignee}, &env); err != nil {
		return nil, err
	}
	return &env.Data, nil
}

// CancelWorkItem closes a work item and cancels any in-flight run.
func (m *MemoryClient) CancelWorkItem(ctx context.Context, canonicalID string) (*WorkItemAction, error) {
	var env successEnvelope[WorkItemAction]
	if err := m.do(ctx, http.MethodPost, m.workItemsPath(ctx)+"/"+url.PathEscape(canonicalID)+"/cancel", nil, &env); err != nil {
		return nil, err
	}
	return &env.Data, nil
}
