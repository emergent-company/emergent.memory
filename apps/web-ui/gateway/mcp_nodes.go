package main

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/labstack/echo/v4"
)

// mcpNodeView is one rendered row of the External MCP nodes page: a currently
// connected session (Live) or a registry record that is no longer connected
// (offline, with LastSeen). The page lists live rows first (connected_at desc),
// then offline rows (last seen desc).
type mcpNodeView struct {
	InstanceID  string
	Version     string
	ToolCount   int
	Live        bool
	ConnectedAt time.Time // live rows only
	LastSeen    time.Time // offline rows only
}

// externalMCPNodesPageData is the payload for ExternalMCPNodesPage. Nodes is
// the unified live+offline list built by reconciling the registry with the
// backend's live sessions. A failed session-list fetch no longer blanks the
// page: LoadErr surfaces as a banner while the registry's offline rows still
// render. A failed per-node tools fetch degrades only the tools panel
// (ToolsErr), keeping the node list usable.
type externalMCPNodesPageData struct {
	settingsNavCommon
	Nodes []mcpNodeView
	// Selected is the relay node chosen via ?node=<instance_id>, nil when no
	// node is selected. Live is true when that node is currently connected
	// (tools come from the backend; otherwise from the stored snapshot).
	Selected *relayNode
	// SelectedLive reports whether the selected node is live.
	SelectedLive bool
	// NodeNotFound reports that ?node= named an instance id neither connected
	// nor present in the registry.
	NodeNotFound bool
	ToolsErr     error
	LoadErr      error
}

// sortRelaySessions orders sessions most-recently-connected first, with an
// InstanceID tiebreak so rendering is deterministic.
func sortRelaySessions(sessions []RelaySession) {
	sort.SliceStable(sessions, func(i, j int) bool {
		if !sessions[i].ConnectedAt.Equal(sessions[j].ConnectedAt) {
			return sessions[i].ConnectedAt.After(sessions[j].ConnectedAt)
		}
		return sessions[i].InstanceID < sessions[j].InstanceID
	})
}

// uiMCPNodes renders the External MCP nodes settings page. On every load it
// lazily reconciles the persisted registry with the backend's live sessions:
// each live session is upserted (refresh version/toolCount/tools, lastSeen =
// now, preserve firstSeen), then the page lists live nodes first (connected_at
// desc) followed by registry nodes no longer connected (last seen desc). The
// tool list is fetched once per live node while reconciling, so the snapshot
// cached in the registry is what offline nodes later show. When the live fetch
// fails the registry still renders (as offline) behind an error banner instead
// of blanking the page.
func (s *Server) uiMCPNodes(c echo.Context) error {
	ctx := c.Request().Context()
	data := externalMCPNodesPageData{}
	data.ProvidersMissing = s.projectHasNoProviders(ctx)

	registry, err := s.loadRelayRegistry(ctx)
	if err != nil {
		data.LoadErr = err
		registry = map[string]relayNodeRecord{}
	}

	now := time.Now().UTC()
	sessions, serr := s.memory.ListRelaySessions(ctx)
	data.LoadErr = errors.Join(data.LoadErr, serr)
	sortRelaySessions(sessions)

	live := make(map[string]RelaySession, len(sessions))
	liveTools := make(map[string][]RelayTool, len(sessions))
	liveToolsErr := make(map[string]error, len(sessions))
	for _, sess := range sessions {
		live[sess.InstanceID] = sess
		tools, terr := s.memory.GetRelaySessionTools(ctx, sess.InstanceID)
		rec := relayNodeRecord{
			InstanceID: sess.InstanceID,
			Version:    sess.Version,
			ToolCount:  sess.ToolCount,
			Tools:      tools,
			LastSeen:   now,
		}
		if terr != nil {
			liveToolsErr[sess.InstanceID] = terr
			// nil Tools leaves the stored snapshot untouched.
			rec.Tools = nil
		} else {
			liveTools[sess.InstanceID] = tools
		}
		if uerr := s.upsertRelayNode(ctx, rec); uerr != nil {
			data.LoadErr = errors.Join(data.LoadErr, uerr)
		}
		data.Nodes = append(data.Nodes, mcpNodeView{
			InstanceID:  sess.InstanceID,
			Version:     sess.Version,
			ToolCount:   sess.ToolCount,
			Live:        true,
			ConnectedAt: sess.ConnectedAt,
		})
	}

	offline := make([]relayNodeRecord, 0, len(registry))
	for id, rec := range registry {
		if _, ok := live[id]; ok {
			continue
		}
		offline = append(offline, rec)
	}
	sort.SliceStable(offline, func(i, j int) bool {
		if !offline[i].LastSeen.Equal(offline[j].LastSeen) {
			return offline[i].LastSeen.After(offline[j].LastSeen)
		}
		return offline[i].InstanceID < offline[j].InstanceID
	})
	for _, rec := range offline {
		data.Nodes = append(data.Nodes, mcpNodeView{
			InstanceID: rec.InstanceID,
			Version:    rec.Version,
			ToolCount:  rec.ToolCount,
			LastSeen:   rec.LastSeen,
		})
	}

	if sel := c.QueryParam("node"); sel != "" {
		if sess, isLive := live[sel]; isLive {
			data.SelectedLive = true
			node := &relayNode{Session: sess}
			if terr, ok := liveToolsErr[sel]; ok {
				data.ToolsErr = terr
			} else {
				node.Tools = liveTools[sel]
			}
			data.Selected = node
		} else if rec, ok := registry[sel]; ok {
			data.Selected = &relayNode{
				Session: RelaySession{
					InstanceID: rec.InstanceID,
					Version:    rec.Version,
					ToolCount:  rec.ToolCount,
				},
				Tools: rec.Tools,
			}
		} else {
			data.NodeNotFound = true
		}
	}
	return s.page(c, pageTitle("External MCP nodes"), ExternalMCPNodesPage(data))
}

// loadRelayNodes fetches the project's connected relay sessions and each
// session's tools for the agent-settings loader (the tool picker's relay
// groups). Best-effort and silent by design: any relay API failure degrades to
// no relay groups while the rest of the page stays intact — the dedicated
// /settings/mcp-nodes page is where the real error state surfaces. Sessions
// are few, so one tools round-trip per connected node stays bounded.
func (s *Server) loadRelayNodes(ctx context.Context) []relayNode {
	sessions, err := s.memory.ListRelaySessions(ctx)
	if err != nil {
		return nil
	}
	sortRelaySessions(sessions)
	nodes := make([]relayNode, 0, len(sessions))
	for _, sess := range sessions {
		node := relayNode{Session: sess}
		if tools, terr := s.memory.GetRelaySessionTools(ctx, sess.InstanceID); terr == nil {
			node.Tools = tools
		}
		nodes = append(nodes, node)
	}
	return nodes
}
