package sources

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/githubapp"
)

func TestIssueNumberForPullRequest(t *testing.T) {
	cases := []struct {
		name string
		pr   githubapp.WebhookPullRequest
		want int
	}{
		{name: "closes", pr: githubapp.WebhookPullRequest{Body: "Closes #811"}, want: 811},
		{name: "fixes", pr: githubapp.WebhookPullRequest{Body: "fixes #726"}, want: 726},
		{name: "branch", pr: githubapp.WebhookPullRequest{Head: githubapp.WebhookRef{Ref: "fix/813-docs"}}, want: 813},
		{name: "title", pr: githubapp.WebhookPullRequest{Title: "fix: thing (#814)"}, want: 814},
		{name: "none", pr: githubapp.WebhookPullRequest{Body: "no issue"}, want: 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, issueNumberForPullRequest(&githubapp.WebhookPayload{PullRequest: tc.pr}))
		})
	}
}
