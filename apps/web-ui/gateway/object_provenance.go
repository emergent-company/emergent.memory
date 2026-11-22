package main

import (
	"context"
	"net/url"
	"strings"
	"time"

	"golang.org/x/sync/errgroup"
)

// --- per-version provenance for the object history timeline -----------------
//
// A version's write stamp is what makes the object history an audit trail rather
// than a diff log: every entry should say who wrote that version, how they were
// authenticated, and when. The server records the stamp on every version row
// (`apps/server/domain/graph/actor.go`: actor_type/actor_id, credential_kind,
// run_id/root_run_id, data_class) and returns it on the history read path; these
// helpers turn it into display strings and never invent an attribution the
// server did not report.

// objectVersionProvenance is the resolved provenance line for one version of an
// object. Every field degrades to "" — a version written before the write stamp
// existed renders no line rather than a guessed actor.
type objectVersionProvenance struct {
	// Actor is the display label: an agent's name, a project member's name, or
	// "System" for background work. "" when the version recorded no actor.
	Actor string
	// ActorKind is the raw actor_type ("user" | "agent" | "system"), used to
	// render the distinguishing badge. "" when absent or unrecognised.
	ActorKind string
	// ActorHref links an agent actor to its definition page; "" otherwise.
	ActorHref string
	// ActorTitle is the audit tooltip: the recorded (actor_type, actor_id) pair,
	// never a name the server did not send.
	ActorTitle string
	// ActorInitials is the avatar fallback for a resolved user actor: the first
	// letters of the resolved name. "" when the label is the unresolved
	// "<Type> · <short id>" fallback — a circle reading "U" for "User · f0364…"
	// would decorate the placeholder instead of the person.
	ActorInitials string
	// ActorAvatar is a servable avatar URL for the resolved actor, when one can
	// be served at all: only the signed-in user's photo is (the gateway's avatar
	// proxy is session-scoped), so this is "" for anyone else and the initials
	// carry the identity instead.
	ActorAvatar string
	// How names the credential the write used ("API key", "agent MCP link",
	// "web session"); "" when the stamp carries none (background writes).
	How string
	// Run is a short handle for the agent run that produced the write; "" when
	// the version was not written inside a run.
	Run string
	// RunTitle carries the full run and root-run ids for the tooltip.
	RunTitle string
	// RunHref links the run chip to that run's transcript page (the gateway's
	// /runs/:runId). "" when the version carries no run, so the chip stays plain
	// text rather than a dead link.
	RunHref string
	// OrderedBy is the person who ordered the run that wrote this version — the
	// human authority behind an agent's write, rendered as "on <name>'s order".
	// "" unless the actor is an agent, the run recorded an ordering user, and
	// that user resolved to a member of the project's org. Never inferred from
	// the actor or from trigger metadata.
	OrderedBy         string
	OrderedByInitials string
	OrderedByAvatar   string
	// OrderedByTitle is the audit tooltip: the recorded ordering user id.
	OrderedByTitle string
	// When is the absolute local timestamp; WhenTitle adds the relative age and
	// the raw server value.
	When      string
	WhenTitle string
}

// objectProvenanceOf resolves one version's provenance line. identities is the
// project's resolved actor identities (see objectActorIdentities); an
// unresolved actor still renders — as "<Type> · <short id>" — rather than
// disappearing, because an unknown writer is still a writer.
func objectProvenanceOf(v GraphObject, identities []objectActorOption, orderers map[string]string) objectVersionProvenance {
	var p objectVersionProvenance
	if v.ActorType != "" {
		// The actor type is a closed set (user | agent | system). An unknown
		// value means a newer server than this gateway: claim nothing rather
		// than leaking the raw value into the row.
		p.ActorKind = normalizeObjectActorType(v.ActorType)
		switch p.ActorKind {
		case "system":
			// Background work has no actor id; the pair's label helper would read
			// the missing id as "Any actor" (a filter concept, not a writer).
			p.Actor = "System"
		case "agent", "user":
			// A recorded type with no id is still an attribution: name the type.
			// The pair's label helper reads an empty id as "Any actor" (a filter
			// concept, not a writer), so it is bypassed for that case.
			if v.ActorID == "" {
				p.Actor = actorTypeLabel(v.ActorType)
			} else if opt, ok := actorOptionOf(identities, v.ActorType, v.ActorID); ok {
				// Resolved: the person's (or agent's) own name, plus the avatar a
				// user actor is drawn as. Both only travel with a real identity —
				// the fallback label below is a placeholder, not somebody to
				// paint a face on.
				p.Actor = opt.Label
				if p.ActorKind == "user" {
					p.ActorInitials = initials(opt.Label)
					p.ActorAvatar = opt.AvatarURL
				}
			} else {
				p.Actor = actorTypeLabel(v.ActorType) + " · " + shortActorID(v.ActorID)
			}
		}
		if p.ActorKind == "agent" && v.ActorID != "" {
			p.ActorHref = "/agents/" + url.PathEscape(v.ActorID)
		}
		p.ActorTitle = actorProvenanceTitle(v.ActorType, v.ActorID)
	}
	p.How = credentialKindLabel(v.CredentialKind)
	if v.RunID != "" {
		p.Run = "run " + shortRunID(v.RunID)
		p.RunHref = "/runs/" + url.PathEscape(v.RunID)
		p.RunTitle = "agent run " + v.RunID
		if v.RootRunID != "" && v.RootRunID != v.RunID {
			p.RunTitle += " · root run " + v.RootRunID
		}
	}
	// An agent's write is the orderer's write: the agent carried out work a
	// person asked for. Name that person when the run recorded one — the row
	// column is the authority, and it is only ever written from an
	// authenticated user at run creation.
	if p.ActorKind == "agent" && v.RunID != "" {
		if uid := strings.TrimSpace(orderers[v.RunID]); uid != "" {
			if opt, ok := actorOptionOf(identities, "user", uid); ok {
				p.OrderedBy = opt.Label
				p.OrderedByInitials = initials(opt.Label)
				p.OrderedByAvatar = opt.AvatarURL
				p.OrderedByTitle = "Ordered by " + opt.Label + " (" + uid + ")"
			}
		}
	}
	p.When, p.WhenTitle = objectVersionWhen(v.CreatedAt)
	return p
}

// shortRunID is a compact, human-scannable handle for a run id: a UUID's first
// dash group (8 hex chars — "0193f2a1", not the misleading "0193f2a1-0"), and
// the leading characters of any other id shape. The full id stays in the tooltip.
func shortRunID(id string) string {
	if i := strings.Index(id, "-"); i >= 8 {
		return id[:i]
	}
	return shortID(id)
}

// actorProvenanceTitle is the hover text for a recorded actor: the raw stamp,
// so the timeline stays auditable without printing UUIDs in the row.
func actorProvenanceTitle(actorType, actorID string) string {
	switch actorType {
	case "user":
		if actorID == "" {
			return "Recorded actor: user"
		}
		return "Recorded actor: user " + actorID
	case "agent":
		if actorID == "" {
			return "Recorded actor: agent"
		}
		return "Recorded actor: agent " + actorID
	case "system":
		return "Recorded actor: system (background work such as extraction or indexing)"
	default:
		return ""
	}
}

// objectVersionWhen formats a version's creation time for the timeline: an
// absolute local stamp (a history spans months, so "3 days ago" stops being
// precise) with the relative age and the raw server value as the tooltip. An
// unparseable value passes through unchanged, as the rest of the UI does.
func objectVersionWhen(iso string) (text, title string) {
	if iso == "" {
		return "", ""
	}
	t, err := time.Parse(time.RFC3339, iso)
	if err != nil {
		return iso, ""
	}
	text = t.Local().Format("2 Jan 2006, 15:04")
	// relTime falls back to a bare date for anything older than a week, which
	// the absolute text already carries — don't repeat it in the tooltip.
	if rel := relTime(iso); rel != "" && rel != "—" && rel != t.Format("2006-01-02") {
		return text, rel + " · " + iso
	}
	return text, "Recorded " + iso
}

// objectPossessive forms a display name's possessive for a provenance line
// ("on Maciek Kucharz's order"). A name already ending in s takes a bare
// apostrophe, which is the form readers expect of a surname like Davies.
func objectPossessive(name string) string {
	if name == "" {
		return ""
	}
	if strings.HasSuffix(name, "s") || strings.HasSuffix(name, "S") {
		return name + "'"
	}
	return name + "'s"
}

// objectRunOrderers resolves who ordered each distinct run among versions, in
// one project-scoped read, so a page of history costs one extra request rather
// than one per entry. Best-effort by design: a failure yields nil, and every line
// then renders exactly as it did before ordering attribution existed. A missing
// read must never invent an orderer, and must never blank the provenance line.
func (s *Server) objectRunOrderers(ctx context.Context, versions []GraphObject) map[string]string {
	ids := make([]string, 0, len(versions))
	seen := make(map[string]struct{}, len(versions))
	for _, v := range versions {
		if v.ActorType != "agent" || v.RunID == "" {
			continue
		}
		if _, dup := seen[v.RunID]; dup {
			continue
		}
		seen[v.RunID] = struct{}{}
		ids = append(ids, v.RunID)
	}
	if len(ids) == 0 {
		return nil
	}
	orderers, err := s.memory.GetRunOrderers(ctx, ids)
	if err != nil {
		captureError(err)
		return nil
	}
	return orderers
}

// objectProvenanceInputs resolves everything a provenance line needs for a set of
// versions: the actor identities, and who ordered each run. The two lookups are
// coupled because an orderer is a person who need not be an actor on any of these
// versions — asking only for the actor kinds would leave every "on <name>'s
// order" unresolved.
func (s *Server) objectProvenanceInputs(ctx context.Context, versions []GraphObject) ([]objectActorOption, map[string]string) {
	orderers := s.objectRunOrderers(ctx, versions)
	wantUsers, wantAgents := objectActorKinds(versions)
	if len(orderers) > 0 {
		wantUsers = true
	}
	if !wantUsers && !wantAgents {
		return nil, orderers
	}
	return s.objectActorIdentities(ctx, wantUsers, wantAgents), orderers
}

// objectActorKinds reports which identity groups a provenance line needs to
// resolve the version actors themselves.
func objectActorKinds(versions []GraphObject) (wantUsers, wantAgents bool) {
	for _, v := range versions {
		switch v.ActorType {
		case "user":
			wantUsers = true
		case "agent":
			wantAgents = true
		}
	}
	return wantUsers, wantAgents
}

// objectActorIdentities fetches the project's selectable actors, optionally
// scoped to one group: people as actor_type "user", agent definitions as
// actor_type "agent". Both fetches are best-effort and run concurrently — a
// failure drops that group rather than blanking the surface, and the caller's
// label falls back to "<Type> · <short id>".
//
// People come from the ORGANIZATION's member list, not the project's, because
// org membership is what grants access to a project's objects: the server's
// write guard admits any member of the project's owning org
// (`pkg/auth/project_membership.go`), and a writer need not hold a
// `kb.project_memberships` row. Resolving from project members alone therefore
// left ordinary writes attributed to "<Type> · <short id>" — the writer is a
// real, named colleague who simply is not on the project roster. A request
// without a session org (an API-key path) keeps the project-member list, which
// is the only roster that token can see.
func (s *Server) objectActorIdentities(ctx context.Context, wantUsers, wantAgents bool) []objectActorOption {
	var (
		orgMembers []OrgMemberDto
		members    []ProjectMemberDto
		agents     []AgentDefinitionSummary
		memberErr  error
		agentErr   error
	)
	// The signed-in user's own photo is the one avatar a page can actually
	// serve: /api/user/avatar is session-scoped, so it is attached only to that
	// user's option and everyone else falls back to initials.
	orgID, selfID, selfLabel, selfAvatar := "", "", "", ""
	if sc, ok := sessionContextFrom(ctx); ok {
		orgID, selfID = sc.OrgID, sc.Sub
		selfAvatar = resolveSelfAvatar(sc.AvatarOverrideURL, sc.Picture)
		// The claims are the one place the signed-in user's name is guaranteed:
		// a roster row can exist with no display name and no email (an
		// autoprovisioned profile), and then the write stamp has nothing to
		// resolve to but an id.
		if name := strings.TrimSpace(sc.Name); name != "" {
			selfLabel = name
		} else {
			selfLabel = strings.TrimSpace(sc.Email)
		}
	}
	var g errgroup.Group
	if wantUsers {
		g.Go(func() error {
			if orgID == "" {
				members, memberErr = s.memory.ListMembers(ctx)
				return nil
			}
			orgMembers, memberErr = s.memory.ListOrgMembers(ctx, orgID)
			if memberErr != nil {
				// An org directory that answers nothing still leaves the project
				// roster to name people by, so fall back rather than losing the
				// filter's user options entirely.
				orgMembers = nil
				members, memberErr = s.memory.ListMembers(ctx)
			}
			return nil
		})
	}
	if wantAgents {
		g.Go(func() error { agents, agentErr = s.memory.ListAgentDefinitions(ctx); return nil })
	}
	_ = g.Wait()
	captureError(memberErr)
	captureError(agentErr)

	options := make([]objectActorOption, 0, len(orgMembers)+len(members)+len(agents))
	for _, m := range orgMembers {
		avatar := ""
		if m.ID == selfID {
			avatar = selfAvatar
		}
		options = append(options, objectActorOption{
			Type:      "user",
			ID:        m.ID,
			Label:     orgMemberDisplayName(m),
			AvatarURL: avatar,
		})
	}
	for _, m := range members {
		avatar := m.AvatarURL
		if m.ID == selfID && avatar == "" {
			avatar = selfAvatar
		}
		options = append(options, objectActorOption{
			Type:      "user",
			ID:        m.ID,
			Label:     memberActorLabel(m),
			AvatarURL: avatar,
		})
	}
	for _, a := range agents {
		options = append(options, objectActorOption{
			Type:  "agent",
			ID:    a.ID,
			Label: a.Name,
			Icon:  agentIcon(a.UIConfig),
			Color: agentColor(a.UIConfig),
		})
	}
	return selfActorOption(options, selfID, selfLabel, selfAvatar)
}

// selfActorOption makes sure the signed-in user is nameable. A roster entry can
// exist with nothing in it — an autoprovisioned profile with no display name, no
// name parts and no email — and then every one of that user's own writes reads
// as "<Type> · <short id>": the least scrutable possible attribution, on the
// one writer whose name the page knows for certain, because it is on the ID
// token. So an option for the caller with an empty or id-shaped label is
// replaced by the claims' name, and a roster that does not list the caller at
// all still gets one row.
func selfActorOption(options []objectActorOption, selfID, selfLabel, selfAvatar string) []objectActorOption {
	if selfID == "" {
		return options
	}
	for i, o := range options {
		if o.Type != "user" || o.ID != selfID {
			continue
		}
		if selfLabel != "" && (strings.TrimSpace(o.Label) == "" || o.Label == o.ID) {
			options[i].Label = selfLabel
		}
		if options[i].AvatarURL == "" {
			options[i].AvatarURL = selfAvatar
		}
		return options
	}
	if selfLabel == "" {
		return options
	}
	return append(options, objectActorOption{Type: "user", ID: selfID, Label: selfLabel, AvatarURL: selfAvatar})
}
