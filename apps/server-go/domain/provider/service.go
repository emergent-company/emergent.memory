package provider

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/emergent-company/emergent/internal/config"
	"github.com/emergent-company/emergent/pkg/auth"
	"github.com/emergent-company/emergent/pkg/crypto"
	"github.com/emergent-company/emergent/pkg/logger"
)

// ResolvedCredential holds the decrypted credential material and metadata
// needed to instantiate an LLM client for a specific request context.
type ResolvedCredential struct {
	// Provider type (google-ai or vertex-ai)
	Provider ProviderType

	// Source describes where the credential was resolved from
	Source CredentialSource

	// APIKey is set for google-ai (decrypted)
	APIKey string

	// Vertex AI fields (set for vertex-ai)
	ServiceAccountJSON string
	GCPProject         string
	Location           string

	// Selected models (may come from org selection, project override, or env config)
	EmbeddingModel  string
	GenerativeModel string
}

// CredentialSource describes where a resolved credential originated.
type CredentialSource string

const (
	SourceProject      CredentialSource = "project"
	SourceOrganization CredentialSource = "organization"
	SourceEnvironment  CredentialSource = "environment"
)

// CredentialService resolves LLM credentials following the hierarchy:
// Project Override → Organization → Environment fallback.
type CredentialService struct {
	repo      *Repository
	registry  *Registry
	encryptor *crypto.Encryptor // nil if encryption key not configured
	cfg       *config.Config
	log       *slog.Logger
}

// NewCredentialService creates a new CredentialService.
// The encryptor may be nil if LLM_ENCRYPTION_KEY is not configured (env-only mode).
func NewCredentialService(
	repo *Repository,
	registry *Registry,
	cfg *config.Config,
	log *slog.Logger,
) *CredentialService {
	s := &CredentialService{
		repo:     repo,
		registry: registry,
		cfg:      cfg,
		log:      log.With(logger.Scope("provider.credential")),
	}

	// Initialize encryptor if key is configured
	if cfg.LLMProvider.IsEncryptionConfigured() {
		enc, err := crypto.NewEncryptor(cfg.LLMProvider.EncryptionKey)
		if err != nil {
			s.log.Error("failed to initialize credential encryptor", logger.Error(err))
		} else {
			s.encryptor = enc
		}
	}

	return s
}

// Resolve determines the effective credentials for the given provider by
// evaluating the context (project ID, org ID) against the resolution hierarchy.
//
// Resolution order:
//  1. If the project has policy=project with overridden credentials → use those
//  2. If the project has policy=organization (or no policy) and the org has credentials → use org creds
//  3. Fall back to server environment variables
func (s *CredentialService) Resolve(ctx context.Context, provider ProviderType) (*ResolvedCredential, error) {
	if !s.registry.IsSupported(provider) {
		return nil, fmt.Errorf("unsupported provider: %s", provider)
	}

	projectID := auth.ProjectIDFromContext(ctx)
	orgID := auth.OrgIDFromContext(ctx)

	// If we have a project ID, try project-level resolution first
	if projectID != "" {
		resolved, err := s.resolveForProject(ctx, projectID, orgID, provider)
		if err != nil {
			return nil, err
		}
		if resolved != nil {
			return resolved, nil
		}
	}

	// If we have an org ID (but no project override), try org-level
	if orgID != "" {
		resolved, err := s.resolveForOrg(ctx, orgID, provider)
		if err != nil {
			return nil, err
		}
		if resolved != nil {
			return resolved, nil
		}
	}

	// Fall back to environment
	return s.resolveFromEnv(provider)
}

// resolveForProject checks if the project has a policy override for this provider.
func (s *CredentialService) resolveForProject(ctx context.Context, projectID, orgID string, provider ProviderType) (*ResolvedCredential, error) {
	policy, err := s.repo.GetProjectPolicy(ctx, projectID, provider)
	if err != nil {
		return nil, fmt.Errorf("failed to get project policy: %w", err)
	}

	if policy == nil {
		// No explicit policy configured — default to organization credentials.
		// This ensures projects without a policy row inherit org-level creds
		// rather than silently falling through to server env vars.
		if orgID == "" {
			resolvedOrgID, err := s.repo.GetOrgIDForProject(ctx, projectID)
			if err != nil {
				// Non-fatal: log and fall through so Resolve() can try orgID from context
				s.log.Warn("failed to resolve org for project (no policy row)",
					slog.String("projectID", projectID),
					logger.Error(err),
				)
				return nil, nil
			}
			orgID = resolvedOrgID
		}
		return s.resolveForOrg(ctx, orgID, provider)
	}

	switch policy.Policy {
	case PolicyProject:
		// Project has its own credentials
		if policy.EncryptedCredential == nil {
			s.log.Warn("project policy is 'project' but no credential stored",
				slog.String("projectID", projectID),
				slog.String("provider", string(provider)),
			)
			return nil, nil
		}
		return s.decryptProjectCredential(policy)

	case PolicyOrganization:
		// Explicitly inheriting from org
		if orgID == "" {
			// Try to resolve org ID from the project
			resolvedOrgID, err := s.repo.GetOrgIDForProject(ctx, projectID)
			if err != nil {
				return nil, fmt.Errorf("failed to resolve org for project %s: %w", projectID, err)
			}
			orgID = resolvedOrgID
		}
		return s.resolveForOrg(ctx, orgID, provider)

	case PolicyNone:
		// Explicitly opting out of org credentials → fall to env
		return s.resolveFromEnv(provider)

	default:
		return nil, nil
	}
}

// resolveForOrg decrypts and returns the organization's stored credentials.
func (s *CredentialService) resolveForOrg(ctx context.Context, orgID string, provider ProviderType) (*ResolvedCredential, error) {
	cred, err := s.repo.GetOrgCredential(ctx, orgID, provider)
	if err != nil {
		return nil, fmt.Errorf("failed to get org credential: %w", err)
	}
	if cred == nil {
		return nil, nil // no org credential, fall through
	}

	resolved, err := s.decryptOrgCredential(cred)
	if err != nil {
		return nil, err
	}

	// Load org model selections
	sel, err := s.repo.GetOrgModelSelection(ctx, orgID, provider)
	if err != nil {
		s.log.Warn("failed to get org model selection, using defaults",
			logger.Error(err),
			slog.String("orgID", orgID),
		)
	}
	if sel != nil {
		resolved.EmbeddingModel = sel.EmbeddingModel
		resolved.GenerativeModel = sel.GenerativeModel
	}

	return resolved, nil
}

// resolveFromEnv constructs credentials from server environment variables.
func (s *CredentialService) resolveFromEnv(provider ProviderType) (*ResolvedCredential, error) {
	switch provider {
	case ProviderGoogleAI:
		if s.cfg.LLM.GoogleAPIKey == "" {
			return nil, fmt.Errorf("no Google AI API key configured: set GOOGLE_API_KEY or configure organization credentials")
		}
		return &ResolvedCredential{
			Provider:        ProviderGoogleAI,
			Source:          SourceEnvironment,
			APIKey:          s.cfg.LLM.GoogleAPIKey,
			GenerativeModel: s.cfg.LLM.Model,
			EmbeddingModel:  s.cfg.Embeddings.Model,
		}, nil

	case ProviderVertexAI:
		if !s.cfg.LLM.UseVertexAI() {
			return nil, fmt.Errorf("no Vertex AI credentials configured: set GCP_PROJECT_ID+VERTEX_AI_LOCATION or configure organization credentials")
		}
		return &ResolvedCredential{
			Provider:        ProviderVertexAI,
			Source:          SourceEnvironment,
			GCPProject:      s.cfg.LLM.GCPProjectID,
			Location:        s.cfg.LLM.VertexAILocation,
			GenerativeModel: s.cfg.LLM.Model,
			EmbeddingModel:  s.cfg.Embeddings.Model,
		}, nil

	default:
		return nil, fmt.Errorf("no environment fallback for provider: %s", provider)
	}
}

// decryptOrgCredential decrypts an organization-level credential.
func (s *CredentialService) decryptOrgCredential(cred *OrganizationProviderCredential) (*ResolvedCredential, error) {
	if s.encryptor == nil {
		return nil, fmt.Errorf("credential encryption not configured (LLM_ENCRYPTION_KEY missing)")
	}

	plaintext, err := s.encryptor.Decrypt(cred.EncryptedCredential, cred.EncryptionNonce)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt credential: %w", err)
	}

	resolved := &ResolvedCredential{
		Provider: cred.Provider,
		Source:   SourceOrganization,
	}

	switch cred.Provider {
	case ProviderGoogleAI:
		resolved.APIKey = string(plaintext)
	case ProviderVertexAI:
		resolved.ServiceAccountJSON = string(plaintext)
		resolved.GCPProject = cred.GCPProject
		resolved.Location = cred.Location
	}

	return resolved, nil
}

// decryptProjectCredential decrypts a project-level credential override.
func (s *CredentialService) decryptProjectCredential(policy *ProjectProviderPolicy) (*ResolvedCredential, error) {
	if s.encryptor == nil {
		return nil, fmt.Errorf("credential encryption not configured (LLM_ENCRYPTION_KEY missing)")
	}

	plaintext, err := s.encryptor.Decrypt(policy.EncryptedCredential, policy.EncryptionNonce)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt project credential: %w", err)
	}

	resolved := &ResolvedCredential{
		Provider:        policy.Provider,
		Source:          SourceProject,
		EmbeddingModel:  policy.EmbeddingModel,
		GenerativeModel: policy.GenerativeModel,
	}

	switch policy.Provider {
	case ProviderGoogleAI:
		resolved.APIKey = string(plaintext)
	case ProviderVertexAI:
		resolved.ServiceAccountJSON = string(plaintext)
		resolved.GCPProject = policy.GCPProject
		resolved.Location = policy.Location
	}

	return resolved, nil
}

// EncryptCredential encrypts a plaintext credential for storage.
// Returns the ciphertext and nonce.
func (s *CredentialService) EncryptCredential(plaintext []byte) (ciphertext, nonce []byte, err error) {
	if s.encryptor == nil {
		return nil, nil, fmt.Errorf("credential encryption not configured (LLM_ENCRYPTION_KEY missing)")
	}
	return s.encryptor.Encrypt(plaintext)
}

// ResolveAny attempts to resolve the best available credential for the request
// context without requiring the caller to specify a provider type.
//
// Resolution order:
//  1. If the context has a project or org, try Vertex AI first (production), then Google AI.
//  2. Fall back to environment variables (same preference order).
//
// Returns nil, nil when no credentials are available (not an error — callers
// should fall back to their own env-based logic).
//
// This method satisfies the adk.CredentialResolver interface.
func (s *CredentialService) ResolveAny(ctx context.Context) (*ResolvedCredential, error) {
	// Try providers in priority order: Vertex AI first (preferred for production),
	// then Google AI (common for development / self-hosted).
	for _, provider := range []ProviderType{ProviderVertexAI, ProviderGoogleAI} {
		cred, err := s.Resolve(ctx, provider)
		if err != nil {
			s.log.Debug("provider resolution failed, trying next",
				slog.String("provider", string(provider)),
				slog.String("error", err.Error()),
			)
			continue
		}
		if cred != nil {
			return cred, nil
		}
	}
	// No credentials found via DB hierarchy or env — caller falls back to own logic.
	return nil, nil
}
