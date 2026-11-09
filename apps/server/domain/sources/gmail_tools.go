package sources

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/emergent-company/emergent.memory/domain/mcp"
	"github.com/emergent-company/emergent.memory/internal/gmail"
	"github.com/emergent-company/emergent.memory/pkg/apperror"
)

// SourceToolProvider is implemented by a provider that exposes account-scoped
// MCP tools.
type SourceToolProvider interface {
	ToolDefinitions(alias string, writeEnabled bool) []mcp.ToolDefinition
	ExecuteTool(ctx context.Context, creds Credentials, cfg map[string]any, action string, args map[string]any) (*mcp.ToolResult, error)
}

// gmailWriteClient is the subset of the Gmail client used by send/draft tools.
type gmailWriteClient interface {
	SendMessage(ctx context.Context, msg gmail.OutgoingMessage) (*gmail.SentMessage, error)
	CreateDraft(ctx context.Context, msg gmail.OutgoingMessage) (*gmail.DraftRef, error)
	UpdateDraft(ctx context.Context, draftID string, msg gmail.OutgoingMessage) (*gmail.DraftRef, error)
	SendDraft(ctx context.Context, draftID string) (*gmail.SentMessage, error)
	ListDrafts(ctx context.Context, max int64) ([]gmail.DraftRef, error)
	DeleteDraft(ctx context.Context, draftID string) error
}

func (p *gmailProvider) ToolDefinitions(alias string, writeEnabled bool) []mcp.ToolDefinition {
	prefix := "gmail-" + alias + "-"
	read := "documents:read"
	write := "email:write"

	defs := []mcp.ToolDefinition{
		{
			Name:          prefix + "message-search",
			Description:   "Search the connected Gmail account's threads using a Gmail query.",
			RequiredScope: read,
			InputSchema: mcp.InputSchema{Type: "object", Properties: map[string]mcp.PropertySchema{
				"query":       {Type: "string", Description: "Gmail search query."},
				"label_ids":   {Type: "array", Items: &mcp.PropertySchema{Type: "string"}, Description: "Require every listed label id."},
				"max_results": {Type: "integer", Description: "Maximum threads to return (default 25, max 100)."},
			}, Required: []string{"query"}},
		},
		{
			Name:          prefix + "thread-get",
			Description:   "Fetch one Gmail thread with its messages.",
			RequiredScope: read,
			InputSchema: mcp.InputSchema{Type: "object", Properties: map[string]mcp.PropertySchema{
				"thread_id": {Type: "string", Description: "Gmail thread id."},
			}, Required: []string{"thread_id"}},
		},
		{
			Name:          prefix + "label-list",
			Description:   "List the connected Gmail account's labels.",
			RequiredScope: read,
			InputSchema:   mcp.InputSchema{Type: "object", Properties: map[string]mcp.PropertySchema{}},
		},
	}
	if !writeEnabled {
		return defs
	}
	defs = append(defs,
		mcp.ToolDefinition{
			Name:          prefix + "message-send",
			Description:   "Send an email message from the connected Gmail account.",
			RequiredScope: write,
			InputSchema: mcp.InputSchema{Type: "object", Properties: map[string]mcp.PropertySchema{
				"to":        {Type: "string", Description: "Recipient address(es), comma-separated."},
				"cc":        {Type: "string"},
				"bcc":       {Type: "string"},
				"subject":   {Type: "string"},
				"text":      {Type: "string", Description: "Plain-text body."},
				"html":      {Type: "string", Description: "HTML body."},
				"thread_id": {Type: "string", Description: "Optional thread to reply into."},
			}, Required: []string{"to"}},
		},
		mcp.ToolDefinition{
			Name:          prefix + "draft-create",
			Description:   "Create a Gmail draft.",
			RequiredScope: write,
			InputSchema:   outgoingSchema(),
		},
		mcp.ToolDefinition{
			Name:          prefix + "draft-update",
			Description:   "Replace a Gmail draft's content.",
			RequiredScope: write,
			InputSchema: mcp.InputSchema{Type: "object", Properties: map[string]mcp.PropertySchema{
				"draft_id": {Type: "string"},
				"to":       {Type: "string"},
				"cc":       {Type: "string"},
				"bcc":      {Type: "string"},
				"subject":  {Type: "string"},
				"text":     {Type: "string"},
				"html":     {Type: "string"},
			}, Required: []string{"draft_id"}},
		},
		mcp.ToolDefinition{
			Name:          prefix + "draft-send",
			Description:   "Send an existing Gmail draft.",
			RequiredScope: write,
			InputSchema: mcp.InputSchema{Type: "object", Properties: map[string]mcp.PropertySchema{
				"draft_id": {Type: "string"},
			}, Required: []string{"draft_id"}},
		},
		mcp.ToolDefinition{
			Name:          prefix + "draft-list",
			Description:   "List Gmail drafts.",
			RequiredScope: write,
			InputSchema: mcp.InputSchema{Type: "object", Properties: map[string]mcp.PropertySchema{
				"max_results": {Type: "integer"},
			}},
		},
		mcp.ToolDefinition{
			Name:          prefix + "draft-delete",
			Description:   "Delete a Gmail draft.",
			RequiredScope: write,
			InputSchema: mcp.InputSchema{Type: "object", Properties: map[string]mcp.PropertySchema{
				"draft_id": {Type: "string"},
			}, Required: []string{"draft_id"}},
		},
	)
	return defs
}

func outgoingSchema() mcp.InputSchema {
	return mcp.InputSchema{Type: "object", Properties: map[string]mcp.PropertySchema{
		"to":        {Type: "string"},
		"cc":        {Type: "string"},
		"bcc":       {Type: "string"},
		"subject":   {Type: "string"},
		"text":      {Type: "string"},
		"html":      {Type: "string"},
		"thread_id": {Type: "string"},
	}, Required: []string{"to"}}
}

func (p *gmailProvider) ExecuteTool(ctx context.Context, creds Credentials, cfg map[string]any, action string, args map[string]any) (*mcp.ToolResult, error) {
	client, err := p.newClient(ctx, gmailCredentials(creds))
	if err != nil {
		return nil, err
	}
	switch action {
	case "message-search":
		query, _ := args["query"].(string)
		if strings.TrimSpace(query) == "" {
			return nil, apperror.NewValidation("query is required")
		}
		labelIDs, _ := stringList(args["label_ids"])
		maxThreads, _ := toInt(args["max_results"])
		if maxThreads <= 0 || maxThreads > 100 {
			maxThreads = 25
		}
		refs, truncated, err := client.ListThreads(ctx, gmail.ListOptions{Query: query, LabelIDs: labelIDs, MaxThreads: maxThreads})
		if err != nil {
			return nil, gmailGrantOrRaw(err)
		}
		return toolJSON(map[string]any{"threads": refs, "truncated": truncated})
	case "thread-get":
		id, _ := args["thread_id"].(string)
		if strings.TrimSpace(id) == "" {
			return nil, apperror.NewValidation("thread_id is required")
		}
		thread, err := client.GetThread(ctx, id)
		if err != nil {
			return nil, gmailGrantOrRaw(err)
		}
		return toolJSON(thread)
	case "label-list":
		labels, err := client.ListLabels(ctx)
		if err != nil {
			return nil, gmailGrantOrRaw(err)
		}
		return toolJSON(map[string]any{"labels": labels})
	}

	writeClient, ok := client.(gmailWriteClient)
	if !ok {
		return nil, apperror.NewInternal("gmail transport does not support write tools", nil)
	}
	switch action {
	case "message-send":
		sent, err := writeClient.SendMessage(ctx, outgoingFromArgs(args))
		if err != nil {
			return nil, gmailGrantOrRaw(err)
		}
		return toolJSON(sent)
	case "draft-create":
		draft, err := writeClient.CreateDraft(ctx, outgoingFromArgs(args))
		if err != nil {
			return nil, gmailGrantOrRaw(err)
		}
		return toolJSON(draft)
	case "draft-update":
		id, _ := args["draft_id"].(string)
		draft, err := writeClient.UpdateDraft(ctx, id, outgoingFromArgs(args))
		if err != nil {
			return nil, gmailGrantOrRaw(err)
		}
		return toolJSON(draft)
	case "draft-send":
		id, _ := args["draft_id"].(string)
		sent, err := writeClient.SendDraft(ctx, id)
		if err != nil {
			return nil, gmailGrantOrRaw(err)
		}
		return toolJSON(sent)
	case "draft-list":
		max, _ := toInt(args["max_results"])
		drafts, err := writeClient.ListDrafts(ctx, int64(max))
		if err != nil {
			return nil, gmailGrantOrRaw(err)
		}
		return toolJSON(map[string]any{"drafts": drafts})
	case "draft-delete":
		id, _ := args["draft_id"].(string)
		if err := writeClient.DeleteDraft(ctx, id); err != nil {
			return nil, gmailGrantOrRaw(err)
		}
		return toolJSON(map[string]any{"deleted": id})
	default:
		return nil, apperror.NewValidation("unknown Gmail tool action: " + action)
	}
}

func outgoingFromArgs(args map[string]any) gmail.OutgoingMessage {
	get := func(k string) string { s, _ := args[k].(string); return s }
	return gmail.OutgoingMessage{
		To:       get("to"),
		Cc:       get("cc"),
		Bcc:      get("bcc"),
		Subject:  get("subject"),
		Text:     get("text"),
		HTML:     get("html"),
		ThreadID: get("thread_id"),
	}
}

func toolJSON(v any) (*mcp.ToolResult, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("marshal tool result: %w", err)
	}
	return &mcp.ToolResult{Content: []mcp.ContentBlock{{Type: "text", Text: string(b)}}}, nil
}
