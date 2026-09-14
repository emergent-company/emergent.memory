package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"runtime/debug"
	"strconv"

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

// captureMemoryError reports a memory REST API failure to Sentry, tagging the
// event with the request context (method, path, HTTP status).
func captureMemoryError(method, path string, status int, err error) {
	if err == nil {
		return
	}
	hub := sentry.CurrentHub()
	if hub.Client() == nil {
		return
	}
	hub.WithScope(func(scope *sentry.Scope) {
		scope.SetTag("memory_method", method)
		scope.SetTag("memory_path", path)
		scope.SetTag("memory_status", strconv.Itoa(status))
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
