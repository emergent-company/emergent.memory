// Package agentexec holds agent execution-lifecycle sentinels shared between the
// agents executor and consumers that must detect them without importing the
// agents domain. It exists to break a would-be import cycle: domain/agents
// imports domain/mcp (see domain/agents/module.go), so domain/mcp cannot import
// domain/agents, yet both must agree on the AGENT_EXECUTION_ENABLED kill-switch
// message.
package agentexec

import "errors"

// ErrExecutionDisabled is returned when the AGENT_EXECUTION_ENABLED kill switch
// is off. It is the single source of truth for the canonical message text;
// domain/agents aliases it and domain/mcp matches against .Error().
var ErrExecutionDisabled = errors.New("agent execution is disabled system-wide")
