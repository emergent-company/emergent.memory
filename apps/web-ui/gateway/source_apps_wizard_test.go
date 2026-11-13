package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

func githubWizardTestServer(f *fakeMemory, apiURL string) *echo.Echo {
	s := &Server{
		cfg: Config{
			DefaultAgent: "memory", MemoryPublicURL: "https://api.memory.example.com",
			PublicBaseURL: "https://app.memory.example.com", SessionSecret: "0123456789abcdef0123456789abcdef",
			GitHubAPIBaseURL: apiURL,
		},
		memory: f,
	}
	e := echo.New()
	e.GET(githubWizardPath, s.uiGitHubReviewWizard)
	e.POST(githubWizardPath+"/start", s.uiGitHubReviewWizardStart)
	e.GET(githubWizardPath+"/callback", s.uiGitHubReviewWizardCallback)
	e.POST(githubWizardPath+"/complete", s.uiGitHubReviewWizardComplete)
	return e
}

func TestGitHubWizardStartRendersManifestAndPinsWebhookKey(t *testing.T) {
	f := sourceAppsAdminFake()
	e := githubWizardTestServer(f, "https://api.github.test")

	form := url.Values{"owner": {""}, "name": {"memory-review-e2e"}, "scope": {"project"}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, githubWizardPath+"/start", strings.NewReader(form.Encode()))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationForm)
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{
		`data-testid="gh-wizard-manifest"`,
		`action="https://github.com/settings/apps/new?state=`,
		`name="manifest"`,
		`memory-review-e2e`,
		`/api/webhooks/source-apps/`,
		`pull_request`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("page missing %q", want)
		}
	}
	var ck *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == githubWizardCookie {
			ck = c
		}
	}
	if ck == nil || ck.Value == "" {
		t.Fatal("wizard state cookie not set")
	}
}

func TestGitHubWizardCallbackRegistersAppWithPinnedKey(t *testing.T) {
	f := sourceAppsAdminFake()
	gh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/conversions") {
			t.Errorf("unexpected GitHub call %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":5745481,"slug":"memory-review-e2e","name":"memory-review-e2e","pem":"test-pem","webhook_secret":"whsec","html_url":"https://github.com/settings/apps/memory-review-e2e"}`)
	}))
	defer gh.Close()
	e := githubWizardTestServer(f, gh.URL)

	st := ghWizardState{ProjectID: "p1", Scope: "project", Name: "memory-review-e2e", WebhookKey: "11111111-1111-4111-8111-111111111111", State: "state-1"}
	raw, _ := json.Marshal(st)
	ck := &http.Cookie{Name: githubWizardCookie, Value: signCookieValue("0123456789abcdef0123456789abcdef", raw), Path: githubWizardPath}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, githubWizardPath+"/callback?state=state-1&code=abc", nil)
	req.AddCookie(ck)
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	loc := rec.Header().Get("Location")
	if !strings.Contains(loc, "step=install") || !strings.Contains(loc, "app=sapp-new") {
		t.Errorf("location = %q", loc)
	}
	got := f.dsCreatedSourceAppReq
	if got == nil {
		t.Fatal("source App was not registered")
	}
	if got.AppID != 5745481 || got.Slug != "memory-review-e2e" || got.Scope != "project" {
		t.Errorf("request = %+v", got)
	}
	if got.WebhookKey != st.WebhookKey {
		t.Errorf("webhookKey = %q, want %q", got.WebhookKey, st.WebhookKey)
	}
}

func TestGitHubWizardInstallRecoversAppFromState(t *testing.T) {
	f := sourceAppsAdminFake()
	e := githubWizardTestServer(f, "https://api.github.test")
	st := ghWizardState{ProjectID: "p1", Scope: "project", AppID: "sapp-9", Slug: "memory-review-e2e", WebhookKey: "11111111-1111-4111-8111-111111111111", State: "state-1"}
	raw, _ := json.Marshal(st)
	ck := &http.Cookie{Name: githubWizardCookie, Value: signCookieValue("0123456789abcdef0123456789abcdef", raw), Path: githubWizardPath}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, githubWizardPath+"?step=installed&installation_id=181621212", nil)
	req.AddCookie(ck)
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{`data-testid="gh-wizard-install"`, `value="sapp-9"`, `value="181621212"`, `github.com/apps/memory-review-e2e/installations/new`} {
		if !strings.Contains(body, want) {
			t.Errorf("install page missing %q", want)
		}
	}
}

func TestGitHubWizardCompleteCreatesSourceAgentTrigger(t *testing.T) {
	f := sourceAppsAdminFake()
	e := githubWizardTestServer(f, "https://api.github.test")

	form := url.Values{
		"app": {"sapp-1"}, "slug": {"memory-review-e2e"}, "installation_id": {"181621212"},
		"repos": {"mkucharz/memory-reviewer-e2e"}, "source_name": {"GitHub reviews"},
		"agent_name": {"security-review"}, "queue": {"security-review"},
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, githubWizardPath+"/complete", strings.NewReader(form.Encode()))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationForm)
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Header().Get("Location"), "step=done") {
		t.Errorf("location = %q", rec.Header().Get("Location"))
	}
	if f.dsCreatedReq == nil || f.dsCreatedReq.ProviderType != "github_app" {
		t.Fatalf("source request = %+v", f.dsCreatedReq)
	}
	cfg := f.dsCreatedReq.Config
	if cfg["app_id"] != "sapp-1" || cfg["installation_id"] != "181621212" {
		t.Errorf("source config = %+v", cfg)
	}
	if len(f.created) == 0 {
		t.Fatal("agent definition not created")
	}
	def := f.created[len(f.created)-1]
	if !slices.Contains(def.Tools, "github-pr-diff") || !slices.Contains(def.Tools, "github-review-submit") {
		t.Errorf("agent tools = %v", def.Tools)
	}
	if def.DefaultQueue != "security-review" || def.DispatchMode != "queued" {
		t.Errorf("agent queue/dispatch = %q/%q", def.DefaultQueue, def.DispatchMode)
	}
	if f.dsCreatedTriggerReq == nil || f.dsCreatedTriggerReq.AgentID != "runtime-agent-1" {
		t.Errorf("trigger request = %+v", f.dsCreatedTriggerReq)
	}
}
