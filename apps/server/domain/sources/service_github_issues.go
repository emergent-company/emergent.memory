package sources

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/emergent-company/emergent.memory/domain/githubapp"
	"github.com/emergent-company/emergent.memory/domain/graph"
	"github.com/emergent-company/emergent.memory/pkg/apperror"
)

// BackfillGitHubIssues imports every open non-PR issue for a github_app source
// as an unassigned GitHubIssue board item. It never starts runs: historical
// issues wait for a human to assign them. Repeated calls are idempotent and
// preserve the status/assignee of an existing item.
func (s *Service) BackfillGitHubIssues(ctx context.Context, projectID, sourceID uuid.UUID) (int, error) {
	src, err := s.sourceForProject(ctx, projectID, sourceID)
	if err != nil {
		return 0, err
	}
	if src.Provider != ProviderTypeGitHubApp {
		return 0, apperror.NewValidation("issue backfill is only supported for github_app sources")
	}
	if src.AppID == nil || src.ExternalAccountID == nil || strings.TrimSpace(*src.ExternalAccountID) == "" {
		return 0, apperror.NewValidation("github_app source is missing its App or installation")
	}
	if s.graph == nil {
		return 0, apperror.NewInternal("graph service is not configured", nil)
	}
	if s.objectTypes != nil {
		if err := s.objectTypes.EnsureObjectTypes(ctx, projectID.String(), (&githubAppProvider{}).ObjectTypes()); err != nil {
			return 0, apperror.NewInternal("failed to ensure GitHub issue object types", err)
		}
	}
	if len(s.key) == 0 {
		return 0, apperror.NewValidation("LLM_ENCRYPTION_KEY is not configured")
	}
	app, err := s.repo.FindSourceAppByID(ctx, *src.AppID)
	if err != nil {
		return 0, apperror.NewNotFound("SourceApp", src.AppID.String())
	}
	privateKey, err := OpenSourceBlob(s.key, app.PrivateKeyEncrypted)
	if err != nil {
		return 0, apperror.NewInternal("failed to open the App private key", err)
	}
	installationID, err := strconv.ParseInt(strings.TrimSpace(*src.ExternalAccountID), 10, 64)
	if err != nil || installationID <= 0 {
		return 0, apperror.NewValidation("github_app source has a non-numeric installation id")
	}
	repos := sourceRepoList(src.Config)
	if len(repos) == 0 {
		return 0, apperror.NewValidation("github_app source has no repositories")
	}
	token, err := githubapp.NewTokenMinter(s.githubAPIBase, nil).MintInstallationToken(
		ctx, app.AppID, privateKey, installationID, repos,
		map[string]string{"contents": "read", "issues": "read", "metadata": "read"},
	)
	if err != nil {
		return 0, apperror.NewInternal("failed to mint an issue-backfill token", err)
	}
	client := githubapp.NewClient(s.githubAPIBase, token, nil)

	total := 0
	for _, repo := range repos {
		for page := 1; page <= 50; page++ {
			issues, listErr := client.ListOpenIssues(ctx, repo, page)
			if listErr != nil {
				return total, apperror.NewInternal("failed to list open issues", listErr)
			}
			for _, issue := range issues {
				if issue.PullRequest != nil {
					continue
				}
				if upsertErr := s.upsertBackfilledIssue(ctx, projectID, sourceID, repo, issue); upsertErr != nil {
					return total, upsertErr
				}
				total++
			}
			if len(issues) < 30 {
				break
			}
		}
	}
	return total, nil
}

func (s *Service) upsertBackfilledIssue(ctx context.Context, projectID, sourceID uuid.UUID, repo string, issue githubapp.IssueState) error {
	key := fmt.Sprintf("github:%s:%s#%d", sourceID.String(), repo, issue.Number)
	title := strings.TrimSpace(issue.Title)
	if title == "" {
		title = fmt.Sprintf("%s#%d", repo, issue.Number)
	}
	status := "ready"
	if existing, err := s.graph.FindHeadObjectByTypeAndKey(ctx, projectID.String(), "GitHubIssue", key); err == nil && existing != nil && existing.Status != nil {
		status = *existing.Status
	}
	labels := make([]string, 0, len(issue.Labels))
	for _, label := range issue.Labels {
		if name := strings.TrimSpace(label.Name); name != "" {
			labels = append(labels, name)
		}
	}
	props := map[string]any{
		"repo": repo, "issue_number": issue.Number, "title": title,
		"body": truncateUTF8(issue.Body, 16000), "state": issue.State,
		"labels": labels, "url": issue.HTMLURL, "source_id": sourceID.String(),
		"status": status,
	}
	_, _, err := s.graph.CreateOrUpdate(ctx, projectID, &graph.CreateGraphObjectRequest{
		Type: "GitHubIssue", Key: &key, Name: &title, Status: &status, Properties: props,
	}, nil)
	if err != nil {
		return apperror.NewInternal("failed to upsert a GitHub issue board item", err)
	}
	return nil
}
