package blueprints

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
)

// LocalBlueprint is a blueprint descriptor loaded from a local directory (an
// extracted GitHub archive, a marketplace canonical bundle, or an operator's
// directory).
type LocalBlueprint struct {
	Name        string
	Version     string
	Description string
	Author      string
	Manifest    BlueprintManifest
}

// LoadLocalBlueprint loads and validates the blueprint descriptor found at root
// using the same manifest/layout loader as the GitHub import. It creates
// nothing.
func LoadLocalBlueprint(root string) (*LocalBlueprint, error) {
	info, err := os.Stat(root)
	if err != nil {
		return nil, apperror.NewBadRequest("blueprint directory is not readable: " + err.Error())
	}
	if !info.IsDir() {
		return nil, apperror.NewBadRequest("blueprint path is not a directory")
	}
	fallback := filepath.Base(filepath.Clean(root))
	desc, err := loadBlueprintDir(os.DirFS(root), ".", fallback)
	if err != nil {
		return nil, err
	}
	return &LocalBlueprint{
		Name:        desc.Name,
		Version:     desc.Version,
		Description: desc.Description,
		Author:      desc.Author,
		Manifest:    desc.BlueprintManifest,
	}, nil
}

// LocalImportRequest imports a blueprint from a directory on the local
// filesystem. It shares the GitHub import pipeline (create-or-adopt → publish →
// apply); Origin and SkillProvenance are server-set by internal callers such as
// the marketplace install path.
type LocalImportRequest struct {
	Root               string
	Apply              bool
	ModelOverrides     map[string]string
	MergeTypeConflicts bool
	// Origin is an internal provenance value ("import" or "market"). Empty
	// defaults to "import".
	Origin string
	// SourceKind/SourceURL/SourceRef are stored on the blueprint. For a
	// marketplace install these record the listing origin.
	SourceKind string
	SourceURL  string
	SourceRef  string
	// SkillProvenance stamps provenance on skills created by the apply.
	SkillProvenance *SkillProvenance
}

// ImportFromLocalDir loads a blueprint from req.Root and runs the shared
// create/publish/apply pipeline. projectID scopes the created blueprint.
func (s *Service) ImportFromLocalDir(ctx context.Context, projectID string, req *LocalImportRequest) (*Blueprint, error) {
	if req == nil || req.Root == "" {
		return nil, apperror.NewBadRequest("blueprint directory is required")
	}
	local, err := LoadLocalBlueprint(req.Root)
	if err != nil {
		return nil, err
	}
	manifest, err := json.Marshal(local.Manifest)
	if err != nil {
		return nil, apperror.NewBadRequest("blueprint manifest is not serializable")
	}
	desc := &blueprintDescriptor{
		Name:              local.Name,
		Version:           local.Version,
		Description:       local.Description,
		Author:            local.Author,
		BlueprintManifest: local.Manifest,
	}
	origin := req.Origin
	if origin == "" {
		origin = OriginImport
	}
	sourceKind := req.SourceKind
	if sourceKind == "" {
		sourceKind = "local"
	}
	return s.importLoaded(ctx, projectID, importPayload{
		desc:           desc,
		manifest:       manifest,
		sourceManifest: manifest,
		sourceKind:     sourceKind,
		sourceURL:      req.SourceURL,
		sourceRef:      req.SourceRef,
		origin:         origin,
	}, importOptions{
		apply:              req.Apply,
		modelOverrides:     req.ModelOverrides,
		mergeTypeConflicts: req.MergeTypeConflicts,
		skillProvenance:    req.SkillProvenance,
	})
}

// MarketSourceURL is the canonical provenance URL recorded for a marketplace
// listing.
func MarketSourceURL(issuer, slug string) string {
	return fmt.Sprintf("%s/listings/%s", issuer, slug)
}
