package mcp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestUpstreamFailureTextDoesNotRelayTheCause is the tool-result half of the
// internal-detail leak class: the text an upstream call produced is logged, not
// handed to the agent.
//
// A transport error carries the resolved address, and an upstream body can echo
// the subscription token — both must stay out of the transcript. The status is
// different: it is part of the API's contract, and 401/403 is the one thing the
// operator needs to know.
func TestUpstreamFailureTextDoesNotRelayTheCause(t *testing.T) {
	tests := []struct {
		name        string
		err         error
		wantContain string
		mustNot     []string
	}{
		{
			name: "upstream status crosses over, its body does not",
			err: &upstreamStatusError{
				Service: "Brave Search",
				Status:  401,
				Detail:  "invalid subscription token BSAxxxxxxxx",
			},
			wantContain: "upstream status 401",
			mustNot:     []string{"BSAxxxxxxxx", "invalid subscription token"},
		},
		{
			name:        "a transport error collapses to the generic text",
			err:         &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connect: connection refused")},
			wantContain: "retrying later may succeed",
			mustNot:     []string{"dial", "connection refused", "tcp"},
		},
		{
			name:        "a wrapped status error is still recognised",
			err:         fmt.Errorf("search: %w", &upstreamStatusError{Service: "Reddit", Status: 429, Detail: "slow down"}),
			wantContain: "upstream status 429",
			mustNot:     []string{"slow down"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			text := upstreamFailureText("Brave Search", tt.err)
			require.Contains(t, text, tt.wantContain)
			for _, forbidden := range tt.mustNot {
				require.NotContains(t, text, forbidden,
					"the failure's own text must not reach the tool result")
			}
		})
	}
}

// TestBraveSearchToolResultDoesNotRelayUpstreamDetail pins the call site, not
// just the helper: the Brave tool must render its failure through
// upstreamFailureText, so the text the agent receives is the caller-facing one
// and not the transport error (which carries the resolved URL).
//
// The timeout is set to a nanosecond so the call fails in transport without any
// network access, which is exactly the case that used to leak the address.
func TestBraveSearchToolResultDoesNotRelayUpstreamDetail(t *testing.T) {
	svc := &Service{
		braveSearchAPIKey:  "test-key",
		braveSearchTimeout: time.Nanosecond,
		log:                slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	res, err := svc.executeBraveWebSearch(context.Background(), "", map[string]any{"query": "anything"})
	require.NoError(t, err)
	require.NotNil(t, res)
	require.True(t, res.IsError)
	require.Len(t, res.Content, 1)

	text := res.Content[0].Text
	require.Contains(t, text, "Brave Search request failed")
	require.NotContains(t, text, "api.search.brave.com",
		"the tool result must not carry the resolved endpoint")
	require.NotContains(t, text, "Client.Timeout",
		"the tool result must not carry Go's transport detail")
}
