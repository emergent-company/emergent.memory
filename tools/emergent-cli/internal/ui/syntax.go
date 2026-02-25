package ui

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"gopkg.in/yaml.v3"
)

// SyntaxStyles contains styles for syntax highlighting.
type SyntaxStyles struct {
	Key     lipgloss.Style
	String  lipgloss.Style
	Number  lipgloss.Style
	Boolean lipgloss.Style
	Null    lipgloss.Style
	Bracket lipgloss.Style
}

// NewSyntaxStyles creates syntax styles based on color mode.
func NewSyntaxStyles(noColor bool) SyntaxStyles {
	if noColor {
		return SyntaxStyles{
			Key:     lipgloss.NewStyle(),
			String:  lipgloss.NewStyle(),
			Number:  lipgloss.NewStyle(),
			Boolean: lipgloss.NewStyle(),
			Null:    lipgloss.NewStyle(),
			Bracket: lipgloss.NewStyle(),
		}
	}

	return SyntaxStyles{
		Key:     lipgloss.NewStyle().Foreground(lipgloss.Color("12")).Bold(true), // Blue
		String:  lipgloss.NewStyle().Foreground(lipgloss.Color("10")),            // Green
		Number:  lipgloss.NewStyle().Foreground(lipgloss.Color("11")),            // Yellow
		Boolean: lipgloss.NewStyle().Foreground(lipgloss.Color("13")),            // Magenta
		Null:    lipgloss.NewStyle().Foreground(lipgloss.Color("8")),             // Gray
		Bracket: lipgloss.NewStyle().Foreground(lipgloss.Color("7")),             // Light gray
	}
}

// FormatJSON formats JSON with syntax highlighting.
func FormatJSON(data interface{}, noColor bool) (string, error) {
	// Marshal to pretty JSON
	jsonBytes, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal JSON: %w", err)
	}

	if noColor {
		return string(jsonBytes), nil
	}

	// Simple syntax highlighting
	styles := NewSyntaxStyles(false)
	output := string(jsonBytes)

	// This is a basic implementation - for production, consider using a proper JSON parser
	// For now, we'll just apply some basic coloring
	lines := strings.Split(output, "\n")
	var highlighted strings.Builder

	for _, line := range lines {
		highlightedLine := highlightJSONLine(line, styles)
		highlighted.WriteString(highlightedLine)
		highlighted.WriteString("\n")
	}

	return highlighted.String(), nil
}

// highlightJSONLine applies syntax highlighting to a JSON line.
func highlightJSONLine(line string, styles SyntaxStyles) string {
	// Very basic highlighting - a full parser would be better
	result := line

	// Highlight brackets
	result = strings.ReplaceAll(result, "{", styles.Bracket.Render("{"))
	result = strings.ReplaceAll(result, "}", styles.Bracket.Render("}"))
	result = strings.ReplaceAll(result, "[", styles.Bracket.Render("["))
	result = strings.ReplaceAll(result, "]", styles.Bracket.Render("]"))

	// Highlight null, true, false
	result = strings.ReplaceAll(result, "null", styles.Null.Render("null"))
	result = strings.ReplaceAll(result, "true", styles.Boolean.Render("true"))
	result = strings.ReplaceAll(result, "false", styles.Boolean.Render("false"))

	return result
}

// FormatYAML formats YAML with syntax highlighting.
func FormatYAML(data interface{}, noColor bool) (string, error) {
	// Marshal to YAML
	yamlBytes, err := yaml.Marshal(data)
	if err != nil {
		return "", fmt.Errorf("failed to marshal YAML: %w", err)
	}

	if noColor {
		return string(yamlBytes), nil
	}

	// Simple syntax highlighting
	styles := NewSyntaxStyles(false)
	output := string(yamlBytes)

	// This is a basic implementation
	lines := strings.Split(output, "\n")
	var highlighted strings.Builder

	for _, line := range lines {
		highlightedLine := highlightYAMLLine(line, styles)
		highlighted.WriteString(highlightedLine)
		highlighted.WriteString("\n")
	}

	return highlighted.String(), nil
}

// highlightYAMLLine applies syntax highlighting to a YAML line.
func highlightYAMLLine(line string, styles SyntaxStyles) string {
	// Very basic highlighting
	result := line

	// Highlight keys (text before colon)
	if strings.Contains(line, ":") {
		parts := strings.SplitN(line, ":", 2)
		if len(parts) == 2 {
			indent := getLeadingSpaces(parts[0])
			key := strings.TrimSpace(parts[0])
			value := parts[1]

			result = indent + styles.Key.Render(key) + ":" + value
		}
	}

	// Highlight null, true, false
	result = strings.ReplaceAll(result, " null", " "+styles.Null.Render("null"))
	result = strings.ReplaceAll(result, " true", " "+styles.Boolean.Render("true"))
	result = strings.ReplaceAll(result, " false", " "+styles.Boolean.Render("false"))

	return result
}

// getLeadingSpaces returns the leading spaces of a string.
func getLeadingSpaces(s string) string {
	for i, c := range s {
		if c != ' ' {
			return s[:i]
		}
	}
	return s
}
