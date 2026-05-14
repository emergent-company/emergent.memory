// Package extraction provides object extraction job processing.
package extraction

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
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
	Keywords []string
	// ExtractionPrompts holds domain guidance written back by the discovery agent.
	ExtractionPrompts *SchemaExtractionPrompts
}

// DocumentClassifier classifies a document into a domain using a two-stage
// pipeline: fast keyword heuristic → LLM confirmation (only when needed).
type DocumentClassifier struct {
	modelFactory *adk.ModelFactory
	log          *slog.Logger
}

// NewDocumentClassifier creates a new DocumentClassifier.
func NewDocumentClassifier(modelFactory *adk.ModelFactory, log *slog.Logger) *DocumentClassifier {
	return &DocumentClassifier{
		modelFactory: modelFactory,
		log:          log.With(logger.Scope("document-classifier")),
	}
}

// Classify attempts to match the document against the provided schema packs.
// Returns a zero-value ClassificationResult when no schema packs are installed.
func (c *DocumentClassifier) Classify(
	ctx context.Context,
	documentText string,
	schemaPacks []InstalledSchemaSummary,
) (ClassificationResult, error) {
	if len(schemaPacks) == 0 {
		return ClassificationResult{}, nil
	}

	// Stage 1: heuristic keyword match.
	heuristicResult, matchedKeywords := c.heuristicMatch(documentText, schemaPacks)
	if heuristicResult != nil && heuristicResult.Confidence >= 0.7 {
		c.log.Info("domain classified via heuristic",
			slog.String("domain", heuristicResult.DomainName),
			slog.Float64("confidence", float64(heuristicResult.Confidence)),
		)
		heuristicResult.Signals.HeuristicKeywords = matchedKeywords
		heuristicResult.Signals.ClassifiedAt = time.Now().UTC().Format(time.RFC3339)
		return *heuristicResult, nil
	}

	// Stage 2: LLM classification.
	llmResult, err := c.llmClassify(ctx, documentText, schemaPacks)
	if err != nil {
		c.log.Warn("LLM classification failed, falling back to heuristic",
			logger.Error(err),
		)
		// Fall back to heuristic result even if low confidence.
		if heuristicResult != nil {
			heuristicResult.Signals.HeuristicKeywords = matchedKeywords
			heuristicResult.Signals.ClassifiedAt = time.Now().UTC().Format(time.RFC3339)
			return *heuristicResult, nil
		}
		return ClassificationResult{}, nil
	}

	llmResult.Signals.HeuristicKeywords = matchedKeywords
	llmResult.Signals.ClassifiedAt = time.Now().UTC().Format(time.RFC3339)
	return llmResult, nil
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

	prompt := fmt.Sprintf(`You are a document classifier. Given the document snippet and list of schema packs, decide which schema pack best describes the document.

Schema packs:
%s
If none match, respond with domain_name = "" and confidence = 0.

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

	if parsed.DomainName == "" || parsed.Confidence < 0.3 {
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
