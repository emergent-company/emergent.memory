package sources

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
)

// ListSavedCredentials returns a project's reusable credentials, optionally
// filtered to one provider. Secrets never leave the service.
func (s *Service) ListSavedCredentials(ctx context.Context, projectID uuid.UUID, providerType string) ([]*SavedCredential, error) {
	if providerType != "" {
		if _, err := s.savedCredentialProvider(providerType); err != nil {
			return nil, err
		}
	}
	creds, counts, err := s.repo.ListCredentials(ctx, projectID, providerType)
	if err != nil {
		return nil, err
	}
	out := make([]*SavedCredential, len(creds))
	for i, cred := range creds {
		out[i] = toSavedCredential(cred, counts[cred.ID])
	}
	return out, nil
}

// CreateSavedCredential creates a reusable, encrypted credential for a provider
// that supports credentials-based connect.
func (s *Service) CreateSavedCredential(ctx context.Context, projectID uuid.UUID, providerType, name string, creds map[string]any) (*SavedCredential, error) {
	p, err := s.savedCredentialProvider(providerType)
	if err != nil {
		return nil, err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, apperror.NewValidation("name is required")
	}
	extracted, err := s.extractSavedCredentials(p, creds)
	if err != nil {
		return nil, err
	}
	blob, err := s.encryptCredentials(extracted)
	if err != nil {
		return nil, err
	}
	now := s.now()
	cred := &SourceCredential{
		ID:                   uuid.New(),
		ProjectID:            projectID,
		Provider:             providerType,
		Name:                 name,
		EncryptedCredentials: blob,
		CreatedAt:            now,
		UpdatedAt:            now,
	}
	created, err := s.repo.CreateCredential(ctx, cred)
	if err != nil {
		return nil, err
	}
	return toSavedCredential(created, 0), nil
}

// UpdateSavedCredential renames a saved credential and/or replaces its secret.
// An empty credentials map keeps the stored secret.
func (s *Service) UpdateSavedCredential(ctx context.Context, projectID, id uuid.UUID, name *string, creds map[string]any) (*SavedCredential, error) {
	cred, err := s.repo.GetCredentialByID(ctx, id, projectID)
	if err != nil {
		return nil, err
	}
	if name != nil {
		trimmed := strings.TrimSpace(*name)
		if trimmed == "" {
			return nil, apperror.NewValidation("name is required")
		}
		cred.Name = trimmed
	}
	if len(creds) > 0 {
		p, err := s.savedCredentialProvider(cred.Provider)
		if err != nil {
			return nil, err
		}
		extracted, err := s.extractSavedCredentials(p, creds)
		if err != nil {
			return nil, err
		}
		blob, err := s.encryptCredentials(extracted)
		if err != nil {
			return nil, err
		}
		cred.EncryptedCredentials = blob
	}
	// len(creds) > 0 means the secret was replaced, not just the name: sealing uses a
	// random nonce, so even a re-submitted identical token produces new bytes and a
	// new grant identity to the sources that read it.
	updated, err := s.repo.UpdateCredential(ctx, cred, len(creds) > 0)
	if err != nil {
		return nil, err
	}
	count, err := s.repo.CountSourcesUsingCredential(ctx, projectID, updated.ID)
	if err != nil {
		return nil, err
	}
	return toSavedCredential(updated, count), nil
}

// DeleteSavedCredential removes an unreferenced saved credential. A credential
// still referenced by a data source is a 409 conflict.
func (s *Service) DeleteSavedCredential(ctx context.Context, projectID, id uuid.UUID) error {
	if _, err := s.repo.GetCredentialByID(ctx, id, projectID); err != nil {
		return err
	}
	count, err := s.repo.CountSourcesUsingCredential(ctx, projectID, id)
	if err != nil {
		return err
	}
	if count > 0 {
		return apperror.NewConflict(fmt.Sprintf("credential is in use by %d data source(s)", count))
	}
	return s.repo.DeleteCredential(ctx, id, projectID)
}

// credentialsForSource resolves the credentials a source uses at runtime: the
// referenced saved credential when set, otherwise its legacy inline blob.
func (s *Service) credentialsForSource(ctx context.Context, src *Source) (Credentials, error) {
	if src == nil {
		return nil, apperror.NewValidation("source is required")
	}
	if src.CredentialID != nil {
		cred, err := s.repo.GetCredentialByID(ctx, *src.CredentialID, src.ProjectID)
		if err != nil {
			return nil, err
		}
		if cred.Provider != src.Provider {
			return nil, apperror.NewValidation(fmt.Sprintf("saved credential provider %q does not match source provider %q", cred.Provider, src.Provider))
		}
		return s.decryptCredentials(cred.EncryptedCredentials)
	}
	return s.decryptCredentials(src.EncryptedCredentials)
}

// credentialsForRequest resolves credentials for a request that may carry a
// saved credential id instead of inline credential keys in its config.
func (s *Service) credentialsForRequest(ctx context.Context, projectID uuid.UUID, p SourceProvider, cfg map[string]any, savedID *string) (Credentials, error) {
	if savedID != nil && strings.TrimSpace(*savedID) != "" {
		id, err := parseCredentialID(*savedID)
		if err != nil {
			return nil, err
		}
		return s.credentialsFromSavedCredential(ctx, projectID, p.Catalog().Type, id)
	}
	return p.ExtractCredentials(cfg)
}

// credentialsFromSavedCredential loads and decrypts a saved credential,
// enforcing project scope and a matching provider.
func (s *Service) credentialsFromSavedCredential(ctx context.Context, projectID uuid.UUID, providerType string, id uuid.UUID) (Credentials, error) {
	cred, err := s.repo.GetCredentialByID(ctx, id, projectID)
	if err != nil {
		return nil, err
	}
	if cred.Provider != providerType {
		return nil, apperror.NewValidation(fmt.Sprintf("saved credential provider %q does not match source provider %q", cred.Provider, providerType))
	}
	return s.decryptCredentials(cred.EncryptedCredentials)
}

// savedCredentialProvider resolves a provider and rejects one that cannot use
// reusable credentials (no credentials auth method or no credential fields).
func (s *Service) savedCredentialProvider(providerType string) (SourceProvider, error) {
	p, ok := s.providers.Get(providerType)
	if !ok {
		return nil, apperror.NewValidation("unsupported providerType: " + providerType)
	}
	if !slices.Contains(p.AuthMethods(), AuthMethodCredentials) || len(p.CredentialFields()) == 0 {
		return nil, apperror.NewValidation("provider " + providerType + " does not support saved credentials")
	}
	return p, nil
}

// extractSavedCredentials keeps only the provider's declared credential keys,
// then validates and returns them. Unknown keys are ignored.
func (s *Service) extractSavedCredentials(p SourceProvider, creds map[string]any) (Credentials, error) {
	filtered := make(Credentials, len(creds))
	for _, key := range p.CredentialKeys() {
		if v, ok := creds[key]; ok {
			filtered[key] = v
		}
	}
	return p.ExtractCredentials(filtered)
}

func parseCredentialID(raw string) (uuid.UUID, error) {
	id, err := uuid.Parse(strings.TrimSpace(raw))
	if err != nil {
		return uuid.Nil, apperror.NewValidation("credentialId must be a UUID")
	}
	return id, nil
}

func toSavedCredential(cred *SourceCredential, usedBy int) *SavedCredential {
	return &SavedCredential{
		ID:           cred.ID.String(),
		ProviderType: cred.Provider,
		Name:         cred.Name,
		UsedByCount:  usedBy,
		CreatedAt:    cred.CreatedAt,
		UpdatedAt:    cred.UpdatedAt,
	}
}
