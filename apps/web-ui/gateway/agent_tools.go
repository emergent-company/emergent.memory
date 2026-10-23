package main

import (
	"path"
	"strings"
)

// The object-merge flow runs as an agent chat, so the agent it is handed to
// must actually be able to do the work. These are the canonical built-in tool
// names (apps/server/domain/agentcompat) that flow needs.
var (
	// mergeWriteAnyTools: at least one is required — create the merged object,
	// or update the surviving target in place.
	mergeWriteAnyTools = []string{"entity-create", "entity-update"}
	// mergeRequiredTools: all are required.
	mergeRequiredTools = []string{"entity-delete", "entity-edges-get", "relationship-create"}
	// mergeRecommendedTools: never gating, but reported by agentToolEligibility
	// so the settings panel can flag an otherwise-eligible agent that cannot
	// clean up stale edges.
	mergeRecommendedTools = []string{"relationship-delete"}
)

// agentToolEligibility reports whether def can run the object-merge flow.
// missing lists the unmet requirements (human-readable; the write requirement
// is reported as "entity-create or entity-update"); recommended lists the
// non-gating tools the agent does not grant. A nil def is ineligible.
func agentToolEligibility(def *AgentDefinition) (missing, recommended []string) {
	if def == nil {
		return []string{"agent definition unavailable"}, nil
	}
	hasWrite := false
	for _, name := range mergeWriteAnyTools {
		if agentToolGranted(def, name) {
			hasWrite = true
			break
		}
	}
	if !hasWrite {
		missing = append(missing, strings.Join(mergeWriteAnyTools, " or "))
	}
	for _, name := range mergeRequiredTools {
		if !agentToolGranted(def, name) {
			missing = append(missing, name)
		}
	}
	for _, name := range mergeRecommendedTools {
		if !agentToolGranted(def, name) {
			recommended = append(recommended, name)
		}
	}
	return missing, recommended
}

// agentToolGranted reports whether def grants toolName. It mirrors the
// server's ToolPool/executor semantics closely enough to gate a UI action:
// the whitelist matches exact names, path.Match globs, and "*"; an empty/nil
// whitelist grants nothing (the server denies all tools for it); banned tools
// (exact, plus globs treated conservatively) subtract; a disabled per-tool or
// per-group policy subtracts; and a "deny" default policy subtracts anything
// without an explicit or group policy.
func agentToolGranted(def *AgentDefinition, toolName string) bool {
	if def == nil || len(def.Tools) == 0 {
		return false
	}
	if !toolWhitelistMatches(def.Tools, toolName) {
		return false
	}
	if bannedToolMatches(def.BannedTools, toolName) {
		return false
	}
	return !agentToolBlockedByPolicy(def, toolName)
}

// agentToolBlockedByPolicy resolves the tool's effective policy: explicit entry
// wins, else the group entry for the group that lists the tool, else the
// agent's default policy. The gateway cannot import the server's toolgroups
// package, so group membership comes from the read-only ToolGroups the server
// returns on GET; when it is absent only explicit/default policies apply.
func agentToolBlockedByPolicy(def *AgentDefinition, toolName string) bool {
	if p, ok := def.ToolPolicies[toolName]; ok {
		return p.Disabled
	}
	if group := mergeToolGroupID(def, toolName); group != "" && group != toolGroupOtherID {
		if p, ok := def.ToolPolicies[toolGroupPolicyPrefix+group]; ok {
			return p.Disabled
		}
	}
	return strings.EqualFold(strings.TrimSpace(def.DefaultToolPolicy), "deny")
}

// mergeToolGroupID returns the ID of the capability group whose membership lists
// toolName, or "" when the definition carries no group catalog. It reads the
// server-computed ToolGroups on the definition, avoiding a duplicate of the
// server's toolgroups mapping. Callers must ignore the display-only "other"
// group: the server never treats it as a policy source.
func mergeToolGroupID(def *AgentDefinition, toolName string) string {
	for _, g := range def.ToolGroups {
		for _, t := range g.Tools {
			if t == toolName {
				return g.ID
			}
		}
	}
	return ""
}

// toolWhitelistMatches reports whether any whitelist pattern grants toolName.
func toolWhitelistMatches(patterns []string, toolName string) bool {
	for _, p := range patterns {
		if p == "*" || p == toolName {
			return true
		}
		if !isToolGlobPattern(p) {
			continue
		}
		if ok, err := path.Match(p, toolName); err == nil && ok {
			return true
		}
	}
	return false
}

// bannedToolMatches reports whether toolName is banned. Banned entries are
// exact names on the server; globs are also honoured here so a user who bans a
// pattern is never told an agent is merge-capable on the strength of a tool the
// server may still withhold.
func bannedToolMatches(patterns []string, toolName string) bool {
	for _, p := range patterns {
		if p == toolName {
			return true
		}
		if !isToolGlobPattern(p) {
			continue
		}
		if ok, err := path.Match(p, toolName); err == nil && ok {
			return true
		}
	}
	return false
}

// isToolGlobPattern reports whether s contains glob metacharacters.
func isToolGlobPattern(s string) bool {
	return strings.ContainsAny(s, "*?[")
}

// mergeCapabilityRequirement is the human-readable list of tools a merge agent
// needs, shared by the settings hint, the save-rejection toast, and the
// dead-end error on the merge deep link.
const mergeCapabilityRequirement = "entity-create or entity-update, entity-delete, entity-edges-get, and relationship-create"
