package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

// shareFormContext builds an echo context over a urlencoded body so the
// create-form mapping helper can be exercised without a route.
func shareFormContext(t *testing.T, form url.Values) echo.Context {
	t.Helper()
	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if err := req.ParseForm(); err != nil {
		t.Fatalf("ParseForm: %v", err)
	}
	return e.NewContext(req, httptest.NewRecorder())
}

// TestShareLinkConfigInputFromFormWelcomeMessage asserts a filled welcome
// message reaches the outgoing create request. Regression: the field was
// captured into ShareLinkCreateValues but never copied into the config input.
func TestShareLinkConfigInputFromFormWelcomeMessage(t *testing.T) {
	in := shareLinkConfigInputFromForm(shareFormContext(t, url.Values{"welcomeMessage": {"Hello there!"}}))
	if in.WelcomeMessage == nil || *in.WelcomeMessage != "Hello there!" {
		t.Fatalf("welcomeMessage not mapped to request: %+v", in.WelcomeMessage)
	}
}

// TestShareLinkConfigInputFromFormWelcomeMessageEmpty asserts an empty welcome
// message is omitted (nil), matching the other optional string fields so the
// server default applies.
func TestShareLinkConfigInputFromFormWelcomeMessageEmpty(t *testing.T) {
	in := shareLinkConfigInputFromForm(shareFormContext(t, url.Values{}))
	if in.WelcomeMessage != nil {
		t.Fatalf("empty welcomeMessage should be omitted, got %q", *in.WelcomeMessage)
	}
}
