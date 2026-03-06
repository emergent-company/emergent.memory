package githubapp

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/uptrace/bun"
)

// Store handles database operations for GitHub App configuration.
type Store struct {
	db *bun.DB
}

// NewStore creates a new GitHub App config store.
func NewStore(db *bun.DB) *Store {
	return &Store{db: db}
}

// Get returns the GitHub App configuration (singleton).
// Returns nil, nil if no configuration exists.
func (s *Store) Get(ctx context.Context) (*GitHubAppConfig, error) {
	config := new(GitHubAppConfig)
	err := s.db.NewSelect().
		Model(config).
		Limit(1).
		Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return config, nil
}

// GetByAppID returns the GitHub App configuration by app ID.
func (s *Store) GetByAppID(ctx context.Context, appID int64) (*GitHubAppConfig, error) {
	config := new(GitHubAppConfig)
	err := s.db.NewSelect().
		Model(config).
		Where("app_id = ?", appID).
		Limit(1).
		Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return config, nil
}

// Create inserts a new GitHub App configuration.
func (s *Store) Create(ctx context.Context, config *GitHubAppConfig) (*GitHubAppConfig, error) {
	config.CreatedAt = time.Now()
	config.UpdatedAt = time.Now()
	_, err := s.db.NewInsert().
		Model(config).
		Returning("*").
		Exec(ctx)
	if err != nil {
		return nil, err
	}
	return config, nil
}

// Update updates an existing GitHub App configuration.
func (s *Store) Update(ctx context.Context, config *GitHubAppConfig, columns ...string) (*GitHubAppConfig, error) {
	config.UpdatedAt = time.Now()

	q := s.db.NewUpdate().
		Model(config).
		WherePK()

	if len(columns) > 0 {
		columns = append(columns, "updated_at")
		q = q.Column(columns...)
	}

	_, err := q.Returning("*").Exec(ctx)
	if err != nil {
		return nil, err
	}
	return config, nil
}

// Delete removes all GitHub App configurations.
func (s *Store) Delete(ctx context.Context) (bool, error) {
	res, err := s.db.NewDelete().
		Model((*GitHubAppConfig)(nil)).
		Where("1=1"). // delete all rows
		Exec(ctx)
	if err != nil {
		return false, err
	}
	rows, _ := res.RowsAffected()
	return rows > 0, nil
}

// UpdateInstallation sets the installation_id and installation_org for an app.
func (s *Store) UpdateInstallation(ctx context.Context, appID int64, installationID int64, org string) error {
	res, err := s.db.NewUpdate().
		Model((*GitHubAppConfig)(nil)).
		Set("installation_id = ?", installationID).
		Set("installation_org = ?", org).
		Set("updated_at = ?", time.Now()).
		Where("app_id = ?", appID).
		Exec(ctx)
	if err != nil {
		return err
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return fmt.Errorf("app_id %d not found", appID)
	}
	return nil
}
