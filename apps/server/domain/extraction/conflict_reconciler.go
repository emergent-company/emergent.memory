package extraction

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"

	"github.com/google/uuid"
	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"

	"github.com/emergent-company/emergent.memory/domain/graph"
	"github.com/emergent-company/emergent.memory/domain/modelconfig"
	"github.com/emergent-company/emergent.memory/pkg/adk"
)

// reconcileMaxOutputTokens bounds one reconcile model call. The prompt carries
// only the residual conflicting fields of one object, so a modest budget is
// more than enough and caps worst-case spend.
const reconcileMaxOutputTokens int32 = 2048

// modelFactory is the slice of the ADK model factory the reconciler needs.
// Implemented by *adk.ModelFactory; faked in tests.
type modelFactory interface {
	CreateModelWithName(ctx context.Context, modelName string) (model.LLM, error)
}

// generativeModelResolver resolves a project's effective generative model name
// for the reconcile path. Implemented by *modelconfig.Service.
type generativeModelResolver interface {
	ResolveGenerativeModel(ctx context.Context, projectID uuid.UUID) (string, modelconfig.ModelSource, error)
}

// conflictReconciler is the agents-backed implementation of
// graph.ConflictReconciler. It reuses the extraction generative client (the
// ADK model factory) with a small COMBINE prompt, and caches resolutions keyed
// by a deterministic hash of the conflict set so re-runs are stable.
type conflictReconciler struct {
	modelFactory modelFactory
	resolver     generativeModelResolver
	log          *slog.Logger

	cacheMu sync.RWMutex
	cache   map[string]reconcileCacheEntry
}

type reconcileCacheEntry struct {
	values map[string]any
	model  string
}

// NewConflictReconciler builds the agents-backed reconcile resolver. Passing a
// nil resolver or model factory is allowed (Reconcile then always errors and
// the graph merge falls back to preserve_target).
func NewConflictReconciler(modelFactory modelFactory, resolver generativeModelResolver, log *slog.Logger) graph.ConflictReconciler {
	if log == nil {
		log = slog.Default()
	}
	return &conflictReconciler{
		modelFactory: modelFactory,
		resolver:     resolver,
		log:          log,
		cache:        make(map[string]reconcileCacheEntry),
	}
}

// Reconcile combines the residual conflicting values of one object. It returns
// the resolved value per key and the model name used (for the merge audit
// marker), or an error to fall back to preserve_target.
func (r *conflictReconciler) Reconcile(ctx context.Context, req graph.ConflictReconcileRequest) (map[string]any, string, error) {
	key := r.cacheKey(req)

	r.cacheMu.RLock()
	if hit, ok := r.cache[key]; ok {
		r.cacheMu.RUnlock()
		return cloneValues(hit.values), hit.model, nil
	}
	r.cacheMu.RUnlock()

	// Model resolution and credential lookups are keyed on the project in the
	// context; pin it explicitly so the reconciler works from the auto-merge
	// worker (a background context) as well as from request handlers.
	ctx = adk.WithProjectID(ctx, req.ProjectID.String())

	modelName, _, err := r.resolver.ResolveGenerativeModel(ctx, req.ProjectID)
	if err != nil {
		return nil, "", fmt.Errorf("resolve generative model: %w", err)
	}
	if modelName == "" {
		return nil, "", fmt.Errorf("no generative model configured for project")
	}

	llm, err := r.modelFactory.CreateModelWithName(ctx, modelName)
	if err != nil {
		return nil, "", fmt.Errorf("create model: %w", err)
	}

	prompt := buildReconcilePrompt(req)
	raw, err := r.completeJSON(ctx, llm, prompt)
	if err != nil {
		return nil, "", err
	}
	values, err := parseReconcileResponse(raw, req.Conflicts)
	if err != nil {
		return nil, "", err
	}

	r.cacheMu.Lock()
	r.cache[key] = reconcileCacheEntry{values: cloneValues(values), model: modelName}
	r.cacheMu.Unlock()

	return values, modelName, nil
}

// cacheKey hashes the reconciliation inputs so identical (type, key, values,
// schema) conflict sets resolve identically across re-runs. The property defs
// stand in for the schema generation.
func (r *conflictReconciler) cacheKey(req graph.ConflictReconcileRequest) string {
	h := sha256.New()

	h.Write([]byte(req.ProjectID.String()))
	h.Write([]byte{0})
	h.Write([]byte(req.TypeName))
	h.Write([]byte{0})

	keys := make([]string, 0, len(req.Conflicts))
	for k := range req.Conflicts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		v := req.Conflicts[k]
		h.Write([]byte(k))
		h.Write([]byte{0})
		sb, _ := json.Marshal(v.Source)
		h.Write(sb)
		h.Write([]byte{0})
		tb, _ := json.Marshal(v.Target)
		h.Write(tb)
		h.Write([]byte{0})
	}

	defKeys := make([]string, 0, len(req.PropertyDefs))
	for k := range req.PropertyDefs {
		defKeys = append(defKeys, k)
	}
	sort.Strings(defKeys)
	for _, k := range defKeys {
		db, _ := json.Marshal(req.PropertyDefs[k])
		h.Write([]byte(k))
		h.Write([]byte{0})
		h.Write(db)
		h.Write([]byte{0})
	}

	return hex.EncodeToString(h.Sum(nil))
}

// completeJSON sends the prompt and returns the model's text output, bounded to
// a JSON MIME type.
func (r *conflictReconciler) completeJSON(ctx context.Context, llm model.LLM, prompt string) (string, error) {
	req := &model.LLMRequest{
		Contents: []*genai.Content{
			genai.NewContentFromText(prompt, "user"),
		},
		Config: &genai.GenerateContentConfig{
			Temperature:      genai.Ptr[float32](0.0),
			MaxOutputTokens:  reconcileMaxOutputTokens,
			ResponseMIMEType: "application/json",
		},
	}
	var sb strings.Builder
	for resp, err := range llm.GenerateContent(ctx, req, false) {
		if err != nil {
			return "", fmt.Errorf("LLM call failed: %w", err)
		}
		if resp != nil && resp.Content != nil {
			for _, part := range resp.Content.Parts {
				if part.Text != "" {
					sb.WriteString(part.Text)
				}
			}
		}
	}
	return sb.String(), nil
}

// buildReconcilePrompt renders the conflict set for the model, asking it to
// COMBINE the two sides (prefer the richer/union of detail) and answer with a
// JSON object keyed by property name.
func buildReconcilePrompt(req graph.ConflictReconcileRequest) string {
	type side struct {
		Key    string `json:"key"`
		Source any    `json:"source"`
		Target any    `json:"target"`
		Type   string `json:"type,omitempty"`
		Help   string `json:"help,omitempty"`
	}
	keys := make([]string, 0, len(req.Conflicts))
	for k := range req.Conflicts {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	sides := make([]side, 0, len(keys))
	for _, k := range keys {
		s := side{Key: k, Source: req.Conflicts[k].Source, Target: req.Conflicts[k].Target}
		if def, ok := req.PropertyDefs[k]; ok {
			s.Type = def.Type
			s.Help = def.Description
		}
		sides = append(sides, s)
	}

	conflictsJSON, _ := json.MarshalIndent(sides, "", "  ")

	var b strings.Builder
	b.WriteString("You are reconciling two conflicting values for properties of a single entity.\n\n")
	b.WriteString("For each property below, COMBINE the source and target values into the single most complete, accurate value. Prefer the richer / more detailed union of information; never invent facts that are not present in either value. When the values are genuinely equal (ignoring case and whitespace), keep one.\n\n")
	fmt.Fprintf(&b, "Entity type: %s\n\n", req.TypeName)
	b.WriteString("Conflicting properties:\n")
	b.WriteString(string(conflictsJSON))
	b.WriteString("\n\n")
	b.WriteString("Respond with ONLY a single JSON object (no prose, no markdown code fences) whose keys are the property names listed above and whose values are the reconciled values — exactly one entry per listed property. For example: {\"job_title\": \"Waitress at Central Perk\"}.\n")
	return b.String()
}

// parseReconcileResponse decodes the model's JSON output into a key→value map.
// It accepts a bare object of key→value or a {"values": {…}} wrapper, and only
// keeps keys that were actually asked about.
func parseReconcileResponse(raw string, asked map[string]graph.ConflictValue) (map[string]any, error) {
	trimmed := extractJSONObject(raw)
	if trimmed == "" {
		return nil, fmt.Errorf("empty model response")
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(trimmed), &decoded); err != nil {
		return nil, fmt.Errorf("invalid JSON from reconciler: %w", err)
	}
	if wrapper, ok := decoded["values"].(map[string]any); ok && len(wrapper) > 0 {
		decoded = wrapper
	}
	out := make(map[string]any, len(decoded))
	for k, v := range decoded {
		if _, ok := asked[k]; !ok {
			continue
		}
		out[k] = v
	}
	return out, nil
}

// cloneValues deep-copies a resolved values map so cached entries cannot be
// mutated by a caller. Values are JSON-derived, so a marshal round-trip is the
// cheapest safe copy.
func cloneValues(in map[string]any) map[string]any {
	b, err := json.Marshal(in)
	if err != nil {
		out := make(map[string]any, len(in))
		for k, v := range in {
			out[k] = v
		}
		return out
	}
	var out map[string]any
	_ = json.Unmarshal(b, &out)
	return out
}

// compile-time check that the reconciler satisfies the graph interface.
var _ graph.ConflictReconciler = (*conflictReconciler)(nil)
