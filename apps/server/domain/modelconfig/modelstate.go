package modelconfig

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/emergent-company/emergent.memory/domain/modelstate"
	"github.com/emergent-company/emergent.memory/domain/provider"
)

// ModelStateReader adapts modelconfig plus project provider credentials to
// modelstate.Reader, for read models that must state which part of a model
// configuration is missing.
//
// It reads metadata only: the effective model comes from the same resolution
// chain a run uses, and provider presence is a row count. No credential is
// decrypted and no provider is called.
type ModelStateReader struct {
	models *Service
	creds  *provider.CredentialService
}

// NewModelStateReader creates the adapter. creds may be nil in tests, in which
// case provider presence is reported as unknown (0) and the copy degrades to
// the unverified fallback rather than guessing.
func NewModelStateReader(models *Service, creds *provider.CredentialService) *ModelStateReader {
	return &ModelStateReader{models: models, creds: creds}
}

// State resolves the project's effective generative and embedding models plus
// the number of provider instances configured for it. Both families are
// resolved because they are separate chains: a project can have a generative
// model and no embedding model, and copy that confused the two would repeat the
// mistake this adapter exists to remove.
//
// A failure to count providers is reported as an error so the caller can fall
// back to neutral copy: claiming "no provider is connected" because a metadata
// read failed would repeat the guess this exists to remove.
func (r *ModelStateReader) State(ctx context.Context, projectID string) (modelstate.State, error) {
	id, err := uuid.Parse(projectID)
	if err != nil {
		return modelstate.State{}, fmt.Errorf("model state: parse project id: %w", err)
	}

	effective, err := r.models.ResolveEffectiveModels(ctx, id)
	if err != nil {
		return modelstate.State{}, fmt.Errorf("model state: resolve models: %w", err)
	}

	state := modelstate.State{
		GenerativeModel:  effective.GenerativeModel,
		GenerativeSource: string(effective.GenerativeModelSource),
		EmbeddingModel:   effective.EmbeddingModel,
		EmbeddingSource:  string(effective.EmbeddingModelSource),
	}
	if state.GenerativeSource == "" {
		state.GenerativeSource = modelstate.SourceNone
	}
	if state.EmbeddingSource == "" {
		state.EmbeddingSource = modelstate.SourceNone
	}

	if r.creds != nil {
		configs, cerr := r.creds.ListProjectConfigs(ctx, projectID)
		if cerr != nil {
			return state, fmt.Errorf("model state: list project providers: %w", cerr)
		}
		state.ProviderCount = len(configs)
	}

	return state, nil
}
