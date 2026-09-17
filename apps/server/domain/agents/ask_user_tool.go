package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/emergent-company/emergent.memory/domain/events"
	"github.com/emergent-company/emergent.memory/domain/notifications"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
)

// AskPauseState tracks the ask_user pause request for a single run.
// Shared between the ask_user tool (sets it) and beforeModelCb (reads it).
type AskPauseState struct {
	requested  atomic.Bool
	questionID atomic.Value // stores string
}

// RequestPause signals that the run should pause after this tool call.
func (s *AskPauseState) RequestPause(questionID string) {
	s.questionID.Store(questionID)
	s.requested.Store(true)
}

// ShouldPause returns true if a pause was requested.
func (s *AskPauseState) ShouldPause() bool {
	return s.requested.Load()
}

// QuestionID returns the question ID that triggered the pause, or empty string.
func (s *AskPauseState) QuestionID() string {
	v := s.questionID.Load()
	if v == nil {
		return ""
	}
	return v.(string)
}

// ToolConfirmPauseState tracks pending tool-policy confirmations for a single
// run. The ADK runner dispatches multiple tool calls from one LLM turn in
// parallel goroutines, so beforeToolCb appends one entry per intercepted tool
// and beforeModelCb snapshots the batch. Thread-safe via mutex.
type ToolConfirmPauseState struct {
	mu      sync.Mutex
	pending []ToolConfirmation
}

// AddConfirmation records a pending tool-policy confirmation. Thread-safe.
func (s *ToolConfirmPauseState) AddConfirmation(c ToolConfirmation) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pending = append(s.pending, c)
}

// Confirmations returns a snapshot of all pending confirmations. Thread-safe.
func (s *ToolConfirmPauseState) Confirmations() []ToolConfirmation {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]ToolConfirmation, len(s.pending))
	copy(out, s.pending)
	return out
}

// HasPending reports whether any confirmation is pending. Thread-safe.
func (s *ToolConfirmPauseState) HasPending() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.pending) > 0
}

// AskUserToolDeps holds the dependencies needed by the ask_user tool.
type AskUserToolDeps struct {
	Repo       *Repository
	Logger     *slog.Logger
	ProjectID  string
	AgentID    string
	RunID      string
	PauseState *AskPauseState
	// UserID is the user who triggered the agent (for notification targeting).
	// If empty, the notification cannot be created.
	UserID string
	// EventsSvc is used to emit a real-time SSE event after notification creation.
	// nil is safe — event emission is best-effort.
	EventsSvc *events.Service
}

// CreateQuestionParams holds parameters for creating and emitting an AgentQuestion.
type CreateQuestionParams struct {
	Repo      *Repository
	Logger    *slog.Logger
	ProjectID string
	AgentID   string
	RunID     string
	UserID    string
	EventsSvc *events.Service
	Question  string
	// Proposal is the optional structured proposal envelope {kind, summary, body}
	// attached to the question. Nil for plain-text questions.
	Proposal        map[string]any
	Options         []AgentQuestionOption
	InteractionType AgentQuestionInteractionType
	Placeholder     string
	MaxLength       int
	// SkipCancel skips cancelling prior pending questions for the run. Set by the
	// tool-policy confirmation gate so parallel confirmations don't cancel siblings.
	SkipCancel bool
}

// CreateAndEmitQuestion creates an AgentQuestion record, optionally creates a
// notification, and emits an SSE event. It is used by both the ask_user tool
// and the tool-policy confirmation gate.
func CreateAndEmitQuestion(ctx context.Context, p CreateQuestionParams) (*AgentQuestion, error) {
	// Cancel any existing pending questions for this run, unless this is a batch
	// confirmation (parallel confirm gates must not cancel each other's questions).
	if !p.SkipCancel {
		if err := p.Repo.CancelPendingQuestionsForRun(ctx, p.RunID); err != nil {
			p.Logger.Warn("failed to cancel prior pending questions",
				slog.String("run_id", p.RunID),
				slog.String("error", err.Error()),
			)
		}
	}

	q := &AgentQuestion{
		RunID:           p.RunID,
		AgentID:         p.AgentID,
		ProjectID:       p.ProjectID,
		Question:        p.Question,
		Proposal:        p.Proposal,
		Options:         p.Options,
		InteractionType: p.InteractionType,
		Placeholder:     p.Placeholder,
		MaxLength:       p.MaxLength,
		Status:          QuestionStatusPending,
	}

	if err := p.Repo.CreateQuestion(ctx, q); err != nil {
		return nil, fmt.Errorf("failed to create question: %w", err)
	}

	p.Logger.Info("question created",
		slog.String("question_id", q.ID),
		slog.String("run_id", p.RunID),
		slog.String("question", p.Question),
	)

	// Notification (requires valid UserID)
	var notifID string
	if p.UserID != "" {
		deps := AskUserToolDeps{
			Repo:      p.Repo,
			Logger:    p.Logger,
			ProjectID: p.ProjectID,
			AgentID:   p.AgentID,
			RunID:     p.RunID,
			UserID:    p.UserID,
			EventsSvc: p.EventsSvc,
		}
		notifID = createQuestionNotificationDirect(ctx, deps, q)
	}
	if notifID != "" {
		if err := p.Repo.UpdateQuestionNotificationID(ctx, q.ID, notifID); err != nil {
			p.Logger.Warn("failed to link notification to question",
				slog.String("question_id", q.ID),
				slog.String("notification_id", notifID),
				slog.String("error", err.Error()),
			)
		}
	}

	// Always emit SSE event
	emitQuestionSSEEventDirect(p.EventsSvc, p.ProjectID, q)

	return q, nil
}

// BuildAskUserTool creates the ask_user ADK tool.
func BuildAskUserTool(deps AskUserToolDeps) (tool.Tool, error) {
	return functiontool.New(
		functiontool.Config{
			Name:        ToolNameAskUser,
			Description: "Ask the user a question and pause execution until they respond. Use this when you encounter ambiguity, need clarification, or require a decision from the user. You can provide structured options for the user to choose from, or leave options empty for a free-text response. Set interaction_type to 'buttons' (default, 2-5 options), 'select' (5+ options), 'multi_select' (multi-pick dropdown), or 'text' (free-text input with optional placeholder and max_length). When proposing a concrete change, also pass an optional 'proposal' argument — the envelope {\"kind\",\"summary\",\"body\"} with kind one of blueprint|skill|agent|mcp_server|provider|object (blueprint body {\"objectTypes\",\"relationshipTypes\"}) — so it renders a structured preview card instead of a raw code fence. After calling this tool, execution will pause and resume automatically when the user responds.",
		},
		func(ctx tool.Context, args map[string]any) (map[string]any, error) {
			// Parse question (required); accept "message" as alias for "question"
			question, _ := args["question"].(string)
			if question == "" {
				question, _ = args["message"].(string)
			}
			if question == "" {
				return map[string]any{"error": "question is required"}, nil
			}

			// Parse options (optional array of {label, value})
			options := parseQuestionOptions(args)

			// Parse interaction_type (optional, defaults to "buttons")
			interactionTypeStr, _ := args["interaction_type"].(string)
			interactionType := QuestionInteractionButtons
			if interactionTypeStr != "" {
				switch AgentQuestionInteractionType(interactionTypeStr) {
				case QuestionInteractionButtons, QuestionInteractionSelect, QuestionInteractionMultiSelect, QuestionInteractionText:
					interactionType = AgentQuestionInteractionType(interactionTypeStr)
				default:
					deps.Logger.Warn("ask_user: unsupported interaction_type, defaulting to buttons",
						slog.String("interaction_type", interactionTypeStr),
					)
				}
			}

			// Validate: options-based interaction types require at least one option
			switch interactionType {
			case QuestionInteractionButtons, QuestionInteractionSelect, QuestionInteractionMultiSelect:
				if len(options) == 0 {
					return map[string]any{
						"error": fmt.Sprintf(
							"interaction_type %q requires at least one option with label and value fields. "+
								"Provide options or use interaction_type \"text\" for a free-text response.",
							interactionType,
						),
					}, nil
				}
			}

			// Parse placeholder and max_length (for text interaction type)
			placeholder, _ := args["placeholder"].(string)
			var maxLength int
			if raw, ok := args["max_length"]; ok {
				switch v := raw.(type) {
				case float64:
					maxLength = int(v)
				case int:
					maxLength = v
				case int64:
					maxLength = int(v)
				case json.Number:
					if n, err := v.Int64(); err == nil {
						maxLength = int(n)
					} else {
						deps.Logger.Warn("ask_user: invalid json.Number max_length, ignoring", slog.Any("max_length", raw))
					}
				default:
					deps.Logger.Warn("ask_user: unsupported max_length type, ignoring", slog.Any("max_length", raw))
				}
				if maxLength < 0 {
					deps.Logger.Warn("ask_user: negative max_length is invalid, ignoring", slog.Int("max_length", maxLength))
					maxLength = 0
				}
			}

			// Parse proposal (optional structured envelope {kind, summary, body}).
			// An invalid proposal is dropped silently so the question degrades to
			// plain text — the run must not hard-error on a malformed proposal.
			proposal := parseProposal(args)
			if _, present := args["proposal"]; present && proposal == nil {
				deps.Logger.Warn("ask_user: invalid proposal dropped, proceeding with plain-text question",
					slog.Any("proposal", args["proposal"]),
				)
			}

			q, err := CreateAndEmitQuestion(ctx, CreateQuestionParams{
				Repo:            deps.Repo,
				Logger:          deps.Logger,
				ProjectID:       deps.ProjectID,
				AgentID:         deps.AgentID,
				RunID:           deps.RunID,
				UserID:          deps.UserID,
				EventsSvc:       deps.EventsSvc,
				Question:        question,
				Proposal:        proposal,
				Options:         options,
				InteractionType: interactionType,
				Placeholder:     placeholder,
				MaxLength:       maxLength,
			})
			if err != nil {
				return map[string]any{"error": err.Error()}, nil
			}

			// Signal the executor to pause on the next beforeModelCb
			deps.PauseState.RequestPause(q.ID)

			return map[string]any{
				"question_id": q.ID,
				"status":      "pausing",
				"message":     "Your question has been sent to the user. Execution will pause now and resume when the user responds.",
			}, nil
		},
	)
}

// createQuestionNotificationDirect inserts a notification for the agent question.
// Returns the notification ID, or empty string on failure.
func createQuestionNotificationDirect(ctx context.Context, deps AskUserToolDeps, q *AgentQuestion) string {
	notifType := "agent_question"
	sourceType := "agent_run"
	relatedType := "agent_question"
	importance := "important"

	notif := &notifications.Notification{
		ProjectID:           &deps.ProjectID,
		UserID:              deps.UserID,
		Title:               "Agent needs your input",
		Message:             q.Question,
		Type:                &notifType,
		Severity:            "info",
		SourceType:          &sourceType,
		SourceID:            &deps.RunID,
		RelatedResourceType: &relatedType,
		RelatedResourceID:   &q.ID,
		Importance:          importance,
	}

	// Map options to notification actions
	if len(q.Options) > 0 {
		actions := make([]map[string]string, 0, len(q.Options))
		for _, opt := range q.Options {
			actions = append(actions, map[string]string{
				"label": opt.Label,
				"value": opt.Value,
			})
		}
		actionsJSON, err := json.Marshal(actions)
		if err == nil {
			notif.Actions = actionsJSON
		}
	} else {
		// Open-ended question: set actionURL for response page
		actionURL := fmt.Sprintf("/agents/questions/%s", q.ID)
		notif.ActionURL = &actionURL
		notif.Actions = json.RawMessage("[]")
	}

	// Insert notification directly (cross-domain insert)
	notifID, err := deps.Repo.CreateNotification(ctx, notif)
	if err != nil {
		deps.Logger.Warn("failed to create question notification",
			slog.String("question_id", q.ID),
			slog.String("error", err.Error()),
		)
		return ""
	}

	deps.Logger.Info("ask_user: notification created",
		slog.String("question_id", q.ID),
		slog.String("notification_id", notifID),
	)

	// NOTE: SSE event is emitted unconditionally by the caller (emitQuestionSSEEventDirect).
	// No need to emit it here as well — that would create duplicates.

	return notifID
}

// emitQuestionSSEEventDirect sends a real-time SSE notification for a question.
func emitQuestionSSEEventDirect(eventsSvc *events.Service, projectID string, q *AgentQuestion) {
	if eventsSvc == nil {
		return
	}
	data := map[string]any{
		"type":             "agent_question",
		"question_id":      q.ID,
		"run_id":           q.RunID,
		"question":         q.Question,
		"options":          q.Options,
		"interaction_type": q.InteractionType,
		"placeholder":      q.Placeholder,
		"max_length":       q.MaxLength,
		"status":           "pending",
	}
	if q.Proposal != nil {
		data["proposal"] = q.Proposal
	}
	eventsSvc.EmitCreated(
		events.EntityNotification,
		q.ID,
		projectID,
		&events.EmitOptions{
			Data: data,
		},
	)
}

// parseQuestionOptions extracts the options array from tool call args.
func parseQuestionOptions(args map[string]any) []AgentQuestionOption {
	optionsRaw, ok := args["options"]
	if !ok {
		return nil
	}

	optionsList, ok := optionsRaw.([]any)
	if !ok || len(optionsList) == 0 {
		return nil
	}

	options := make([]AgentQuestionOption, 0, len(optionsList))
	for _, item := range optionsList {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		label, _ := m["label"].(string)
		value, _ := m["value"].(string)
		if label == "" || value == "" {
			continue
		}
		opt := AgentQuestionOption{
			Label: label,
			Value: value,
		}
		if desc, ok := m["description"].(string); ok {
			opt.Description = desc
		}
		options = append(options, opt)
	}

	return options
}

// parseProposal extracts and validates the optional "proposal" argument on
// ask_user. It returns the validated envelope when present and well-formed, or
// nil when the proposal is absent or invalid. Validation only checks the
// envelope shape {kind: string, summary: string, body: object}; the body's
// kind-specific contents are the gateway's concern. Invalid proposals are
// dropped by the caller so the question proceeds as plain text (no tool error).
func parseProposal(args map[string]any) map[string]any {
	raw, ok := args["proposal"]
	if !ok || raw == nil {
		return nil
	}

	m, ok := raw.(map[string]any)
	if !ok {
		return nil
	}

	kind, _ := m["kind"].(string)
	if kind == "" {
		return nil
	}

	summary, _ := m["summary"].(string)
	if summary == "" {
		return nil
	}

	body, ok := m["body"]
	if !ok {
		return nil
	}
	if _, isObject := body.(map[string]any); !isObject {
		return nil
	}

	return m
}

// CreateNotification inserts a notification record directly into kb.notifications.
// This is a cross-domain insert used by the agents module to create question notifications.
func (r *Repository) CreateNotification(ctx context.Context, n *notifications.Notification) (string, error) {
	if n.CreatedAt.IsZero() {
		n.CreatedAt = time.Now()
	}
	if n.UpdatedAt.IsZero() {
		n.UpdatedAt = time.Now()
	}
	_, err := r.db.NewInsert().Model(n).Exec(ctx)
	if err != nil {
		return "", err
	}
	return n.ID, nil
}
