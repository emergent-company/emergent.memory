package main

import (
	"strings"
	"testing"
)

// sentryCDNScript identifies the third-party SDK bundle. It is a ~281 kB
// download served from browser.sentry-cdn.com, so where and how it is loaded is
// a page-load performance property, not just a config detail.
const sentryCDNScript = "browser.sentry-cdn.com"

// sentryTestOpts is a DSN-configured head: the branch that emits the SDK.
func sentryTestOpts() sentryHeadOpts {
	return sentryHeadOpts{
		DSN:                     "https://key@sentry.example.com/1",
		Environment:             "production",
		TracesSampleRate:        0.2,
		ReplaySessionSampleRate: 0.1,
		ReplayOnErrorSampleRate: 1.0,
	}
}

// sentryScriptTag returns the single <script …> open tag that loads the Sentry
// CDN bundle, failing when there is not exactly one.
func sentryScriptTag(t *testing.T, html string) string {
	t.Helper()
	if n := strings.Count(html, sentryCDNScript); n != 1 {
		t.Fatalf("want exactly 1 Sentry CDN script reference, got %d", n)
	}
	idx := strings.Index(html, sentryCDNScript)
	start := strings.LastIndex(html[:idx], "<script")
	if start < 0 {
		t.Fatalf("no <script tag before the Sentry CDN URL")
	}
	end := strings.Index(html[idx:], ">")
	if end < 0 {
		t.Fatalf("unterminated Sentry <script> tag")
	}
	return html[start : idx+end+1]
}

// assertSentryHeadNonBlocking is the shared contract for every document head
// that wires Sentry: the SDK must be deferred, and its init must not call
// Sentry.init before the deferred script has executed.
func assertSentryHeadNonBlocking(t *testing.T, html string) {
	t.Helper()
	tag := sentryScriptTag(t, html)
	if !strings.Contains(tag, "defer") {
		t.Errorf("Sentry SDK script is parse-blocking (no defer/async): %s", tag)
	}
	if strings.Contains(tag, "async") {
		// async would race the inline bootstrap's assumptions; defer is the
		// ordering-preserving choice here.
		t.Errorf("Sentry SDK script must be deferred, not async: %s", tag)
	}
	initIdx := strings.Index(html, "Sentry.init")
	if initIdx < 0 {
		t.Fatalf("no Sentry.init in the rendered head")
	}
	if !strings.Contains(html[:initIdx], "DOMContentLoaded") {
		t.Error("Sentry.init must run after the deferred SDK (inside a DOMContentLoaded bootstrap)")
	}
	if !strings.Contains(html[:initIdx], "window.Sentry") {
		t.Error("Sentry.init must be guarded by window.Sentry so a blocked/failed CDN is a no-op, not an uncaught error")
	}
}

// TestAppShellSentrySDKIsDeferredAndAfterStylesheet pins issue #209: the SDK was
// loaded synchronously in <head> above the stylesheet link, so first paint waited
// on a third-party CDN round-trip (measured 0.44–1.16 s) before the browser even
// discovered app.css.
func TestAppShellSentrySDKIsDeferredAndAfterStylesheet(t *testing.T) {
	html := renderPageShellWithSentry(t, sentryTestOpts())

	assertSentryHeadNonBlocking(t, html)

	scriptIdx := strings.Index(html, sentryCDNScript)
	cssIdx := strings.Index(html, "/assets/css/app.css")
	if cssIdx < 0 {
		t.Fatal("shell has no app.css reference")
	}
	if scriptIdx < cssIdx {
		t.Error("the Sentry SDK must be emitted after the stylesheet link, so its presence can never delay CSS discovery")
	}
}

// TestEveryDocumentHeadDefersSentry pins the same contract on all three documents
// that wire Sentry — the app shell, the sign-in page, and the public share page.
// They share sentryHead(), but each could reintroduce a blocking tag inline.
func TestEveryDocumentHeadDefersSentry(t *testing.T) {
	opts := sentryTestOpts()

	docs := map[string]string{
		"app shell": renderPageShellWithSentry(t, opts),
		"login": renderHTML(t, loginPage(
			opts.DSN, opts.Environment,
			opts.TracesSampleRate, opts.ReplaySessionSampleRate, opts.ReplayOnErrorSampleRate,
			"/",
		)),
		"share": renderHTML(t, ShareAgentPage(ShareAgentPageProps{
			SentryDSN:                     opts.DSN,
			SentryEnvironment:             opts.Environment,
			SentryTracesSampleRate:        opts.TracesSampleRate,
			SentryReplaySessionSampleRate: opts.ReplaySessionSampleRate,
			SentryReplayOnErrorSampleRate: opts.ReplayOnErrorSampleRate,
		})),
	}
	for name, html := range docs {
		t.Run(name, func(t *testing.T) { assertSentryHeadNonBlocking(t, html) })
	}
}

// TestSentryHeadWithoutDSNOmitsSDK keeps the disabled path unchanged: the meta
// tags stay (client code reads them) but no third-party request is made.
func TestSentryHeadWithoutDSNOmitsSDK(t *testing.T) {
	html := renderHTML(t, sentryHead(sentryHeadOpts{Environment: "development"}))

	if strings.Contains(html, sentryCDNScript) {
		t.Error("no DSN configured: the SDK must not be requested at all")
	}
	for _, meta := range []string{
		`sentry-dsn`, `sentry-environment`, `sentry-traces-sample-rate`,
		`sentry-replay-session-sample-rate`, `sentry-replay-on-error-sample-rate`,
	} {
		if !strings.Contains(html, `name="`+meta+`"`) {
			t.Errorf("meta tag %q missing from the disabled block", meta)
		}
	}
}
