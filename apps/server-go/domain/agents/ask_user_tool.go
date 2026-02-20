package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/emergent-company/emergent/domain/notifications"
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
}

// BuildAskUserTool creates the ask_user ADK tool.
func BuildAskUserTool(deps AskUserToolDeps) (tool.Tool, error) {
	return functiontool.New(
		functiontool.Config{
			Name:        ToolNameAskUser,
			Description: "Ask the user a question and pause execution until they respond. Use this when you encounter ambiguity, need clarification, or require a decision from the user. You can provide structured options for the user to choose from, or leave options empty for a free-text response. After calling this tool, execution will pause and resume automatically when the user responds.",
		},
		func(ctx tool.Context, args map[string]any) (map[string]any, error) {
			// Parse question (required)
			question, _ := args["question"].(string)
			if question == "" {
				return map[string]any{"error": "question is required"}, nil
			}

			// Parse options (optional array of {label, value})
			options := parseQuestionOptions(args)

			// Cancel any existing pending questions for this run
			if err := deps.Repo.CancelPendingQuestionsForRun(ctx, deps.RunID); err != nil {
				deps.Logger.Warn("failed to cancel prior pending questions",
					slog.String("run_id", deps.RunID),
					slog.String("error", err.Error()),
				)
			}

			// Create the question record
			q := &AgentQuestion{
				RunID:     deps.RunID,
				AgentID:   deps.AgentID,
				ProjectID: deps.ProjectID,
				Question:  question,
				Options:   options,
				Status:    QuestionStatusPending,
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

			// Create a notification for the user
			if deps.UserID != "" {
				notificationID := createQuestionNotification(ctx, deps, q)
				if notificationID != "" {
					// Link notification to question
					if err := deps.Repo.UpdateQuestionNotificationID(ctx, q.ID, notificationID); err != nil {
						deps.Logger.Warn("failed to link notification to question",
							slog.String("question_id", q.ID),
							slog.String("notification_id", notificationID),
							slog.String("error", err.Error()),
						)
					}
				}
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

	return notifID
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
