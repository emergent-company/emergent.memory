package main

import (
	"context"
	"encoding/json"

	"github.com/a-h/templ"
	components "github.com/emergent-company/emergent.memory/apps/web-ui/components"
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
// ProposalHTML is the sanitized proposal-card markup for a question that
// carries a structured proposal; the dock is the pending-work surface, so a
// proposal question re-rendered here must keep its reviewable card rather than
// dropping it (the inline transcript path renders the same markup).
type QuestionCard struct {
	QuestionID   string
	Prompt       string
	Kind         string
	Options      []AgentQuestionOption
	ProposalHTML string
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

// dockApprovalProps maps a dock approval card onto the shared approval
// component. The dock owns its identifiers and its control row (the
// data-dock-action buttons) through the component's Attrs and Actions slots.
func dockApprovalProps(a ApprovalCard) components.ApprovalCardProps {
	return components.ApprovalCardProps{
		ToolName: a.ToolName,
		Title:    "Approval",
		ArgsJSON: a.ArgsSummary,
		Mode:     components.ApprovalCardPending,
		Actions:  dockApprovalActions(a.QuestionID),
		Class:    "mx-3 my-2",
		Attrs: templ.Attributes{
			"data-testid":          "dock-approval",
			"data-dock-kind":       "approval",
			"data-question-id":     a.QuestionID,
			"data-conversation-id": a.ConversationID,
		},
	}
}

// dockQuestionProps maps a dock question card onto the shared question
// component: option rows carrying the dock's data-dock-option-value marker, the
// sanitized proposal slot, and the dock's footer controls.
func dockQuestionProps(q QuestionCard) components.QuestionCardProps {
	props := components.QuestionCardProps{
		Prompt:   templ.EscapeString(q.Prompt),
		Options:  dockQuestionOptions(q.Options),
		Multi:    q.Kind == "multi_select",
		Proposal: dockQuestionProposal(q),
		Controls: dockQuestionControls(q.QuestionID),
		Class:    "mx-3 my-2",
		Attrs: templ.Attributes{
			"data-testid":      "dock-question",
			"data-dock-kind":   "question",
			"data-dock-type":   q.Kind,
			"data-question-id": q.QuestionID,
		},
	}
	if q.Kind == "text" || len(q.Options) == 0 {
		props.Options = nil
		props.Text = &components.QuestionCardText{Placeholder: "Type your answer", AriaLabel: q.Prompt}
	}
	return props
}

// dockQuestionOptions maps the dock's option list onto the card's option rows,
// exposing each option's submitted value on data-dock-option-value.
func dockQuestionOptions(opts []AgentQuestionOption) []components.QuestionCardOption {
	if len(opts) == 0 {
		return nil
	}
	out := make([]components.QuestionCardOption, 0, len(opts))
	for _, o := range opts {
		out = append(out, components.QuestionCardOption{
			Label:       questionOptionLabel(o),
			Value:       o.Value,
			Description: o.Description,
			Attrs:       templ.Attributes{"data-dock-option-value": o.Value},
		})
	}
	return out
}

// dockQuestionProposal wraps the question's already-sanitized proposal card with
// the dock's stable testid, or returns nil when there is no proposal.
func dockQuestionProposal(q QuestionCard) templ.Component {
	if q.ProposalHTML == "" {
		return nil
	}
	return templ.Raw(`<div data-testid="dock-proposal">` + q.ProposalHTML + `</div>`)
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
			QuestionID:   qid,
			Prompt:       q.Question,
			Kind:         q.InteractionType,
			Options:      q.Options,
			ProposalHTML: renderProposalHTML(q.Proposal),
		})
	}
	return approvals, questions
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
		if detail, err := s.memory.GetConversation(ctx, convID); err == nil && detail.SessionID != "" {
			if todos, terr := s.memory.ListSessionTodos(ctx, detail.SessionID); terr == nil {
				items = todoItems(todos)
			} else {
				captureError(terr)
			}
		}
	}
	renderPartial(c.Response().Writer, c.Request(), TodoCard(items))
	return nil
}
