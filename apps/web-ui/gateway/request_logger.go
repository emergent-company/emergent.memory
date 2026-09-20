package main

import (
	"context"
	"log/slog"
	"net/url"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

// requestLogger is echo's RequestLogger with a redacting LogValuesFunc: query
// parameters named `key` or `token` are masked before the URI is written, so
// share keys and tokens never land in the access log. It otherwise matches the
// default RequestLogger field set.
func requestLogger() echo.MiddlewareFunc {
	return middleware.RequestLoggerWithConfig(middleware.RequestLoggerConfig{
		LogLatency:       true,
		LogRemoteIP:      true,
		LogHost:          true,
		LogMethod:        true,
		LogURI:           true,
		LogRequestID:     true,
		LogUserAgent:     true,
		LogStatus:        true,
		LogError:         true,
		LogContentLength: true,
		LogResponseSize:  true,
		HandleError:      true,
		LogValuesFunc: func(c echo.Context, v middleware.RequestLoggerValues) error {
			attrs := []slog.Attr{
				slog.String("method", v.Method),
				slog.String("uri", redactSensitiveQuery(v.URI)),
				slog.Int("status", v.Status),
				slog.Duration("latency", v.Latency),
				slog.String("host", v.Host),
				slog.String("bytes_in", v.ContentLength),
				slog.Int64("bytes_out", v.ResponseSize),
				slog.String("user_agent", v.UserAgent),
				slog.String("remote_ip", v.RemoteIP),
				slog.String("request_id", v.RequestID),
			}
			lvl := slog.LevelInfo
			if v.Error != nil {
				lvl = slog.LevelError
				attrs = append(attrs, slog.String("error", v.Error.Error()))
			}
			slog.LogAttrs(context.Background(), lvl, "REQUEST", attrs...)
			return nil
		},
	})
}

// redactSensitiveQuery masks query-parameter values for known secret names in a
// request URI, leaving every other parameter untouched.
func redactSensitiveQuery(rawURI string) string {
	u, err := url.Parse(rawURI)
	if err != nil || u.RawQuery == "" {
		return rawURI
	}
	q := u.Query()
	changed := false
	for _, k := range []string{"key", "token"} {
		if q.Has(k) {
			q.Set(k, "[REDACTED]")
			changed = true
		}
	}
	if !changed {
		return rawURI
	}
	u.RawQuery = q.Encode()
	return u.String()
}
