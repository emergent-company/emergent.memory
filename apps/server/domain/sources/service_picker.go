package sources

import (
	"context"
	"slices"
	"strings"

	"github.com/google/uuid"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
)

// Picker session config keys stored in the resolved OAuth client's non-secret
// Config map. They are set from Settings → Credentials and are required for the
// Google Picker to load.
const (
	pickerDeveloperKeyConfigKey = "pickerDeveloperKey"
	pickerAppIDConfigKey        = "pickerAppId"
)

// PickerSession resolves a short-lived resource-picker credential for a source.
// The access token is minted from the source's own stored refresh token, so the
// browser picker browses exactly the account the source syncs. The picker's
// developer key and app id come from the resolved OAuth client's non-secret
// config (project → organization → deployment).
func (s *Service) PickerSession(ctx context.Context, projectID, id uuid.UUID) (*PickerSession, error) {
	src, err := s.repo.GetByIDWithCredentials(ctx, id, projectID)
	if err != nil {
		return nil, err
	}
	p, ok := s.providers.Get(src.Provider)
	if !ok {
		return nil, apperror.NewValidation("unsupported providerType: " + src.Provider)
	}
	picker, ok := p.(ResourcePicker)
	if !ok {
		return nil, apperror.NewValidation("provider " + src.Provider + " has no folder picker")
	}
	client, err := s.resolveOAuthClient(ctx, projectID, src.Provider)
	if err != nil {
		return nil, err
	}
	developerKey := strings.TrimSpace(configString(client.Config, pickerDeveloperKeyConfigKey))
	appID := strings.TrimSpace(configString(client.Config, pickerAppIDConfigKey))
	if developerKey == "" || appID == "" {
		return nil, apperror.NewValidation("the Google Picker is not configured: set the Picker developer key and Picker app ID under Settings → Credentials")
	}
	creds, err := s.credentialsForSource(ctx, src)
	if err != nil {
		return nil, err
	}
	// The granted strategy wins over the configured one: the token was issued for
	// a fixed scope at connect, so a drive.file source stays unusable for folder
	// browsing even after the organization's strategy changes, and a readonly
	// grandfathered source stays usable after a switch to drive.file. Fall back to
	// the configured strategy for sources connected before the grant was stored.
	strategy := configString(src.Config, oauthGrantedStrategyKey)
	if strategy == "" {
		strategy = s.effectiveOAuthStrategy(ctx, projectID, src.Provider)
	}
	session, err := picker.PickerSession(ctx, creds, src.Config, PickerConfig{
		DeveloperKey: developerKey,
		AppID:        appID,
		Origin:       s.oauthJavascriptOrigin(),
		Strategy:     strategy,
	})
	if err != nil {
		return nil, err
	}
	return session, nil
}

// SaveSyncTargets replaces a source's provider resource targets with the given
// ids. The ids are resolved against the source's own credentials first, so the
// persisted names and drive ids are authoritative and a client cannot name a
// folder it cannot see. It backs the folder-picker save.
func (s *Service) SaveSyncTargets(ctx context.Context, projectID, id uuid.UUID, ids []string) (*Integration, error) {
	src, err := s.repo.GetByIDWithCredentials(ctx, id, projectID)
	if err != nil {
		return nil, err
	}
	p, ok := s.providers.Get(src.Provider)
	if !ok {
		return nil, apperror.NewValidation("unsupported providerType: " + src.Provider)
	}
	resolver, ok := p.(ResourceTargetResolver)
	if !ok {
		return nil, apperror.NewValidation("provider " + src.Provider + " has no resource target resolver")
	}
	unique := make([]string, 0, len(ids))
	for _, raw := range ids {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" || slices.Contains(unique, trimmed) {
			continue
		}
		unique = append(unique, trimmed)
	}
	if len(unique) == 0 {
		return nil, apperror.NewValidation("select at least one folder")
	}
	creds, err := s.credentialsForSource(ctx, src)
	if err != nil {
		return nil, err
	}
	refs, err := resolver.ResolveResourceTargets(ctx, creds, unique)
	if err != nil {
		return nil, err
	}
	cfg := cloneConfig(src.Config)
	entries := make([]any, 0, len(refs))
	for _, ref := range refs {
		entry := map[string]any{"folder_id": ref.ID, "name": ref.Name}
		if driveID := strings.TrimSpace(ref.DriveID); driveID != "" {
			entry["drive_id"] = driveID
		}
		entries = append(entries, entry)
	}
	cfg["folders"] = entries
	// The canonical list supersedes the legacy single-target keys.
	delete(cfg, "folder_id")
	delete(cfg, "shared_drive_id")
	return s.Update(ctx, projectID, id, &UpdateIntegrationRequest{Config: cfg})
}

// configString reads a trimmed string from a non-secret config map.
func configString(cfg map[string]any, key string) string {
	if cfg == nil {
		return ""
	}
	v, _ := cfg[key].(string)
	return strings.TrimSpace(v)
}
