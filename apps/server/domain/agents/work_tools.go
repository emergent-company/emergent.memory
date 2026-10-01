package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/emergent-company/emergent.memory/domain/tasks"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
)

// WorkToolDeps holds the dependencies needed by the work_complete and
// work_block run-finalizing terminators.
type WorkToolDeps struct {
	Store       WorkObjectStore
	Repo        *Repository
	Logger      *slog.Logger
	ProjectID   string
	CanonicalID string // the subject work object's canonical_id
	WorkConfig  AgentWorkConfig
	Terminator  *WorkTerminatorState
}

// BuildWorkCompleteTool creates the work_complete terminator. It transitions
// the subject object to done (or review+needs_review when requiresReview) and
// records the run-finalizing signal.
func BuildWorkCompleteTool(deps WorkToolDeps) (tool.Tool, error) {
	return functiontool.New(
		functiontool.Config{
			Name:        ToolNameWorkComplete,
			Description: "Mark the current work item complete and end the run. Call this exactly once when the work is finished. Provide a summary of what was accomplished and (optionally) the list of produced object keys/artifacts. After this call the run ends — do not call any further tools. If your agent requires review, the item moves to review instead of done.",
		},
		func(ctx tool.Context, args map[string]any) (map[string]any, error) {
			summary, _ := args["summary"].(string)
			var artifacts []string
			if raw, ok := args["artifacts"]; ok {
				switch v := raw.(type) {
				case []any:
					for _, item := range v {
						if s, ok := item.(string); ok {
							artifacts = append(artifacts, s)
						}
					}
				case []string:
					artifacts = append(artifacts, v...)
				}
			}

			wc := deps.WorkConfig
			doneStatus := wc.DoneStatus()
			reviewStatus := wc.ReviewStatus()
			inProgressStatus := wc.InProgressStatus()

			if deps.Store == nil || deps.CanonicalID == "" {
				return map[string]any{"error": "work_complete: no subject work object to complete"}, nil
			}

			transitioned, err := deps.Store.CompleteWorkObject(
				ctx, deps.ProjectID, deps.CanonicalID, inProgressStatus, doneStatus, reviewStatus, wc.RequiresReview)
			if err != nil {
				deps.Logger.Error("work_complete: transition failed",
					slog.String("canonical_id", deps.CanonicalID),
					slog.String("error", err.Error()),
				)
				return map[string]any{"error": "work_complete: " + err.Error()}, nil
			}
			if !transitioned {
				// A failed compare-and-transition (e.g. a concurrent cancel/block)
				// means the object was never completed. Do not finalize the run: it
				// must surface as an error so the run is retried/blocked, not
				// cleared as a successful terminator.
				deps.Logger.Warn("work_complete: transition skipped (object not in progress)",
					slog.String("canonical_id", deps.CanonicalID),
				)
				return map[string]any{"error": "work_complete: object is not in the in-progress status (concurrent cancel/block or already transitioned)"}, nil
			}

			targetStatus := doneStatus
			if wc.RequiresReview {
				targetStatus = reviewStatus
			}

			result := map[string]any{
				"status":          "completed",
				"work_status":     targetStatus,
				"summary":         summary,
				"artifacts":       artifacts,
				"requires_review": wc.RequiresReview,
			}

			// Run-finalizing: record the terminator so the executor stops the run
			// and reports the outcome; any later steps are ignored.
			deps.Terminator.Finalize(WorkTerminatorComplete, map[string]any{
				"work_terminator": WorkTerminatorComplete,
				"work_status":     targetStatus,
				"summary":         summary,
				"artifacts":       artifacts,
			})

			return result, nil
		},
	)
}

// BuildWorkBlockTool creates the work_block terminator. It transitions the
// subject object to blocked and creates a human-facing kb.tasks row, then
// records the run-finalizing signal.
func BuildWorkBlockTool(deps WorkToolDeps) (tool.Tool, error) {
	return functiontool.New(
		functiontool.Config{
			Name:        ToolNameWorkBlock,
			Description: "Block the current work item because it cannot proceed, and end the run. Provide the reason the work is blocked and an optional kind. This surfaces a human-facing task so a person can retry, reassign, or cancel. After this call the run ends — do not call any further tools.",
		},
		func(ctx tool.Context, args map[string]any) (map[string]any, error) {
			reason, _ := args["reason"].(string)
			if reason == "" {
				return map[string]any{"error": "reason is required"}, nil
			}
			kind, _ := args["kind"].(string)

			wc := deps.WorkConfig
			inProgressStatus := wc.InProgressStatus()
			blockedStatus := wc.BlockedStatus()

			if deps.Store == nil || deps.CanonicalID == "" {
				return map[string]any{"error": "work_block: no subject work object to block"}, nil
			}

			transitioned, err := deps.Store.BlockWorkObject(
				ctx, deps.ProjectID, deps.CanonicalID, inProgressStatus, blockedStatus)
			if err != nil {
				deps.Logger.Error("work_block: transition failed",
					slog.String("canonical_id", deps.CanonicalID),
					slog.String("error", err.Error()),
				)
				return map[string]any{"error": "work_block: " + err.Error()}, nil
			}
			if !transitioned {
				// The block transition failed (concurrent cancel/complete): do not
				// create the human task and do not finalize the run. The run must
				// surface as an error so it is retried/blocked by the failure policy.
				deps.Logger.Warn("work_block: transition skipped (object not in progress)",
					slog.String("canonical_id", deps.CanonicalID),
				)
				return map[string]any{"error": "work_block: object is not in the in-progress status (concurrent cancel/complete or already transitioned)"}, nil
			}

			// Human-facing task (kb.tasks) — best-effort; a failure to create the
			// task does not undo the block.
			taskID := ""
			if deps.Repo != nil {
				desc := reason
				meta, _ := json.Marshal(map[string]any{
					"canonical_id": deps.CanonicalID,
					"kind":         kind,
					"reason":       reason,
				})
				t := &tasks.Task{
					ProjectID:   deps.ProjectID,
					Title:       fmt.Sprintf("Work item blocked: %s", reason),
					Description: &desc,
					Type:        "work-escalation",
					Status:      "pending",
					SourceType:  strPtr("work_object"),
					SourceID:    &deps.CanonicalID,
					Metadata:    meta,
					CreatedAt:   time.Now(),
					UpdatedAt:   time.Now(),
				}
				if id, err := deps.Repo.CreateWorkTask(ctx, t); err != nil {
					deps.Logger.Warn("work_block: failed to create human task",
						slog.String("canonical_id", deps.CanonicalID),
						slog.String("error", err.Error()),
					)
				} else {
					taskID = id
				}
			}

			result := map[string]any{
				"status":      "blocked",
				"work_status": blockedStatus,
				"reason":      reason,
				"task_id":     taskID,
			}

			deps.Terminator.Finalize(WorkTerminatorBlock, map[string]any{
				"work_terminator": WorkTerminatorBlock,
				"work_status":     blockedStatus,
				"reason":          reason,
			})

			return result, nil
		},
	)
}

// CreateWorkTask inserts a human-facing task into kb.tasks directly. It is the
// cross-domain insert used by work_block (and the dead-letter / escalation
// paths) so the agents domain can surface human work without importing the
// tasks service.
func (r *Repository) CreateWorkTask(ctx context.Context, t *tasks.Task) (string, error) {
	if t.CreatedAt.IsZero() {
		t.CreatedAt = time.Now()
	}
	if t.UpdatedAt.IsZero() {
		t.UpdatedAt = time.Now()
	}
	_, err := r.db.NewInsert().Model(t).Exec(ctx)
	if err != nil {
		return "", err
	}
	return t.ID, nil
}
