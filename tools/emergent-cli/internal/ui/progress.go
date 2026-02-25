package ui

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"golang.org/x/term"
)

// Spinner provides a progress spinner for long operations.
type Spinner struct {
	message  string
	frames   []string
	interval time.Duration
	writer   io.Writer
	noColor  bool
	active   bool
	stopCh   chan struct{}
}

// NewSpinner creates a new progress spinner.
func NewSpinner(message string, noColor bool) *Spinner {
	frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	if noColor || !isTerminal() {
		frames = []string{"|", "/", "-", "\\"}
	}

	return &Spinner{
		message:  message,
		frames:   frames,
		interval: 100 * time.Millisecond,
		writer:   os.Stderr,
		noColor:  noColor,
		stopCh:   make(chan struct{}),
	}
}

// Start starts the spinner animation.
func (s *Spinner) Start() {
	if !isTerminal() {
		// Don't show spinner in non-terminal output
		return
	}

	s.active = true
	go s.run()
}

// Stop stops the spinner and clears the line.
func (s *Spinner) Stop() {
	if !s.active {
		return
	}

	s.active = false
	close(s.stopCh)
	time.Sleep(s.interval) // Wait for last frame

	// Clear the line
	fmt.Fprintf(s.writer, "\r%s\r", strings.Repeat(" ", 80))
}

// run is the spinner animation loop.
func (s *Spinner) run() {
	frameIndex := 0
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			frame := s.frames[frameIndex%len(s.frames)]
			frameIndex++

			var output string
			if s.noColor {
				output = fmt.Sprintf("\r%s %s", frame, s.message)
			} else {
				style := lipgloss.NewStyle().Foreground(lipgloss.Color("12")) // Blue
				output = fmt.Sprintf("\r%s %s", style.Render(frame), s.message)
			}

			fmt.Fprint(s.writer, output)
		}
	}
}

// ProgressBar provides a progress bar for operations with known total.
type ProgressBar struct {
	total      int
	current    int
	width      int
	message    string
	writer     io.Writer
	noColor    bool
	useUnicode bool
}

// NewProgressBar creates a new progress bar.
func NewProgressBar(total int, message string, noColor bool) *ProgressBar {
	useUnicode := !noColor && isTerminal()

	return &ProgressBar{
		total:      total,
		current:    0,
		width:      40,
		message:    message,
		writer:     os.Stderr,
		noColor:    noColor,
		useUnicode: useUnicode,
	}
}

// Update updates the progress bar with current progress.
func (p *ProgressBar) Update(current int) {
	if !isTerminal() {
		return
	}

	p.current = current
	p.render()
}

// Increment increments the progress by 1.
func (p *ProgressBar) Increment() {
	p.Update(p.current + 1)
}

// Finish completes the progress bar and clears the line.
func (p *ProgressBar) Finish() {
	if !isTerminal() {
		return
	}

	p.current = p.total
	p.render()
	fmt.Fprintln(p.writer) // New line after completion
}

// render renders the progress bar.
func (p *ProgressBar) render() {
	percent := float64(p.current) / float64(p.total)
	if percent > 1.0 {
		percent = 1.0
	}

	filledWidth := int(float64(p.width) * percent)
	emptyWidth := p.width - filledWidth

	var bar string
	if p.useUnicode {
		bar = strings.Repeat("█", filledWidth) + strings.Repeat("░", emptyWidth)
	} else {
		bar = strings.Repeat("#", filledWidth) + strings.Repeat("-", emptyWidth)
	}

	percentStr := fmt.Sprintf("%3.0f%%", percent*100)

	var output string
	if p.noColor {
		output = fmt.Sprintf("\r[%s] %s %s (%d/%d)", bar, percentStr, p.message, p.current, p.total)
	} else {
		barStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("10")) // Green
		percentStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("12")).Bold(true)
		output = fmt.Sprintf("\r[%s] %s %s (%d/%d)",
			barStyle.Render(bar),
			percentStyle.Render(percentStr),
			p.message,
			p.current,
			p.total)
	}

	fmt.Fprint(p.writer, output)
}

// isTerminal checks if output is going to a terminal.
func isTerminal() bool {
	return term.IsTerminal(int(os.Stdout.Fd()))
}

// isPiped checks if output is being piped.
func IsPiped() bool {
	return !isTerminal()
}
