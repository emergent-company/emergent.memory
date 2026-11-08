package main

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/labstack/echo/v4"

	ui "github.com/emergent-company/go-daisy/components/ui"
)

// --- skill-capture proposal inbox ---

// captureProposalFilterStatuses is the filter strip the inbox offers, in display
// order. "all" removes the filter; the transient "applying" state is reachable
// only through "all" (a row there is being written by the reviewer that claimed
// it, so it must not be offered for a second decision).
var captureProposalFilterStatuses = []string{captureProposalPending, captureProposalApplied, captureProposalRejected, captureProposalFailed, "all"}

// normalizeCaptureProposalStatus maps a query param to one of the offered filters,
// defaulting an unknown value to pending so a hand-typed URL still renders the
// actionable view.
func normalizeCaptureProposalStatus(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	switch s {
	case captureProposalApplied, captureProposalRejected, captureProposalFailed, "all":
		return s
	default:
		return captureProposalPending
	}
}

// captureProposalStatusLabel is the human label for a proposal status.
func captureProposalStatusLabel(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case captureProposalApplying:
		return "Applying"
	case captureProposalApplied:
		return "Applied"
	case captureProposalRejected:
		return "Rejected"
	case captureProposalFailed:
		return "Failed"
	default:
		return "Pending"
	}
}

// captureProposalStatusIntent is the badge tone for a proposal status.
func captureProposalStatusIntent(status string) ui.BadgeIntent {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case captureProposalApplied:
		return ui.BadgeSuccess
	case captureProposalApplying:
		return ui.BadgeInfo
	case captureProposalFailed:
		return ui.BadgeError
	case captureProposalRejected:
		return ui.BadgeGhost
	default:
		return ui.BadgeWarning
	}
}

// captureProposalKindLabel is the human label for what accepting the proposal does.
func captureProposalKindLabel(kind string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "patch":
		return "Update"
	case "delete":
		return "Delete"
	default:
		return "New skill"
	}
}

// captureProposalKindIntent is the badge tone for a proposal kind. A delete is
// destructive, so it reads as an error even while pending.
func captureProposalKindIntent(kind string) ui.BadgeIntent {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "patch":
		return ui.BadgeInfo
	case "delete":
		return ui.BadgeError
	default:
		return ui.BadgePrimary
	}
}

// captureProposalKindIcon is the icon tile glyph for a proposal kind.
func captureProposalKindIcon(kind string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "patch":
		return "lucide--pencil-line"
	case "delete":
		return "lucide--trash-2"
	default:
		return "lucide--sparkles"
	}
}

// captureProposalKindTone is the IconTile tone for a proposal kind.
func captureProposalKindTone(kind string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "patch":
		return "info"
	case "delete":
		return "error"
	default:
		return "primary"
	}
}

// captureProposalConfidenceLabel renders the review's confidence as a percentage. The
// server always sets it (0–1); an unstaged zero reads as unknown rather than 0%.
func captureProposalConfidenceLabel(confidence float64) string {
	if confidence <= 0 {
		return "confidence unknown"
	}
	return strconv.FormatFloat(confidence*100, 'f', 0, 64) + "% confident"
}

// captureProposalSourceLabel names where the proposal came from.
func captureProposalSourceLabel(source string) string {
	switch strings.ToLower(strings.TrimSpace(source)) {
	case "foreground":
		return "from a chat review"
	case "background", "":
		return "from a background review"
	default:
		return source
	}
}

// captureProposalFilterVariant highlights the active filter in the segmented control.
func captureProposalFilterVariant(status, active string) ui.ButtonVariant {
	if status == active {
		return ui.ButtonPrimary
	}
	return ""
}

// lintFindingIntent is the badge tone for a lint finding's severity. Errors never
// stage (the server drops the candidate), so "error" here is defensive.
func lintFindingIntent(severity string) ui.BadgeIntent {
	switch strings.ToLower(strings.TrimSpace(severity)) {
	case "error":
		return ui.BadgeError
	case "info":
		return ui.BadgeInfo
	default:
		return ui.BadgeWarning
	}
}

// captureProposalApproveLabel names what approving actually does, so the destructive
// case (delete) never reads like an addition.
func captureProposalApproveLabel(kind string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "patch":
		return "Approve and update skill"
	case "delete":
		return "Approve and delete skill"
	default:
		return "Approve and add skill"
	}
}

// captureProposalInboxAriaLabel names the inbox link for screen readers, including the
// count that is otherwise only a coloured bubble.
func captureProposalInboxAriaLabel(pending int) string {
	switch pending {
	case 0:
		return "Skill proposals — none waiting"
	case 1:
		return "Skill proposals — 1 awaiting review"
	default:
		return "Skill proposals — " + strconv.Itoa(pending) + " awaiting review"
	}
}

// captureProposalFilterHref is the inbox URL for one filter.
func captureProposalFilterHref(status string) string {
	return "/skills/proposals?status=" + url.QueryEscape(status)
}

// captureProposalFilterLabel names one filter. "all" is the unfiltered history;
// the rest read as the status they select ("Pending", "Applied", …) rather than
// pluralised nouns, which the status label would produce wrongly.
func captureProposalFilterLabel(status string) string {
	if status == "all" {
		return "All"
	}
	return captureProposalStatusLabel(status)
}

// uiSkillCaptureProposals renders the capture-review inbox (GET /skills/proposals).
// status filters the list; approved/rejected flash the outcome of a decision.
func (s *Server) uiSkillCaptureProposals(c echo.Context) error {
	ctx := c.Request().Context()
	status := normalizeCaptureProposalStatus(c.QueryParam("status"))
	proposals, err := s.memory.ListSkillProposals(ctx, status, 0)
	var loadErr error
	if err != nil {
		loadErr = userFacingError(err)
	}
	var flashMsg string
	switch {
	case c.QueryParam("approved") != "":
		flashMsg = "Proposal approved — it is in the skill library."
	case c.QueryParam("rejected") != "":
		flashMsg = "Proposal rejected."
	}
	// Best-effort diagnostic: with capture off the inbox is empty by
	// construction, which otherwise reads as "the feature is broken".
	captureMode := ""
	if project, perr := s.memory.GetCurrentProject(ctx); perr == nil && project != nil {
		captureMode = project.SkillCaptureMode()
	}
	return s.page(c, pageTitle("Skill proposals"), SkillCaptureProposalsPage(proposals, status, captureMode, loadErr, flashMsg, flashError(c)))
}

// uiSkillCaptureProposal renders one proposal (GET /skills/proposals/:id): the candidate
// content, the evidence behind it, lint findings, and the decision form.
func (s *Server) uiSkillCaptureProposal(c echo.Context) error {
	ctx := c.Request().Context()
	proposal, err := s.memory.GetSkillProposal(ctx, c.Param("id"))
	if err != nil {
		return s.page(c, pageTitle("Skill proposal"), SkillCaptureProposalDetailPage(nil, projectCaptureMode(ctx, s), userFacingError(err), "", flashError(c)))
	}
	var flashMsg string
	switch {
	case c.QueryParam("approved") != "":
		flashMsg = "Proposal approved — it is in the skill library."
	case c.QueryParam("rejected") != "":
		flashMsg = "Proposal rejected."
	}
	return s.page(c, pageTitle("Proposal: "+proposal.Name), SkillCaptureProposalDetailPage(proposal, projectCaptureMode(ctx, s), nil, flashMsg, flashError(c)))
}

// projectCaptureMode reads the project's capture mode for the detail page's
// diagnostics. A failed fetch returns "" (unknown) and the page omits the hint.
func projectCaptureMode(ctx context.Context, s *Server) string {
	project, err := s.memory.GetCurrentProject(ctx)
	if err != nil || project == nil {
		return ""
	}
	return project.SkillCaptureMode()
}

// captureDecisionError is the message a failed approve/reject shows on the
// detail page. A memory 4xx carries the operator's answer — "proposal is not
// pending", "security scan blocked the write" — so its own copy is surfaced
// (without the host-shaped "memory <status> <code>:" prefix the raw error
// carries). Transport failures and 5xx stay behind the generic copy.
func captureDecisionError(err error) error {
	var he *memoryHTTPError
	if errors.As(err, &he) && he.Status >= 400 && he.Status < http.StatusInternalServerError && he.Message != "" {
		return errors.New(he.Message)
	}
	return userFacingError(err)
}

// uiApproveSkillCaptureProposal applies a proposal (POST
// /skills/proposals/:id/approve → PRG back to the detail page). memory re-runs
// the security scan, so a blocked write comes back as an error message here.
func (s *Server) uiApproveSkillCaptureProposal(c echo.Context) error {
	id := c.Param("id")
	note := strings.TrimSpace(c.FormValue("note"))
	if err := s.memory.ApproveSkillProposal(c.Request().Context(), id, note); err != nil {
		return redirectWithError(c, "/skills/proposals/"+url.PathEscape(id), captureDecisionError(err))
	}
	return c.Redirect(http.StatusSeeOther, "/skills/proposals/"+url.PathEscape(id)+"?approved=1")
}

// uiRejectSkillCaptureProposal discards a proposal (POST
// /skills/proposals/:id/reject → PRG back to the detail page).
func (s *Server) uiRejectSkillCaptureProposal(c echo.Context) error {
	id := c.Param("id")
	note := strings.TrimSpace(c.FormValue("note"))
	if err := s.memory.RejectSkillProposal(c.Request().Context(), id, note); err != nil {
		return redirectWithError(c, "/skills/proposals/"+url.PathEscape(id), captureDecisionError(err))
	}
	return c.Redirect(http.StatusSeeOther, "/skills/proposals/"+url.PathEscape(id)+"?rejected=1")
}

// pendingCaptureProposalCount is the badge number on the Skills header link. It is
// best-effort: an older memory without the proposals route simply shows none.
func (s *Server) pendingCaptureProposalCount(ctx context.Context) int {
	proposals, err := s.memory.ListSkillProposals(ctx, captureProposalPending, 200)
	if err != nil {
		return 0
	}
	return len(proposals)
}
