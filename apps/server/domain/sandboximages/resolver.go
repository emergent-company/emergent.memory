package sandboximages

import (
	"context"

	"github.com/emergent-company/emergent.memory/domain/sandbox"
)

// imageResolverAdapter wraps Service to implement sandbox.ImageResolver.
type imageResolverAdapter struct {
	svc *Service
}

// ResolveImage looks up an image name in the catalog and returns
// the resolved provider + docker ref for provisioning.
func (a *imageResolverAdapter) ResolveImage(ctx context.Context, projectID, imageName string) (*sandbox.ResolvedImage, error) {
	img, err := a.svc.Resolve(ctx, projectID, imageName)
	if err != nil {
		return nil, err
	}

	resolved := &sandbox.ResolvedImage{
		Name:     img.Name,
		Provider: sandbox.ProviderType(img.Provider),
	}
	if img.DockerRef != nil {
		resolved.DockerRef = *img.DockerRef
	}
	return resolved, nil
}

// GetImageStatus returns the current status string of an image in the catalog.
// Returns ("", nil) if the image is not found.
func (a *imageResolverAdapter) GetImageStatus(ctx context.Context, projectID, imageName string) (string, error) {
	img, err := a.svc.store.GetByName(ctx, projectID, imageName)
	if err != nil {
		return "", err
	}
	if img == nil {
		return "", nil
	}
	return string(img.Status), nil
}

// AsImageResolver returns an adapter that implements sandbox.ImageResolver.
func (s *Service) AsImageResolver() sandbox.ImageResolver {
	return &imageResolverAdapter{svc: s}
}
