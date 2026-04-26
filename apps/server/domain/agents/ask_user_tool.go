package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
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

// BuildAskUserTool creates the ask_user ADK tool.
func BuildAskUserTool(deps AskUserToolDeps) (tool.Tool, error) {
	return functiontool.New(
		functiontool.Config{
			Name:        ToolNameAskUser,
			Description: "Ask the user a question and pause execution until they respond. Use this when you encounter ambiguity, need clarification, or require a decision from the user. You can provide structured options for the user to choose from, or leave options empty for a free-text response. Set interaction_type to 'buttons' (default, 2-5 options), 'select' (5+ options), 'multi_select' (multi-pick dropdown), or 'text' (free-text input with optional placeholder and max_length). After calling this tool, execution will pause and resume automatically when the user responds.",
		},
		func(ctx tool.Context, args map[string]any) (map[string]any, error) {
			// Parse question (required)
			question, _ := args["question"].(string)
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

			// Cancel any existing pending questions for this run
			if err := deps.Repo.CancelPendingQuestionsForRun(ctx, deps.RunID); err != nil {
				deps.Logger.Warn("failed to cancel prior pending questions",
					slog.String("run_id", deps.RunID),
					slog.String("error", err.Error()),
				)
			}

			// Create the question record
			q := &AgentQuestion{
				RunID:           deps.RunID,
				AgentID:         deps.AgentID,
				ProjectID:       deps.ProjectID,
				Question:        question,
				Options:         options,
				InteractionType: interactionType,
				Placeholder:     placeholder,
				MaxLength:       maxLength,
				Status:          QuestionStatusPending,
			}

			if err := deps.Repo.CreateQuestion(ctx, q); err != nil {
				return map[string]any{"error": fmt.Sprintf("failed to create question: %s", err.Error())}, nil
			}

			deps.Logger.Info("ask_user: question created",
				slog.String("question_id", q.ID),
				slog.String("run_id", deps.RunID),
				slog.String("question", question),
				slog.Int("options_count", len(options)),
			)

			// Create a notification for the user (requires valid UserID).
			// For API-token-triggered runs UserID may be empty — skip the DB notification
			// but still emit the SSE event so connected clients (Discord bot) receive it.
			var notifID string
			if deps.UserID != "" {
				notifID = createQuestionNotification(ctx, deps, q)
			}
			if notifID != "" {
				// Link notification to question
				if err := deps.Repo.UpdateQuestionNotificationID(ctx, q.ID, notifID); err != nil {
					deps.Logger.Warn("failed to link notification to question",
						slog.String("question_id", q.ID),
						slog.String("notification_id", notifID),
						slog.String("error", err.Error()),
					)
				}
			}

			// Always emit the SSE event so connected clients (Discord bot, admin UI)
			// receive the question in real-time, even without a DB notification.
			emitQuestionSSEEvent(ctx, deps, q)

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

// createQuestionNotification inserts a notification for the agent question.
// Returns the notification ID, or empty string on failure.
func createQuestionNotification(ctx tool.Context, deps AskUserToolDeps, q *AgentQuestion) string {
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

	// NOTE: SSE event is emitted unconditionally by the caller (emitQuestionSSEEvent).
	// No need to emit it here as well — that would create duplicates.

	return notifID
}

// emitQuestionSSEEvent sends a real-time SSE notification for a question.
// This runs unconditionally so connected clients (Discord bot, admin UI) receive
// the question even when no DB notification was created (e.g. API-token-triggered runs).
func emitQuestionSSEEvent(ctx tool.Context, deps AskUserToolDeps, q *AgentQuestion) {
	if deps.EventsSvc == nil {
		return
	}
	deps.EventsSvc.EmitCreated(
		events.EntityNotification,
		q.ID, // use question ID as entity ID since there may be no notification
		deps.ProjectID,
		&events.EmitOptions{
			Data: map[string]any{
				"type":             "agent_question",
				"question_id":      q.ID,
				"run_id":           q.RunID,
				"question":         q.Question,
				"options":          q.Options,
				"interaction_type": q.InteractionType,
				"placeholder":      q.Placeholder,
				"max_length":       q.MaxLength,
				"status":           "pending",
			},
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
