package mcpregistry

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/mcpconfig"
)

// importTargetAuto resolves each entry's delivery mode from its transport:
// remote servers run on the shared Memory host; local stdio commands are pushed
// to a connected connector device, which is the only place they can run.
const importTargetAuto MCPServerTarget = "auto"

// ImportServers creates or updates MCP servers from a Claude-format config
// document. The import is per-entry tolerant: a malformed entry, a rejected
// URL, an unauthorized device target, or a name conflict never aborts the rest.
// With DryRun nothing is written; the result is the plan.
func (s *Service) ImportServers(ctx context.Context, projectID string, dto *ImportMCPServersDTO) (*ImportMCPServersResultDTO, error) {
	if dto == nil {
		return nil, fmt.Errorf("import request is required")
	}
	if len(dto.Config) == 0 {
		return nil, fmt.Errorf("config is required")
	}
	target := dto.Target
	if target == "" {
		target = importTargetAuto
	}
	if target != importTargetAuto && !ValidTarget(target) {
		return nil, fmt.Errorf("invalid target %q: must be auto, server, or device", dto.Target)
	}

	parsed, err := mcpconfig.ParseClaudeConfig(dto.Config)
	if err != nil {
		return nil, err
	}

	result := &ImportMCPServersResultDTO{
		Created:  []string{},
		Updated:  []string{},
		Skipped:  []ImportSkipDTO{},
		Failed:   []ImportFailureDTO{},
		Warnings: []ImportWarningDTO{},
		DryRun:   dto.DryRun,
	}
	for _, entryErr := range parsed.Errors {
		result.Failed = append(result.Failed, ImportFailureDTO{Name: entryErr.Name, Error: entryErr.Err.Error()})
	}
	for _, warning := range parsed.Warnings {
		result.Warnings = append(result.Warnings, ImportWarningDTO{Name: warning.Name, Message: warning.Message})
	}

	for _, entry := range parsed.Servers {
		createDTO, err := buildImportCreateDTO(entry, target, dto.TargetInstanceID, s.encryptor != nil)
		if err != nil {
			result.Failed = append(result.Failed, ImportFailureDTO{Name: entry.Name, Error: err.Error()})
			continue
		}

		existing, err := s.repo.FindServerByName(ctx, projectID, entry.Name)
		if err != nil {
			result.Failed = append(result.Failed, ImportFailureDTO{Name: entry.Name, Error: fmt.Sprintf("checking existing server: %v", err)})
			continue
		}

		if existing != nil {
			if existing.Type == ServerTypeBuiltin {
				result.Skipped = append(result.Skipped, ImportSkipDTO{Name: entry.Name, Reason: "builtin server cannot be overwritten"})
				continue
			}
			if !dto.Overwrite {
				result.Skipped = append(result.Skipped, ImportSkipDTO{Name: entry.Name, Reason: "already exists"})
				continue
			}
			updateDTO := updateDTOFromImport(createDTO)
			if dto.DryRun {
				if err := s.validateImportUpdate(ctx, projectID, existing, updateDTO); err != nil {
					result.Failed = append(result.Failed, ImportFailureDTO{Name: entry.Name, Error: err.Error()})
					continue
				}
				result.Updated = append(result.Updated, entry.Name)
				continue
			}
			if _, err := s.UpdateServer(ctx, existing.ID, projectID, updateDTO); err != nil {
				result.Failed = append(result.Failed, ImportFailureDTO{Name: entry.Name, Error: err.Error()})
				continue
			}
			result.Updated = append(result.Updated, entry.Name)
			continue
		}

		if dto.DryRun {
			if err := s.validateImportCreate(ctx, projectID, createDTO); err != nil {
				result.Failed = append(result.Failed, ImportFailureDTO{Name: entry.Name, Error: err.Error()})
				continue
			}
			result.Created = append(result.Created, entry.Name)
			continue
		}
		if _, err := s.CreateServer(ctx, projectID, createDTO); err != nil {
			result.Failed = append(result.Failed, ImportFailureDTO{Name: entry.Name, Error: err.Error()})
			continue
		}
		result.Created = append(result.Created, entry.Name)
	}

	return result, nil
}

// buildImportCreateDTO maps one parsed Claude entry onto the create DTO.
// markSecrets is false when credential encryption is not configured: the
// values are then stored in the plaintext maps rather than failing the import.
func buildImportCreateDTO(entry mcpconfig.Server, target MCPServerTarget, instanceID *string, markSecrets bool) (*CreateMCPServerDTO, error) {
	resolved := resolveImportTarget(target, entry.Transport)
	dto := &CreateMCPServerDTO{
		Name:   entry.Name,
		Type:   MCPServerType(entry.Transport),
		Target: resolved,
	}
	if resolved == TargetDevice {
		dto.TargetInstanceID = instanceID
	}

	switch entry.Transport {
	case mcpconfig.TransportStdio:
		command := entry.Command
		dto.Command = &command
		dto.Args = append([]string{}, entry.Args...)
		dto.Env = toStringAnyMap(entry.Env)
		if markSecrets {
			dto.SecretEnvKeys = secretEnvKeys(entry.Env)
		}
	case mcpconfig.TransportHTTP, mcpconfig.TransportSSE:
		url := entry.URL
		dto.URL = &url
		dto.Headers = toStringAnyMap(entry.Headers)
		if markSecrets {
			dto.SecretHeadersKeys = sortedKeys(entry.Headers)
		}
	default:
		return nil, fmt.Errorf("unsupported transport %q", entry.Transport)
	}
	return dto, nil
}

// updateDTOFromImport turns a create DTO into a full-replace update DTO. Both
// transport maps are non-nil so an overwrite clears stale env, headers, and
// args instead of silently keeping the old values.
func updateDTOFromImport(create *CreateMCPServerDTO) *UpdateMCPServerDTO {
	name := create.Name
	transport := create.Type
	update := &UpdateMCPServerDTO{
		Name:              &name,
		Type:              &transport,
		Args:              append([]string{}, create.Args...),
		Env:               create.Env,
		Headers:           create.Headers,
		SecretEnvKeys:     create.SecretEnvKeys,
		SecretHeadersKeys: create.SecretHeadersKeys,
		Target:            &create.Target,
		TargetInstanceID:  create.TargetInstanceID,
	}
	if update.Env == nil {
		update.Env = map[string]any{}
	}
	if update.Headers == nil {
		update.Headers = map[string]any{}
	}
	update.Command = create.Command
	update.URL = create.URL
	return update
}

// validateImportCreate runs the same write-time checks CreateServer applies
// (device authorization and the outbound SSRF guard) without writing, so a
// dry-run reports the entries that a real import would reject.
func (s *Service) validateImportCreate(ctx context.Context, projectID string, dto *CreateMCPServerDTO) error {
	if dto.Target == TargetDevice {
		if err := s.authorizeDeviceWrite(ctx, projectID, dto.TargetInstanceID); err != nil {
			return err
		}
	}
	return guardOutboundFetchURL(ctx, dto.Type, dto.URL)
}

// validateImportUpdate mirrors the write-time checks UpdateServer applies to
// the resulting row.
func (s *Service) validateImportUpdate(ctx context.Context, projectID string, existing *MCPServer, dto *UpdateMCPServerDTO) error {
	resulting := *existing
	if dto.Type != nil {
		resulting.Type = *dto.Type
	}
	if dto.Target != nil {
		resulting.Target = *dto.Target
	}
	if dto.TargetInstanceID != nil {
		resulting.TargetInstanceID = dto.TargetInstanceID
	}
	if resulting.Target == "" {
		resulting.Target = TargetServer
	}
	if resulting.Target == TargetDevice {
		if err := s.authorizeDeviceWrite(ctx, projectID, resulting.TargetInstanceID); err != nil {
			return err
		}
	}
	return guardOutboundFetchURL(ctx, resulting.Type, dto.URL)
}

// resolveImportTarget maps the request's target choice onto the entry's
// delivery mode. "auto" sends remote servers to the shared host and stdio
// commands to a device.
func resolveImportTarget(target MCPServerTarget, transport mcpconfig.Transport) MCPServerTarget {
	if target != "" && target != importTargetAuto {
		return target
	}
	if transport == mcpconfig.TransportStdio {
		return TargetDevice
	}
	return TargetServer
}

// secretEnvKeys flags environment keys whose name suggests a credential. The
// import cannot know which values are secret, so it errs on the side of
// encrypting the obvious ones; the user can move a key between plain/secret
// afterwards.
func secretEnvKeys(env map[string]string) []string {
	var keys []string
	for key := range env {
		upper := strings.ToUpper(key)
		for _, marker := range []string{"TOKEN", "SECRET", "PASSWORD", "PASSWD", "API_KEY", "APIKEY", "AUTH", "CREDENTIAL", "PRIVATE_KEY"} {
			if strings.Contains(upper, marker) {
				keys = append(keys, key)
				break
			}
		}
	}
	return sortedKeysFromSlice(keys)
}

func toStringAnyMap(in map[string]string) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return sortedKeysFromSlice(keys)
}

func sortedKeysFromSlice(keys []string) []string {
	if len(keys) == 0 {
		return nil
	}
	sort.Strings(keys)
	return keys
}
