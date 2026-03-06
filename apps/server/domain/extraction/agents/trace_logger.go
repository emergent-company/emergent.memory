// Package agents provides ADK-based extraction agents for entity and relationship extraction.
package agents

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// ExtractionTraceLogger logs full extraction pipeline communication to a per-job log file.
// This includes prompts, LLM responses, parsed results, and all pipeline stages.
type ExtractionTraceLogger struct {
	jobID      string
	documentID string
	projectID  string
	logDir     string
	file       *os.File
	mu         sync.Mutex
	startTime  time.Time
}

// TraceLoggerConfig holds configuration for the trace logger.
type TraceLoggerConfig struct {
	// JobID is the extraction job ID.
	JobID string

	// DocumentID is the document being extracted (optional).
	DocumentID string

	// ProjectID is the project ID.
	ProjectID string

	// LogDir is the directory for log files. Default: "logs/extractions"
	LogDir string
}

// NewExtractionTraceLogger creates a new trace logger for an extraction job.
// It creates a log file at: {logDir}/{date}_{jobID}.log
func NewExtractionTraceLogger(cfg TraceLoggerConfig) (*ExtractionTraceLogger, error) {
	logDir := cfg.LogDir
	if logDir == "" {
		logDir = "logs/extractions"
	}

	// Ensure log directory exists
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return nil, fmt.Errorf("create log directory: %w", err)
	}

	// Create log file with date and job ID
	now := time.Now()
	filename := fmt.Sprintf("%s_%s.log", now.Format("2006-01-02"), cfg.JobID)
	filePath := filepath.Join(logDir, filename)

	file, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, fmt.Errorf("create log file: %w", err)
	}

	logger := &ExtractionTraceLogger{
		jobID:      cfg.JobID,
		documentID: cfg.DocumentID,
		projectID:  cfg.ProjectID,
		logDir:     logDir,
		file:       file,
		startTime:  now,
	}

	// Write header
	logger.writeHeader()

	return logger, nil
}

// Close closes the log file.
func (l *ExtractionTraceLogger) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.file != nil {
		l.writeLine("\n" + strings.Repeat("=", 80))
		l.writeLine("EXTRACTION COMPLETED")
		l.writeLine(fmt.Sprintf("Duration: %s", time.Since(l.startTime)))
		l.writeLine(strings.Repeat("=", 80))
		return l.file.Close()
	}
	return nil
}

// LogFilePath returns the path to the log file.
func (l *ExtractionTraceLogger) LogFilePath() string {
	if l.file != nil {
		return l.file.Name()
	}
	return ""
}

// writeHeader writes the initial header to the log file.
func (l *ExtractionTraceLogger) writeHeader() {
	l.writeLine(strings.Repeat("=", 80))
	l.writeLine("EXTRACTION JOB TRACE LOG")
	l.writeLine(strings.Repeat("=", 80))
	l.writeLine("")
	l.writeLine(fmt.Sprintf("Job ID:      %s", l.jobID))
	l.writeLine(fmt.Sprintf("Document ID: %s", l.documentID))
	l.writeLine(fmt.Sprintf("Project ID:  %s", l.projectID))
	l.writeLine(fmt.Sprintf("Started:     %s", l.startTime.Format(time.RFC3339)))
	l.writeLine("")
}

// LogStageStart logs the beginning of a pipeline stage.
func (l *ExtractionTraceLogger) LogStageStart(stageName string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.writeLine("")
	l.writeLine(strings.Repeat("=", 80))
	l.writeLine(fmt.Sprintf("STAGE: %s", stageName))
	l.writeLine(fmt.Sprintf("Time: %s", time.Now().Format(time.RFC3339)))
	l.writeLine(strings.Repeat("=", 80))
}

// LogPrompt logs a prompt sent to the LLM.
func (l *ExtractionTraceLogger) LogPrompt(agentName string, prompt string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.writeLine("")
	l.writeLine(fmt.Sprintf("--- %s PROMPT ---", agentName))
	l.writeLine(fmt.Sprintf("Length: %d characters", len(prompt)))
	l.writeLine("")
	l.writeLine(prompt)
	l.writeLine("")
	l.writeLine(fmt.Sprintf("--- END %s PROMPT ---", agentName))
}

// LogResponse logs an LLM response.
func (l *ExtractionTraceLogger) LogResponse(agentName string, response string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.writeLine("")
	l.writeLine(fmt.Sprintf("--- %s RESPONSE ---", agentName))
	l.writeLine(fmt.Sprintf("Length: %d characters", len(response)))
	l.writeLine("")
	l.writeLine(response)
	l.writeLine("")
	l.writeLine(fmt.Sprintf("--- END %s RESPONSE ---", agentName))
}

// LogParsedResult logs a parsed/structured result.
func (l *ExtractionTraceLogger) LogParsedResult(stageName string, result any) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.writeLine("")
	l.writeLine(fmt.Sprintf("--- %s PARSED RESULT ---", stageName))

	// Pretty-print JSON
	jsonBytes, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		l.writeLine(fmt.Sprintf("Error marshaling result: %v", err))
		l.writeLine(fmt.Sprintf("Raw: %+v", result))
	} else {
		l.writeLine(string(jsonBytes))
	}

	l.writeLine(fmt.Sprintf("--- END %s PARSED RESULT ---", stageName))
}

// LogSchemas logs the schemas being used for extraction.
func (l *ExtractionTraceLogger) LogSchemas(objectSchemas map[string]ObjectSchema, relationshipSchemas map[string]RelationshipSchema) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.writeLine("")
	l.writeLine("--- EXTRACTION SCHEMAS ---")

	l.writeLine("")
	l.writeLine(fmt.Sprintf("Object Schemas (%d):", len(objectSchemas)))
	for name, schema := range objectSchemas {
		l.writeLine(fmt.Sprintf("  - %s: %s", name, schema.Description))
		if len(schema.Properties) > 0 {
			l.writeLine(fmt.Sprintf("    Properties: %d", len(schema.Properties)))
			for propName, propDef := range schema.Properties {
				l.writeLine(fmt.Sprintf("      - %s (%s): %s", propName, propDef.Type, propDef.Description))
			}
		}
	}

	l.writeLine("")
	l.writeLine(fmt.Sprintf("Relationship Schemas (%d):", len(relationshipSchemas)))
	for name, schema := range relationshipSchemas {
		l.writeLine(fmt.Sprintf("  - %s: %s", name, schema.Description))
		if len(schema.SourceTypes) > 0 {
			l.writeLine(fmt.Sprintf("    Source types: %v", schema.SourceTypes))
		}
		if len(schema.TargetTypes) > 0 {
			l.writeLine(fmt.Sprintf("    Target types: %v", schema.TargetTypes))
		}
	}

	l.writeLine("")
	l.writeLine("--- END EXTRACTION SCHEMAS ---")
}

// LogDocumentText logs the document text being extracted.
func (l *ExtractionTraceLogger) LogDocumentText(text string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.writeLine("")
	l.writeLine("--- DOCUMENT TEXT ---")
	l.writeLine(fmt.Sprintf("Length: %d characters", len(text)))
	l.writeLine("")

	// For very long documents, truncate but show beginning and end
	const maxLen = 10000
	if len(text) > maxLen {
		l.writeLine(text[:maxLen/2])
		l.writeLine("")
		l.writeLine(fmt.Sprintf("... [%d characters truncated] ...", len(text)-maxLen))
		l.writeLine("")
		l.writeLine(text[len(text)-maxLen/2:])
	} else {
		l.writeLine(text)
	}

	l.writeLine("")
	l.writeLine("--- END DOCUMENT TEXT ---")
}

// LogEntities logs extracted entities.
func (l *ExtractionTraceLogger) LogEntities(entities []InternalEntity) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.writeLine("")
	l.writeLine(fmt.Sprintf("--- EXTRACTED ENTITIES (%d) ---", len(entities)))

	for i, entity := range entities {
		l.writeLine(fmt.Sprintf("\n[%d] %s (temp_id: %s)", i+1, entity.Name, entity.TempID))
		l.writeLine(fmt.Sprintf("    Type: %s", entity.Type))
		if entity.Description != "" {
			l.writeLine(fmt.Sprintf("    Description: %s", entity.Description))
		}
		if len(entity.Properties) > 0 {
			propsJSON, _ := json.Marshal(entity.Properties)
			l.writeLine(fmt.Sprintf("    Properties: %s", string(propsJSON)))
		}
		if entity.Action != "" {
			l.writeLine(fmt.Sprintf("    Action: %s", entity.Action))
		}
		if entity.ExistingEntityID != "" {
			l.writeLine(fmt.Sprintf("    Existing Entity ID: %s", entity.ExistingEntityID))
		}
	}

	l.writeLine("")
	l.writeLine("--- END EXTRACTED ENTITIES ---")
}

// LogRelationships logs extracted relationships.
func (l *ExtractionTraceLogger) LogRelationships(relationships []ExtractedRelationship) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.writeLine("")
	l.writeLine(fmt.Sprintf("--- EXTRACTED RELATIONSHIPS (%d) ---", len(relationships)))

	for i, rel := range relationships {
		l.writeLine(fmt.Sprintf("\n[%d] %s -[%s]-> %s", i+1, rel.SourceRef, rel.Type, rel.TargetRef))
		if rel.Description != "" {
			l.writeLine(fmt.Sprintf("    Description: %s", rel.Description))
		}
	}

	l.writeLine("")
	l.writeLine("--- END EXTRACTED RELATIONSHIPS ---")
}

// LogQualityCheck logs quality check results.
func (l *ExtractionTraceLogger) LogQualityCheck(iteration int, orphanRate float64, threshold float64, orphanIDs []string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.writeLine("")
	l.writeLine(fmt.Sprintf("--- QUALITY CHECK (Iteration %d) ---", iteration))
	l.writeLine(fmt.Sprintf("Orphan Rate: %.1f%% (threshold: %.1f%%)", orphanRate*100, threshold*100))
	l.writeLine(fmt.Sprintf("Passed: %v", orphanRate <= threshold))

	if len(orphanIDs) > 0 {
		l.writeLine(fmt.Sprintf("Orphan Entity IDs (%d):", len(orphanIDs)))
		for _, id := range orphanIDs {
			l.writeLine(fmt.Sprintf("  - %s", id))
		}
	}

	l.writeLine("--- END QUALITY CHECK ---")
}

// LogError logs an error that occurred during extraction.
func (l *ExtractionTraceLogger) LogError(stage string, err error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.writeLine("")
	l.writeLine(fmt.Sprintf("!!! ERROR in %s !!!", stage))
	l.writeLine(fmt.Sprintf("Error: %v", err))
	l.writeLine("")
}

// LogInfo logs a general info message.
func (l *ExtractionTraceLogger) LogInfo(message string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.writeLine(fmt.Sprintf("[INFO] %s", message))
}

// LogEvent logs an ADK session event.
func (l *ExtractionTraceLogger) LogEvent(eventType string, author string, content string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.writeLine("")
	l.writeLine(fmt.Sprintf("--- EVENT: %s (Author: %s) ---", eventType, author))
	if content != "" {
		l.writeLine(content)
	}
	l.writeLine(fmt.Sprintf("--- END EVENT ---"))
}

// writeLine writes a line to the log file.
func (l *ExtractionTraceLogger) writeLine(line string) {
	if l.file != nil {
		l.file.WriteString(line + "\n")
	}
}

// NullTraceLogger is a no-op implementation for when tracing is disabled.
type NullTraceLogger struct{}

func (l *NullTraceLogger) Close() error                                  { return nil }
func (l *NullTraceLogger) LogFilePath() string                           { return "" }
func (l *NullTraceLogger) LogStageStart(stageName string)                {}
func (l *NullTraceLogger) LogPrompt(agentName string, prompt string)     {}
func (l *NullTraceLogger) LogResponse(agentName string, response string) {}
func (l *NullTraceLogger) LogParsedResult(stageName string, result any)  {}
func (l *NullTraceLogger) LogSchemas(objectSchemas map[string]ObjectSchema, relationshipSchemas map[string]RelationshipSchema) {
}
func (l *NullTraceLogger) LogDocumentText(text string)                            {}
func (l *NullTraceLogger) LogEntities(entities []InternalEntity)                  {}
func (l *NullTraceLogger) LogRelationships(relationships []ExtractedRelationship) {}
func (l *NullTraceLogger) LogQualityCheck(iteration int, orphanRate float64, threshold float64, orphanIDs []string) {
}
func (l *NullTraceLogger) LogError(stage string, err error)                         {}
func (l *NullTraceLogger) LogInfo(message string)                                   {}
func (l *NullTraceLogger) LogEvent(eventType string, author string, content string) {}

// TraceLogger interface defines the tracing contract.
type TraceLogger interface {
	Close() error
	LogFilePath() string
	LogStageStart(stageName string)
	LogPrompt(agentName string, prompt string)
	LogResponse(agentName string, response string)
	LogParsedResult(stageName string, result any)
	LogSchemas(objectSchemas map[string]ObjectSchema, relationshipSchemas map[string]RelationshipSchema)
	LogDocumentText(text string)
	LogEntities(entities []InternalEntity)
	LogRelationships(relationships []ExtractedRelationship)
	LogQualityCheck(iteration int, orphanRate float64, threshold float64, orphanIDs []string)
	LogError(stage string, err error)
	LogInfo(message string)
	LogEvent(eventType string, author string, content string)
}
