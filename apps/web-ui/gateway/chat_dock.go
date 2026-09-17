package main

import (
	"context"
	"encoding/json"

	"github.com/emergent-company/go-daisy/render"
	"github.com/labstack/echo/v4"
)

// ApprovalCard is one pending tool-policy approval rendered in the chat dock.
// Frozen interface (design.md): field names and types are a hard contract for
// lane B.
type ApprovalCard struct {
	QuestionID     string
	ToolName       string
	ArgsSummary    string
	ConversationID string
}

// QuestionCard is one pending ask_user question rendered in the chat dock.
// Options carries the server's full option shape (label + value + description)
// so the dock can display the label and submit the value — the same contract
// the inline question card uses (chat-stream.js keys selection on opt.value).
type QuestionCard struct {
	QuestionID string
	Prompt     string
	Kind       string
	Options    []AgentQuestionOption
}

// TodoItem is one session todo rendered by the todo card.
type TodoItem struct {
	ID      string
	Content string
	Status  string
	Order   int
}

// argsSummaryString renders a tool approval's args map as a compact JSON
// summary for display; an empty/nil map renders as "".
func argsSummaryString(args map[string]any) string {
	if len(args) == 0 {
		return ""
	}
	b, err := json.Marshal(args)
	if err != nil {
		return ""
	}
	return string(b)
}

// questionOptionLabel renders one ask_user option as its display label,
// falling back to the raw value when no label is set.
func questionOptionLabel(o AgentQuestionOption) string {
	if o.Label != "" {
		return o.Label
	}
	return o.Value
}

// railBucketLabel maps a run bucket to its human rail-badge label. The done
// (idle) bucket has no label so idle rows read as plain.
func railBucketLabel(bucket string) string {
	switch bucket {
	case runBucketNeedsInput:
		return "Needs input"
	case runBucketFailed:
		return "Failed"
	case runBucketRunning:
		return "Running"
	default:
		return ""
	}
}

// todoItems maps session todos onto the dock's TodoItem shape, preserving
// order.
func todoItems(todos []SessionTodo) []TodoItem {
	items := make([]TodoItem, 0, len(todos))
	for _, t := range todos {
		items = append(items, TodoItem{ID: t.ID, Content: t.Content, Status: t.Status, Order: t.Order})
	}
	return items
}

// pendingDockCards resolves the active conversation's pending approvals and
// pending ask_user questions for the dock. Approvals come from the project
// approval snapshot filtered to this conversation; questions come from the
// project question snapshot correlated to this conversation's history via
// conversationState (which reuses the already-fetched snapshots, adding only
// one history fetch). Any fetch failure degrades to an empty side rather than
// an error.
func (s *Server) pendingDockCards(ctx context.Context, convID string) (approvals []ApprovalCard, questions []QuestionCard) {
	approvals = []ApprovalCard{}
	questions = []QuestionCard{}
	if convID == "" {
		return approvals, questions
	}
	appr, aErr := s.memory.ListToolApprovals(ctx)
	if aErr != nil {
		captureError(aErr)
		appr = nil
	}
	for _, a := range appr {
		if a.ConversationID == convID && a.Decision == "pending" {
			approvals = append(approvals, ApprovalCard{
				QuestionID:     a.QuestionID,
				ToolName:       a.ToolName,
				ArgsSummary:    argsSummaryString(a.ArgsSummary),
				ConversationID: a.ConversationID,
			})
		}
	}
	allQ, qErr := s.memory.ListAgentQuestions(ctx)
	if qErr != nil {
		captureError(qErr)
		allQ = nil
	}
	st, serr := s.conversationState(ctx, convID, appr, allQ)
	if serr != nil {
		return approvals, questions
	}
	byID := make(map[string]AgentQuestionItem, len(allQ))
	for _, q := range allQ {
		byID[q.ID] = q
	}
	for _, qid := range st.pendingQuestions {
		q, ok := byID[qid]
		if !ok {
			continue
		}
		questions = append(questions, QuestionCard{
			QuestionID: qid,
			Prompt:     q.Question,
			Kind:       q.InteractionType,
			Options:    q.Options,
		})
	}
	return approvals, questions
}

// uiChatDock implements GET /partial/chat-dock?c=<conversationId>: the
// DockPanel fragment for the active conversation's pending work, or an empty
// fragment when there is none. DockPanel renders nothing when both sides are
// empty, so this handler always returns the component.
func (s *Server) uiChatDock(c echo.Context) error {
	convID := c.QueryParam("c")
	approvals, questions := s.pendingDockCards(c.Request().Context(), convID)
	render.RenderPartial(c.Response().Writer, c.Request(), DockPanel(convID, approvals, questions))
	return nil
}

// uiChatTodos implements GET /partial/chat-todos?c=<conversationId>: the
// TodoCard fragment for the conversation's ACP session todos, or an empty
// fragment when the conversation has no ACP session or its session has no
// todos. It never errors — a missing conversation, missing session, or a
// fetch failure all degrade to an empty card.
func (s *Server) uiChatTodos(c echo.Context) error {
	convID := c.QueryParam("c")
	ctx := c.Request().Context()
	var items []TodoItem
	if convID != "" {
		if detail, err := s.memory.GetConversation(ctx, convID); err == nil && detail.ACPSessionID != "" {
			if todos, terr := s.memory.ListSessionTodos(ctx, detail.ACPSessionID); terr == nil {
				items = todoItems(todos)
			} else {
				captureError(terr)
			}
		}
	}
	render.RenderPartial(c.Response().Writer, c.Request(), TodoCard(items))
	return nil
}
