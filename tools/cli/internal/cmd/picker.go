package cmd

// picker.go — interactive Bubbletea project picker.
//
// Launched by promptProjectPicker when stdin is a real terminal and no project
// has been configured. Renders a compact arrow-key list of projects to stderr
// so that stdout remains clean for piped usage.
//
// Keyboard:
//   ↑/k  move up
//   ↓/j  move down
//   enter  confirm selection
//   esc/q/ctrl+c  cancel
//
// A 30-second inactivity timer fires tea.Quit automatically; the caller
// receives a timeout error and the command exits cleanly.

import (
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ErrPickerCancelled is returned when the user presses Esc/q/Ctrl+C.
var ErrPickerCancelled = errors.New("project selection cancelled")

// ErrPickerTimeout is returned when the 30-second idle timer fires.
var ErrPickerTimeout = errors.New("project selection timed out (30s) — use --project or set MEMORY_PROJECT")

// PickerItem is a project entry shown in the picker list.
type PickerItem struct {
	ID   string
	Name string
}

// Implement list.Item interface.
func (p PickerItem) Title() string       { return p.Name }
func (p PickerItem) Description() string { return p.ID }
func (p PickerItem) FilterValue() string { return p.Name }

// pickerTimeoutMsg is sent by the timeout timer.
type pickerTimeoutMsg struct{}

// pickerResultMsg carries the final outcome.
type pickerResultMsg struct {
	item PickerItem
	err  error
}

// pickerModel is the Bubbletea model for the project picker.
type pickerModel struct {
	list     list.Model
	result   *PickerItem
	err      error
	quitting bool
	timeout  time.Duration
}

// pickerKeyMap defines the keys for the picker.
type pickerKeyMap struct {
	Choose key.Binding
	Cancel key.Binding
}

var pickerKeys = pickerKeyMap{
	Choose: key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("enter", "select"),
	),
	Cancel: key.NewBinding(
		key.WithKeys("esc", "q", "ctrl+c"),
		key.WithHelp("esc", "cancel"),
	),
}

func newPickerModel(items []PickerItem, timeout time.Duration) pickerModel {
	listItems := make([]list.Item, len(items))
	for i, it := range items {
		listItems[i] = it
	}

	delegate := list.NewDefaultDelegate()
	delegate.Styles.SelectedTitle = delegate.Styles.SelectedTitle.
		Foreground(lipgloss.Color("12")).
		BorderForeground(lipgloss.Color("12"))
	delegate.Styles.SelectedDesc = delegate.Styles.SelectedDesc.
		Foreground(lipgloss.Color("240")).
		BorderForeground(lipgloss.Color("12"))

	height := len(items) + 5 // list items + header + help line
	if height > 14 {
		height = 14
	}

	l := list.New(listItems, delegate, 60, height)
	l.Title = "Select a project"
	l.Styles.Title = lipgloss.NewStyle().
		Foreground(lipgloss.Color("12")).
		Bold(true)
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(true)
	l.AdditionalShortHelpKeys = func() []key.Binding {
		return []key.Binding{pickerKeys.Choose, pickerKeys.Cancel}
	}

	return pickerModel{
		list:    l,
		timeout: timeout,
	}
}

func (m pickerModel) Init() tea.Cmd {
	return tea.Tick(m.timeout, func(_ time.Time) tea.Msg {
		return pickerTimeoutMsg{}
	})
}

func (m pickerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case pickerTimeoutMsg:
		m.err = ErrPickerTimeout
		m.quitting = true
		return m, tea.Quit

	case tea.WindowSizeMsg:
		m.list.SetWidth(msg.Width)
		return m, nil

	case tea.KeyMsg:
		// Let filter handle keys when it's active.
		if m.list.FilterState() == list.Filtering {
			break
		}
		switch {
		case key.Matches(msg, pickerKeys.Choose):
			selected, ok := m.list.SelectedItem().(PickerItem)
			if ok {
				m.result = &selected
			}
			m.quitting = true
			return m, tea.Quit

		case key.Matches(msg, pickerKeys.Cancel):
			m.err = ErrPickerCancelled
			m.quitting = true
			return m, tea.Quit
		}
	}

	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m pickerModel) View() string {
	if m.quitting {
		return ""
	}
	hint := lipgloss.NewStyle().Foreground(lipgloss.Color("240")).
		Render(fmt.Sprintf("  (auto-cancels in %s if no selection)", m.timeout.Round(time.Second)))
	return "\n" + m.list.View() + "\n" + hint + "\n"
}

// PickProject launches the interactive project picker and returns the selected
// project's ID and name. Returns an error if the user cancels or the timeout
// fires. Renders to w (pass os.Stderr to keep stdout clean).
func PickProject(projects []PickerItem, timeout time.Duration, w io.Writer) (id, name string, err error) {
	if len(projects) == 0 {
		return "", "", fmt.Errorf("no projects available to pick from")
	}

	m := newPickerModel(projects, timeout)

	p := tea.NewProgram(
		m,
		tea.WithInput(os.Stdin),
		tea.WithOutput(w),
	)

	finalModel, runErr := p.Run()
	if runErr != nil {
		return "", "", fmt.Errorf("picker error: %w", runErr)
	}

	fm, ok := finalModel.(pickerModel)
	if !ok {
		return "", "", fmt.Errorf("unexpected picker model type")
	}

	if fm.err != nil {
		return "", "", fm.err
	}

	if fm.result == nil {
		return "", "", ErrPickerCancelled
	}

	return fm.result.ID, fm.result.Name, nil
}
