package workspaceimages

import (
	"context"

	"github.com/emergent-company/emergent/domain/workspace"
)

// imageResolverAdapter wraps Service to implement workspace.ImageResolver.
type imageResolverAdapter struct {
	svc *Service
}

// ResolveImage looks up an image name in the catalog and returns
// the resolved provider + docker ref for provisioning.
func (a *imageResolverAdapter) ResolveImage(ctx context.Context, projectID, imageName string) (*workspace.ResolvedImage, error) {
	img, err := a.svc.Resolve(ctx, projectID, imageName)
	if err != nil {
		return nil, err
	}

	resolved := &workspace.ResolvedImage{
		Name:     img.Name,
		Provider: workspace.ProviderType(img.Provider),
	}
	if img.DockerRef != nil {
		resolved.DockerRef = *img.DockerRef
	}
	return resolved, nil
}

// AsImageResolver returns an adapter that implements workspace.ImageResolver.
func (s *Service) AsImageResolver() workspace.ImageResolver {
	return &imageResolverAdapter{svc: s}
}
