package main

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/emergent-company/go-daisy/components/ui"
	"github.com/labstack/echo/v4"
)

// --- Schedules (scheduled agents) UI ---

// uiSchedules renders the Schedules page: the list of scheduled agents with
// run-now and edit actions. A failed list fetch renders the whole-page error
// state; the agent-definition catalog is best-effort (a failure renders the
// page with an empty selector). ?created=1 / ?updated=1 / ?deleted=1 /
// ?triggered=1 / ?err=1 surface PRG feedback.
//
// ListScheduledAgents is a raw proxy of memory GET /projects/:id/agents, which
// returns ALL runtime agents (manual, webhook, reaction, …) — not just
// scheduled ones. Only triggerType "schedule" agents belong on this page, so
// the list is filtered here (matching scheduledSessionSummaries).
func (s *Server) uiSchedules(c echo.Context) error {
	ctx := c.Request().Context()
	all, err := s.memory.ListScheduledAgents(ctx)
	var schedules []ScheduledAgent
	for _, a := range all {
		if a.TriggerType == "schedule" {
			schedules = append(schedules, a)
		}
	}
	defs, defsErr := s.memory.ListAgentDefinitions(ctx)
	captureError(defsErr)
	flashErr := flashError(c)
	var flashMsg string
	switch {
	case c.QueryParam("created") != "":
		flashMsg = "Schedule created."
	case c.QueryParam("updated") != "":
		flashMsg = "Schedule updated."
	case c.QueryParam("deleted") != "":
		flashMsg = "Schedule deleted."
	case c.QueryParam("triggered") != "":
		flashMsg = "Run triggered."
	}
	return s.page(c, pageTitle("Schedules"), SchedulesPage(schedules, defs, err, flashMsg, flashErr))
}

// scheduleDetailData is the payload for ScheduleDetailPage: the scheduled
// agent being edited, its recent runs, the agent-definition catalog for the
// selector, and PRG feedback. A failed agent fetch renders the whole-page
// error state.
type scheduleDetailData struct {
	Agent    *ScheduledAgent
	Runs     []ScheduledAgentRun
	Defs     []AgentDefinitionSummary
	LoadErr  error
	FlashMsg string
	FlashErr error
}

// uiSchedule renders the per-schedule detail/edit page: breadcrumbs, run-now,
// the edit form (name, prompt, cron, enabled, agent definition), recent runs,
// and delete. ?updated=1 / ?err=1 surface PRG feedback from the update flow.
func (s *Server) uiSchedule(c echo.Context) error {
	ctx := c.Request().Context()
	id := c.Param("id")

	data := scheduleDetailData{}
	if c.QueryParam("updated") != "" {
		data.FlashMsg = "Schedule updated."
	}
	data.FlashErr = flashError(c)

	agent, err := s.memory.GetScheduledAgent(ctx, id)
	if err != nil {
		data.LoadErr = err
		return s.page(c, pageTitle("Schedule"), ScheduleDetailPage(data))
	}
	data.Agent = agent
	defs, err := s.memory.ListAgentDefinitions(ctx)
	captureError(err)
	data.Defs = defs
	// Runs are best-effort: a failure leaves the section empty rather than
	// failing the whole page.
	runs, err := s.memory.ListScheduledAgentRuns(ctx, id)
	captureError(err)
	data.Runs = runs

	return s.page(c, pageTitle(agent.Name), ScheduleDetailPage(data))
}

// uiSchedulesNew renders the create form for a new schedule. ?err=1 surfaces
// PRG feedback from the create flow.
func (s *Server) uiSchedulesNew(c echo.Context) error {
	defs, err := s.memory.ListAgentDefinitions(c.Request().Context())
	captureError(err)
	return s.page(c, pageTitle("New schedule"), SchedulesNewPage(defs, flashError(c)))
}

// scheduleFromForm parses the schedule create/edit form. strategyType is
// always "definition" and agentDefinitionId is required — the schedule runs
// the selected agent definition (memory resolves model/system-prompt/tools
// through the FK).
func scheduleFromForm(c echo.Context) (*ScheduledAgent, error) {
	name := strings.TrimSpace(c.FormValue("name"))
	cron := strings.TrimSpace(c.FormValue("cronSchedule"))
	defID := strings.TrimSpace(c.FormValue("agentDefinitionId"))
	if name == "" {
		return nil, fmt.Errorf("name is required")
	}
	if cron == "" {
		return nil, fmt.Errorf("cron schedule is required")
	}
	if fields := strings.Fields(cron); len(fields) != 5 {
		return nil, fmt.Errorf("cron schedule must be 5 fields (minute hour day month weekday), e.g. \"0 9 * * *\"")
	}
	if defID == "" {
		return nil, fmt.Errorf("pick an agent definition — the schedule runs that agent")
	}
	prompt := c.FormValue("prompt")
	a := &ScheduledAgent{
		StrategyType:      "definition",
		Name:              name,
		Prompt:            &prompt,
		CronSchedule:      cron,
		Enabled:           c.FormValue("enabled") == "on",
		TriggerType:       "schedule",
		AgentDefinitionID: &defID,
	}
	if d := strings.TrimSpace(c.FormValue("description")); d != "" {
		a.Description = &d
	}
	return a, nil
}

// uiScheduleCreate handles the new-schedule form (PRG): it forwards the fields
// to memory and redirects back to the list, or to ?err=1 when memory rejects
// them (invalid cron, missing definition, …).
func (s *Server) uiScheduleCreate(c echo.Context) error {
	in, err := scheduleFromForm(c)
	if err != nil {
		return redirectWithError(c, "/schedules/new", err)
	}
	if _, err := s.memory.CreateScheduledAgent(c.Request().Context(), in); err != nil {
		return redirectWithError(c, "/schedules/new", err)
	}
	return c.Redirect(http.StatusSeeOther, "/schedules?created=1")
}

// uiScheduleUpdate handles the edit form on the detail page (PRG). All form
// fields are sent, so the update is a full replacement of the editable
// fields.
func (s *Server) uiScheduleUpdate(c echo.Context) error {
	id := c.Param("id")
	in, err := scheduleFromForm(c)
	if err != nil {
		return redirectWithError(c, "/schedules", err)
	}
	if _, err := s.memory.UpdateScheduledAgent(c.Request().Context(), id, in); err != nil {
		return redirectWithError(c, "/schedules", err)
	}
	return c.Redirect(http.StatusSeeOther, "/schedules?updated=1")
}

// uiScheduleDelete handles the delete confirm form on the detail page (PRG).
func (s *Server) uiScheduleDelete(c echo.Context) error {
	id := c.Param("id")
	if err := s.memory.DeleteScheduledAgent(c.Request().Context(), id); err != nil {
		return redirectWithError(c, "/schedules", err)
	}
	return c.Redirect(http.StatusSeeOther, "/schedules?deleted=1")
}

// uiScheduleTrigger handles the manual-trigger button (PRG): it fires the
// agent immediately and redirects back to the list. A rejected trigger (e.g.
// memory refusing the run) surfaces its error via ?err=1.
func (s *Server) uiScheduleTrigger(c echo.Context) error {
	id := c.Param("id")
	res, err := s.memory.TriggerScheduledAgent(c.Request().Context(), id)
	if err != nil {
		return redirectWithError(c, "/schedules", err)
	}
	if res != nil && !res.Success {
		msg := "trigger rejected"
		if res.Error != nil && *res.Error != "" {
			msg = *res.Error
		}
		return redirectWithError(c, "/schedules", fmt.Errorf("%s", msg))
	}
	return c.Redirect(http.StatusSeeOther, "/schedules?triggered=1")
}

// uiScheduleToggle flips a schedule's enabled state from the row switch (PRG).
// The switch is a checkbox: checked → enabled, unchecked → disabled.
func (s *Server) uiScheduleToggle(c echo.Context) error {
	id := c.Param("id")
	enabled := c.FormValue("enabled") == "on"
	if _, err := s.memory.SetScheduledAgentEnabled(c.Request().Context(), id, enabled); err != nil {
		return redirectWithError(c, "/schedules", err)
	}
	return c.Redirect(http.StatusSeeOther, "/schedules")
}

// --- schedules display helpers (used by SchedulesPage) ---

// scheduleDefinitionName resolves the linked agent definition's name for
// display ("" when the id is unknown or unset).
func scheduleDefinitionName(defs []AgentDefinitionSummary, a ScheduledAgent) string {
	if a.AgentDefinitionID == nil {
		return ""
	}
	for _, d := range defs {
		if d.ID == *a.AgentDefinitionID {
			return d.Name
		}
	}
	return shortID(*a.AgentDefinitionID)
}

// scheduleLastRunLabel renders the "never run" state for agents without a
// last-run timestamp, else the relative time.
func scheduleLastRunLabel(a ScheduledAgent) string {
	if a.LastRunAt == nil || *a.LastRunAt == "" {
		return "Never run"
	}
	return relTime(*a.LastRunAt)
}

// scheduleStatusIntent maps a run status to a badge colour.
func scheduleStatusIntent(status *string) ui.BadgeIntent {
	if status == nil {
		return ui.BadgeNeutral
	}
	switch strings.ToLower(*status) {
	case "completed":
		return ui.BadgeSuccess
	case "failed":
		return ui.BadgeError
	case "working", "submitted":
		return ui.BadgeInfo
	default:
		return ui.BadgeNeutral
	}
}

// scheduleStatusLabel renders the last-run status for display: "Never run"
// when absent, otherwise the raw status.
func scheduleStatusLabel(a ScheduledAgent) string {
	if a.LastRunStatus == nil || *a.LastRunStatus == "" {
		return "Never run"
	}
	return *a.LastRunStatus
}

// schedulePromptValue returns the raw prompt text for form prefill ("" when
// unset) — distinct from schedulePrompt, whose placeholder is for display
// only and must never become the textarea's value.
func schedulePromptValue(a *ScheduledAgent) string {
	if a == nil || a.Prompt == nil {
		return ""
	}
	return *a.Prompt
}

// scheduleRunTitle labels a run row: "Run <short id>" plus its status.
func scheduleRunTitle(r ScheduledAgentRun) string {
	t := "Run " + shortID(r.ID)
	if r.Status != "" {
		t += " · " + r.Status
	}
	return t
}

// scheduleRunSubtitle is the meta line for a run row: started/finished times
// plus a short summary preview.
func scheduleRunSubtitle(r ScheduledAgentRun) string {
	s := "started " + relTime(r.StartedAt)
	if r.CompletedAt != nil && *r.CompletedAt != "" {
		s += " · finished " + relTime(*r.CompletedAt)
	}
	if p := scheduleRunSummary(r); p != "" {
		s += " · " + p
	}
	return s
}

// scheduleRunSummary renders a short preview of a run's summary for the
// schedule detail "Latest runs" list ("" when the run has none).
func scheduleRunSummary(r ScheduledAgentRun) string {
	p := scheduledRunPreview(r, r.AgentName)
	if p == "" {
		return ""
	}
	return truncateRunes(p, 60)
}
