package sources

import (
	"context"

	"github.com/google/uuid"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
)

// UpsertSourceIssuePRLink records the server-owned issue/PR link.
func (r *Repository) UpsertSourceIssuePRLink(ctx context.Context, link *SourceIssuePRLink) error {
	if link == nil {
		return nil
	}
	err := r.projectTx(ctx, link.ProjectID, func(ctx context.Context, tx bun.Tx) error {
		_, err := tx.NewInsert().Model(link).
			On("CONFLICT (project_id, repo, pr_number) DO UPDATE").
			Set("issue_canonical_id = EXCLUDED.issue_canonical_id").
			Set("issue_number = EXCLUDED.issue_number").
			Set("issue_source_id = EXCLUDED.issue_source_id").
			Set("pr_url = EXCLUDED.pr_url").
			Set("branch = EXCLUDED.branch").
			Set("head_sha = EXCLUDED.head_sha").
			Set("state = EXCLUDED.state").
			Set("closes_issue = EXCLUDED.closes_issue").
			Set("updated_at = now()").
			Returning("*").
			Exec(ctx)
		return err
	})
	if err != nil {
		return apperror.NewDatabase("failed to upsert issue pull request link", err)
	}
	return nil
}

// FindSourceIssuePRLink returns the server-owned link for a repository PR.
func (r *Repository) FindSourceIssuePRLink(ctx context.Context, projectID uuid.UUID, repo string, prNumber int) (*SourceIssuePRLink, error) {
	var link SourceIssuePRLink
	err := r.projectTx(ctx, projectID, func(ctx context.Context, tx bun.Tx) error {
		return tx.NewSelect().Model(&link).
			Where("ipr.project_id = ?", projectID).
			Where("ipr.repo = ?", repo).
			Where("ipr.pr_number = ?", prNumber).
			Scan(ctx)
	})
	if err != nil {
		return nil, err
	}
	return &link, nil
}
