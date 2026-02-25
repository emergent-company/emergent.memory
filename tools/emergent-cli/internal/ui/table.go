package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"golang.org/x/term"
)

// TableConfig holds configuration for table rendering.
type TableConfig struct {
	NoColor    bool
	Compact    bool
	MaxWidth   int  // 0 means auto-detect
	UseUnicode bool // Use Unicode box-drawing characters
}

// Table represents a styled table renderer.
type Table struct {
	config  TableConfig
	headers []string
	rows    [][]string
	styles  TableStyles
}

// TableStyles contains lipgloss styles for different table elements.
type TableStyles struct {
	Header       lipgloss.Style
	Cell         lipgloss.Style
	Border       lipgloss.Style
	SelectedRow  lipgloss.Style
	AlternateRow lipgloss.Style
}

// NewTable creates a new table with the given configuration.
func NewTable(config TableConfig) *Table {
	styles := defaultStyles(config.NoColor)

	return &Table{
		config: config,
		styles: styles,
	}
}

// SetHeaders sets the table headers.
func (t *Table) SetHeaders(headers []string) {
	t.headers = headers
}

// AddRow adds a row to the table.
func (t *Table) AddRow(row []string) {
	t.rows = append(t.rows, row)
}

// Render renders the table as a string.
func (t *Table) Render() string {
	if len(t.headers) == 0 {
		return ""
	}

	// Detect terminal width
	termWidth := t.config.MaxWidth
	if termWidth == 0 {
		termWidth = detectTerminalWidth()
	}

	// Calculate column widths
	colWidths := t.calculateColumnWidths(termWidth)

	var sb strings.Builder

	// Render top border
	if !t.config.Compact {
		sb.WriteString(t.renderBorder(colWidths, "top"))
		sb.WriteString("\n")
	}

	// Render header
	sb.WriteString(t.renderRow(t.headers, colWidths, true))
	sb.WriteString("\n")

	// Render header separator
	sb.WriteString(t.renderBorder(colWidths, "middle"))
	sb.WriteString("\n")

	// Render rows
	for i, row := range t.rows {
		style := t.styles.Cell
		if i%2 == 1 && !t.config.NoColor {
			style = t.styles.AlternateRow
		}
		sb.WriteString(t.renderRowWithStyle(row, colWidths, style))
		sb.WriteString("\n")
	}

	// Render bottom border
	if !t.config.Compact {
		sb.WriteString(t.renderBorder(colWidths, "bottom"))
		sb.WriteString("\n")
	}

	return sb.String()
}

// calculateColumnWidths calculates optimal column widths based on content and terminal width.
func (t *Table) calculateColumnWidths(maxWidth int) []int {
	numCols := len(t.headers)
	if numCols == 0 {
		return nil
	}

	// Calculate minimum widths (based on content)
	minWidths := make([]int, numCols)
	for i, header := range t.headers {
		minWidths[i] = len(header)
	}
	for _, row := range t.rows {
		for i, cell := range row {
			if i < numCols && len(cell) > minWidths[i] {
				minWidths[i] = len(cell)
			}
		}
	}

	// Calculate total width with borders and padding
	// Format: "│ cell1 │ cell2 │ cell3 │"
	borderChars := numCols + 1  // vertical bars
	paddingChars := numCols * 2 // space on each side of cells
	totalMinWidth := borderChars + paddingChars
	for _, w := range minWidths {
		totalMinWidth += w
	}

	// If content fits in terminal, use minimum widths
	if totalMinWidth <= maxWidth {
		return minWidths
	}

	// Need to shrink columns - distribute available space proportionally
	availableWidth := maxWidth - borderChars - paddingChars
	if availableWidth < numCols*3 {
		// Minimum 3 chars per column
		availableWidth = numCols * 3
	}

	// Proportional distribution
	totalMinContentWidth := 0
	for _, w := range minWidths {
		totalMinContentWidth += w
	}

	widths := make([]int, numCols)
	remainingWidth := availableWidth
	for i := 0; i < numCols-1; i++ {
		widths[i] = (minWidths[i] * availableWidth) / totalMinContentWidth
		if widths[i] < 3 {
			widths[i] = 3
		}
		remainingWidth -= widths[i]
	}
	// Give remaining width to last column
	widths[numCols-1] = remainingWidth

	return widths
}

// renderRow renders a table row.
func (t *Table) renderRow(cells []string, widths []int, isHeader bool) string {
	style := t.styles.Cell
	if isHeader {
		style = t.styles.Header
	}
	return t.renderRowWithStyle(cells, widths, style)
}

// renderRowWithStyle renders a table row with a specific style.
func (t *Table) renderRowWithStyle(cells []string, widths []int, style lipgloss.Style) string {
	var sb strings.Builder

	border := "│"
	if !t.config.UseUnicode {
		border = "|"
	}

	sb.WriteString(border)
	for i, cell := range cells {
		if i >= len(widths) {
			break
		}
		// Truncate or pad cell
		formatted := t.formatCell(cell, widths[i])
		sb.WriteString(" ")
		sb.WriteString(style.Render(formatted))
		sb.WriteString(" ")
		sb.WriteString(border)
	}

	return sb.String()
}

// formatCell formats a cell to fit within the given width.
func (t *Table) formatCell(cell string, width int) string {
	if len(cell) <= width {
		// Pad with spaces
		return cell + strings.Repeat(" ", width-len(cell))
	}
	// Truncate with ellipsis
	if width < 3 {
		return strings.Repeat(".", width)
	}
	return cell[:width-1] + "…"
}

// renderBorder renders a table border (top, middle, or bottom).
func (t *Table) renderBorder(widths []int, position string) string {
	var left, middle, right, horizontal string

	if t.config.UseUnicode {
		horizontal = "─"
		switch position {
		case "top":
			left, middle, right = "┌", "┬", "┐"
		case "middle":
			left, middle, right = "├", "┼", "┤"
		case "bottom":
			left, middle, right = "└", "┴", "┘"
		}
	} else {
		horizontal = "-"
		switch position {
		case "top":
			left, middle, right = "+", "+", "+"
		case "middle":
			left, middle, right = "+", "+", "+"
		case "bottom":
			left, middle, right = "+", "+", "+"
		}
	}

	var sb strings.Builder
	sb.WriteString(left)
	for i, width := range widths {
		sb.WriteString(horizontal)
		sb.WriteString(strings.Repeat(horizontal, width))
		sb.WriteString(horizontal)
		if i < len(widths)-1 {
			sb.WriteString(middle)
		}
	}
	sb.WriteString(right)

	return sb.String()
}

// detectTerminalWidth detects the current terminal width.
func detectTerminalWidth() int {
	width, _, err := term.GetSize(0)
	if err != nil || width <= 0 {
		return 80 // default fallback
	}
	return width
}

// defaultStyles creates default table styles.
func defaultStyles(noColor bool) TableStyles {
	if noColor {
		return TableStyles{
			Header:       lipgloss.NewStyle().Bold(true),
			Cell:         lipgloss.NewStyle(),
			Border:       lipgloss.NewStyle(),
			SelectedRow:  lipgloss.NewStyle(),
			AlternateRow: lipgloss.NewStyle(),
		}
	}

	return TableStyles{
		Header: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("12")).  // Bright blue
			Background(lipgloss.Color("236")), // Dark gray
		Cell: lipgloss.NewStyle().
			Foreground(lipgloss.Color("15")), // White
		Border: lipgloss.NewStyle().
			Foreground(lipgloss.Color("240")), // Gray
		SelectedRow: lipgloss.NewStyle().
			Background(lipgloss.Color("237")). // Light gray background
			Foreground(lipgloss.Color("15")),
		AlternateRow: lipgloss.NewStyle().
			Foreground(lipgloss.Color("252")), // Light gray text
	}
}

// StatusIndicator renders status indicators with color.
type StatusIndicator string

const (
	StatusSuccess StatusIndicator = "success"
	StatusError   StatusIndicator = "error"
	StatusWarning StatusIndicator = "warning"
	StatusInfo    StatusIndicator = "info"
	StatusPending StatusIndicator = "pending"
)

// RenderStatus renders a status indicator with appropriate styling.
func RenderStatus(status StatusIndicator, noColor bool, useUnicode bool) string {
	if noColor {
		switch status {
		case StatusSuccess:
			return "[OK]"
		case StatusError:
			return "[ERR]"
		case StatusWarning:
			return "[WARN]"
		case StatusInfo:
			return "[INFO]"
		case StatusPending:
			return "[...]"
		default:
			return "[-]"
		}
	}

	var symbol, color string
	if useUnicode {
		switch status {
		case StatusSuccess:
			symbol, color = "✓", "10" // Green
		case StatusError:
			symbol, color = "✗", "9" // Red
		case StatusWarning:
			symbol, color = "⚠", "11" // Yellow
		case StatusInfo:
			symbol, color = "ℹ", "12" // Blue
		case StatusPending:
			symbol, color = "⋯", "8" // Gray
		default:
			symbol, color = "•", "15" // White
		}
	} else {
		switch status {
		case StatusSuccess:
			symbol, color = "✓", "10"
		case StatusError:
			symbol, color = "X", "9"
		case StatusWarning:
			symbol, color = "!", "11"
		case StatusInfo:
			symbol, color = "i", "12"
		case StatusPending:
			symbol, color = ".", "8"
		default:
			symbol, color = "-", "15"
		}
	}

	style := lipgloss.NewStyle().Foreground(lipgloss.Color(color)).Bold(true)
	return style.Render(symbol)
}

// TruncateWithEllipsis truncates a string to maxLen with ellipsis.
func TruncateWithEllipsis(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen < 3 {
		return strings.Repeat(".", maxLen)
	}
	return s[:maxLen-1] + "…"
}
