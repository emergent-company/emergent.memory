package modelconfig

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/emergent-company/emergent.memory/pkg/adk"
)

// ADKModelResolverAdapter adapts modelconfig.Service to adk.ModelResolver.
// This breaks the import cycle: pkg/adk cannot import domain/modelconfig,
// so domain/modelconfig provides this adapter and registers it via fx.
type ADKModelResolverAdapter struct {
	svc *Service
}

// NewADKModelResolverAdapter creates a new adapter.
func NewADKModelResolverAdapter(svc *Service) adk.ModelResolver {
	return &ADKModelResolverAdapter{svc: svc}
}

// ResolveGenerativeModelByID implements adk.ModelResolver.
func (a *ADKModelResolverAdapter) ResolveGenerativeModelByID(ctx context.Context, projectIDStr string) (string, string, error) {
	projectID, err := uuid.Parse(projectIDStr)
	if err != nil {
		return "", "", fmt.Errorf("modelconfig adapter: invalid project id %q: %w", projectIDStr, err)
	}
	model, source, err := a.svc.ResolveGenerativeModel(ctx, projectID)
	if err != nil {
		return "", "", err
	}
	return model, string(source), nil
}
