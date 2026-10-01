// Package invites handles invitation management
package invites

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/domain/email"
	"github.com/emergent-company/emergent.memory/domain/notifications"
	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// notifier is the minimal notifications.Service surface the invites service
// needs. *notifications.Service satisfies it; a fake stands in for tests.
type notifier interface {
	Create(context.Context, notifications.CreateInput) (*notifications.Notification, error)
}

// Service handles invitation operations
type Service struct {
	db       bun.IDB
	emailSvc *email.JobsService
	baseURL  string
	log      *slog.Logger
	// notificationsSvc is the central notification producer (nil-safe).
	notificationsSvc notifier
}

// NewService creates a new invites service
func NewService(db bun.IDB, emailSvc *email.JobsService, cfg *config.Config, notificationsSvc *notifications.Service, log *slog.Logger) *Service {
	var n notifier
	if notificationsSvc != nil {
		n = notificationsSvc
	}
	return &Service{
		db:               db,
		emailSvc:         emailSvc,
		baseURL:          cfg.AppURL,
		log:              log,
		notificationsSvc: n,
	}
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

// ListByProject returns invites sent for a specific project, with each invite's
// delivery state resolved from its most recent invite-scoped kb.email_jobs row in
// the same query (no per-row round trip).
func (s *Service) ListByProject(ctx context.Context, projectID string) ([]SentInvite, error) {
	var invites []SentInvite
	err := s.db.NewRaw(`
		SELECT i.id, i.email, i.role, i.status, i.created_at, i.expires_at,
		       ej.delivery_status, ej.delivery_status_at
		FROM kb.invites i
		LEFT JOIN LATERAL (
			SELECT delivery_status, delivery_status_at
			FROM kb.email_jobs
			WHERE source_type = 'invite' AND source_id = i.id
			ORDER BY created_at DESC
			LIMIT 1
		) ej ON true
		WHERE i.project_id = ?
		ORDER BY i.created_at DESC
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
		"org_admin":      true,
		"project_admin":  true,
		"project_user":   true,
		"project_viewer": true,
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

	var inviterID *string
	if req.InviterID != "" {
		inviterID = &req.InviterID
	}

	invite := &Invite{
		OrganizationID:  req.OrgID,
		ProjectID:       projectIDPtr,
		Email:           strings.ToLower(req.Email),
		Role:            req.Role,
		Token:           token,
		Status:          "pending",
		ExpiresAt:       &expiresAt,
		CreatedAt:       time.Now(),
		InvitedByUserID: inviterID,
	}

	_, err = s.db.NewInsert().Model(invite).Exec(ctx)
	if err != nil {
		return nil, apperror.ErrDatabase.WithInternal(err)
	}

	// Enqueue invitation email (non-fatal: invitation is still valid even if email fails to queue)
	_ = s.enqueueInviteEmail(ctx, s.db, invite, req.ProjectName, req.InviterName)

	// Notify the invitee in-app if they already have an account. Best-effort:
	// when the email maps to no user (or the producer is unwired), the email
	// above remains the delivery channel.
	s.emitInviteReceived(ctx, invite, req.ProjectName)

	return invite, nil
}

// emitInviteReceived sends an actionable account-scope invite.received
// notification to the invitee when their email already resolves to a user.
func (s *Service) emitInviteReceived(ctx context.Context, invite *Invite, projectName string) {
	if s.notificationsSvc == nil {
		return
	}

	targetUserID, err := s.resolveUserIDByEmail(ctx, invite.Email)
	if err != nil {
		s.log.Warn("failed to resolve invitee user id for notification",
			slog.String("inviteID", invite.ID),
			slog.String("email", invite.Email),
			slog.String("error", err.Error()))
		return
	}
	if targetUserID == "" {
		return // no account yet — email is the channel
	}

	if projectName == "" {
		projectName = "a project"
	}
	acceptURL := fmt.Sprintf("%s/invites/accept?token=%s", s.baseURL, invite.Token)
	acceptLabel := "Accept invite"
	actions, _ := json.Marshal([]map[string]string{
		{"label": "Accept", "value": "accept"},
		{"label": "Decline", "value": "decline"},
	})
	category := "invites"

	_, err = s.notificationsSvc.Create(ctx, notifications.CreateInput{
		UserID:         targetUserID,
		ProjectID:      invite.ProjectID,
		Scope:          notifications.ScopeAccount,
		EventKey:       "invite.received",
		Title:          "Project invitation",
		Message:        fmt.Sprintf("You've been invited to join %s.", projectName),
		Severity:       "info",
		Category:       &category,
		RequiresAction: true,
		ActionURL:      &acceptURL,
		ActionLabel:    &acceptLabel,
		Actions:        actions,
	})
	if err != nil {
		s.log.Warn("failed to emit invite.received notification",
			slog.String("inviteID", invite.ID),
			slog.String("userID", targetUserID),
			slog.String("error", err.Error()))
	}
}

// resolveUserIDByEmail returns the user_id registered with the given email, or
// empty string when no account matches.
func (s *Service) resolveUserIDByEmail(ctx context.Context, email string) (string, error) {
	var userID string
	err := s.db.NewRaw(`
		SELECT user_id FROM core.user_emails WHERE LOWER(email) = LOWER(?) LIMIT 1
	`, email).Scan(ctx, &userID)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", nil
		}
		return "", err
	}
	return userID, nil
}

// enqueueInviteEmail enqueues the project-invitation email for the given invite.
// It returns any enqueue error but treats it as non-fatal: the caller continues
// and the invitation remains valid.
func (s *Service) enqueueInviteEmail(ctx context.Context, db bun.IDB, invite *Invite, projectName, inviterName string) error {
	if s.emailSvc == nil {
		return nil
	}
	roleLabel := roleLabelFor(invite.Role)
	acceptURL := fmt.Sprintf("%s/invites/accept?token=%s", s.baseURL, invite.Token)
	if projectName == "" {
		projectName = "the project"
	}
	if inviterName == "" {
		inviterName = "A team member"
	}
	toName := invite.Email
	_, emailErr := s.emailSvc.EnqueueTx(ctx, db, email.EnqueueOptions{
		TemplateName: "project-invitation",
		ToEmail:      invite.Email,
		ToName:       &toName,
		Subject:      fmt.Sprintf("You've been invited to join %s on emergent.memory", projectName),
		TemplateData: map[string]interface{}{
			"inviterName": inviterName,
			"projectName": projectName,
			"roleLabel":   roleLabel,
			"acceptUrl":   acceptURL,
			"plainText":   email.ProjectInvitationPlainText(inviterName, projectName, roleLabel, acceptURL),
		},
		SourceType: stringPtr("invite"),
		SourceID:   &invite.ID,
	})
	if emailErr != nil {
		s.log.Warn("failed to enqueue project invitation email",
			slog.String("inviteID", invite.ID),
			slog.String("email", invite.Email),
			slog.String("error", emailErr.Error()))
	}
	return emailErr
}

func roleLabelFor(role string) string {
	switch role {
	case "project_admin":
		return "Admin"
	case "project_user":
		return "Member"
	case "project_viewer":
		return "Viewer (read-only)"
	case "org_admin":
		return "Organization Admin"
	default:
		return role
	}
}

func stringPtr(s string) *string {
	return &s
}

func timePtr(t time.Time) *time.Time {
	return &t
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

	// Resolve the organization membership role the invitation grants, failing
	// closed on a role Create's validation would never have written, so an
	// unexpected stored role is never inserted blindly.
	orgRole, ok := orgMembershipRole(invite.Role)
	if !ok {
		return apperror.NewInternal(
			fmt.Sprintf("invite %s has unexpected role %q", invite.ID, invite.Role),
			nil,
		)
	}

	// Defence in depth (issue #979): a project-scoped invitation grants project
	// membership only. org_admin is an organization-level role with no meaning
	// in project scope, so a project-scoped org_admin invitation — a pre-existing
	// legacy row that predates the create-side rejection — would write the
	// out-of-vocabulary value "org_admin" into kb.project_memberships.role AND
	// grant org-level admin in kb.organization_memberships. Refuse it fail
	// closed here so no membership rows are written.
	if invite.ProjectID != nil && invite.Role == "org_admin" {
		return apperror.NewForbidden("org_admin is not valid for a project-scoped invite")
	}

	// Defence in depth (issue #967): an org_admin membership grant must originate
	// from an inviter who holds org_admin (or superadmin_full) authority over the
	// invite's organization. Re-verify at acceptance time so a pre-existing
	// invitation minted by a plain member before the create-side gate existed
	// cannot be used to self-escalate. An invitation with no recorded inviter has
	// no authority to assert, so it is refused (fail closed).
	if orgRole == "org_admin" {
		admin, err := s.inviterIsOrgAdmin(ctx, &invite)
		if err != nil {
			return apperror.NewDatabase("failed to verify inviter authority", err)
		}
		if !admin {
			return apperror.NewForbidden("org_admin invitations require an org_admin inviter")
		}
	}

	// Begin transaction
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return apperror.ErrDatabase.WithInternal(err)
	}
	defer tx.Rollback() //nolint:errcheck

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

	// Add user to org membership if needed, with the role the invitation
	// carried (org_admin for org_admin invitations, member otherwise).
	_, err = tx.NewRaw(`
		INSERT INTO kb.organization_memberships (organization_id, user_id, role, created_at)
		VALUES (?, ?, ?, NOW())
		ON CONFLICT (organization_id, user_id) DO NOTHING
	`, invite.OrganizationID, userID, orgRole).Exec(ctx)
	if err != nil {
		return apperror.ErrDatabase.WithInternal(err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit invite acceptance: %w", err)
	}

	s.emitMembershipGranted(ctx, userID, &invite)
	return nil
}

// emitMembershipGranted notifies the accepting user that a membership was
// granted: project invites yield project.member.added, org-only invites yield
// user.access.granted. Best-effort.
func (s *Service) emitMembershipGranted(ctx context.Context, userID string, invite *Invite) {
	if s.notificationsSvc == nil {
		return
	}

	var in notifications.CreateInput
	if invite.ProjectID != nil {
		category := "membership"
		relatedType := "project"
		in = notifications.CreateInput{
			UserID:              userID,
			ProjectID:           invite.ProjectID,
			Scope:               notifications.ScopeAccount,
			EventKey:            "project.member.added",
			Title:               "Added to project",
			Message:             "You were added to a project.",
			Severity:            "info",
			Category:            &category,
			RelatedResourceType: &relatedType,
			RelatedResourceID:   invite.ProjectID,
		}
	} else {
		category := "permissions"
		relatedType := "organization"
		in = notifications.CreateInput{
			UserID:              userID,
			Scope:               notifications.ScopeAccount,
			EventKey:            "user.access.granted",
			Title:               "Access granted",
			Message:             "You were granted access to an organization.",
			Severity:            "info",
			Category:            &category,
			RelatedResourceType: &relatedType,
			RelatedResourceID:   &invite.OrganizationID,
		}
	}

	if _, err := s.notificationsSvc.Create(ctx, in); err != nil {
		s.log.Warn("failed to emit membership-granted notification",
			slog.String("userID", userID),
			slog.String("inviteID", invite.ID),
			slog.String("error", err.Error()))
	}
}

// orgMembershipRole maps an invitation role to the kb.organization_memberships
// role it grants on acceptance. org_admin invitations grant org_admin
// membership; the project-scoped roles (project_admin, project_user,
// project_viewer) grant plain member membership at the organization level. Any
// other role fails closed (ok=false) so an unexpected invite role is never
// inserted blindly.
func orgMembershipRole(inviteRole string) (role string, ok bool) {
	switch inviteRole {
	case "org_admin":
		return "org_admin", true
	case "project_admin", "project_user", "project_viewer":
		return "member", true
	default:
		return "", false
	}
}

// inviterIsOrgAdmin reports whether the invitation's recorded inviter holds
// org_admin authority over the invitation's organization, or is an active
// superadmin_full. It is the acceptance-side defence-in-depth counterpart to the
// create-side role gate: an org_admin grant is refused unless its inviter could
// have minted it (issue #967). The decision is the single shared
// org-administration entitlement check (pkg/auth.CanAdministerOrgOrPlatform), so
// it cannot drift from the create-side gate (issue #812 §4.5, issue #1162).
func (s *Service) inviterIsOrgAdmin(ctx context.Context, invite *Invite) (bool, error) {
	if invite.InvitedByUserID == nil || *invite.InvitedByUserID == "" {
		return false, nil
	}
	return auth.CanAdministerOrgOrPlatform(ctx, s.db, invite.OrganizationID, *invite.InvitedByUserID)
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

// inviteResendGuardWindow is the server-enforced minimum interval between two
// invitation emails for the same invitation. A resend for an invitation that
// already has an invite-scoped kb.email_jobs row created within this window is
// an idempotent no-op: it does not enqueue a second job. The window covers the
// double-submit / client-retry cases where the earlier email is still in flight
// or was just delivered, so repeated POSTs cannot send unbounded duplicate
// invitation emails. It is a server guarantee; the UI's confirmation dialog is
// not.
const inviteResendGuardWindow = 60 * time.Second

// hasRecentInviteEmailJob reports whether the invitation already has an
// invite-scoped email job (source_type='invite', source_id=inviteID) created
// within inviteResendGuardWindow. The lookup is served by the existing
// idx_email_jobs_source (source_type, source_id) index. It runs against the
// executor the caller supplies so a resend can evaluate it inside the same
// transaction that holds the per-invite advisory lock.
func (s *Service) hasRecentInviteEmailJob(ctx context.Context, db bun.IDB, inviteID string) (bool, error) {
	var count int
	err := db.NewRaw(`
		SELECT COUNT(*) FROM kb.email_jobs
		WHERE source_type = 'invite' AND source_id = ?
		  AND created_at > now() - (? || ' seconds')::interval
	`, inviteID, fmt.Sprintf("%d", int(inviteResendGuardWindow.Seconds()))).Scan(ctx, &count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// resendGuardLockKey returns the pg_advisory_xact_lock key for an invitation.
// The invite id is hashed with hashtextextended (stable for the lifetime of a
// database) under a namespace prefix so invite keys cannot collide with other
// advisory-lock users. A hash collision between two invites merely serialises
// those two resends momentarily; it never merges their jobs, because every
// query in the critical section is still scoped by the concrete invite id.
func resendGuardLockKey(inviteID string) string {
	return "invite_resend:" + inviteID
}

// Resend re-sends a pending invitation email: it keeps the SAME token, extends
// expires_at to now + 7 days, and re-enqueues the project-invitation job. Only a
// pending invite may be resent; an unknown invite or a non-pending invite is 404
// (matching the revoke guard). Enqueue failure is non-fatal: the invitation stays
// valid.
//
// Resend is server-side idempotent over a short window: if the invitation
// already has an invite-scoped email job created within
// inviteResendGuardWindow, the request is a no-op and the existing invitation is
// returned. This guards against double-submits and repeated POSTs sending
// unbounded duplicate invitation emails.
//
// The guard is an invariant, not best-effort: the check and the insert run in
// one transaction that first takes a per-invite pg_advisory_xact_lock. Two
// truly-concurrent resends for the same invite are therefore serialised, so
// exactly one of them observes "no recent job" and enqueues; the other observes
// the committed job and is a clean no-op. Different invites hash to different
// keys and never contend.
func (s *Service) Resend(ctx context.Context, inviteID string) (*Invite, error) {
	var invite Invite
	err := s.db.NewSelect().
		Model(&invite).
		Where("id = ?", inviteID).
		Scan(ctx)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, apperror.NewNotFound("invite", inviteID)
		}
		return nil, apperror.NewDatabase("resend invitation", err)
	}

	if invite.Status != "pending" {
		return nil, apperror.NewNotFound("invite", inviteID)
	}

	// Derive the project name for the email the same way Create does when the
	// caller supplied one: look it up from kb.projects, fall back to "the project".
	// Done inside the transaction and only on the enqueue path, so a no-op
	// resend costs no extra query.
	projectName := "the project"

	// The inviter name is not stored on the invite; pass "" so the helper's
	// existing "A team member" fallback applies.
	now := time.Now()
	err = s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		// Serialise concurrent resends for this invite. pg_advisory_xact_lock is
		// transaction-scoped: it is released automatically on commit/rollback,
		// so it cannot leak or deadlock a connection pool.
		if _, err := tx.NewRaw(
			`SELECT pg_advisory_xact_lock(hashtextextended(?, 0))`,
			resendGuardLockKey(inviteID),
		).Exec(ctx); err != nil {
			return apperror.NewDatabase("acquire resend guard lock", err)
		}

		// Idempotency guard, evaluated under the lock: an email for this
		// invitation was already enqueued recently (in flight or just
		// delivered). The request is a clean no-op rather than a duplicate email.
		recent, err := s.hasRecentInviteEmailJob(ctx, tx, inviteID)
		if err != nil {
			return apperror.NewDatabase("check recent invitation email", err)
		}
		if recent {
			return nil
		}

		if invite.ProjectID != nil {
			var name string
			if e := tx.NewSelect().
				TableExpr("kb.projects").
				Column("name").
				Where("id = ?", *invite.ProjectID).
				Limit(1).
				Scan(ctx, &name); e == nil && name != "" {
				projectName = name
			}
		}

		// Keep the same token; only extend the expiry.
		invite.ExpiresAt = timePtr(now.Add(7 * 24 * time.Hour))
		if _, err := tx.NewRaw(
			`UPDATE kb.invites SET expires_at = ? WHERE id = ?`,
			invite.ExpiresAt, invite.ID,
		).Exec(ctx); err != nil {
			return apperror.NewDatabase("extend invitation expiry", err)
		}

		// Enqueue on the same transaction so the job commits atomically with the
		// lock's release; the next waiter then sees it and stays a no-op.
		_ = s.enqueueInviteEmail(ctx, tx, &invite, projectName, "")
		return nil
	})
	if err != nil {
		return nil, err
	}

	return &invite, nil
}

// ProjectOrg resolves the owning organization of a project server-side
// (kb.projects.organization_id). It is the authoritative org source for the
// orgId-binding check in Create: neither the body orgId nor the body projectId
// is trusted, the owning org is derived and compared (issue #960).
func (s *Service) ProjectOrg(ctx context.Context, projectID string) (string, error) {
	var orgID string
	err := s.db.NewSelect().
		TableExpr("kb.projects").
		Column("organization_id").
		Where("id = ?", projectID).
		Limit(1).
		Scan(ctx, &orgID)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", apperror.NewNotFound("project", projectID)
		}
		return "", apperror.NewDatabase("failed to resolve project organization", err)
	}
	return orgID, nil
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
