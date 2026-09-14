package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
)

func TestRenderSchedulesPage(t *testing.T) {
	status := "completed"
	lastRun := time.Now().Add(-25 * time.Minute).Format(time.RFC3339)
	schedules := []ScheduledAgent{
		{
			ID:            "a1",
			Name:          "Daily briefing",
			CronSchedule:  "0 8 * * *",
			Enabled:       true,
			TriggerType:   "schedule",
			LastRunAt:     &lastRun,
			LastRunStatus: &status,
			UpdatedAt:     time.Now().Add(-30 * time.Minute).Format(time.RFC3339),
		},
		{
			ID:           "a2",
			Name:         "Nightly cleanup",
			CronSchedule: "0 2 * * *",
			Enabled:      false,
			UpdatedAt:    time.Now().Add(-2 * time.Hour).Format(time.RFC3339),
		},
	}

	html := renderHTML(t, SchedulesPage(schedules, nil, nil, "", nil))
	for _, want := range []string{
		"Daily briefing", "Nightly cleanup",
		"0 8 * * *", "0 2 * * *",
		"completed", "25m ago",
		`action="/schedules/a1/trigger"`, "Run now",
		`href="/schedules/a1"`,
		`action="/schedules/a1/toggle"`, `data-href="/schedules/a1"`, `name="enabled"`, `type="checkbox"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("schedules page missing %q", want)
		}
	}
	if strings.Contains(html, "No schedules yet") {
		t.Error("list page must not render the empty state when schedules exist")
	}
	// the list must not carry the inline edit form or the delete dialog —
	// those live on the detail page now.
	for _, absent := range []string{`action="/schedules/a1/update"`, `action="/schedules/a1/delete"`, `name="agentDefinitionId"`, "schedule-delete-modal", "lucide--check", "lucide--pause", "lucide--pencil"} {
		if strings.Contains(html, absent) {
			t.Errorf("list page must not contain %q", absent)
		}
	}
}

func TestRenderScheduleDetailPage(t *testing.T) {
	prompt := "Summarize yesterday's notes"
	status := "completed"
	lastRun := time.Now().Add(-25 * time.Minute).Format(time.RFC3339)
	defID := "def-1"
	data := scheduleDetailData{
		Agent: &ScheduledAgent{
			ID:                "a1",
			Name:              "Daily briefing",
			Prompt:            &prompt,
			CronSchedule:      "0 8 * * *",
			Enabled:           true,
			TriggerType:       "schedule",
			LastRunAt:         &lastRun,
			LastRunStatus:     &status,
			AgentDefinitionID: &defID,
			UpdatedAt:         time.Now().Add(-30 * time.Minute).Format(time.RFC3339),
		},
		Defs: []AgentDefinitionSummary{{ID: "def-1", Name: "memory"}},
	}

	html := renderHTML(t, ScheduleDetailPage(data))
	for _, want := range []string{
		// breadcrumbs: Schedules > Daily briefing
		`href="/schedules"`, "Schedules", "Daily briefing",
		// edit form (posts to update)
		`action="/schedules/a1/update"`, "Save changes", `name="agentDefinitionId"`, "memory", "0 8 * * *",
		// run now
		`action="/schedules/a1/trigger"`, "Run now",
		// delete affordance + confirm dialog
		"Danger zone", "Delete this schedule", `action="/schedules/a1/delete"`,
		// summary badges
		"enabled", "completed", "25m ago",
		// prompt prefills the textarea with the raw value
		"Summarize yesterday", // templ escapes the apostrophe (&#39;)
	} {
		if !strings.Contains(html, want) {
			t.Errorf("schedule detail page missing %q", want)
		}
	}
}

func TestRenderScheduleDetailPageError(t *testing.T) {
	html := renderHTML(t, ScheduleDetailPage(scheduleDetailData{LoadErr: errTest}))
	if !strings.Contains(html, "Schedule unavailable") {
		t.Error("load error state missing")
	}
}

func TestRenderSchedulesPageEmpty(t *testing.T) {
	html := renderHTML(t, SchedulesPage(nil, nil, nil, "", nil))
	if !strings.Contains(html, "No schedules yet") {
		t.Error("empty state missing")
	}
	if strings.Contains(html, "Daily briefing") {
		t.Error("empty state must not render rows")
	}
	// a failed list fetch renders the whole-page error, not the empty state
	htmlErr := renderHTML(t, SchedulesPage(nil, nil, errTest, "", nil))
	if !strings.Contains(htmlErr, "Failed to load schedules") {
		t.Error("load error state missing")
	}
}

func TestRenderSchedulesNewPage(t *testing.T) {
	defs := []AgentDefinitionSummary{{ID: "def-1", Name: "memory"}, {ID: "def-2", Name: "diane"}}
	html := renderHTML(t, SchedulesNewPage(defs, nil))
	for _, want := range []string{
		`action="/schedules"`,
		`name="name"`,
		`name="prompt"`,
		`name="cronSchedule"`,
		`name="agentDefinitionId"`,
		`name="enabled"`,
		"Create schedule",
		"memory", "diane",
		`href="/schedules"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("new-schedule page missing %q", want)
		}
	}
}

// TestRenderChatPageOriginFilter asserts the chat rail carries the origin
// filter select (All types / Manual / Scheduled) and tags each session row
// with a data-origin attribute: "manual" for conversations, "scheduled" for
// scheduled-agent runs — the markup the rail JS filters on.
func TestRenderChatPageOriginFilter(t *testing.T) {
	agents := []AgentDefinitionSummary{{ID: "a1", Name: "memory"}}
	convs := &ConversationList{Conversations: []Conversation{{ID: "c1", Title: "Morning chat", AgentDefinitionID: "a1", UpdatedAt: "2026-08-26T09:00:00Z"}}}
	schedRuns := []scheduledRunRow{{ID: "run-1", AgentName: "Daily briefing", Status: "completed", StartedAt: "2026-08-27T08:00:00Z"}}

	html := renderHTML(t, ChatPage(agents, convs, map[string]string{"a1": "memory"}, schedRuns, "", "", "", nil, false, nil))

	// the filter select with the three options
	if !strings.Contains(html, `id="chat-origin-filter"`) {
		t.Error("origin filter select missing")
	}
	for _, opt := range []string{"All types", "Manual", "Scheduled"} {
		if !strings.Contains(html, opt) {
			t.Errorf("origin filter missing option %q", opt)
		}
	}
	// conversation row is manual
	if !strings.Contains(html, `data-origin="manual"`) {
		t.Error("conversation row missing data-origin=\"manual\"")
	}
	// scheduled run row is scheduled and opens in the chat workspace (the run
	// transcript loads in /chat via resumeRun, keyed off data-run-id)
	if !strings.Contains(html, `data-origin="scheduled"`) {
		t.Error("scheduled run row missing data-origin=\"scheduled\"")
	}
	if !strings.Contains(html, `data-action="open-run"`) {
		t.Error("scheduled run row missing open-run action")
	}
	if !strings.Contains(html, `data-run-id="run-1"`) {
		t.Error("scheduled run row missing data-run-id attribute")
	}
	if !strings.Contains(html, "Daily briefing") {
		t.Error("scheduled run row missing agent name")
	}
}

// TestUIRouteSchedules exercises the /schedules web routes against the fake
// backend: list (with filtering to triggerType "schedule"), detail/edit page,
// empty state, and the create/update/delete/trigger PRG redirects.
func TestUIRouteSchedules(t *testing.T) {
	f := &fakeMemory{
		scheduledAgents: []ScheduledAgent{
			{ID: "a1", Name: "Daily briefing", CronSchedule: "0 8 * * *", Enabled: true, StrategyType: "definition", TriggerType: "schedule", UpdatedAt: "2026-08-26T11:00:00Z"},
		},
		agents: []AgentDefinitionSummary{{ID: "def-1", Name: "memory"}},
	}
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
	e := echo.New()
	e.GET("/schedules", s.uiSchedules)
	e.GET("/schedules/new", s.uiSchedulesNew)
	e.GET("/schedules/:id", s.uiSchedule)
	e.POST("/schedules", s.uiScheduleCreate)
	e.POST("/schedules/:id/update", s.uiScheduleUpdate)
	e.POST("/schedules/:id/delete", s.uiScheduleDelete)
	e.POST("/schedules/:id/trigger", s.uiScheduleTrigger)
	e.POST("/schedules/:id/toggle", s.uiScheduleToggle)

	// list page renders the schedule row with run-now + toggle switch
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/schedules", nil))
	body := rec.Body.String()
	if rec.Code != http.StatusOK || !strings.Contains(body, "Daily briefing") {
		t.Fatalf("list page: status=%d body=%s", rec.Code, body)
	}
	for _, want := range []string{`action="/schedules/a1/trigger"`, `href="/schedules/a1"`} {
		if !strings.Contains(body, want) {
			t.Errorf("list page missing %q", want)
		}
	}

	// list filters to triggerType "schedule": a manual/chat-session agent is
	// NOT shown.
	fFilter := &fakeMemory{
		scheduledAgents: []ScheduledAgent{
			{ID: "a1", Name: "Daily briefing", TriggerType: "schedule"},
			{ID: "m1", Name: "Chat session for diane", StrategyType: "chat-session:d1", TriggerType: "manual"},
			{ID: "m2", Name: "Definition agent", StrategyType: "definition", TriggerType: "manual"},
		},
	}
	sF := &Server{cfg: Config{DefaultAgent: "memory"}, memory: fFilter}
	eF := echo.New()
	eF.GET("/schedules", sF.uiSchedules)
	rec = httptest.NewRecorder()
	eF.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/schedules", nil))
	bodyF := rec.Body.String()
	if !strings.Contains(bodyF, "Daily briefing") {
		t.Errorf("filtered list missing the scheduled agent: %s", bodyF)
	}
	for _, absent := range []string{"Chat session for diane", "Definition agent"} {
		if strings.Contains(bodyF, absent) {
			t.Errorf("filtered list must not show non-schedule agent %q", absent)
		}
	}

	// only manual agents → empty state, not a list of them
	fNone := &fakeMemory{scheduledAgents: []ScheduledAgent{{ID: "m1", Name: "Chat session", TriggerType: "manual"}}}
	sNone := &Server{cfg: Config{DefaultAgent: "memory"}, memory: fNone}
	eNone := echo.New()
	eNone.GET("/schedules", sNone.uiSchedules)
	rec = httptest.NewRecorder()
	eNone.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/schedules", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "No schedules yet") {
		t.Fatalf("no-schedules list: status=%d body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "Chat session") {
		t.Error("manual agents must not render as schedule rows")
	}

	// detail page: breadcrumbs + edit form + run now + definition select
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/schedules/a1", nil))
	bodyD := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("detail status = %d body=%s", rec.Code, bodyD)
	}
	for _, want := range []string{`href="/schedules"`, "Daily briefing", `action="/schedules/a1/update"`, `name="agentDefinitionId"`, "memory", `action="/schedules/a1/trigger"`, "Run now", `action="/schedules/a1/delete"`, "Latest runs", "No runs yet"} {
		if !strings.Contains(bodyD, want) {
			t.Errorf("detail page missing %q", want)
		}
	}

	// unknown schedule id → whole-page error, not a broken render
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/schedules/nope", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Schedule unavailable") {
		t.Fatalf("unknown schedule: status=%d body=%s", rec.Code, rec.Body.String())
	}

	// empty list → empty state, not an error
	f2 := &fakeMemory{}
	s2 := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f2}
	e2 := echo.New()
	e2.GET("/schedules", s2.uiSchedules)
	rec = httptest.NewRecorder()
	e2.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/schedules", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "No schedules yet") {
		t.Fatalf("empty list: status=%d body=%s", rec.Code, rec.Body.String())
	}

	// new-schedule page: create form with the definition selector
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/schedules/new", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("new-schedule status = %d", rec.Code)
	}
	newBody := rec.Body.String()
	for _, want := range []string{`action="/schedules"`, `name="agentDefinitionId"`, "Create schedule", `href="/schedules"`} {
		if !strings.Contains(newBody, want) {
			t.Errorf("new-schedule page missing %q", want)
		}
	}

	// create → redirect + forwarded fields (strategyType/triggerType defaulted)
	form := url.Values{
		"name":              {"Morning digest"},
		"prompt":            {"run it"},
		"cronSchedule":      {"30 7 * * *"},
		"enabled":           {"on"},
		"agentDefinitionId": {"def-1"},
	}
	rec = httptest.NewRecorder()
	createReq := httptest.NewRequest(http.MethodPost, "/schedules", strings.NewReader(form.Encode()))
	createReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	e.ServeHTTP(rec, createReq)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("create redirect status = %d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/schedules?created=1" {
		t.Errorf("create location = %q", loc)
	}
	if len(f.createdScheduled) != 1 {
		t.Fatalf("created = %+v", f.createdScheduled)
	}
	c := f.createdScheduled[0]
	if c.Name != "Morning digest" || c.CronSchedule != "30 7 * * *" {
		t.Errorf("created = %+v", c)
	}
	if c.StrategyType != "definition" || c.TriggerType != "schedule" || !c.Enabled {
		t.Errorf("defaults not applied: %+v", c)
	}
	if c.AgentDefinitionID == nil || *c.AgentDefinitionID != "def-1" {
		t.Errorf("agentDefinitionId = %v", c.AgentDefinitionID)
	}

	// create with missing definition → ?err= (validation before memory call)
	bad := url.Values{"name": {"X"}, "cronSchedule": {"0 8 * * *"}}
	rec = httptest.NewRecorder()
	badReq := httptest.NewRequest(http.MethodPost, "/schedules", strings.NewReader(bad.Encode()))
	badReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	e.ServeHTTP(rec, badReq)
	if loc := rec.Header().Get("Location"); rec.Code != http.StatusSeeOther || !strings.Contains(loc, "/schedules/new?err=") {
		t.Fatalf("create validation: status=%d location=%q", rec.Code, loc)
	}
	if len(f.createdScheduled) != 1 {
		t.Error("no agent should be created for an invalid form")
	}

	// create with a 6-field cron (seconds included) → rejected before the
	// memory call: memory expects exactly 5 fields (minute hour day month
	// weekday), so the form must catch the mismatch with a friendly message.
	badCron := url.Values{"name": {"X"}, "cronSchedule": {"0 30 9 * * *"}, "agentDefinitionId": {"def-1"}}
	rec = httptest.NewRecorder()
	badCronReq := httptest.NewRequest(http.MethodPost, "/schedules", strings.NewReader(badCron.Encode()))
	badCronReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	e.ServeHTTP(rec, badCronReq)
	if loc := rec.Header().Get("Location"); rec.Code != http.StatusSeeOther || !strings.Contains(loc, "/schedules/new?err=") {
		t.Fatalf("create cron validation: status=%d location=%q", rec.Code, loc)
	}
	if len(f.createdScheduled) != 1 {
		t.Error("no agent should be created for an invalid cron")
	}

	// update → redirect + forwarded id
	form = url.Values{"name": {"Renamed"}, "cronSchedule": {"0 9 * * *"}, "enabled": {"on"}, "agentDefinitionId": {"def-1"}}
	rec = httptest.NewRecorder()
	updateReq := httptest.NewRequest(http.MethodPost, "/schedules/a1/update", strings.NewReader(form.Encode()))
	updateReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	e.ServeHTTP(rec, updateReq)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/schedules?updated=1" {
		t.Fatalf("update redirect: status=%d location=%q", rec.Code, rec.Header().Get("Location"))
	}
	if f.updatedSchedID != "a1" || f.updatedScheduled == nil || f.updatedScheduled.Name != "Renamed" {
		t.Errorf("update not forwarded: id=%q agent=%+v", f.updatedSchedID, f.updatedScheduled)
	}

	// delete → redirect + forwarded id
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/schedules/a1/delete", nil))
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/schedules?deleted=1" {
		t.Fatalf("delete redirect: status=%d location=%q", rec.Code, rec.Header().Get("Location"))
	}
	if f.deletedSchedID != "a1" {
		t.Errorf("delete not forwarded: id=%q", f.deletedSchedID)
	}

	// trigger → redirect + forwarded id
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/schedules/a1/trigger", nil))
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/schedules?triggered=1" {
		t.Fatalf("trigger redirect: status=%d location=%q", rec.Code, rec.Header().Get("Location"))
	}
	if f.triggeredSchedID != "a1" {
		t.Errorf("trigger not forwarded: id=%q", f.triggeredSchedID)
	}

	// toggle on → redirect + forwarded id/enabled
	rec = httptest.NewRecorder()
	toggleReq := httptest.NewRequest(http.MethodPost, "/schedules/a1/toggle", strings.NewReader("enabled=on"))
	toggleReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	e.ServeHTTP(rec, toggleReq)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/schedules" {
		t.Fatalf("toggle on redirect: status=%d location=%q", rec.Code, rec.Header().Get("Location"))
	}
	if f.setEnabledSchedID != "a1" || f.setEnabledSchedVal == nil || !*f.setEnabledSchedVal {
		t.Errorf("toggle on not forwarded: id=%q enabled=%v", f.setEnabledSchedID, f.setEnabledSchedVal)
	}

	// toggle off (unchecked → no enabled field) → forwarded enabled=false
	rec = httptest.NewRecorder()
	offReq := httptest.NewRequest(http.MethodPost, "/schedules/a1/toggle", nil)
	e.ServeHTTP(rec, offReq)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/schedules" {
		t.Fatalf("toggle off redirect: status=%d location=%q", rec.Code, rec.Header().Get("Location"))
	}
	if f.setEnabledSchedVal == nil || *f.setEnabledSchedVal {
		t.Errorf("toggle off should forward enabled=false, got %v", f.setEnabledSchedVal)
	}
}
