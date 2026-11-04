package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/livekit/protocol/auth"
	"github.com/livekit/protocol/livekit"
)

type tokenRequest struct {
	Identity       string `json:"identity"`
	Room           string `json:"room"`
	Agent          string `json:"agent"`
	Client         string `json:"client"`
	ConversationID string `json:"conversation_id"`
}

// errVoiceSessionRequired reports a voice request with no authenticated
// session. The removed key/device-mode fallback makes this a first-class auth
// failure (401), not an upstream error.
var errVoiceSessionRequired = errors.New("voice requires an authenticated session")

// errVoiceAgentDisabled reports a voice request for an agent whose definition
// is disabled; no worker may serve it.
var errVoiceAgentDisabled = errors.New("voice agent is disabled")

// mintToken mints a short-lived room-join JWT for a LiveKit client (iOS app or
// browser), with agent dispatch baked into the room config. The room is
// optional: when omitted a fresh per-token room `<agent>-<client>-<32hex>` is
// derived (client defaults to "web") so each client session gets its own
// agent-dispatch room. Fail-closed when unconfigured.
func (s *Server) mintToken(c echo.Context) error {
	if s.cfg.LiveKitAPIKey == "" || s.cfg.LiveKitAPISecret == "" {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "token service not configured"})
	}
	var in tokenRequest
	if err := c.Bind(&in); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
	}
	if in.Identity == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "identity required"})
	}
	agent := in.Agent
	if agent == "" {
		agent = s.cfg.DefaultAgent
	}
	client := in.Client
	if client == "" {
		client = "web"
	}
	room := in.Room
	if room == "" {
		room = agent + "-" + client + "-" + randomHex(16)
	}
	ok, err := s.roomAllowed(c.Request().Context(), room)
	if err != nil {
		captureError(err)
		return c.JSON(http.StatusBadGateway, map[string]string{"error": "memory service unavailable"})
	}
	if !ok {
		return c.JSON(http.StatusForbidden, map[string]string{"error": "room not allowed"})
	}

	// Resolve the per-session voice binding (project/org, agent-definition id,
	// language, and a project-scoped Memory token) and stash it server-side so
	// the bridge worker can fetch it without any credential in the join JWT.
	binding, enabled, err := s.voiceBindingFor(c.Request().Context(), agent, room)
	if err != nil {
		if errors.Is(err, errVoiceSessionRequired) {
			return c.JSON(http.StatusUnauthorized, map[string]string{"error": err.Error()})
		}
		if errors.Is(err, errVoiceAgentDisabled) {
			if s.supervisor != nil {
				s.supervisor.StopWorker(agent)
			}
			return c.JSON(http.StatusForbidden, map[string]string{"error": err.Error()})
		}
		captureError(err)
		return c.JSON(http.StatusBadGateway, map[string]string{"error": "memory service unavailable"})
	}
	if binding == nil {
		return c.JSON(http.StatusForbidden, map[string]string{"error": "agent not found in active project"})
	}
	if s.bindings != nil {
		s.bindings.Set(room, agent, *binding)
	}
	// Spawn the agent's bridge worker on demand. Only enabled agents get a warm
	// worker; voiceBindingFor rejects disabled agents before reaching here.
	if enabled && s.supervisor != nil {
		s.supervisor.EnsureWorker(agent)
	}

	at := auth.NewAccessToken(s.cfg.LiveKitAPIKey, s.cfg.LiveKitAPISecret)
	at.SetIdentity(in.Identity)
	at.SetName("Memory " + client + " client")
	at.SetVideoGrant(&auth.VideoGrant{RoomJoin: true, Room: room})
	dispatch := &livekit.RoomAgentDispatch{AgentName: agent}
	if in.ConversationID != "" {
		dispatch.Metadata = in.ConversationID
	}
	at.SetRoomConfig(&livekit.RoomConfiguration{
		Agents: []*livekit.RoomAgentDispatch{dispatch},
	})
	jwt, err := at.ToJWT()
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "token minting failed: " + err.Error()})
	}

	serverURL := s.cfg.LiveKitPublicURL
	if serverURL == "" {
		serverURL = s.cfg.LiveKitURL
	}
	return c.JSON(http.StatusOK, map[string]string{
		"server_url":        serverURL,
		"participant_token": jwt,
	})
}

// randomHex returns n random bytes as lowercase hex (2n chars), using
// crypto/rand. Panics only if the OS CSPRNG fails (unrecoverable).
func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic("randomHex: crypto/rand failed: " + err.Error())
	}
	return hex.EncodeToString(b)
}

// roomAllowed enforces the room allow-list: TOKEN_ALLOWED_ROOMS (exact) if set,
// else a room prefixed by a known agent name, else the legacy "memory-" prefix.
func (s *Server) roomAllowed(ctx context.Context, room string) (bool, error) {
	if s.cfg.TokenAllowedRooms != "" {
		for _, r := range strings.Split(s.cfg.TokenAllowedRooms, ",") {
			if strings.TrimSpace(r) == room {
				return true, nil
			}
		}
		return false, nil
	}
	agents, err := s.memory.ListAgentDefinitions(ctx)
	if err != nil {
		return false, err
	}
	for _, a := range agents {
		if strings.HasPrefix(room, a.Name+"-") {
			return true, nil
		}
	}
	return strings.HasPrefix(room, "memory-"), nil
}

// resolveVoiceAgent resolves an agent name to its definition id, language code
// and enabled flag in the request's active project. ok is false when the agent
// is not in that project.
func (s *Server) resolveVoiceAgent(ctx context.Context, name string) (defID, lang string, enabled, ok bool) {
	agents, err := s.memory.ListAgentDefinitions(ctx)
	if err != nil {
		return "", "", false, false
	}
	var summary *AgentDefinitionSummary
	for i := range agents {
		if agents[i].Name == name {
			summary = &agents[i]
			break
		}
	}
	if summary == nil {
		return "", "", false, false
	}
	def, err := s.memory.GetAgentDefinition(ctx, summary.ID)
	if err != nil || def == nil {
		return summary.ID, "", summary.Enabled, true
	}
	return summary.ID, agentLanguageCode(agentLanguageValue(def)), summary.Enabled, true
}

// voiceBindingFor builds the per-room voice binding for a minted token from the
// signed-in user's active project/org, and mints a short-lived project-scoped
// Memory token for the worker. Voice requires a session OR a scoped device
// credential: there is no key/device-mode static-token fallback. For a device
// caller the device credential itself is proxied verbatim as the worker token
// (the device surface guard already forbids a device credential from minting a
// new token), so no mint is attempted. A nil binding with a nil error means the
// agent is not present in the active project; the enabled flag reports whether
// the agent should get a warm bridge worker.
func (s *Server) voiceBindingFor(ctx context.Context, agent, room string) (*voiceBinding, bool, error) {
	sc, ok := sessionContextFrom(ctx)
	if !ok || sc.ProjectID == "" {
		return nil, false, errVoiceSessionRequired
	}
	defID, lang, enabled, ok := s.resolveVoiceAgent(ctx, agent)
	if !ok {
		return nil, false, nil
	}
	if !enabled {
		return nil, false, errVoiceAgentDisabled
	}
	token := sc.Token
	if !sc.Device {
		tok, err := s.memory.CreateAPIToken(ctx, "voice-"+room+"-"+randomHex(4), []string{"chat:use"})
		if err != nil {
			return nil, false, err
		}
		token = tok.Token
	}
	return &voiceBinding{
		ProjectID:         sc.ProjectID,
		OrgID:             sc.OrgID,
		AgentDefinitionID: defID,
		Language:          lang,
		Token:             token,
	}, enabled, nil
}
