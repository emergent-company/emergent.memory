package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/emergent-company/emergent.memory/domain/graph"
	"github.com/emergent-company/emergent.memory/domain/tasks"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/functiontool"
)

// WorkToolDeps holds the dependencies needed by the work_complete, work_block,
// and comment run tools.
type WorkToolDeps struct {
	Store       WorkObjectStore
	Repo        *Repository
	Logger      *slog.Logger
	ProjectID   string
	CanonicalID string // the subject work object's canonical_id
	// SubjectObjectType is the subject work object's type; it selects the
	// per-type phase map and transition matrix.
	SubjectObjectType string
	// ActorName is the display name attributed to an agent-authored comment.
	ActorName string
	// RunID is the current run's id, stamped on an agent-authored comment.
	RunID      string
	WorkConfig AgentWorkConfig
	Terminator *WorkTerminatorState
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
		func(ctx agent.Context, args map[string]any) (map[string]any, error) {
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
	st := wc.EffectiveStatus(deps.SubjectObjectType)
	doneStatus := st.Done
	reviewStatus := st.Review

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

	head, err := deps.Store.GetHeadObject(ctx, deps.ProjectID, deps.CanonicalID)
	if err != nil {
		return map[string]any{"error": "work_complete: " + err.Error()}, nil
	}
	if head == nil {
		return map[string]any{"error": "work_complete: work object not found"}, nil
	}
	fromStatus := head.Status
	targetStatus := doneStatus
	if wc.RequiresReview {
		targetStatus = reviewStatus
	}
	rule := wc.RuleFor(deps.SubjectObjectType)
	if !rule.PermitsTransition(fromStatus, targetStatus, st) {
		return map[string]any{"error": fmt.Sprintf("work_complete: transition %q → %q is not allowed by this agent's work rules", fromStatus, targetStatus)}, nil
	}
	needsReview := wc.RequiresReview
	transitioned, err := deps.Store.TransitionWorkObject(ctx, deps.ProjectID, deps.CanonicalID, graph.WorkObjectTransition{
		FromStatus:     fromStatus,
		ToStatus:       targetStatus,
		SetNeedsReview: &needsReview,
		ClearAssignee:  rule != nil && rule.ClearAssigneeOnComplete,
	})
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
		func(ctx agent.Context, args map[string]any) (map[string]any, error) {
			reason, _ := args["reason"].(string)
			if reason == "" {
				return map[string]any{"error": "reason is required"}, nil
			}
			kind, _ := args["kind"].(string)

			if deps.Store == nil || deps.CanonicalID == "" {
				return map[string]any{"error": "work_block: no subject work object to block"}, nil
			}
			head, err := deps.Store.GetHeadObject(ctx, deps.ProjectID, deps.CanonicalID)
			if err != nil {
				return map[string]any{"error": "work_block: " + err.Error()}, nil
			}
			if head == nil {
				return map[string]any{"error": "work_block: work object not found"}, nil
			}
			// Blocked is durable execution state, not a status: the workflow
			// position is preserved for the human retry.
			blockedBy := deps.ActorName
			class := "agent"
			if deps.Repo != nil {
				if err := deps.Repo.UpsertWorkBlock(ctx, &WorkBlock{
					CanonicalID:  deps.CanonicalID,
					ProjectID:    deps.ProjectID,
					Reason:       reason,
					FailureClass: &class,
					BlockedBy:    &blockedBy,
					RunID:        strPtrIfNotEmpty(deps.RunID),
				}); err != nil {
					return map[string]any{"error": "work_block: " + err.Error()}, nil
				}
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
				"work_status": head.Status,
				"reason":      reason,
				"task_id":     taskID,
			}

			deps.Terminator.Finalize(WorkTerminatorBlock, map[string]any{
				"work_terminator": WorkTerminatorBlock,
				"work_status":     head.Status,
				"reason":          reason,
			})

			return result, nil
		},
	)
}

// BuildWorkTransitionTool creates the generic rule-gated status transition
// terminator. It moves the subject object along a from→to edge declared by the
// agent's per-type rule (an empty matrix allows the standard terminator edges),
// applies the review/blocked side effects, and ends the run.
func BuildWorkTransitionTool(deps WorkToolDeps) (tool.Tool, error) {
	return functiontool.New(
		functiontool.Config{
			Name:        ToolNameWorkTransition,
			Description: "Move the current work item to another status along an edge this agent is allowed to perform, then end the run. Provide the target status as 'to' and an optional summary/reason. The allowed edges come from the agent's per-object-type work rules; an unlisted edge is rejected and the item is unchanged.",
		},
		func(ctx agent.Context, args map[string]any) (map[string]any, error) {
			to, _ := args["to"].(string)
			if to == "" {
				return map[string]any{"error": "to is required"}, nil
			}
			if deps.Store == nil || deps.CanonicalID == "" {
				return map[string]any{"error": "work_transition: no subject work object to transition"}, nil
			}
			head, err := deps.Store.GetHeadObject(ctx, deps.ProjectID, deps.CanonicalID)
			if err != nil {
				return map[string]any{"error": "work_transition: " + err.Error()}, nil
			}
			if head == nil {
				return map[string]any{"error": "work_transition: work object not found"}, nil
			}
			st := deps.WorkConfig.EffectiveStatus(deps.SubjectObjectType)
			if !deps.WorkConfig.RuleFor(deps.SubjectObjectType).PermitsTransition(head.Status, to, st) {
				return map[string]any{"error": fmt.Sprintf("work_transition: transition %q → %q is not allowed by this agent's work rules", head.Status, to)}, nil
			}
			// Blocked is durable execution state, not a workflow status. Route a
			// transition to the blocked status through the same block row as
			// work_block so pickup stays suppressed.
			if to == st.Blocked {
				reason, _ := args["reason"].(string)
				if reason == "" {
					reason = "agent transitioned the item to blocked"
				}
				if deps.Repo != nil {
					class := "agent"
					blockedBy := deps.ActorName
					if err := deps.Repo.UpsertWorkBlock(ctx, &WorkBlock{
						CanonicalID:  deps.CanonicalID,
						ProjectID:    deps.ProjectID,
						Reason:       reason,
						FailureClass: &class,
						BlockedBy:    &blockedBy,
						RunID:        strPtrIfNotEmpty(deps.RunID),
					}); err != nil {
						return map[string]any{"error": "work_transition: " + err.Error()}, nil
					}
				}
				deps.Terminator.Finalize(WorkTerminatorTransition, map[string]any{
					"work_terminator": WorkTerminatorTransition,
					"work_status":     head.Status,
					"reason":          reason,
				})
				return map[string]any{"status": "blocked", "work_status": head.Status, "reason": reason}, nil
			}
			needsReview := to == st.Review
			transitioned, err := deps.Store.TransitionWorkObject(ctx, deps.ProjectID, deps.CanonicalID, graph.WorkObjectTransition{
				FromStatus:     head.Status,
				ToStatus:       to,
				SetNeedsReview: &needsReview,
			})
			if err != nil {
				return map[string]any{"error": "work_transition: " + err.Error()}, nil
			}
			if !transitioned {
				return map[string]any{"error": "work_transition: object is no longer in the expected status"}, nil
			}
			summary, _ := args["summary"].(string)
			reason, _ := args["reason"].(string)
			if to == st.Blocked {
				desc := reason
				if desc == "" {
					desc = "agent transitioned the item to blocked"
				}
				meta, _ := json.Marshal(map[string]any{"canonical_id": deps.CanonicalID, "reason": reason})
				if deps.Repo != nil {
					t := &tasks.Task{
						ProjectID:   deps.ProjectID,
						Title:       fmt.Sprintf("Work item blocked: %s", desc),
						Description: &desc,
						Type:        "work-escalation",
						Status:      "pending",
						SourceType:  strPtr("work_object"),
						SourceID:    &deps.CanonicalID,
						Metadata:    meta,
					}
					if _, err := deps.Repo.CreateWorkTask(ctx, t); err != nil && deps.Logger != nil {
						deps.Logger.Warn("work_transition: failed to create human task", slog.String("canonical_id", deps.CanonicalID), slog.String("error", err.Error()))
					}
				}
			}
			deps.Terminator.Finalize(WorkTerminatorTransition, map[string]any{
				"work_terminator": WorkTerminatorTransition,
				"work_status":     to,
				"summary":         summary,
				"reason":          reason,
			})
			return map[string]any{"status": "transitioned", "work_status": to}, nil
		},
	)
}

// BuildWorkCommentTool creates the per-run comment tool. It appends a durable
// comment to the subject work object with actor_type='agent' and the run id. It
// does NOT end the run and is not a substitute for work_complete's summary.
func BuildWorkCommentTool(deps WorkToolDeps) (tool.Tool, error) {
	return functiontool.New(
		functiontool.Config{
			Name:        ToolNameWorkComment,
			Description: "Post a durable note on the work item you are working on, for a human reviewer to read. Use this to record context, findings, or decisions a reviewer should see alongside the work. Provide the note as the 'body' argument. This does not change the work item's status and does not end the run; it is not a substitute for work_complete, which is still how you finish the work.",
		},
		func(ctx agent.Context, args map[string]any) (map[string]any, error) {
			return commentWork(ctx, deps, args)
		},
	)
}

// commentWork is the comment tool handler body, extracted for testability. It
// appends a durable comment with actor_type='agent' attributed to the run's
// agent name and stamped with the run id. It never ends the run.
func commentWork(ctx context.Context, deps WorkToolDeps, args map[string]any) (map[string]any, error) {
	body, _ := args["body"].(string)
	if body == "" {
		return map[string]any{"error": "body is required"}, nil
	}
	if deps.CanonicalID == "" {
		return map[string]any{"error": "comment: no subject work object to comment on"}, nil
	}
	if deps.Repo == nil {
		return map[string]any{"error": "comment: repository unavailable"}, nil
	}
	var runID *string
	if deps.RunID != "" {
		runID = &deps.RunID
	}
	if _, err := deps.Repo.AppendWorkComment(ctx, deps.ProjectID, deps.CanonicalID, deps.ActorName, body, "agent", runID); err != nil {
		deps.Logger.Error("comment: append failed",
			slog.String("canonical_id", deps.CanonicalID),
			slog.String("error", err.Error()),
		)
		return map[string]any{"error": "comment: " + err.Error()}, nil
	}
	return map[string]any{"status": "recorded"}, nil
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
