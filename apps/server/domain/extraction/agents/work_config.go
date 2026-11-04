package agents

// ObjectTypeWorkConfig is the per-object-type object-driven work configuration
// (P4). It is carried on an ObjectSchema and declares whether the type is
// board-enabled, its allowed work-status values, its operational pipeline
// skip-flags, and optional per-type failure-budget/retry overrides (P4.3).
//
// The zero value means "not board-enabled, unconstrained": a type without a
// boardEnabled flag behaves exactly as today, and its status values are
// unconstrained. The operational flags are required for board-enabled types,
// because every status transition creates a new version that would otherwise
// enqueue embeddings/FTS/extraction.
type ObjectTypeWorkConfig struct {
	// BoardEnabled marks the type as a work-item type: its status column is the
	// authoritative work state and it is surfaced on the Kanban projection.
	BoardEnabled bool `json:"boardEnabled,omitempty"`
	// AllowedStatuses is the declared set of work-status values. When non-empty
	// on a board-enabled type, a write setting a status outside the set is
	// rejected. Empty on a board-enabled type leaves status unconstrained.
	AllowedStatuses []string `json:"allowedStatuses,omitempty"`
	// SkipEmbeddings, when true, suppresses embedding enqueue on every version
	// write of this type (a board-enabled type transitions many times).
	SkipEmbeddings bool `json:"skipEmbeddings,omitempty"`
	// SkipExtraction, when true, excludes this type from the object extraction
	// pipeline (its objects are never produced by extraction).
	SkipExtraction bool `json:"skipExtraction,omitempty"`
	// ExcludeFromSearch, when true, excludes this type's objects from the
	// default search (FTS/vector/hybrid).
	ExcludeFromSearch bool `json:"excludeFromSearch,omitempty"`
	// FailureLimit overrides the agent-definition failure budget for work items
	// of this type (P4.3). Zero means "inherit the agent definition / default".
	FailureLimit int `json:"failureLimit,omitempty"`
	// RetryPolicy overrides the agent-definition retry backoff for work items of
	// this type (P4.3). Nil means "inherit the agent definition / default".
	RetryPolicy *RetryPolicy `json:"retryPolicy,omitempty"`
}

// RetryPolicy is the per-type retry backoff override (P4.3). It mirrors the
// agent-definition AgentRetryPolicy field shape so a type can override any
// subset; zero fields inherit the agent definition's values.
type RetryPolicy struct {
	MaxAttempts        int     `json:"maxAttempts,omitempty"`
	InitialIntervalMS  int     `json:"initialIntervalMs,omitempty"`
	BackoffCoefficient float64 `json:"backoffCoefficient,omitempty"`
	MaxIntervalMS      int     `json:"maxIntervalMs,omitempty"`
}

// ApplyConfig populates the work config from a raw schema definition map (the
// decoded per-type JSON object from object_type_schemas). Unknown or
// malformed keys are ignored so a schema pack without the new surface is
// unaffected.
func (w *ObjectTypeWorkConfig) ApplyConfig(m map[string]any) {
	if m == nil {
		return
	}
	if v, ok := m["boardEnabled"].(bool); ok {
		w.BoardEnabled = v
	}
	if raw, ok := m["allowedStatuses"].([]any); ok {
		for _, item := range raw {
			if s, ok := item.(string); ok {
				w.AllowedStatuses = append(w.AllowedStatuses, s)
			}
		}
	}
	if v, ok := m["skipEmbeddings"].(bool); ok {
		w.SkipEmbeddings = v
	}
	if v, ok := m["skipExtraction"].(bool); ok {
		w.SkipExtraction = v
	}
	if v, ok := m["excludeFromSearch"].(bool); ok {
		w.ExcludeFromSearch = v
	}
	if v, ok := m["failureLimit"].(float64); ok {
		w.FailureLimit = int(v)
	}
	if raw, ok := m["retryPolicy"].(map[string]any); ok {
		rp := &RetryPolicy{}
		if v, ok := raw["maxAttempts"].(float64); ok {
			rp.MaxAttempts = int(v)
		}
		if v, ok := raw["initialIntervalMs"].(float64); ok {
			rp.InitialIntervalMS = int(v)
		}
		if v, ok := raw["backoffCoefficient"].(float64); ok {
			rp.BackoffCoefficient = v
		}
		if v, ok := raw["maxIntervalMs"].(float64); ok {
			rp.MaxIntervalMS = int(v)
		}
		w.RetryPolicy = rp
	}
}
