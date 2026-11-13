package main

import (
	"strings"
	"testing"

	"github.com/a-h/templ"
)

// renderShellWithFeedback renders the app shell with the given overlay config,
// so the script tag's attributes can be asserted on. The API base defaults to
// the bundle URL, which is what a deployed service does.
func renderShellWithFeedback(t *testing.T, overlayURL string, order feedbackOrderConfig) string {
	t.Helper()
	return renderShellWithFeedbackAPI(t, overlayURL, overlayURL, order)
}

// renderShellWithFeedbackAPI renders the shell with the bundle and the API base
// separated, the shape a local bundle against the deployed API uses.
func renderShellWithFeedbackAPI(t *testing.T, overlayURL, overlayAPIURL string, order feedbackOrderConfig) string {
	t.Helper()
	return renderHTML(t, appShell(
		"Objects", nil, false, nil, "", nil, "", nil, nil, nil, nil, false,
		0,
		nil,
		nil, nil, templ.NopComponent, nil,
		"", "", 0, 0, 0, overlayURL, overlayAPIURL, order,
	))
}

// TestAppShellFeedbackOrderAttributes pins the wiring that turns the overlay's
// session destination on: the report the user makes on a page under review is
// delivered into the session editing that page, so the shell must carry the box
// URL and the session id (and the optional token, when one is configured).
func TestAppShellFeedbackOrderAttributes(t *testing.T) {
	const session = "a1b2c3d4-e5f6-4a7b-8c9d-0e1f2a3b4c5d"

	configured := renderShellWithFeedback(t, "https://feedback.test", feedbackOrderConfig{
		Box:     "http://box.test:8101",
		Session: session,
	})
	for _, want := range []string{
		`data-order-box="http://box.test:8101"`,
		`data-order-session="` + session + `"`,
	} {
		if !strings.Contains(configured, want) {
			t.Errorf("shell is missing %q", want)
		}
	}
	// The cookie is the default credential: a token must not be emitted unless
	// one is configured.
	if strings.Contains(configured, "data-order-token") {
		t.Error("a token attribute appeared without a configured token")
	}

	withToken := renderShellWithFeedback(t, "https://feedback.test", feedbackOrderConfig{
		Box:     "http://box.test",
		Session: session,
		Token:   "shared-secret",
	})
	if !strings.Contains(withToken, `data-order-token="shared-secret"`) {
		t.Error("a configured token must reach the script tag")
	}

	// Half a destination is no destination: the overlay would warn and do
	// nothing, so the shell must not advertise it.
	half := renderShellWithFeedback(t, "https://feedback.test", feedbackOrderConfig{Box: "http://box.test"})
	if strings.Contains(half, "data-order-") {
		t.Error("a box without a session must not emit order attributes")
	}

	absent := renderShellWithFeedback(t, "https://feedback.test", feedbackOrderConfig{})
	if strings.Contains(absent, "data-order-") {
		t.Error("no order attributes may appear when the destination is off")
	}

	noOverlay := renderShellWithFeedback(t, "", feedbackOrderConfig{
		Box:     "http://box.test",
		Session: session,
	})
	if strings.Contains(noOverlay, "emergent-feedback.js") {
		t.Error("no overlay script may render without an overlay URL")
	}
}

// TestFeedbackOrderConfigEnabled covers the gate the template uses.
func TestFeedbackOrderConfigEnabled(t *testing.T) {
	cases := []struct {
		name string
		cfg  feedbackOrderConfig
		want bool
	}{
		{"both", feedbackOrderConfig{Box: "http://box.test", Session: "id"}, true},
		{"box only", feedbackOrderConfig{Box: "http://box.test"}, false},
		{"session only", feedbackOrderConfig{Session: "id"}, false},
		{"neither", feedbackOrderConfig{}, false},
	}
	for _, c := range cases {
		if got := c.cfg.enabled(); got != c.want {
			t.Errorf("%s: enabled() = %v, want %v", c.name, got, c.want)
		}
	}
}

// TestLoadConfigFeedbackOrder asserts the environment mapping: the box URL is
// normalised (no trailing slash) and the session id is lowercased, because the
// daemon matches the id exactly and a pasted uppercase UUID would be rejected.
func TestLoadConfigFeedbackOrder(t *testing.T) {
	t.Setenv("FEEDBACK_ORDER_BOX", "http://box.test:8101/")
	t.Setenv("FEEDBACK_ORDER_SESSION", "A1B2C3D4-E5F6-4A7B-8C9D-0E1F2A3B4C5D")
	t.Setenv("FEEDBACK_ORDER_TOKEN", "shared-secret")

	cfg := LoadConfig()
	if cfg.FeedbackOrderBox != "http://box.test:8101" {
		t.Errorf("FeedbackOrderBox = %q, want the trailing slash trimmed", cfg.FeedbackOrderBox)
	}
	if cfg.FeedbackOrderSession != "a1b2c3d4-e5f6-4a7b-8c9d-0e1f2a3b4c5d" {
		t.Errorf("FeedbackOrderSession = %q, want lowercase", cfg.FeedbackOrderSession)
	}
	if cfg.FeedbackOrderToken != "shared-secret" {
		t.Errorf("FeedbackOrderToken = %q", cfg.FeedbackOrderToken)
	}

	order := cfg.feedbackOrder()
	if !order.enabled() {
		t.Error("configured env must enable the destination")
	}
	if order.Box != cfg.FeedbackOrderBox || order.Session != cfg.FeedbackOrderSession || order.Token != cfg.FeedbackOrderToken {
		t.Errorf("feedbackOrder() = %+v, want the loaded values", order)
	}
}

// TestLoadConfigFeedbackOrderDefaultsEmpty keeps the destination off unless it
// is configured: an empty box must not render a destination that cannot deliver.
func TestLoadConfigFeedbackOrderDefaultsEmpty(t *testing.T) {
	t.Setenv("FEEDBACK_ORDER_BOX", "")
	t.Setenv("FEEDBACK_ORDER_SESSION", "")
	t.Setenv("FEEDBACK_ORDER_TOKEN", "")

	if order := LoadConfig().feedbackOrder(); order.enabled() {
		t.Errorf("destination enabled without configuration: %+v", order)
	}
}

// TestAppShellFeedbackOverlayAPIURL pins the bundle/API split: a locally served
// bundle still talks to the deployed service for comments and issue export, and
// the API base falls back to the bundle URL when nothing else is configured.
func TestAppShellFeedbackOverlayAPIURL(t *testing.T) {
	split := renderShellWithFeedbackAPI(
		t,
		"http://localhost:8104",
		"https://feedback.test",
		feedbackOrderConfig{},
	)
	if !strings.Contains(split, `src="http://localhost:8104/emergent-feedback.js"`) {
		t.Error("the bundle must load from the bundle URL")
	}
	if !strings.Contains(split, `data-api="https://feedback.test"`) {
		t.Error("the overlay API base must be the configured API URL")
	}
}

// TestFeedbackOverlayAPIFallsBackToBundle covers the Config helper the shell
// reads: explicit override wins, otherwise the bundle origin serves both.
func TestFeedbackOverlayAPIFallsBackToBundle(t *testing.T) {
	t.Setenv("FEEDBACK_OVERLAY_URL", "http://bundle.test/")
	t.Setenv("FEEDBACK_OVERLAY_API_URL", "")
	if got := LoadConfig().feedbackOverlayAPI(); got != "http://bundle.test/" {
		t.Errorf("feedbackOverlayAPI() = %q, want the bundle URL", got)
	}

	t.Setenv("FEEDBACK_OVERLAY_API_URL", "https://feedback.test/")
	cfg := LoadConfig()
	if got := cfg.feedbackOverlayAPI(); got != "https://feedback.test" {
		t.Errorf("feedbackOverlayAPI() = %q, want the override with no trailing slash", got)
	}
}
