package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"runtime/debug"
	"strconv"
	"strings"

	"github.com/getsentry/sentry-go"
	"github.com/labstack/echo/v4"
)

// initSentry configures the global Sentry client. When dsn is empty, error
// tracking is disabled and every helper in this file is a no-op.
func initSentry(dsn, environment string, tracesSampleRate float64) error {
	if dsn == "" {
		return nil
	}
	return sentry.Init(sentry.ClientOptions{
		Dsn:              dsn,
		Environment:      environment,
		AttachStacktrace: true,
		EnableTracing:    true,
		TracesSampleRate: tracesSampleRate,
	})
}

// captureError reports err to Sentry when a client is configured.
func captureError(err error) {
	if err == nil {
		return
	}
	// Context cancellation is the expected signal when a request is aborted
	// (client navigated away, server shutting down, poller torn down). It is
	// not a defect and must not be reported to Sentry as one.
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return
	}
	hub := sentry.CurrentHub()
	if hub.Client() != nil {
		hub.CaptureException(err)
	}
}

var (
	// uuidSegment matches a canonical UUID path segment.
	uuidSegment = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
	// hexSegment matches a long (>=16 hex chars) opaque identifier.
	hexSegment = regexp.MustCompile(`^[0-9a-fA-F]{16,}$`)
	// numericSegment matches an all-digit identifier.
	numericSegment = regexp.MustCompile(`^[0-9]+$`)
)

// normalizePathTemplate replaces per-entity path segments (UUIDs, long hex ids
// and pure-numeric ids) with a stable {id} placeholder, so
// /api/chat/1d47dbf5-c4fd-4d28-8e3a-f9fa122d56bb/history becomes
// /api/chat/{id}/history. Used as part of the Sentry fingerprint so identical
// failures across many entities collapse into one issue.
func normalizePathTemplate(path string) string {
	if path == "" {
		return path
	}
	segments := strings.Split(path, "/")
	for i, seg := range segments {
		if uuidSegment.MatchString(seg) || hexSegment.MatchString(seg) || numericSegment.MatchString(seg) {
			segments[i] = "{id}"
		}
	}
	return strings.Join(segments, "/")
}

// memoryErrorDisposition decides how a memory REST API failure should be
// reported to Sentry:
//
//   - status 0 (transport error) is captured at error level;
//   - 401/403 (bad/missing credentials) at warning level;
//   - any 5xx at error level;
//   - every other 4xx is a client input/config error, not a gateway defect,
//     and is not captured (the caller still logs it).
func memoryErrorDisposition(status int) (capture bool, level sentry.Level) {
	switch {
	case status == 0:
		return true, sentry.LevelError
	case status == http.StatusUnauthorized, status == http.StatusForbidden:
		return true, sentry.LevelWarning
	case status >= 500:
		return true, sentry.LevelError
	default:
		return false, sentry.LevelInfo
	}
}

// captureMemoryError reports a memory REST API failure to Sentry, tagging the
// event with the request context (method, path, HTTP status).
//
// Reporting policy (keeps Sentry signal high and issue count low):
//   - 4xx other than 401/403 are client data/config errors, not gateway
//     defects: callers log them, but they are NOT captured.
//   - 401/403 are captured at warning level (usually a bad key or config).
//   - 5xx and transport errors (status 0) are captured at error level.
//
// Captured events carry a stable fingerprint keyed by the normalized path
// template, so the same failure across many entities/instances groups into a
// single issue instead of one issue per concrete path.
func captureMemoryError(method, path string, status int, err error) {
	if err == nil {
		return
	}
	hub := sentry.CurrentHub()
	if hub.Client() == nil {
		return
	}
	capture, level := memoryErrorDisposition(status)
	if !capture {
		return
	}
	fingerprintStatus := strconv.Itoa(status)
	if status == 0 {
		fingerprintStatus = "transport"
	}
	template := normalizePathTemplate(path)
	hub.WithScope(func(scope *sentry.Scope) {
		scope.SetLevel(level)
		scope.SetFingerprint([]string{"memory", fingerprintStatus, template})
		scope.SetTag("memory_method", method)
		scope.SetTag("memory_path", path)
		scope.SetTag("memory_path_template", template)
		scope.SetTag("memory_status", strconv.Itoa(status))
		if attemptErr, ok := errors.AsType[*memoryAttemptError](err); ok {
			scope.SetTag("memory_retried", strconv.FormatBool(attemptErr.attempts > 1))
		}
		hub.CaptureException(err)
	})
}

// sentryRecoverMiddleware replaces echo's default Recover middleware: it
// reports the panic to Sentry (when configured) and preserves the existing
// 500 response behavior.
func sentryRecoverMiddleware() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) (err error) {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("[PANIC RECOVER] %v", r)
					debug.PrintStack()
					captureError(fmt.Errorf("panic: %v", r))
					err = echo.NewHTTPError(http.StatusInternalServerError, "internal server error")
				}
			}()
			return next(c)
		}
	}
}

// sentryErrorMiddleware reports handler errors to Sentry: every non-HTTP
// error and every HTTP error with status >= 500. The error is returned
// unchanged so existing client-facing behavior is preserved.
func sentryErrorMiddleware() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			err := next(c)
			if err != nil {
				if httpErr, ok := err.(*echo.HTTPError); ok {
					if httpErr.Code >= 500 {
						captureError(err)
					}
				} else {
					captureError(err)
				}
			}
			return err
		}
	}
}

// sentryTracingMiddleware starts a transaction per request and continues any
// inbound Sentry trace (sentry-trace/baggage headers), so backend spans nest
// under the browser's page-load/navigation transaction in the trace waterfall.
// It also attaches a request-scoped hub so in-request captures share the trace.
func sentryTracingMiddleware() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			ctx := c.Request().Context()
			hub := sentry.GetHubFromContext(ctx)
			if hub == nil {
				hub = sentry.CurrentHub().Clone()
				ctx = sentry.SetHubOnContext(ctx, hub)
			}
			hub.Scope().SetRequest(c.Request())

			name := c.Request().Method + " " + c.Path()
			if c.Path() == "" {
				name = c.Request().Method + " " + c.Request().URL.Path
			}
			span := sentry.StartTransaction(
				ctx,
				name,
				sentry.WithOpName("http.server"),
				sentry.ContinueFromRequest(c.Request()),
			)
			defer func() {
				span.Status = sentry.HTTPtoSpanStatus(c.Response().Status)
				span.SetData("http.response.status_code", c.Response().Status)
				span.Finish()
			}()
			c.SetRequest(c.Request().WithContext(span.Context()))
			return next(c)
		}
	}
}
