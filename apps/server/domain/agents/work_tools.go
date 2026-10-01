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
// records the run-finalizing signal. When the agent's work contract declares
// required deliverables, those must be produced and declared before the item
// can be completed.
func BuildWorkCompleteTool(deps WorkToolDeps) (tool.Tool, error) {
	return functiontool.New(
		functiontool.Config{
			Name:        ToolNameWorkComplete,
			Description: "Mark the current work item complete and end the run. Call this exactly once when the work is finished. Provide a summary of what was accomplished, the list of produced object keys/artifacts, and/or structured deliverables (an array of {type, key} entries naming the objects you produced). After this call the run ends — do not call any further tools. If your agent requires review, the item moves to review instead of done. If your agent declares a work contract, the required deliverables must be produced and declared before the item can be completed.",
		},
		func(ctx tool.Context, args map[string]any) (map[string]any, error) {
			return completeWork(ctx, deps, args)
		},
	)
}

// workDeliverable is a declared deliverable on a work_complete call: an object
// type plus its key.
type workDeliverable struct {
	Type string `json:"type"`
	Key  string `json:"key"`
}

// completeWork is the work_complete handler body, extracted for testability. It
// validates the work contract (when non-empty), then transitions the subject
// object and records the run-finalizing signal. A rejected contract returns an
// error tool result (nil Go error) so the agent may continue in the same run —
// no failure budget is consumed.
func completeWork(ctx context.Context, deps WorkToolDeps, args map[string]any) (map[string]any, error) {
	summary, _ := args["summary"].(string)
	artifacts := parseStringList(args["artifacts"])
	deliverables := parseDeliverables(args["deliverables"])

	wc := deps.WorkConfig
	doneStatus := wc.DoneStatus()
	reviewStatus := wc.ReviewStatus()
	inProgressStatus := wc.InProgressStatus()

	if deps.Store == nil || deps.CanonicalID == "" {
		return map[string]any{"error": "work_complete: no subject work object to complete"}, nil
	}

	// Work-contract validation (P6): an agent that declares required
	// deliverables must have produced them. A rejected completion returns an
	// error tool result and does NOT finalize the run (no terminator recorded,
	// item unchanged), so the agent may continue in the same run.
	if msg := validateWorkContract(ctx, deps.Store, deps.ProjectID, wc.WorkContract, summary, artifacts, deliverables); msg != "" {
		deps.Logger.Warn("work_complete: work contract not satisfied",
			slog.String("canonical_id", deps.CanonicalID),
			slog.String("reason", msg),
		)
		return map[string]any{"error": msg}, nil
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
		"deliverables":    deliverablesForOutput(deliverables),
		"requires_review": wc.RequiresReview,
	}

	// Run-finalizing: record the terminator so the executor stops the run
	// and reports the outcome; any later steps are ignored.
	deps.Terminator.Finalize(WorkTerminatorComplete, map[string]any{
		"work_terminator": WorkTerminatorComplete,
		"work_status":     targetStatus,
		"summary":         summary,
		"artifacts":       artifacts,
		"deliverables":    deliverablesForOutput(deliverables),
	})

	return result, nil
}

// parseStringList decodes a []string argument ([]any or []string) into a string
// slice.
func parseStringList(raw any) []string {
	var out []string
	switch v := raw.(type) {
	case []any:
		for _, item := range v {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
	case []string:
		out = append(out, v...)
	}
	return out
}

// parseDeliverables decodes the deliverables argument ([] of {type, key}) into
// a slice of workDeliverable, dropping malformed entries.
func parseDeliverables(raw any) []workDeliverable {
	var out []workDeliverable
	items, ok := raw.([]any)
	if !ok {
		return out
	}
	for _, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		d := workDeliverable{}
		d.Type, _ = m["type"].(string)
		d.Key, _ = m["key"].(string)
		if d.Type != "" && d.Key != "" {
			out = append(out, d)
		}
	}
	return out
}

// deliverablesForOutput converts declared deliverables to the JSON-friendly
// output shape.
func deliverablesForOutput(ds []workDeliverable) []map[string]string {
	out := make([]map[string]string, 0, len(ds))
	for _, d := range ds {
		out = append(out, map[string]string{"type": d.Type, "key": d.Key})
	}
	return out
}

// validateWorkContract checks a work_complete call against the resolved work
// contract and returns an error message (empty = satisfied). It never consumes
// the per-item failure budget: a rejected completion returns an error tool
// result and the run continues.
func validateWorkContract(ctx context.Context, store WorkObjectStore, projectID string, contract AgentWorkContract, summary string, artifacts []string, deliverables []workDeliverable) string {
	if contract.IsZero() {
		return ""
	}
	if contract.RequireArtifacts && summary == "" && len(artifacts) == 0 {
		return "work_complete: work contract requires artifacts or a summary; provide produced artifacts or a summary before completing"
	}
	if len(contract.RequiredDeliverableTypes) == 0 {
		return ""
	}

	declared := make(map[string][]string)
	for _, d := range deliverables {
		declared[d.Type] = append(declared[d.Type], d.Key)
	}

	for _, dt := range contract.RequiredDeliverableTypes {
		keys := declared[dt]
		if len(keys) == 0 {
			return fmt.Sprintf("work_complete: work contract requires a deliverable of type %q, but none was declared", dt)
		}
		found := false
		for _, key := range keys {
			if key == "" {
				continue
			}
			head, err := store.FindHeadByTypeAndKey(ctx, projectID, dt, key)
			if err != nil {
				return fmt.Sprintf("work_complete: could not verify deliverable of type %q: %v", dt, err)
			}
			if head != nil {
				found = true
				break
			}
		}
		if !found {
			return fmt.Sprintf("work_complete: work contract requires a deliverable of type %q that resolves to an existing object", dt)
		}
	}
	return ""
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
