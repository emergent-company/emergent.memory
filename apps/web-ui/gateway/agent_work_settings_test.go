package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

// TestApplyAgentWorkSection pins the Work subpage applier: every field group
// maps onto def.WorkConfig (status phases, review gate, budgets, retry policy,
// deliverable contract, trigger statuses); empty numerics read 0 (the server
// "use default" sentinel) and a typed 0 does too; comma-separated lists split,
// trim, and collapse duplicates; and an out-of-range value is rejected without
// mutating the definition.
func TestApplyAgentWorkSection(t *testing.T) {
	t.Run("full mapping", func(t *testing.T) {
		form := url.Values{
			"workStatus.ready":        {"todo"},
			"workStatus.inProgress":   {"doing"},
			"workStatus.review":       {"in_review"},
			"workStatus.revision":     {"rework"},
			"workStatus.blocked":      {"waiting"},
			"workStatus.done":         {"shipped"},
			"workRequiresReview":      {"false", "true"},
			"failureLimit":            {"5"},
			"revisionLimit":           {"7"},
			"maxAttempts":             {"4"},
			"initialIntervalMs":       {"1500"},
			"backoffCoefficient":      {"2.5"},
			"maxIntervalMs":           {"60000"},
			"workRequireArtifacts":    {"false", "true"},
			"requiredDeliverableType": {"kb.documents, kb.chunks"},
			"triggerStatus":           {"todo, rework"},
		}
		def := &AgentDefinition{ID: "a1", Name: "diane"}
		if err := applyAgentWorkSection(def, generalSectionContext(t, form)); err != nil {
			t.Fatalf("applyAgentWorkSection: %v", err)
		}
		wc := def.WorkConfig
		if wc == nil {
			t.Fatal("WorkConfig must be set")
		}
		if wc.Status.Ready != "todo" || wc.Status.InProgress != "doing" || wc.Status.Review != "in_review" ||
			wc.Status.Revision != "rework" || wc.Status.Blocked != "waiting" || wc.Status.Done != "shipped" {
			t.Errorf("status mapping wrong: %+v", wc.Status)
		}
		if !wc.RequiresReview {
			t.Error("requiresReview must be true when the checkbox is submitted checked")
		}
		if wc.FailureLimit != 5 || wc.RevisionLimit != 7 {
			t.Errorf("budgets wrong: failure=%d revision=%d", wc.FailureLimit, wc.RevisionLimit)
		}
		rp := wc.RetryPolicy
		if rp.MaxAttempts != 4 || rp.InitialIntervalMS != 1500 || rp.BackoffCoefficient != 2.5 || rp.MaxIntervalMS != 60000 {
			t.Errorf("retry policy wrong: %+v", rp)
		}
		if !wc.WorkContract.RequireArtifacts {
			t.Error("requireArtifacts must be true")
		}
		if strings.Join(wc.WorkContract.RequiredDeliverableTypes, ",") != "kb.documents,kb.chunks" {
			t.Errorf("required deliverable types wrong: %v", wc.WorkContract.RequiredDeliverableTypes)
		}
		if strings.Join(wc.TriggerStatuses, ",") != "todo,rework" {
			t.Errorf("trigger statuses wrong: %v", wc.TriggerStatuses)
		}
	})

	t.Run("empty numerics read zero", func(t *testing.T) {
		form := url.Values{
			"workStatus.ready":        {""},
			"failureLimit":            {""},
			"revisionLimit":           {""},
			"maxAttempts":             {""},
			"initialIntervalMs":       {""},
			"backoffCoefficient":      {""},
			"maxIntervalMs":           {""},
			"workRequiresReview":      {"false"},
			"workRequireArtifacts":    {"false"},
			"requiredDeliverableType": {""},
			"triggerStatus":           {""},
		}
		def := &AgentDefinition{ID: "a1", Name: "diane"}
		if err := applyAgentWorkSection(def, generalSectionContext(t, form)); err != nil {
			t.Fatalf("applyAgentWorkSection: %v", err)
		}
		wc := def.WorkConfig
		if wc.FailureLimit != 0 || wc.RevisionLimit != 0 || wc.RetryPolicy.MaxAttempts != 0 ||
			wc.RetryPolicy.InitialIntervalMS != 0 || wc.RetryPolicy.BackoffCoefficient != 0 || wc.RetryPolicy.MaxIntervalMS != 0 {
			t.Errorf("empty numerics must read 0, got %+v", wc)
		}
		if wc.Status.Ready != "" {
			t.Errorf("empty status must read empty (default), got %q", wc.Status.Ready)
		}
		if wc.WorkContract.RequiredDeliverableTypes != nil || wc.TriggerStatuses != nil {
			t.Errorf("empty lists must read nil, got %v / %v", wc.WorkContract.RequiredDeliverableTypes, wc.TriggerStatuses)
		}
	})

	t.Run("zero limits read as platform default", func(t *testing.T) {
		form := url.Values{
			"failureLimit":  {"0"},
			"revisionLimit": {"0"},
		}
		def := &AgentDefinition{ID: "a1", Name: "diane"}
		if err := applyAgentWorkSection(def, generalSectionContext(t, form)); err != nil {
			t.Fatalf("typed 0 must be accepted as the default sentinel: %v", err)
		}
		if def.WorkConfig.FailureLimit != 0 || def.WorkConfig.RevisionLimit != 0 {
			t.Errorf("typed 0 must persist as 0 (default sentinel), got failure=%d revision=%d",
				def.WorkConfig.FailureLimit, def.WorkConfig.RevisionLimit)
		}
	})

	t.Run("duplicate list entries collapse", func(t *testing.T) {
		form := url.Values{
			"requiredDeliverableType": {"kb.documents, kb.documents, kb.chunks"},
			"triggerStatus":           {"todo, todo, rework"},
		}
		def := &AgentDefinition{ID: "a1", Name: "diane"}
		if err := applyAgentWorkSection(def, generalSectionContext(t, form)); err != nil {
			t.Fatalf("applyAgentWorkSection: %v", err)
		}
		if got := strings.Join(def.WorkConfig.WorkContract.RequiredDeliverableTypes, ","); got != "kb.documents,kb.chunks" {
			t.Errorf("required deliverable types = %q, want duplicates collapsed", got)
		}
		if got := strings.Join(def.WorkConfig.TriggerStatuses, ","); got != "todo,rework" {
			t.Errorf("trigger statuses = %q, want duplicates collapsed", got)
		}
	})

	t.Run("out-of-range rejected", func(t *testing.T) {
		for name, form := range map[string]url.Values{
			"failureLimit":       {"failureLimit": {"-1"}},
			"revisionLimit":      {"revisionLimit": {"-1"}},
			"maxAttempts":        {"maxAttempts": {"0"}},
			"initialIntervalMs":  {"initialIntervalMs": {"-1"}},
			"maxIntervalMs":      {"maxIntervalMs": {"-1"}},
			"backoffCoefficient": {"backoffCoefficient": {"0.5"}},
		} {
			def := &AgentDefinition{ID: "a1", Name: "diane"}
			if err := applyAgentWorkSection(def, generalSectionContext(t, form)); err == nil {
				t.Errorf("%s out of range must be rejected", name)
			}
		}
	})

	t.Run("non-numeric rejected", func(t *testing.T) {
		def := &AgentDefinition{ID: "a1", Name: "diane"}
		err := applyAgentWorkSection(def, generalSectionContext(t, url.Values{"failureLimit": {"abc"}}))
		if err == nil {
			t.Error("non-numeric failureLimit must be rejected")
		}
	})
}

// TestSanitizeAgentWritePreservesWorkConfig asserts the write path keeps the
// object-driven work config (a first-class persisted field) while still
// dropping the server-computed toolGroups.
func TestSanitizeAgentWritePreservesWorkConfig(t *testing.T) {
	wc := &AgentWorkConfig{
		Status:       AgentWorkStatusConfig{Ready: "todo", InProgress: "doing"},
		FailureLimit: 5,
		RetryPolicy:  AgentRetryPolicy{MaxAttempts: 4},
	}
	def := &AgentDefinition{
		ID:         "a1",
		WorkConfig: wc,
		ToolGroups: []ToolGroup{{ID: "g", Tools: []string{"t"}}},
	}
	sanitizeAgentWrite(def)
	if def.ToolGroups != nil {
		t.Errorf("sanitizeAgentWrite must clear toolGroups, got %+v", def.ToolGroups)
	}
	if def.WorkConfig == nil || def.WorkConfig != wc || def.WorkConfig.FailureLimit != 5 {
		t.Errorf("sanitizeAgentWrite must preserve workConfig, got %+v", def.WorkConfig)
	}
}

// TestAgentWorkSettingsForm renders the Work subpage: every field group exposes
// its field name(s), numeric fields carry min/step bounds, empty values show the
// platform-default placeholders, and a blueprint-derived agent gates the save
// behind the "Save anyway" confirmation dialog.
func TestAgentWorkSettingsForm(t *testing.T) {
	agent := &AgentDefinition{
		ID:   "a1",
		Name: "diane",
		WorkConfig: &AgentWorkConfig{
			Status:          AgentWorkStatusConfig{Ready: "todo", InProgress: "doing"},
			RequiresReview:  true,
			FailureLimit:    5,
			RetryPolicy:     AgentRetryPolicy{MaxAttempts: 4, BackoffCoefficient: 2.5},
			WorkContract:    AgentWorkContract{RequireArtifacts: true, RequiredDeliverableTypes: []string{"kb.documents"}},
			TriggerStatuses: []string{"todo", "rework"},
		},
	}
	html := renderHTML(t, agentWorkSettingsForm(agentSettingsData{Agent: agent}))

	// Every field group's field name renders.
	for _, want := range []string{
		`name="workStatus.ready"`,
		`name="workStatus.inProgress"`,
		`name="workStatus.review"`,
		`name="workStatus.revision"`,
		`name="workStatus.blocked"`,
		`name="workStatus.done"`,
		`name="workRequiresReview"`,
		`name="failureLimit"`,
		`name="revisionLimit"`,
		`name="maxAttempts"`,
		`name="initialIntervalMs"`,
		`name="backoffCoefficient"`,
		`name="maxIntervalMs"`,
		`name="workRequireArtifacts"`,
		`name="requiredDeliverableType"`,
		`name="triggerStatus"`,
		`/agents/a1/settings/work`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("Work form missing %q", want)
		}
	}

	// Numeric min/step bounds.
	for _, want := range []string{
		`name="failureLimit" type="number" min="0" step="1"`,
		`name="revisionLimit" type="number" min="0" step="1"`,
		`name="maxAttempts" type="number" min="1" step="1"`,
		`name="initialIntervalMs" type="number" min="0" step="1"`,
		`name="backoffCoefficient" type="number" min="1" step="any"`,
		`name="maxIntervalMs" type="number" min="0" step="1"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("Work form numeric bound missing %q", want)
		}
	}

	// The Retry policy section must disclose that only max attempts is applied
	// today (the interval/coefficient fields are persisted but not consumed).
	if !strings.Contains(html, `data-testid="agent-work-retry-policy-gap"`) {
		t.Error("Retry policy section must disclose that only max attempts is applied")
	}

	// Stored values prefill; an empty status shows the default placeholder.
	if !strings.Contains(html, `name="workStatus.ready" type="text" value="todo"`) {
		t.Error("stored ready status must prefill")
	}
	if !strings.Contains(html, `name="workStatus.review" type="text" value="" placeholder="review"`) {
		t.Error("empty review status must show the default placeholder")
	}
	if !strings.Contains(html, `value="kb.documents"`) || !strings.Contains(html, `value="todo, rework"`) {
		t.Error("stored deliverable types and trigger statuses must prefill")
	}

	// A non-blueprint agent must NOT render the confirmation dialog or gate.
	if strings.Contains(html, "agent-work-blueprint-warning") {
		t.Error("non-blueprint agent must not render the blueprint warning")
	}
	if strings.Contains(html, "Save anyway") {
		t.Error("non-blueprint agent must not render the Save anyway dialog")
	}
}

// TestAgentWorkSettingsFormBlueprintWarning renders the Work subpage for a
// blueprint-derived agent: the form carries data-requires-confirm and the
// "Save anyway" dialog renders.
func TestAgentWorkSettingsFormBlueprintWarning(t *testing.T) {
	bp := "bp-1"
	agent := &AgentDefinition{ID: "a1", Name: "diane", SourceBlueprintID: &bp}
	html := renderHTML(t, agentWorkSettingsForm(agentSettingsData{Agent: agent}))

	if !strings.Contains(html, `data-requires-confirm="agent-work-blueprint-warning"`) {
		t.Error("blueprint-derived Work form must gate the save behind the warning dialog")
	}
	for _, want := range []string{
		`id="agent-work-blueprint-warning"`,
		"Save anyway",
		"Keep editing",
		"blueprint-derived agent",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("blueprint warning dialog missing %q", want)
		}
	}
}

// TestUIAgentUpdateWorkRoute exercises the PRG POST handler end-to-end: the Work
// form maps onto the definition, delegation survives, success redirects to
// ?updated=1, and the persisted WorkConfig is not dropped.
func TestUIAgentUpdateWorkRoute(t *testing.T) {
	f := &fakeMemory{
		defs:   map[string]*AgentDefinition{"a1": {ID: "a1", Name: "diane"}},
		agents: []AgentDefinitionSummary{{ID: "a1", Name: "diane"}},
	}
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
	e := echo.New()
	e.POST("/agents/:id/settings/work", s.uiAgentUpdateWork)

	body := "workStatus.ready=todo&workStatus.inProgress=doing&workStatus.review=review&workStatus.revision=revision&workStatus.blocked=blocked&workStatus.done=done&workRequiresReview=false&failureLimit=5&revisionLimit=7&maxAttempts=4&initialIntervalMs=1500&backoffCoefficient=2.5&maxIntervalMs=60000&workRequireArtifacts=false&requiredDeliverableType=&triggerStatus=todo"
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/agents/a1/settings/work", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status %d, want 303", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/agents/a1/settings/work?updated=1" {
		t.Errorf("redirect = %q, want work?updated=1", loc)
	}
	u := f.updatedAgent
	if u == nil {
		t.Fatal("UpdateAgentDefinition not called")
	}
	if u.WorkConfig == nil {
		t.Fatal("WorkConfig must survive the update")
	}
	if u.WorkConfig.Status.Ready != "todo" || u.WorkConfig.FailureLimit != 5 || u.WorkConfig.RetryPolicy.MaxAttempts != 4 {
		t.Errorf("work mapping wrong: %+v", u.WorkConfig)
	}
}
