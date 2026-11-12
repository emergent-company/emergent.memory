package mcp

import (
	"errors"
	"fmt"
	"net"
	"testing"

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
