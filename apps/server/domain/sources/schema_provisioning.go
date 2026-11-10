package sources

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"

	"github.com/emergent-company/emergent.memory/domain/schemas"
)

// ObjectTypeProvisioner installs a provider-declared object-type pack into a
// project. It is nil-safe at the Service: unit tests that do not exercise typed
// sources leave it unset.
type ObjectTypeProvisioner interface {
	EnsureObjectTypes(ctx context.Context, projectID string, pack *ObjectTypePack) error
}

type schemaProvisioner struct {
	svc *schemas.Service
}

// NewSchemaProvisioner adapts the schemas service to the sources domain.
func NewSchemaProvisioner(svc *schemas.Service) ObjectTypeProvisioner {
	return &schemaProvisioner{svc: svc}
}

func (p *schemaProvisioner) EnsureObjectTypes(ctx context.Context, projectID string, pack *ObjectTypePack) error {
	if p == nil || p.svc == nil || pack == nil {
		return nil
	}

	objectTypes := make([]map[string]any, 0, len(pack.ObjectTypes))
	for _, def := range pack.ObjectTypes {
		entry := map[string]any{
			"name":        def.Name,
			"label":       def.Label,
			"description": def.Description,
			"properties":  def.Properties,
		}
		if len(def.Required) > 0 {
			entry["required"] = def.Required
		}
		if def.SkipExtraction {
			entry["skipExtraction"] = true
		}
		objectTypes = append(objectTypes, entry)
	}
	relationships := make([]map[string]any, 0, len(pack.RelationshipTypes))
	for _, def := range pack.RelationshipTypes {
		relationships = append(relationships, map[string]any{
			"name":        def.Name,
			"sourceType":  def.SourceType,
			"targetType":  def.TargetType,
			"description": def.Description,
		})
	}
	objJSON, err := json.Marshal(objectTypes)
	if err != nil {
		return err
	}
	relJSON, err := json.Marshal(relationships)
	if err != nil {
		return err
	}
	req := &schemas.CreatePackRequest{
		Name:                         pack.Name,
		Version:                      pack.Version,
		Description:                  &pack.Description,
		Author:                       stringPtr("Memory"),
		ObjectTypeSchemasSnake:       objJSON,
		RelationshipTypeSchemasSnake: relJSON,
	}
	schema, err := p.svc.EnsureProjectPack(ctx, projectID, req)
	if err != nil {
		return err
	}
	_, err = p.svc.AssignPack(ctx, projectID, "", &schemas.AssignPackRequest{
		SchemaID: schema.ID,
		Merge:    true,
	})
	return err
}

// ensureObjectTypes installs a typed provider's declared pack when the
// provisioner is configured. It is idempotent.
func (s *Service) ensureObjectTypes(ctx context.Context, projectID uuid.UUID, p SourceProvider) error {
	if s.objectTypes == nil {
		return nil
	}
	typed, ok := p.(TypedObjectProvider)
	if !ok {
		return nil
	}
	pack := typed.ObjectTypes()
	if pack == nil {
		return nil
	}
	return s.objectTypes.EnsureObjectTypes(ctx, projectID.String(), pack)
}
