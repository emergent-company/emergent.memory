package sources

import (
	"context"
	"fmt"
)

// errGitHubNotImplemented is the shared not-implemented sentinel for the GitHub
// connector readiness stub.
var errGitHubNotImplemented = fmt.Errorf("github: connector not yet implemented")

// githubConnector implements Connector as a documented readiness stub. The
// repo → markdown/docs enumeration and sync (§7.3) ships in a follow-up change;
// until then every transport operation reports not-implemented.
type githubConnector struct{}

func (c *githubConnector) Enumerate(ctx context.Context, cfg map[string]any) ([]SourceItem, error) {
	return nil, errGitHubNotImplemented
}

func (c *githubConnector) Fetch(ctx context.Context, item SourceItem) (content, mimeType string, err error) {
	return "", "", errGitHubNotImplemented
}

func (c *githubConnector) TestConnection(ctx context.Context) error {
	return errGitHubNotImplemented
}
