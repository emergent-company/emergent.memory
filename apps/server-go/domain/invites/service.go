// Package invites handles invitation management
package invites

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"strings"
	"time"

	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent/pkg/apperror"
)

// Service handles invitation operations
type Service struct {
	db bun.IDB
}

// NewService creates a new invites service
func NewService(db bun.IDB) *Service {
	return &Service{db: db}
}

type inviteRow struct {
	ID             string     `bun:"id"`
	Email          string     `bun:"email"`
	OrganizationID string     `bun:"organization_id"`
	ProjectID      *string    `bun:"project_id"`
	ProjectName    *string    `bun:"project_name"`
	Role           string     `bun:"role"`
	Token          string     `bun:"token"`
	Status         string     `bun:"status"`
	ExpiresAt      *time.Time `bun:"expires_at"`
	CreatedAt      time.Time  `bun:"created_at"`
}

// ListPendingForUser returns pending invitations for a user
func (s *Service) ListPendingForUser(ctx context.Context, userID string) ([]PendingInvite, error) {
	if userID == "" {
		return []PendingInvite{}, nil
	}

	// Get user's emails
	var emails []string
	err := s.db.NewRaw(`
		SELECT email FROM core.user_emails WHERE user_id = ?
	`, userID).Scan(ctx, &emails)
	if err != nil {
		return nil, err
	}

	if len(emails) == 0 {
		return []PendingInvite{}, nil
	}

	// Lowercase all emails for case-insensitive matching
	for i, email := range emails {
		emails[i] = strings.ToLower(email)
	}

	// Find pending invitations for these emails
	var inviteRows []inviteRow
	err = s.db.NewRaw(`
		SELECT 
			i.id, i.email, i.organization_id, i.project_id, 
			p.name as project_name, i.role, i.token, i.status, 
			i.expires_at, i.created_at
		FROM kb.invites i
		LEFT JOIN kb.projects p ON p.id = i.project_id
		WHERE LOWER(i.email) IN (?)
		  AND i.status = 'pending'
		  AND (i.expires_at IS NULL OR i.expires_at > NOW())
		ORDER BY i.created_at DESC
	`, bun.In(emails)).Scan(ctx, &inviteRows)
	if err != nil {
		return nil, err
	}

	if len(inviteRows) == 0 {
		return []PendingInvite{}, nil
	}

	// Get organization names
	orgIDs := make([]string, 0)
	seen := make(map[string]bool)
	for _, inv := range inviteRows {
		if !seen[inv.OrganizationID] {
			seen[inv.OrganizationID] = true
			orgIDs = append(orgIDs, inv.OrganizationID)
		}
	}

	type orgName struct {
		ID   string `bun:"id"`
		Name string `bun:"name"`
	}
	var orgs []orgName
	err = s.db.NewRaw(`
		SELECT id, name FROM kb.orgs WHERE id IN (?)
	`, bun.In(orgIDs)).Scan(ctx, &orgs)
	if err != nil {
		return nil, err
	}

	orgMap := make(map[string]string)
	for _, org := range orgs {
		orgMap[org.ID] = org.Name
	}

	// Build response
	result := make([]PendingInvite, len(inviteRows))
	for i, inv := range inviteRows {
		invite := PendingInvite{
			ID:             inv.ID,
			OrganizationID: inv.OrganizationID,
			Role:           inv.Role,
			Token:          inv.Token,
			CreatedAt:      inv.CreatedAt,
		}
		if inv.ProjectID != nil {
			invite.ProjectID = inv.ProjectID
		}
		if inv.ProjectName != nil {
			invite.ProjectName = inv.ProjectName
		}
		if name, ok := orgMap[inv.OrganizationID]; ok {
			invite.OrganizationName = &name
		}
		if inv.ExpiresAt != nil {
			invite.ExpiresAt = inv.ExpiresAt
		}
		result[i] = invite
	}

	return result, nil
}

// ListByProject returns invites sent for a specific project
func (s *Service) ListByProject(ctx context.Context, projectID string) ([]SentInvite, error) {
	var invites []SentInvite
	err := s.db.NewRaw(`
		SELECT id, email, role, status, created_at, expires_at
		FROM kb.invites
		WHERE project_id = ?
		ORDER BY created_at DESC
	`, projectID).Scan(ctx, &invites)
	if err != nil {
		return nil, apperror.ErrDatabase.WithInternal(err)
	}
	if invites == nil {
		return []SentInvite{}, nil
	}
	return invites, nil
}

// Create creates a new invitation
func (s *Service) Create(ctx context.Context, req *CreateInviteRequest) (*Invite, error) {
	// Validate role
	validRoles := map[string]bool{
		"org_admin":     true,
		"project_admin": true,
		"project_user":  true,
	}
	if !validRoles[req.Role] {
		return nil, apperror.ErrBadRequest.WithMessage("invalid role")
	}

	// Validate email
	if req.Email == "" || !strings.Contains(req.Email, "@") {
		return nil, apperror.ErrBadRequest.WithMessage("invalid email")
	}

	// Generate token
	token, err := generateToken()
	if err != nil {
		return nil, apperror.ErrInternal.WithInternal(err)
	}

	// Check if invite already exists for this email+project
	var existing int
	var projectIDPtr *string
	if req.ProjectID != "" {
		projectIDPtr = &req.ProjectID
		err = s.db.NewRaw(`
			SELECT COUNT(*) FROM kb.invites 
			WHERE LOWER(email) = LOWER(?) 
			  AND project_id = ? 
			  AND status = 'pending'
		`, req.Email, req.ProjectID).Scan(ctx, &existing)
	} else {
		err = s.db.NewRaw(`
			SELECT COUNT(*) FROM kb.invites 
			WHERE LOWER(email) = LOWER(?) 
			  AND project_id IS NULL 
			  AND organization_id = ?
			  AND status = 'pending'
		`, req.Email, req.OrgID).Scan(ctx, &existing)
	}
	if err != nil {
		return nil, apperror.ErrDatabase.WithInternal(err)
	}
	if existing > 0 {
		return nil, apperror.ErrBadRequest.WithMessage("invite already exists for this email")
	}

	// Set expiry (7 days from now)
	expiresAt := time.Now().Add(7 * 24 * time.Hour)

	invite := &Invite{
		OrganizationID: req.OrgID,
		ProjectID:      projectIDPtr,
		Email:          strings.ToLower(req.Email),
		Role:           req.Role,
		Token:          token,
		Status:         "pending",
		ExpiresAt:      &expiresAt,
		CreatedAt:      time.Now(),
	}

	_, err = s.db.NewInsert().Model(invite).Exec(ctx)
	if err != nil {
		return nil, apperror.ErrDatabase.WithInternal(err)
	}

	return invite, nil
}

// Accept accepts an invitation by token
func (s *Service) Accept(ctx context.Context, userID, token string) error {
	// Find the invite
	var invite Invite
	err := s.db.NewSelect().
		Model(&invite).
		Where("token = ?", token).
		Where("status = ?", "pending").
		Where("expires_at IS NULL OR expires_at > NOW()").
		Scan(ctx)
	if err != nil {
		if err == sql.ErrNoRows {
			return apperror.ErrNotFound.WithMessage("invite not found or expired")
		}
		return apperror.ErrDatabase.WithInternal(err)
	}

	// Get user's emails to verify the invite is for them
	var emails []string
	err = s.db.NewRaw(`
		SELECT LOWER(email) FROM core.user_emails WHERE user_id = ?
	`, userID).Scan(ctx, &emails)
	if err != nil {
		return apperror.ErrDatabase.WithInternal(err)
	}

	// Check if invite is for this user
	found := false
	inviteEmail := strings.ToLower(invite.Email)
	for _, email := range emails {
		if email == inviteEmail {
			found = true
			break
		}
	}
	if !found {
		return apperror.ErrForbidden.WithMessage("this invite is not for you")
	}

	// Begin transaction
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return apperror.ErrDatabase.WithInternal(err)
	}
	defer tx.Rollback()

	// Update invite status
	now := time.Now()
	_, err = tx.NewUpdate().
		Model(&invite).
		Set("status = ?", "accepted").
		Set("accepted_at = ?", now).
		Where("id = ?", invite.ID).
		Exec(ctx)
	if err != nil {
		return apperror.ErrDatabase.WithInternal(err)
	}

	// Add user to project membership
	if invite.ProjectID != nil {
		_, err = tx.NewRaw(`
			INSERT INTO kb.project_memberships (user_id, project_id, role, created_at)
			VALUES (?, ?, ?, NOW())
			ON CONFLICT (user_id, project_id) DO UPDATE SET role = EXCLUDED.role
		`, userID, *invite.ProjectID, invite.Role).Exec(ctx)
		if err != nil {
			return apperror.ErrDatabase.WithInternal(err)
		}
	}

	// Add user to org membership if needed
	_, err = tx.NewRaw(`
		INSERT INTO kb.org_memberships (user_id, org_id, role, created_at)
		VALUES (?, ?, 'member', NOW())
		ON CONFLICT (user_id, org_id) DO NOTHING
	`, userID, invite.OrganizationID).Exec(ctx)
	if err != nil {
		return apperror.ErrDatabase.WithInternal(err)
	}

	return tx.Commit()
}

// Decline declines an invitation
func (s *Service) Decline(ctx context.Context, userID, inviteID string) error {
	// Find the invite
	var invite Invite
	err := s.db.NewSelect().
		Model(&invite).
		Where("id = ?", inviteID).
		Where("status = ?", "pending").
		Scan(ctx)
	if err != nil {
		if err == sql.ErrNoRows {
			return apperror.ErrNotFound.WithMessage("invite not found")
		}
		return apperror.ErrDatabase.WithInternal(err)
	}

	// Verify the invite is for this user
	var emails []string
	err = s.db.NewRaw(`
		SELECT LOWER(email) FROM core.user_emails WHERE user_id = ?
	`, userID).Scan(ctx, &emails)
	if err != nil {
		return apperror.ErrDatabase.WithInternal(err)
	}

	found := false
	inviteEmail := strings.ToLower(invite.Email)
	for _, email := range emails {
		if email == inviteEmail {
			found = true
			break
		}
	}
	if !found {
		return apperror.ErrForbidden.WithMessage("this invite is not for you")
	}

	// Update status to declined
	_, err = s.db.NewUpdate().
		Model(&invite).
		Set("status = ?", "declined").
		Where("id = ?", inviteID).
		Exec(ctx)
	if err != nil {
		return apperror.ErrDatabase.WithInternal(err)
	}

	return nil
}

// Revoke revokes/cancels an invitation (by project admin)
func (s *Service) Revoke(ctx context.Context, inviteID string) error {
	now := time.Now()
	result, err := s.db.NewUpdate().
		Model((*Invite)(nil)).
		Set("status = ?", "revoked").
		Set("revoked_at = ?", now).
		Where("id = ?", inviteID).
		Where("status = ?", "pending").
		Exec(ctx)
	if err != nil {
		return apperror.ErrDatabase.WithInternal(err)
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return apperror.ErrNotFound.WithMessage("invite not found or already processed")
	}

	return nil
}

// GetByID retrieves an invite by ID
func (s *Service) GetByID(ctx context.Context, inviteID string) (*Invite, error) {
	var invite Invite
	err := s.db.NewSelect().
		Model(&invite).
		Where("id = ?", inviteID).
		Scan(ctx)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, apperror.ErrNotFound.WithMessage("invite not found")
		}
		return nil, apperror.ErrDatabase.WithInternal(err)
	}
	return &invite, nil
}

func generateToken() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}
