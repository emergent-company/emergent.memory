// Package extraction provides object extraction job processing.
package extraction

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"time"

	"google.golang.org/adk/model"
	"google.golang.org/genai"

	"github.com/emergent-company/emergent.memory/pkg/adk"
	"github.com/emergent-company/emergent.memory/pkg/logger"
)

// ClassificationResult holds the result of document domain classification.
type ClassificationResult struct {
	// DomainName is the matched domain / schema-pack name. Empty = no match.
	DomainName string
	// Confidence is the classification confidence (0–1).
	Confidence float32
	// MatchedSchemaID is the ID of the best-matching schema pack.
	MatchedSchemaID *string
	// Signals contains the raw classification signals for write-back.
	Signals ClassificationSignals
	// DomainGuidance is schema-pack domain context to inject into extraction.
	DomainGuidance string
}

// InstalledSchemaSummary is a lightweight view of a project schema pack used
// during classification.
type InstalledSchemaSummary struct {
	ID          string
	Name        string
	Description string
	// Keywords are extracted from schema name + type names for heuristic matching.
	// Retained for fallback logging; primary classification uses Embedding.
	Keywords []string
	// Embedding is a pre-computed vector of the schema's domain description +
	// type hints. Used for vector-similarity classification (stage 1).
	Embedding []float32
	// ExtractionPrompts holds domain guidance written back by the discovery agent.
	ExtractionPrompts *SchemaExtractionPrompts
}

// DocumentClassifier classifies a document into a domain using a two-stage
// pipeline: vector similarity → LLM confirmation (only when ambiguous).
type DocumentClassifier struct {
	modelFactory     *adk.ModelFactory
	embeddingService EmbeddingService
	log              *slog.Logger
}

// NewDocumentClassifier creates a new DocumentClassifier.
// embeddingService may be nil — when nil or disabled, falls through to LLM only.
func NewDocumentClassifier(modelFactory *adk.ModelFactory, embeddingService EmbeddingService, log *slog.Logger) *DocumentClassifier {
	return &DocumentClassifier{
		modelFactory:     modelFactory,
		embeddingService: embeddingService,
		log:              log.With(logger.Scope("document-classifier")),
	}
}

// Classify attempts to match the document against the provided schema packs.
// Returns a zero-value ClassificationResult when no schema packs are installed.
//
// Pipeline:
//  1. Vector similarity  — cosine(doc_embedding, pack_embedding) ≥ 0.75 → match
//     0.55–0.75 → ambiguous, fall through to LLM
//     < 0.55    → no match (new domain)
//  2. LLM classification — used when vector is ambiguous or embeddings unavailable
func (c *DocumentClassifier) Classify(
	ctx context.Context,
	documentText string,
	schemaPacks []InstalledSchemaSummary,
) (ClassificationResult, error) {
	if len(schemaPacks) == 0 {
		return ClassificationResult{}, nil
	}

	// Stage 1: vector similarity (only when embedding service is available and
	// at least one pack has a pre-computed embedding).
	vectorResult, vectorAvailable := c.vectorMatch(ctx, documentText, schemaPacks)
	if vectorAvailable {
		if vectorResult != nil && vectorResult.Confidence >= 0.85 {
			vectorResult.Signals.ClassifiedAt = time.Now().UTC().Format(time.RFC3339)
			c.log.Info("domain classified via vector similarity",
				slog.String("domain", vectorResult.DomainName),
				slog.Float64("confidence", float64(vectorResult.Confidence)),
			)
			return *vectorResult, nil
		}
		if vectorResult == nil {
			// Best vector score < 0.55 — embeddings alone are inconclusive.
			// Fall through to LLM; schema embeddings are sparse text so low
			// cosine doesn't mean no match.
			c.log.Info("vector similarity: no strong match, falling through to LLM")
		} else {
			// 0.55 ≤ score < 0.85: ambiguous — fall through to LLM.
			c.log.Info("vector similarity ambiguous, falling through to LLM",
				slog.String("best_domain", vectorResult.DomainName),
				slog.Float64("confidence", float64(vectorResult.Confidence)),
			)
		}
	}

	// Stage 2: LLM classification.
	llmResult, err := c.llmClassify(ctx, documentText, schemaPacks)
	if err != nil {
		c.log.Warn("LLM classification failed", logger.Error(err))
		// Fall back to vector result if available (even if ambiguous).
		if vectorResult != nil {
			vectorResult.Signals.ClassifiedAt = time.Now().UTC().Format(time.RFC3339)
			return *vectorResult, nil
		}
		return ClassificationResult{}, nil
	}

	llmResult.Signals.ClassifiedAt = time.Now().UTC().Format(time.RFC3339)
	return llmResult, nil
}

// vectorMatch computes cosine similarity between the document and each schema
// pack's pre-computed embedding. Returns:
//   - (result, true)  when embedding service is available and packs have embeddings
//   - (nil, false)    when embeddings are unavailable (fall through to LLM)
//
// result is nil when the best similarity is below the ambiguous threshold (0.55).
// result has Confidence set to the cosine similarity score otherwise.
func (c *DocumentClassifier) vectorMatch(
	ctx context.Context,
	documentText string,
	packs []InstalledSchemaSummary,
) (*ClassificationResult, bool) {
	if c.embeddingService == nil || !c.embeddingService.IsEnabled() {
		return nil, false
	}

	// Check that at least one pack has an embedding.
	hasEmbeddings := false
	for _, p := range packs {
		if len(p.Embedding) > 0 {
			hasEmbeddings = true
			break
		}
	}
	if !hasEmbeddings {
		return nil, false
	}

	// Embed the document snippet (first 1500 chars — enough signal, cheap).
	snippet := documentText
	if len(snippet) > 1500 {
		snippet = snippet[:1500]
	}
	docEmb, err := c.embeddingService.EmbedQuery(ctx, snippet)
	if err != nil {
		c.log.Warn("vector classify: embed document failed", logger.Error(err))
		return nil, false
	}

	const ambiguousThreshold = 0.65

	bestScore := float32(-1)
	var bestPack *InstalledSchemaSummary
	for i := range packs {
		if len(packs[i].Embedding) == 0 {
			continue
		}
		score := cosineSimilarity(docEmb, packs[i].Embedding)
		if score > bestScore {
			bestScore = score
			bestPack = &packs[i]
		}
	}

	if bestPack == nil || bestScore < ambiguousThreshold {
		return nil, true // embeddings available but no match
	}

	result := &ClassificationResult{
		DomainName:      bestPack.Name,
		Confidence:      bestScore,
		MatchedSchemaID: &bestPack.ID,
		Signals: ClassificationSignals{
			MatchedSchemaID:   &bestPack.ID,
			MatchedSchemaName: &bestPack.Name,
		},
	}
	if bestPack.ExtractionPrompts != nil {
		result.DomainGuidance = bestPack.ExtractionPrompts.DomainContext
	}
	return result, true
}

// cosineSimilarity returns the cosine similarity between two vectors (0–1).
// Returns 0 when either vector has zero magnitude.
func cosineSimilarity(a, b []float32) float32 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, magA, magB float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		magA += float64(a[i]) * float64(a[i])
		magB += float64(b[i]) * float64(b[i])
	}
	mag := math.Sqrt(magA) * math.Sqrt(magB)
	if mag == 0 {
		return 0
	}
	sim := dot / mag
	// Clamp to [0, 1] — cosine can be slightly > 1 due to float precision.
	if sim > 1.0 {
		sim = 1.0
	}
	if sim < 0 {
		sim = 0
	}
	return float32(sim)
}

// heuristicMatch scores each schema pack by keyword overlap with documentText.
// Returns the best match and the matched keywords. Returns nil when no pack
// has enough signal.
func (c *DocumentClassifier) heuristicMatch(
	documentText string,
	packs []InstalledSchemaSummary,
) (*ClassificationResult, []string) {
	lower := strings.ToLower(documentText)

	bestScore := 0
	var bestPack *InstalledSchemaSummary
	var bestKeywords []string

	for i := range packs {
		pack := &packs[i]
		var matched []string
		for _, kw := range pack.Keywords {
			if strings.Contains(lower, strings.ToLower(kw)) {
				matched = append(matched, kw)
			}
		}
		if len(matched) > bestScore {
			bestScore = len(matched)
			bestPack = pack
			bestKeywords = matched
		}
	}

	if bestPack == nil || bestScore == 0 {
		return nil, nil
	}

	// Confidence: saturates at 1.0 after 5 keyword hits.
	confidence := float32(bestScore) / 5.0
	if confidence > 1.0 {
		confidence = 1.0
	}

	result := &ClassificationResult{
		DomainName:      bestPack.Name,
		Confidence:      confidence,
		MatchedSchemaID: &bestPack.ID,
		Signals: ClassificationSignals{
			MatchedSchemaID:   &bestPack.ID,
			MatchedSchemaName: &bestPack.Name,
		},
	}
	if bestPack.ExtractionPrompts != nil {
		result.DomainGuidance = bestPack.ExtractionPrompts.DomainContext
	}

	return result, bestKeywords
}

// llmClassify asks the LLM to pick the best schema pack for the document.
func (c *DocumentClassifier) llmClassify(
	ctx context.Context,
	documentText string,
	packs []InstalledSchemaSummary,
) (ClassificationResult, error) {
	if c.modelFactory == nil {
		return ClassificationResult{}, fmt.Errorf("no model factory configured")
	}

	// Build schema-pack list for prompt.
	var packList strings.Builder
	for i, p := range packs {
		packList.WriteString(fmt.Sprintf("%d. %s — %s\n", i+1, p.Name, p.Description))
	}

	// Truncate document to first 2000 chars for classification.
	snippet := documentText
	if len(snippet) > 2000 {
		snippet = snippet[:2000]
	}

	prompt := fmt.Sprintf(`You are a document classifier. Given the document snippet and list of schema packs, decide which schema pack best describes the document's PRIMARY PURPOSE and FORMAT.

Schema packs:
%s
Rules:
- Match ONLY if the document's primary format/purpose fits the schema, not just because it mentions related topics.
- A personal diary or notes document is NOT a match for a "chat transcript" or "booking" schema even if it mentions travel.
- A lab report is NOT a match for a general "health" schema unless the schema is specifically for lab results.
- If confidence is below 0.65, respond with domain_name = "" and confidence = 0.
- If none match well, respond with domain_name = "" and confidence = 0.

Document snippet:
---
%s
---

Respond with ONLY a JSON object (no markdown):
{
  "domain_name": "<pack name or empty string>",
  "confidence": <0.0-1.0>,
  "reason": "<one sentence>"
}`, packList.String(), snippet)

	llmModel, err := c.modelFactory.CreateModel(ctx)
	if err != nil {
		return ClassificationResult{}, fmt.Errorf("create model: %w", err)
	}

	req := &model.LLMRequest{
		Contents: []*genai.Content{
			genai.NewContentFromText(prompt, "user"),
		},
		Config: &genai.GenerateContentConfig{
			Temperature:     genai.Ptr[float32](0.0),
			MaxOutputTokens: 256,
		},
	}

	var sb strings.Builder
	for resp, err := range llmModel.GenerateContent(ctx, req, false) {
		if err != nil {
			return ClassificationResult{}, fmt.Errorf("LLM generate: %w", err)
		}
		if resp != nil && resp.Content != nil {
			for _, part := range resp.Content.Parts {
				if part.Text != "" {
					sb.WriteString(part.Text)
				}
			}
		}
	}

	rawResp := strings.TrimSpace(sb.String())
	// Strip markdown fences if present.
	if strings.HasPrefix(rawResp, "```") {
		if idx := strings.Index(rawResp[3:], "```"); idx >= 0 {
			rawResp = strings.TrimSpace(rawResp[3 : 3+idx])
		}
		if strings.HasPrefix(rawResp, "json") {
			rawResp = strings.TrimSpace(rawResp[4:])
		}
	}

	var parsed struct {
		DomainName string  `json:"domain_name"`
		Confidence float32 `json:"confidence"`
		Reason     string  `json:"reason"`
	}
	if err := json.Unmarshal([]byte(rawResp), &parsed); err != nil {
		return ClassificationResult{}, fmt.Errorf("parse LLM response: %w (raw: %s)", err, rawResp)
	}

	if parsed.DomainName == "" || parsed.Confidence < 0.6 {
		return ClassificationResult{}, nil
	}

	// Find matching pack to get SchemaID and guidance.
	result := ClassificationResult{
		DomainName: parsed.DomainName,
		Confidence: parsed.Confidence,
		Signals: ClassificationSignals{
			LLMReason: parsed.Reason,
		},
	}
	for i := range packs {
		if strings.EqualFold(packs[i].Name, parsed.DomainName) {
			result.MatchedSchemaID = &packs[i].ID
			result.Signals.MatchedSchemaID = &packs[i].ID
			result.Signals.MatchedSchemaName = &packs[i].Name
			if packs[i].ExtractionPrompts != nil {
				result.DomainGuidance = packs[i].ExtractionPrompts.DomainContext
			}
			break
		}
	}

	c.log.Info("domain classified via LLM",
		slog.String("domain", result.DomainName),
		slog.Float64("confidence", float64(result.Confidence)),
		slog.String("reason", parsed.Reason),
	)
	return result, nil
}
