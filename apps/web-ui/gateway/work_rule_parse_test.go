package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

func TestParseRuleRowCarriesInvocation(t *testing.T) {
	form := url.Values{
		"ruleCreate":                  {"true"},
		"ruleUpdate":                  {"true"},
		"ruleDelete":                  {"true"},
		"ruleTriggers":                {"ready | do it"},
		"rule-GitHubIssue-invocation": {`{"max_steps":80,"credential":{"kind":"github_app","permissions":{"contents":"write"}}}`},
	}
	req := httptest.NewRequest(http.MethodPost, "/work/settings/rules", strings.NewReader(form.Encode()))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationForm)
	rule, err := parseRuleRow(echo.New().NewContext(req, httptest.NewRecorder()), "GitHubIssue")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(rule.Triggers) != 1 || rule.Triggers[0].Invocation == nil {
		t.Fatalf("triggers = %#v", rule.Triggers)
	}
	if rule.Triggers[0].Invocation["max_steps"] != float64(80) {
		t.Fatalf("invocation = %#v", rule.Triggers[0].Invocation)
	}
}

func TestParseRuleRowRejectsBadInvocationJSON(t *testing.T) {
	form := url.Values{"ruleInvocation": {"[not an object"}}
	req := httptest.NewRequest(http.MethodPost, "/work/settings/rules", strings.NewReader(form.Encode()))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationForm)
	if _, err := parseRuleRow(echo.New().NewContext(req, httptest.NewRecorder()), "GitHubIssue"); err == nil {
		t.Fatal("invalid invocation JSON accepted")
	}
}

func TestRuleInvocationFieldNameUsesSelectedTypeOnCreate(t *testing.T) {
	if got := ruleInvocationFieldName(AgentObjectRule{}, "GitHubIssue"); got != "rule-GitHubIssue-invocation" {
		t.Fatalf("field = %q", got)
	}
	if got := ruleInvocationFieldName(AgentObjectRule{ObjectType: "GitHubIssue"}, ""); got != "rule-GitHubIssue-invocation" {
		t.Fatalf("field = %q", got)
	}
	if got := ruleInvocationFieldName(AgentObjectRule{}, ""); got != "ruleInvocation" {
		t.Fatalf("fallback = %q", got)
	}
}
