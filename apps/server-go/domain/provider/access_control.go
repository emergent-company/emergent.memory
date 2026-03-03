package provider

import (
	"context"
	"fmt"

	"github.com/emergent-company/emergent/pkg/apperror"
	"github.com/emergent-company/emergent/pkg/auth"
)

// --- Application-level cross-tenant access guards ---
//
// These helpers enforce that mutations only operate on resources belonging to
// the authenticated caller's organization / project. They are called by
// CredentialService write methods and any future API handlers before any DB
// mutation.
//
// Design rationale:
//   - All read methods scope queries by org/project IDs already — no extra guard needed.
//   - Write / delete methods must verify the caller owns the resource before acting.
//   - We do NOT rely solely on database-level RLS for this; the application layer
//     validates first so that error messages are clear and observable.

// assertCallerOwnsOrg verifies that the authenticated org in the context matches
// the provided orgID. Returns ErrForbidden if the check fails.
func assertCallerOwnsOrg(ctx context.Context, orgID string) error {
	callerOrgID := auth.OrgIDFromContext(ctx)
	if callerOrgID == "" {
		return apperror.ErrUnauthorized.WithMessage("organization context required")
	}
	if callerOrgID != orgID {
		return apperror.ErrForbidden.WithMessage("access to organization credential denied")
	}
	return nil
}

// assertCallerOwnsProject verifies that the project's org matches the
// authenticated org in the context. It also ensures the projectID is non-empty.
//
// If the projectID is valid but the org is unknown (e.g. super-admin bypass),
// this function looks up the project's org via the repository.
func (s *CredentialService) assertCallerOwnsProject(ctx context.Context, projectID string) error {
	if projectID == "" {
		return fmt.Errorf("project ID is required")
	}
	callerOrgID := auth.OrgIDFromContext(ctx)
	if callerOrgID == "" {
		return apperror.ErrUnauthorized.WithMessage("organization context required")
	}

	// Look up the project's owning org and confirm it matches the caller.
	projectOrgID, err := s.repo.GetOrgIDForProject(ctx, projectID)
	if err != nil {
		return err
	}
	if projectOrgID != callerOrgID {
		return apperror.ErrForbidden.WithMessage("access to project credential denied")
	}
	return nil
}

// SaveOrgCredential encrypts and stores a credential for an organization.
// Enforces that the caller belongs to the same organization.
func (s *CredentialService) SaveOrgCredential(ctx context.Context, orgID string, provider ProviderType, plaintext []byte, gcpProject, location string) error {
	if err := assertCallerOwnsOrg(ctx, orgID); err != nil {
		return err
	}

	ciphertext, nonce, err := s.EncryptCredential(plaintext)
	if err != nil {
		return fmt.Errorf("failed to encrypt credential: %w", err)
	}

	cred := &OrganizationProviderCredential{
		OrgID:               orgID,
		Provider:            provider,
		EncryptedCredential: ciphertext,
		EncryptionNonce:     nonce,
		GCPProject:          gcpProject,
		Location:            location,
	}
	return s.repo.UpsertOrgCredential(ctx, cred)
}

// DeleteOrgCredential removes an organization's stored credential.
// Enforces that the caller belongs to the same organization.
func (s *CredentialService) DeleteOrgCredential(ctx context.Context, orgID string, provider ProviderType) error {
	if err := assertCallerOwnsOrg(ctx, orgID); err != nil {
		return err
	}
	return s.repo.DeleteOrgCredential(ctx, orgID, provider)
}

// SaveProjectPolicy sets a project's provider policy.
// Enforces that the caller's organization owns the project.
func (s *CredentialService) SaveProjectPolicy(ctx context.Context, projectID string, provider ProviderType, policy ProviderPolicy, plaintext []byte, gcpProject, location, embeddingModel, generativeModel string) error {
	if err := s.assertCallerOwnsProject(ctx, projectID); err != nil {
		return err
	}

	p := &ProjectProviderPolicy{
		ProjectID:       projectID,
		Provider:        provider,
		Policy:          policy,
		GCPProject:      gcpProject,
		Location:        location,
		EmbeddingModel:  embeddingModel,
		GenerativeModel: generativeModel,
	}

	// Only encrypt if the project policy includes its own credentials
	if policy == PolicyProject && len(plaintext) > 0 {
		ciphertext, nonce, err := s.EncryptCredential(plaintext)
		if err != nil {
			return fmt.Errorf("failed to encrypt project credential: %w", err)
		}
		p.EncryptedCredential = ciphertext
		p.EncryptionNonce = nonce
	}

	return s.repo.UpsertProjectPolicy(ctx, p)
}
