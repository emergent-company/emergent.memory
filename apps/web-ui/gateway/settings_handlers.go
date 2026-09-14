package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/emergent-company/go-daisy/render"
	"github.com/labstack/echo/v4"
)

// settingsRememberCategory / settingsRememberKey are the generic-settings
// coordinates of the remember pipeline config (which agent definition powers
// POST /remember).
const (
	settingsRememberCategory = "remember_config"
	settingsRememberKey      = "agent_name"
)

// settingsDedupCategory / settingsDedupKey are the generic-settings
// coordinates of the entity-create dedup threshold.
const (
	settingsDedupCategory = "entity_create"
	settingsDedupKey      = "similarity_threshold"
)

// settingsAssistantCategory / settingsAssistantKey are the generic-settings
// coordinates of the assistant agent selection (which agent definition powers
// the global assistant side panel).
const (
	settingsAssistantCategory = "assistant"
	settingsAssistantKey      = "agent_id"
)

// settingsEditorCategory / settingsEditorKey are the generic-settings
// coordinates of the editor agent selection (which agent definition runs
// object-content edits).
const (
	settingsEditorCategory = "editor"
	settingsEditorKey      = "agent_id"
)

// defaultRememberAgent is the canonical agent definition that powers the
// remember pipeline when no override is set (memory's
// agents.CanonicalRememberAgentName). An override to this name, or an unset
// setting, both resolve to this built-in agent.
const defaultRememberAgent = "domain-remember-agent"

// voiceCategory is the generic-settings category of the per-project voice
// configuration. Secrets are never stored — they stay env-driven.
const voiceCategory = "voice"

const (
	voiceKeyEnabled              = "enabled"
	voiceKeyTTSProvider          = "tts_provider"
	voiceKeyTTSModel             = "tts_model"
	voiceKeyTTSVoice             = "tts_voice"
	voiceKeySTTProvider          = "stt_provider"
	voiceKeySTTModel             = "stt_model"
	voiceKeySTTLanguage          = "stt_language"
	voiceKeyLiveKitURL           = "livekit_url"
	voiceKeyLiveKitPublicURL     = "livekit_public_url"
	voiceKeyExitKeywords         = "exit_keywords"
	voiceKeyGoodbyeText          = "goodbye_text"
	voiceKeyAwayTimeout          = "away_timeout"
	voiceKeyEndpointMinDelay     = "endpoint_min_delay"
	voiceKeyEndpointMaxDelay     = "endpoint_max_delay"
	voiceKeyAllowInterruptions   = "allow_interruptions"
	voiceKeyPreemptiveGeneration = "preemptive_generation"
)

// defaultSTTProvider is the speech-to-text provider when nothing is stored.
const defaultSTTProvider = "deepgram"

// voiceStrKeys / voiceNumKeys / voiceBoolKeys are the canonical per-key voice
// settings accepted by the inline-save endpoint POST /settings/voice/:key.
// Strings are stored trimmed (empty deletes); numerics must parse as a
// non-negative float when non-empty but are stored as their trimmed string so
// resolveVoiceSettings (which reads these keys as strings) round-trips them;
// booleans are derived from form value "on"/absent.
var (
	voiceStrKeys = map[string]bool{
		voiceKeyTTSProvider:      true,
		voiceKeyTTSModel:         true,
		voiceKeyTTSVoice:         true,
		voiceKeySTTProvider:      true,
		voiceKeySTTModel:         true,
		voiceKeySTTLanguage:      true,
		voiceKeyLiveKitURL:       true,
		voiceKeyLiveKitPublicURL: true,
		voiceKeyExitKeywords:     true,
		voiceKeyGoodbyeText:      true,
	}
	voiceNumKeys = map[string]bool{
		voiceKeyAwayTimeout:      true,
		voiceKeyEndpointMinDelay: true,
		voiceKeyEndpointMaxDelay: true,
	}
	voiceBoolKeys = map[string]bool{
		voiceKeyEnabled:              true,
		voiceKeyAllowInterruptions:   true,
		voiceKeyPreemptiveGeneration: true,
	}
)

// voiceSettings is the effective voice-flow configuration shown and edited on
// the Project Settings page: env defaults overlaid with the project's stored
// settings. Secrets are surfaced only as set/unset booleans.
type voiceSettings struct {
	Enabled              bool
	TTSProvider          string
	TTSModel             string
	TTSVoice             string
	STTProvider          string
	STTModel             string
	STTLanguage          string
	LiveKitURL           string
	LiveKitPublicURL     string
	ExitKeywords         string
	GoodbyeText          string
	AwayTimeout          string
	EndpointMinDelay     string
	EndpointMaxDelay     string
	AllowInterruptions   bool
	PreemptiveGeneration bool
	CartesiaKeySet       bool
	DeepgramKeySet       bool
	LiveKitSecretSet     bool
}

// settingsNavCommon is embedded (anonymous) into every settings-page payload so
// the page can render the settings sub-nav's provider warning badge: true when
// the active project has ZERO configured LLM providers.
type settingsNavCommon struct {
	ProvidersMissing bool
}

// projectSettingsData is the payload for ProjectSettingsPage (General): the
// project record and the remember/dedup config. A failed project fetch renders
// the whole-page error state (LoadErr); the settings reads are best-effort —
// an absent value renders as "not set".
type projectSettingsData struct {
	settingsNavCommon
	Project        *Project
	Agents         []AgentDefinitionSummary // the agent-dropdown options
	RememberAgent  string
	DedupThreshold string
	EditorAgent    string
	LoadErr        error
	FlashMsg       string
	FlashErr       error
}

// assistantSettingsPageData is the payload for AssistantSettingsPage: the
// assistant agent selection plus PRG flash feedback from the save flow.
type assistantSettingsPageData struct {
	settingsNavCommon
	AssistantAgent string
	Agents         []AgentDefinitionSummary
	FlashMsg       string
	FlashErr       error
}

// overridesSettingsPageData is the payload for OverridesSettingsPage: the
// agent-definition overrides plus PRG flash feedback from the save flow.
type overridesSettingsPageData struct {
	settingsNavCommon
	Overrides []AgentOverrideEntry
	FlashMsg  string
	FlashErr  error
}

// providersSettingsPageData is the payload for ProvidersSettingsPage: the
// provider rate groups plus PRG flash feedback from the save flow.
type providersSettingsPageData struct {
	settingsNavCommon
	Providers providerPanelData
	FlashMsg  string
	FlashErr  error
}

// providerConfigPageData is the payload for the add/edit provider form page.
// Draft + DraftProvider are set when re-rendering after a failed save so the
// form repopulates the user's submitted values (designer reads these).
type providerConfigPageData struct {
	settingsNavCommon
	Provider      *ProjectProviderConfig
	Draft         *ProviderConfigInput
	DraftProvider string
	FlashMsg      string
	FlashErr      error
	// GenerativeModels/EmbeddingModels are the credential-prefixed model
	// options ("provider/model") for the fallback dropdowns.
	GenerativeModels []Model
	EmbeddingModels  []Model
}

// voiceSettingsPageData is the payload for VoiceSettingsPage: the effective
// voice configuration plus PRG flash feedback from the save flow.
type voiceSettingsPageData struct {
	settingsNavCommon
	Voice    voiceSettings
	FlashMsg string
	FlashErr error
}

// devicesSettingsPageData is the payload for DevicesSettingsPage: the iOS
// setup QR and the registered-devices list plus PRG flash feedback.
type devicesSettingsPageData struct {
	settingsNavCommon
	IOSQRImage string
	Devices    []device
	FlashMsg   string
	FlashErr   error
}

// uiProjectSettings renders the project settings page. ?updated=1 / ?err=1
// surface PRG feedback from the save flows, mirroring uiAgentSettings.
func (s *Server) uiProjectSettings(c echo.Context) error {
	ctx := c.Request().Context()
	data := projectSettingsData{}
	if c.QueryParam("updated") != "" {
		data.FlashMsg = "Project settings saved."
	}
	data.FlashErr = flashError(c)
	data.ProvidersMissing = s.projectHasNoProviders(ctx)

	project, err := s.memory.GetCurrentProject(ctx)
	if err != nil {
		data.LoadErr = err
		return s.page(c, pageTitle("Project Settings"), ProjectSettingsPage(data))
	}
	if project == nil {
		// No active project in the session and no static project — the genuine
		// "pick a project first" case, not an error. Land on the org/project
		// picker so the user can select the project to configure.
		return c.Redirect(http.StatusSeeOther, "/orgs")
	}
	data.Project = project

	agents, err := s.memory.ListAgentDefinitions(ctx)
	captureError(err)
	data.Agents = agents

	if ps, perr := s.memory.GetProjectSetting(ctx, settingsRememberCategory, settingsRememberKey); perr == nil && ps != nil {
		if name, ok := ps.Value["name"].(string); ok {
			data.RememberAgent = name
		}
	}
	if ps, perr := s.memory.GetProjectSetting(ctx, settingsDedupCategory, settingsDedupKey); perr == nil && ps != nil {
		if v, ok := ps.Value["value"].(float64); ok {
			data.DedupThreshold = strconv.FormatFloat(v, 'f', -1, 64)
		}
	}
	if ps, perr := s.memory.GetProjectSetting(ctx, settingsEditorCategory, settingsEditorKey); perr == nil && ps != nil {
		if id, ok := ps.Value["agentId"].(string); ok {
			data.EditorAgent = id
		}
	}
	return s.page(c, pageTitle("Project Settings"), ProjectSettingsPage(data))
}

// uiProjectAssistantSettings renders the Assistant settings page. ?updated=1 /
// ?err=1 surface PRG feedback from the assistant save flow.
func (s *Server) uiProjectAssistantSettings(c echo.Context) error {
	ctx := c.Request().Context()
	data := assistantSettingsPageData{}
	if c.QueryParam("updated") != "" {
		data.FlashMsg = "Assistant settings saved."
	}
	data.FlashErr = flashError(c)
	data.ProvidersMissing = s.projectHasNoProviders(ctx)
	agents, err := s.memory.ListAgentDefinitions(ctx)
	captureError(err)
	data.Agents = agents
	if ps, perr := s.memory.GetProjectSetting(ctx, settingsAssistantCategory, settingsAssistantKey); perr == nil && ps != nil {
		if id, ok := ps.Value["agentId"].(string); ok {
			data.AssistantAgent = id
		}
	}
	return s.page(c, pageTitle("Assistant"), AssistantSettingsPage(data))
}

// uiProjectOverridesSettings renders the Agent overrides settings page.
// ?updated=1 / ?err=1 surface PRG feedback from the override save/delete flow.
func (s *Server) uiProjectOverridesSettings(c echo.Context) error {
	ctx := c.Request().Context()
	data := overridesSettingsPageData{}
	if c.QueryParam("updated") != "" {
		data.FlashMsg = "Agent overrides saved."
	}
	data.FlashErr = flashError(c)
	data.ProvidersMissing = s.projectHasNoProviders(ctx)
	overrides, err := s.memory.ListAgentOverrides(ctx)
	captureError(err)
	data.Overrides = overrides
	return s.page(c, pageTitle("Agent overrides"), OverridesSettingsPage(data))
}

// uiProjectProvidersSettings renders the Providers settings page. ?updated=1 /
// ?err=1 surface PRG feedback from the provider rate override flow.
func (s *Server) uiProjectProvidersSettings(c echo.Context) error {
	ctx := c.Request().Context()
	data := providersSettingsPageData{}
	if c.QueryParam("updated") != "" {
		data.FlashMsg = "Providers saved."
	}
	data.FlashErr = flashError(c)
	data.ProvidersMissing = s.projectHasNoProviders(ctx)
	data.Providers = s.loadProviderPanel(ctx)
	return s.page(c, pageTitle("Providers"), ProvidersSettingsPage(data))
}

// uiProjectProviderNew renders the "add provider" form page.
func (s *Server) uiProjectProviderNew(c echo.Context) error {
	ctx := c.Request().Context()
	data := providerConfigPageData{}
	data.FlashErr = flashError(c)
	data.ProvidersMissing = s.projectHasNoProviders(ctx)
	data.GenerativeModels, data.EmbeddingModels = s.providerModelOptions(ctx)
	return s.page(c, pageTitle("Add provider"), providerConfigPage(data))
}

// uiProjectProviderEdit renders the "edit provider" form page, prefilled from
// the project's stored config. Credentials are never returned by the API, so
// apiKey and serviceAccountJson stay blank (leave blank to keep the stored one).
func (s *Server) uiProjectProviderEdit(c echo.Context) error {
	ctx := c.Request().Context()
	provider := strings.TrimSpace(c.Param("provider"))
	providers, err := s.memory.ListProjectProviders(ctx)
	if err != nil {
		return redirectWithError(c, "/settings/providers", err)
	}
	var cfg *ProjectProviderConfig
	for i := range providers {
		if providers[i].Provider == provider {
			cfg = &providers[i]
			break
		}
	}
	if cfg == nil {
		return redirectWithError(c, "/settings/providers", fmt.Errorf("provider %q not found", provider))
	}
	data := providerConfigPageData{Provider: cfg}
	data.FlashErr = flashError(c)
	data.ProvidersMissing = s.projectHasNoProviders(ctx)
	data.GenerativeModels, data.EmbeddingModels = s.providerModelOptions(ctx)
	return s.page(c, pageTitle("Edit provider"), providerConfigPage(data))
}

// uiProjectVoiceSettings renders the Voice settings page. ?updated=1 / ?err=1
// surface PRG feedback from the voice save flow.
func (s *Server) uiProjectVoiceSettings(c echo.Context) error {
	ctx := c.Request().Context()
	data := voiceSettingsPageData{}
	if c.QueryParam("updated") != "" {
		data.FlashMsg = "Voice settings saved."
	}
	data.FlashErr = flashError(c)
	data.ProvidersMissing = s.projectHasNoProviders(ctx)
	data.Voice = resolveVoiceSettings(ctx, s.cfg, s.memory)
	return s.page(c, pageTitle("Voice"), VoiceSettingsPage(data))
}

// uiProjectDeviceSettings renders the Devices settings page. ?updated=1 /
// ?err=1 surface PRG feedback from the revoke flow.
func (s *Server) uiProjectDeviceSettings(c echo.Context) error {
	ctx := c.Request().Context()
	data := devicesSettingsPageData{}
	if c.QueryParam("updated") != "" {
		data.FlashMsg = "Devices saved."
	}
	data.FlashErr = flashError(c)
	data.ProvidersMissing = s.projectHasNoProviders(ctx)
	data.IOSQRImage = s.setupQRImage(c)
	data.Devices, _ = s.listDeviceKeys(ctx) // list errors degrade to an empty list
	return s.page(c, pageTitle("Devices"), DevicesSettingsPage(data))
}

// uiProjectSettingsProjectField handles one inline per-field project-record
// save (HTMX → POST /settings/project/:field). Each field PATCHes only itself
// via UpdateProject; the project name is required, and budget must be a
// non-negative number when present. The outcome surfaces as a toast via the
// HX-Trigger header.
func (s *Server) uiProjectSettingsProjectField(c echo.Context) error {
	ctx := c.Request().Context()
	project, err := s.memory.GetCurrentProject(ctx)
	if err != nil {
		return toastTrigger(c, "error", err.Error())
	}
	if project == nil {
		// Defensive: the settings page itself redirects to the project picker
		// when no project is active, so reaching this save without one means
		// the session changed mid-flight. Message says what to do, not what
		// memory did.
		return toastTrigger(c, "error", "Select a project first.")
	}

	upd := &ProjectUpdate{}
	switch c.Param("field") {
	case "name":
		name := strings.TrimSpace(c.FormValue("name"))
		if name == "" {
			return toastTrigger(c, "error", "project name is required")
		}
		upd.Name = name
	case "project_info":
		pi := c.FormValue("project_info")
		upd.ProjectInfo = &pi
	case "chat_prompt_template":
		cpt := c.FormValue("chat_prompt_template")
		upd.ChatPromptTemplate = &cpt
	case "auto_extract_objects":
		upd.AutoExtractObjects = boolPtr(c.FormValue("auto_extract_objects") == "on")
	case "auto_merge_extraction_branches":
		upd.AutoMergeExtractionBranches = boolPtr(c.FormValue("auto_merge_extraction_branches") == "on")
	case "budget_usd":
		v := strings.TrimSpace(c.FormValue("budget_usd"))
		if v == "" {
			return toastTrigger(c, "success", "Saved") // empty leaves budget unchanged
		}
		b, berr := strconv.ParseFloat(v, 64)
		if berr != nil || b < 0 {
			return toastTrigger(c, "error", "budget must be a non-negative number")
		}
		upd.BudgetUSD = &b
	default:
		return toastTrigger(c, "error", fmt.Sprintf("Unknown project field %q.", c.Param("field")))
	}

	if _, err := s.memory.UpdateProject(ctx, project.ID, upd); err != nil {
		return toastTrigger(c, "error", err.Error())
	}
	return toastTrigger(c, "success", "Saved")
}

// uiProjectSettingsOverride handles the add/update override form (PRG → POST
// /settings/overrides). Writing an existing agent name updates its override,
// a new name creates one. Only the fields the form set are sent.
func (s *Server) uiProjectSettingsOverride(c echo.Context) error {
	ctx := c.Request().Context()
	name := strings.TrimSpace(c.FormValue("agentName"))
	if name == "" {
		return redirectWithError(c, "/settings/overrides", fmt.Errorf("agent name is required"))
	}
	in := &AgentOverrideInput{}
	if v := c.FormValue("systemPrompt"); v != "" {
		in.SystemPrompt = &v
	}
	if v := strings.TrimSpace(c.FormValue("model")); v != "" {
		in.Model = &ModelConfig{Name: v}
	}
	if v := strings.TrimSpace(c.FormValue("tools")); v != "" {
		var tools []string
		for _, t := range strings.Split(v, ",") {
			if t = strings.TrimSpace(t); t != "" {
				tools = append(tools, t)
			}
		}
		in.Tools = tools
	}
	if v := strings.TrimSpace(c.FormValue("maxSteps")); v != "" {
		if n, nerr := strconv.Atoi(v); nerr == nil && n > 0 {
			in.MaxSteps = &n
		}
	}

	if err := s.memory.SetAgentOverride(ctx, name, in); err != nil {
		return redirectWithError(c, "/settings/overrides", err)
	}
	return c.Redirect(http.StatusSeeOther, "/settings/overrides?updated=1")
}

// uiProjectSettingsOverrideDelete handles the per-override delete form (PRG →
// POST /settings/overrides/:agentName/delete).
func (s *Server) uiProjectSettingsOverrideDelete(c echo.Context) error {
	name := c.Param("agentName")
	if name == "" {
		return redirectWithError(c, "/settings/overrides", fmt.Errorf("agent name is required"))
	}
	if err := s.memory.DeleteAgentOverride(c.Request().Context(), name); err != nil {
		return redirectWithError(c, "/settings/overrides", err)
	}
	return c.Redirect(http.StatusSeeOther, "/settings/overrides?updated=1")
}

// uiRevokeDevice handles the per-device revoke form (PRG → POST
// /settings/devices/:key/revoke). Revoking a device key cuts that client's
// access to the /api/* endpoints immediately.
func (s *Server) uiRevokeDevice(c echo.Context) error {
	key := c.Param("key")
	if key == "" {
		return redirectWithError(c, "/settings/devices", fmt.Errorf("device key is required"))
	}
	if err := s.revokeDeviceKey(c.Request().Context(), key); err != nil {
		return redirectWithError(c, "/settings/devices", err)
	}
	return c.Redirect(http.StatusSeeOther, "/settings/devices?updated=1")
}

// uiProjectSettingsRememberField handles one inline per-field remember/dedup
// save (HTMX → POST /settings/remember/:field). "agent" stores the remember
// agent name (empty deletes); "dedup" validates 0.0–1.0 and stores the
// threshold (empty leaves it unchanged). The outcome surfaces as a toast via
// the HX-Trigger header.
func (s *Server) uiProjectSettingsRememberField(c echo.Context) error {
	ctx := c.Request().Context()
	switch c.Param("field") {
	case "agent":
		agent := strings.TrimSpace(c.FormValue("remember_agent"))
		if agent == "" {
			if err := s.memory.DeleteProjectSetting(ctx, settingsRememberCategory, settingsRememberKey); err != nil {
				return toastTrigger(c, "error", err.Error())
			}
		} else if err := s.memory.SetProjectSetting(ctx, settingsRememberCategory, settingsRememberKey, map[string]any{"name": agent}); err != nil {
			return toastTrigger(c, "error", err.Error())
		}
		return toastTrigger(c, "success", "Saved")
	case "dedup":
		dedup := strings.TrimSpace(c.FormValue("dedup_threshold"))
		if dedup == "" {
			return toastTrigger(c, "success", "Saved") // empty leaves it unchanged
		}
		th, err := strconv.ParseFloat(dedup, 64)
		if err != nil || th < 0 || th > 1 {
			return toastTrigger(c, "error", "dedup threshold must be a number between 0.0 and 1.0")
		}
		if err := s.memory.SetProjectSetting(ctx, settingsDedupCategory, settingsDedupKey, map[string]any{"value": th}); err != nil {
			return toastTrigger(c, "error", err.Error())
		}
		return toastTrigger(c, "success", "Saved")
	default:
		return toastTrigger(c, "error", fmt.Sprintf("Unknown remember field %q.", c.Param("field")))
	}
}

// uiProjectSettingsAssistant handles the assistant agent inline save (HTMX →
// POST /settings/assistant). An empty value clears the setting via delete; any
// other value stores the agent ID so /api/chat can resolve it. The assistant
// selection is shell chrome — it gates the topbar "Assistant" button and the
// sidepanel, both rendered outside #main-content — so success forces a full
// reload (render.RedirectAfterMutation) instead of a toast: a boosted/no-swap
// save would leave the button hidden until a manual refresh. The PRG target
// (?updated=1) carries the "Assistant settings saved." flash on the reload.
func (s *Server) uiProjectSettingsAssistant(c echo.Context) error {
	ctx := c.Request().Context()
	id := strings.TrimSpace(c.FormValue("assistant_agent"))
	if id == "" {
		if err := s.memory.DeleteProjectSetting(ctx, settingsAssistantCategory, settingsAssistantKey); err != nil {
			return toastTrigger(c, "error", err.Error())
		}
	} else if err := s.memory.SetProjectSetting(ctx, settingsAssistantCategory, settingsAssistantKey, map[string]any{"agentId": id}); err != nil {
		return toastTrigger(c, "error", err.Error())
	}
	render.RedirectAfterMutation(c.Response().Writer, c.Request(), "/settings/assistant?updated=1")
	return nil
}

// uiProjectSettingsEditor handles the editor agent inline save (HTMX → POST
// /settings/editor). An empty value clears the setting via delete (object
// edits fall back to the default agent); any other value stores the agent ID
// so object-chat sessions can resolve it. The outcome surfaces as a toast via
// the HX-Trigger header.
func (s *Server) uiProjectSettingsEditor(c echo.Context) error {
	ctx := c.Request().Context()
	id := strings.TrimSpace(c.FormValue("editor_agent"))
	if id == "" {
		if err := s.memory.DeleteProjectSetting(ctx, settingsEditorCategory, settingsEditorKey); err != nil {
			return toastTrigger(c, "error", err.Error())
		}
	} else if err := s.memory.SetProjectSetting(ctx, settingsEditorCategory, settingsEditorKey, map[string]any{"agentId": id}); err != nil {
		return toastTrigger(c, "error", err.Error())
	}
	return toastTrigger(c, "success", "Saved")
}

// resolveVoiceSettings computes the effective voice configuration: env
// defaults overlaid with the project's stored settings (category "voice").
// Stored values win when present; absent keys and read errors degrade to the
// env default, with Enabled defaulting to true (preserving the always-on
// behavior). Secrets are never read from storage.
func resolveVoiceSettings(ctx context.Context, cfg Config, mem MemoryBackend) voiceSettings {
	v := voiceSettings{
		Enabled:              true,
		TTSProvider:          cfg.TTSProvider,
		TTSModel:             cfg.CartesiaModel,
		TTSVoice:             cfg.CartesiaVoice,
		STTProvider:          defaultSTTProvider,
		STTModel:             cfg.DeepgramModel,
		STTLanguage:          cfg.DeepgramLang,
		LiveKitURL:           cfg.LiveKitURL,
		LiveKitPublicURL:     cfg.LiveKitPublicURL,
		ExitKeywords:         cfg.ExitKeywords,
		GoodbyeText:          cfg.GoodbyeText,
		AwayTimeout:          cfg.UserAwayTimeout,
		EndpointMinDelay:     cfg.EndpointMinDelay,
		EndpointMaxDelay:     cfg.EndpointMaxDelay,
		AllowInterruptions:   cfg.AllowInterruptions,
		PreemptiveGeneration: cfg.PreemptiveGeneration,
		CartesiaKeySet:       cfg.CartesiaAPIKey != "",
		DeepgramKeySet:       cfg.DeepgramAPIKey != "",
		LiveKitSecretSet:     cfg.LiveKitAPISecret != "",
	}
	strs := []struct {
		key string
		set func(string)
	}{
		{voiceKeyTTSProvider, func(s string) { v.TTSProvider = s }},
		{voiceKeyTTSModel, func(s string) { v.TTSModel = s }},
		{voiceKeyTTSVoice, func(s string) { v.TTSVoice = s }},
		{voiceKeySTTProvider, func(s string) { v.STTProvider = s }},
		{voiceKeySTTModel, func(s string) { v.STTModel = s }},
		{voiceKeySTTLanguage, func(s string) { v.STTLanguage = s }},
		{voiceKeyLiveKitURL, func(s string) { v.LiveKitURL = s }},
		{voiceKeyLiveKitPublicURL, func(s string) { v.LiveKitPublicURL = s }},
		{voiceKeyExitKeywords, func(s string) { v.ExitKeywords = s }},
		{voiceKeyGoodbyeText, func(s string) { v.GoodbyeText = s }},
		{voiceKeyAwayTimeout, func(s string) { v.AwayTimeout = s }},
		{voiceKeyEndpointMinDelay, func(s string) { v.EndpointMinDelay = s }},
		{voiceKeyEndpointMaxDelay, func(s string) { v.EndpointMaxDelay = s }},
	}
	strVals := make([]*ProjectSetting, len(strs))
	runConcurrently(len(strs), func(i int) {
		if ps, err := mem.GetProjectSetting(ctx, voiceCategory, strs[i].key); err == nil && ps != nil {
			strVals[i] = ps
		}
	})
	for i, s := range strs {
		if ps := strVals[i]; ps != nil {
			if sv, ok := ps.Value["value"].(string); ok && sv != "" {
				s.set(sv)
			}
		}
	}
	bools := []struct {
		key string
		set func(bool)
	}{
		{voiceKeyEnabled, func(b bool) { v.Enabled = b }},
		{voiceKeyAllowInterruptions, func(b bool) { v.AllowInterruptions = b }},
		{voiceKeyPreemptiveGeneration, func(b bool) { v.PreemptiveGeneration = b }},
	}
	boolVals := make([]*ProjectSetting, len(bools))
	runConcurrently(len(bools), func(i int) {
		if ps, err := mem.GetProjectSetting(ctx, voiceCategory, bools[i].key); err == nil && ps != nil {
			boolVals[i] = ps
		}
	})
	for i, b := range bools {
		if ps := boolVals[i]; ps != nil {
			if bv, ok := ps.Value["value"].(bool); ok {
				b.set(bv)
			}
		}
	}
	return v
}

// persistVoiceSettings writes the effective voice configuration to the
// project's settings. Booleans are always written explicitly (true/false); a
// non-empty string is stored, an empty string clears the stored value so the
// field falls back to its env default. Secrets are never written. The first
// error is returned.
func persistVoiceSettings(ctx context.Context, mem MemoryBackend, v voiceSettings) error {
	writeBool := func(key string, b bool) error {
		return mem.SetProjectSetting(ctx, voiceCategory, key, map[string]any{"value": b})
	}
	writeStr := func(key, s string) error {
		if s == "" {
			return mem.DeleteProjectSetting(ctx, voiceCategory, key)
		}
		return mem.SetProjectSetting(ctx, voiceCategory, key, map[string]any{"value": s})
	}
	if err := writeBool(voiceKeyEnabled, v.Enabled); err != nil {
		return err
	}
	if err := writeBool(voiceKeyAllowInterruptions, v.AllowInterruptions); err != nil {
		return err
	}
	if err := writeBool(voiceKeyPreemptiveGeneration, v.PreemptiveGeneration); err != nil {
		return err
	}
	for _, s := range []struct{ key, val string }{
		{voiceKeyTTSProvider, v.TTSProvider},
		{voiceKeyTTSModel, v.TTSModel},
		{voiceKeyTTSVoice, v.TTSVoice},
		{voiceKeySTTProvider, v.STTProvider},
		{voiceKeySTTModel, v.STTModel},
		{voiceKeySTTLanguage, v.STTLanguage},
		{voiceKeyLiveKitURL, v.LiveKitURL},
		{voiceKeyLiveKitPublicURL, v.LiveKitPublicURL},
		{voiceKeyExitKeywords, v.ExitKeywords},
		{voiceKeyGoodbyeText, v.GoodbyeText},
		{voiceKeyAwayTimeout, v.AwayTimeout},
		{voiceKeyEndpointMinDelay, v.EndpointMinDelay},
		{voiceKeyEndpointMaxDelay, v.EndpointMaxDelay},
	} {
		if err := writeStr(s.key, s.val); err != nil {
			return err
		}
	}
	return nil
}

// voiceEnabled reports whether voice is enabled for the current project
// (stored setting over env default; defaults to on).
func (s *Server) voiceEnabled(ctx context.Context) bool {
	return resolveVoiceSettings(ctx, s.cfg, s.memory).Enabled
}

// uiProjectSettingsVoice handles the Voice section form (PRG → POST
// /settings/voice). Toggles are always sent as explicit on/off; string fields
// are trimmed, stored when non-empty, cleared via delete when empty. Away
// timeout and the endpoint delays must be non-negative numbers when present —
// validation runs before anything is written so an invalid value never
// persists a partial write.
func (s *Server) uiProjectSettingsVoice(c echo.Context) error {
	ctx := c.Request().Context()
	v := voiceSettings{
		Enabled:              c.FormValue("voice_enabled") == "on",
		AllowInterruptions:   c.FormValue("allow_interruptions") == "on",
		PreemptiveGeneration: c.FormValue("preemptive_generation") == "on",
		TTSProvider:          strings.TrimSpace(c.FormValue("tts_provider")),
		TTSModel:             strings.TrimSpace(c.FormValue("tts_model")),
		TTSVoice:             strings.TrimSpace(c.FormValue("tts_voice")),
		STTProvider:          strings.TrimSpace(c.FormValue("stt_provider")),
		STTModel:             strings.TrimSpace(c.FormValue("stt_model")),
		STTLanguage:          strings.TrimSpace(c.FormValue("stt_language")),
		LiveKitURL:           strings.TrimSpace(c.FormValue("livekit_url")),
		LiveKitPublicURL:     strings.TrimSpace(c.FormValue("livekit_public_url")),
		ExitKeywords:         strings.TrimSpace(c.FormValue("exit_keywords")),
		GoodbyeText:          strings.TrimSpace(c.FormValue("goodbye_text")),
		AwayTimeout:          strings.TrimSpace(c.FormValue("away_timeout")),
		EndpointMinDelay:     strings.TrimSpace(c.FormValue("endpoint_min_delay")),
		EndpointMaxDelay:     strings.TrimSpace(c.FormValue("endpoint_max_delay")),
	}
	for _, f := range []struct{ name, val string }{
		{"away timeout", v.AwayTimeout},
		{"endpoint min delay", v.EndpointMinDelay},
		{"endpoint max delay", v.EndpointMaxDelay},
	} {
		if f.val == "" {
			continue
		}
		if n, err := strconv.ParseFloat(f.val, 64); err != nil || n < 0 {
			return redirectWithError(c, "/settings/voice", fmt.Errorf("%s must be a non-negative number", f.name))
		}
	}
	if err := persistVoiceSettings(ctx, s.memory, v); err != nil {
		return redirectWithError(c, "/settings/voice", err)
	}
	return c.Redirect(http.StatusSeeOther, "/settings/voice?updated=1")
}

// voiceSettingLabel maps a canonical voice key to its human display name for
// inline error messages.
func voiceSettingLabel(key string) string {
	switch key {
	case voiceKeyEnabled:
		return "voice"
	case voiceKeyTTSProvider:
		return "tts provider"
	case voiceKeyTTSModel:
		return "tts model"
	case voiceKeyTTSVoice:
		return "tts voice"
	case voiceKeySTTProvider:
		return "stt provider"
	case voiceKeySTTModel:
		return "stt model"
	case voiceKeySTTLanguage:
		return "stt language"
	case voiceKeyLiveKitURL:
		return "LiveKit url"
	case voiceKeyLiveKitPublicURL:
		return "LiveKit public url"
	case voiceKeyExitKeywords:
		return "exit keywords"
	case voiceKeyGoodbyeText:
		return "goodbye text"
	case voiceKeyAwayTimeout:
		return "away timeout"
	case voiceKeyEndpointMinDelay:
		return "endpoint min delay"
	case voiceKeyEndpointMaxDelay:
		return "endpoint max delay"
	default:
		return key
	}
}

// toastTrigger signals a toast to the client via the HX-Trigger response
// header. The "memory-toast" event carries {kind, message}; app.js listens for
// it and pushes the message into the Alpine toast queue. It always returns
// HTTP 200: the app-wide htmx noSwap config suppresses non-2xx, and the
// outcome is carried by the trigger detail, not the HTTP status or body.
func toastTrigger(c echo.Context, kind, msg string) error {
	detail, err := json.Marshal(map[string]any{"kind": kind, "message": msg})
	if err != nil {
		detail = []byte(`{"kind":"info","message":""}`)
	}
	c.Response().Header().Set("HX-Trigger", `{"memory-toast": `+string(detail)+`}`)
	c.Response().WriteHeader(http.StatusOK)
	return nil
}

// uiProjectVoiceField handles one inline per-field voice save (HTMX → POST
// /settings/voice/:key). The request carries only the changed input's own
// value (hx-include="this"; the input's name equals the canonical key).
// Validation is per key: strings are trimmed and an empty value deletes the
// stored row (falling back to the env default); numerics must parse as a
// non-negative float when non-empty; booleans are stored as value == "on".
// Rejected values persist nothing and return an inline error fragment.
func (s *Server) uiProjectVoiceField(c echo.Context) error {
	ctx := c.Request().Context()
	key := c.Param("key")
	val := strings.TrimSpace(c.FormValue(key))
	switch {
	case voiceStrKeys[key]:
		if val == "" {
			if err := s.memory.DeleteProjectSetting(ctx, voiceCategory, key); err != nil {
				return toastTrigger(c, "error", err.Error())
			}
			return toastTrigger(c, "success", "Saved")
		}
		if err := s.memory.SetProjectSetting(ctx, voiceCategory, key, map[string]any{"value": val}); err != nil {
			return toastTrigger(c, "error", err.Error())
		}
		return toastTrigger(c, "success", "Saved")
	case voiceNumKeys[key]:
		if val != "" {
			if n, err := strconv.ParseFloat(val, 64); err != nil || n < 0 {
				return toastTrigger(c, "error", fmt.Sprintf("%s must be a non-negative number", voiceSettingLabel(key)))
			}
		}
		if val == "" {
			if err := s.memory.DeleteProjectSetting(ctx, voiceCategory, key); err != nil {
				return toastTrigger(c, "error", err.Error())
			}
			return toastTrigger(c, "success", "Saved")
		}
		if err := s.memory.SetProjectSetting(ctx, voiceCategory, key, map[string]any{"value": val}); err != nil {
			return toastTrigger(c, "error", err.Error())
		}
		return toastTrigger(c, "success", "Saved")
	case voiceBoolKeys[key]:
		if err := s.memory.SetProjectSetting(ctx, voiceCategory, key, map[string]any{"value": c.FormValue(key) == "on"}); err != nil {
			return toastTrigger(c, "error", err.Error())
		}
		return toastTrigger(c, "success", "Saved")
	default:
		return toastTrigger(c, "error", fmt.Sprintf("Unknown voice setting %q.", key))
	}
}

// uiProjectVoiceGroup handles one inline group save (HTMX → POST
// /settings/voice/group/:group). Coupled fields commit as a single unit:
//   - endpoint_delays re-validates min <= max (both non-empty) before writing
//     either member, so an invalid combination persists nothing;
//   - tts persists provider/model/voice together and clears model/voice when
//     the provider does not use them (client/none).
//
// Unknown groups and validation failures return an inline error fragment.
func (s *Server) uiProjectVoiceGroup(c echo.Context) error {
	ctx := c.Request().Context()
	group := c.Param("group")

	// setOrDelete persists one group member, returning the first write error.
	setOrDelete := func(key, val string) error {
		if val == "" {
			return s.memory.DeleteProjectSetting(ctx, voiceCategory, key)
		}
		return s.memory.SetProjectSetting(ctx, voiceCategory, key, map[string]any{"value": val})
	}
	// parseNonNeg validates a delay/timeout group member: empty is valid
	// (clears), non-empty must parse as a non-negative float.
	parseNonNeg := func(raw string) (float64, bool) {
		if raw == "" {
			return 0, true
		}
		n, err := strconv.ParseFloat(raw, 64)
		return n, err == nil && n >= 0
	}

	switch group {
	case "endpoint_delays":
		minRaw := strings.TrimSpace(c.FormValue(voiceKeyEndpointMinDelay))
		maxRaw := strings.TrimSpace(c.FormValue(voiceKeyEndpointMaxDelay))
		if minV, ok := parseNonNeg(minRaw); !ok {
			return toastTrigger(c, "error", fmt.Sprintf("%s must be a non-negative number", voiceSettingLabel(voiceKeyEndpointMinDelay)))
		} else if maxV, ok := parseNonNeg(maxRaw); !ok {
			return toastTrigger(c, "error", fmt.Sprintf("%s must be a non-negative number", voiceSettingLabel(voiceKeyEndpointMaxDelay)))
		} else if minRaw != "" && maxRaw != "" && minV > maxV {
			return toastTrigger(c, "error", "endpoint min delay cannot exceed endpoint max delay")
		}
		if err := setOrDelete(voiceKeyEndpointMinDelay, minRaw); err != nil {
			return toastTrigger(c, "error", err.Error())
		}
		if err := setOrDelete(voiceKeyEndpointMaxDelay, maxRaw); err != nil {
			return toastTrigger(c, "error", err.Error())
		}
		return toastTrigger(c, "success", "Saved")
	case "tts":
		provider := strings.TrimSpace(c.FormValue(voiceKeyTTSProvider))
		model := strings.TrimSpace(c.FormValue(voiceKeyTTSModel))
		voice := strings.TrimSpace(c.FormValue(voiceKeyTTSVoice))
		// A provider that does not use a model/voice clears them as part of
		// the same atomic save.
		if provider == "client" || provider == "none" {
			model, voice = "", ""
		}
		if err := setOrDelete(voiceKeyTTSProvider, provider); err != nil {
			return toastTrigger(c, "error", err.Error())
		}
		if err := setOrDelete(voiceKeyTTSModel, model); err != nil {
			return toastTrigger(c, "error", err.Error())
		}
		if err := setOrDelete(voiceKeyTTSVoice, voice); err != nil {
			return toastTrigger(c, "error", err.Error())
		}
		return toastTrigger(c, "success", "Saved")
	default:
		return toastTrigger(c, "error", fmt.Sprintf("Unknown voice setting group %q.", group))
	}
}

// --- display helpers (used by ProjectSettingsPage) ---

// overrideSummary renders one line describing which fields an override sets,
// e.g. "model gpt-4o · 2 tools · max 10 steps".
func overrideSummary(o AgentOverrideInput) string {
	parts := []string{}
	if o.SystemPrompt != nil && *o.SystemPrompt != "" {
		parts = append(parts, "system prompt")
	}
	if o.Model != nil && o.Model.Name != "" {
		parts = append(parts, "model "+o.Model.Name)
	}
	if len(o.Tools) > 0 {
		parts = append(parts, strconv.Itoa(len(o.Tools))+" tool"+plural(len(o.Tools)))
	}
	if o.MaxSteps != nil && *o.MaxSteps > 0 {
		parts = append(parts, "max "+strconv.Itoa(*o.MaxSteps)+" steps")
	}
	if len(o.SandboxConfig) > 0 {
		parts = append(parts, "sandbox config")
	}
	if len(parts) == 0 {
		return "no overridden fields"
	}
	return strings.Join(parts, " · ")
}

// budgetInputValue renders the budget form value, or "" when unset.
func budgetInputValue(b *float64) string {
	if b == nil {
		return ""
	}
	return strconv.FormatFloat(*b, 'f', -1, 64)
}

// rememberAgentIsDefault reports whether the remember agent resolves to the
// canonical default (unset, empty, or explicitly set to the canonical name).
func rememberAgentIsDefault(stored string) bool {
	return stored == "" || stored == defaultRememberAgent
}

// agentListed reports whether name is one of the given agent definitions.
// The remember-agent dropdown uses it to keep a stored value visible (as an
// "(unlisted)" option) when the agent it refers to no longer exists.
func agentListed(agents []AgentDefinitionSummary, name string) bool {
	for _, a := range agents {
		if a.Name == name {
			return true
		}
	}
	return false
}

// agentListedByID reports whether id is one of the given agent definitions.
// The assistant dropdown uses it to keep a stored value visible (as an
// "(unlisted)" option) when the agent it refers to no longer exists.
func agentListedByID(agents []AgentDefinitionSummary, id string) bool {
	for _, a := range agents {
		if a.ID == id {
			return true
		}
	}
	return false
}

// agentNameByID resolves an agent definition's display name from its ID,
// returning "" when the ID is not among the given definitions.
func agentNameByID(agents []AgentDefinitionSummary, id string) string {
	for _, a := range agents {
		if a.ID == id {
			return a.Name
		}
	}
	return ""
}

// deviceDisplayName returns the best human label for a registered device: the
// user-assigned name, else the marketing model name, else the raw model id.
// Empty for legacy entries (no manifest) — deviceRow falls back to the masked
// key.
func deviceDisplayName(d device) string {
	switch {
	case d.Name != "":
		return d.Name
	case d.ModelDisplay != "":
		return d.ModelDisplay
	case d.ModelID != "":
		return d.ModelID
	}
	return ""
}

// deviceMetaLine joins the platform and OS (name + version) a device reported,
// e.g. "macos · macOS 15.0". Empty when the device reported neither — deviceRow
// then renders the bare registration time.
func deviceMetaLine(d device) string {
	var parts []string
	if d.Platform != "" {
		parts = append(parts, d.Platform)
	}
	if os := strings.TrimSpace(d.OSName + " " + d.OSVersion); os != "" {
		parts = append(parts, os)
	}
	return strings.Join(parts, " · ")
}
