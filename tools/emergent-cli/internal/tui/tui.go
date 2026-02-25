// Package tui provides the terminal UI (TUI) for interactive browsing.
package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/emergent-company/emergent/tools/emergent-cli/internal/client"
)

// ViewMode represents the current view mode
type ViewMode int

const (
	ProjectsView ViewMode = iota
	DocumentsView
	ExtractionsView
	DetailsView
)

// Model represents the state of the TUI application.
type Model struct {
	client *client.Client
	width  int
	height int
	ready  bool
	err    error

	// View state
	currentView ViewMode
	activeTab   int

	// Lists
	projectsList    list.Model
	documentsList   list.Model
	extractionsList list.Model

	// Search
	searchMode  bool
	searchInput textinput.Model

	// Help
	help     help.Model
	keyMap   KeyMap
	showHelp bool

	// Status
	statusMsg string

	// Selected items
	selectedProjectID string
	selectedDocID     string
}

// KeyMap defines keybindings
type KeyMap struct {
	Up       key.Binding
	Down     key.Binding
	Enter    key.Binding
	Back     key.Binding
	Tab      key.Binding
	Search   key.Binding
	Help     key.Binding
	Quit     key.Binding
	NextPage key.Binding
	PrevPage key.Binding
}

// DefaultKeyMap returns the default keybindings
func DefaultKeyMap() KeyMap {
	return KeyMap{
		Up: key.NewBinding(
			key.WithKeys("up", "k"),
			key.WithHelp("↑/k", "move up"),
		),
		Down: key.NewBinding(
			key.WithKeys("down", "j"),
			key.WithHelp("↓/j", "move down"),
		),
		Enter: key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("enter", "select"),
		),
		Back: key.NewBinding(
			key.WithKeys("esc", "backspace"),
			key.WithHelp("esc", "back"),
		),
		Tab: key.NewBinding(
			key.WithKeys("tab"),
			key.WithHelp("tab", "switch tab"),
		),
		Search: key.NewBinding(
			key.WithKeys("/"),
			key.WithHelp("/", "search"),
		),
		Help: key.NewBinding(
			key.WithKeys("?"),
			key.WithHelp("?", "toggle help"),
		),
		Quit: key.NewBinding(
			key.WithKeys("q", "ctrl+c"),
			key.WithHelp("q", "quit"),
		),
		NextPage: key.NewBinding(
			key.WithKeys("n", "pgdown"),
			key.WithHelp("n", "next page"),
		),
		PrevPage: key.NewBinding(
			key.WithKeys("p", "pgup"),
			key.WithHelp("p", "prev page"),
		),
	}
}

// ShortHelp returns keybindings to be shown in the mini help view
func (k KeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Up, k.Down, k.Enter, k.Search, k.Help, k.Quit}
}

// FullHelp returns keybindings for the expanded help view
func (k KeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.Enter, k.Back},
		{k.Tab, k.Search, k.NextPage, k.PrevPage},
		{k.Help, k.Quit},
	}
}

// Init initializes the model.
func (m Model) Init() tea.Cmd {
	return tea.Batch(
		tea.EnterAltScreen,
		loadProjects(m.client),
	)
}

// Update handles messages and updates the model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.ready = true

		// Check minimum size
		if m.width < 80 || m.height < 24 {
			m.err = fmt.Errorf("terminal too small (minimum 80x24, current %dx%d)", m.width, m.height)
		}

		// Update list sizes
		m.projectsList.SetSize(m.width-4, m.height-10)
		m.documentsList.SetSize(m.width-4, m.height-10)
		m.extractionsList.SetSize(m.width-4, m.height-10)

	case tea.KeyMsg:
		// Global keybindings
		if m.searchMode {
			return m.handleSearchInput(msg)
		}

		switch {
		case key.Matches(msg, m.keyMap.Quit):
			return m, tea.Quit

		case key.Matches(msg, m.keyMap.Help):
			m.showHelp = !m.showHelp
			return m, nil

		case key.Matches(msg, m.keyMap.Search):
			m.searchMode = true
			m.searchInput.Focus()
			return m, textinput.Blink

		case key.Matches(msg, m.keyMap.Tab):
			m.activeTab = (m.activeTab + 1) % 3
			m.currentView = ViewMode(m.activeTab)
			return m, nil

		case key.Matches(msg, m.keyMap.Back):
			if m.currentView == DocumentsView {
				m.currentView = ProjectsView
				m.selectedProjectID = ""
			}
			return m, nil

		case key.Matches(msg, m.keyMap.Enter):
			return m.handleEnter()
		}

	case projectsLoadedMsg:
		m.statusMsg = fmt.Sprintf("Loaded %d projects", len(msg.projects))
		// Convert to list items and update projectsList
		return m, nil

	case documentsLoadedMsg:
		m.statusMsg = fmt.Sprintf("Loaded %d documents", len(msg.documents))
		// Convert to list items and update documentsList
		return m, nil

	case errMsg:
		m.err = msg.err
		return m, nil
	}

	// Update active list
	switch m.currentView {
	case ProjectsView:
		m.projectsList, cmd = m.projectsList.Update(msg)
		cmds = append(cmds, cmd)
	case DocumentsView:
		m.documentsList, cmd = m.documentsList.Update(msg)
		cmds = append(cmds, cmd)
	case ExtractionsView:
		m.extractionsList, cmd = m.extractionsList.Update(msg)
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}

// handleEnter handles the Enter key press
func (m Model) handleEnter() (tea.Model, tea.Cmd) {
	switch m.currentView {
	case ProjectsView:
		// Get selected project
		if item, ok := m.projectsList.SelectedItem().(projectItem); ok {
			m.selectedProjectID = item.id
			m.currentView = DocumentsView
			return m, loadDocuments(m.client, item.id)
		}
	case DocumentsView:
		// Get selected document
		if item, ok := m.documentsList.SelectedItem().(documentItem); ok {
			m.selectedDocID = item.id
			m.currentView = DetailsView
		}
	}
	return m, nil
}

// handleSearchInput handles search input
func (m Model) handleSearchInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEscape:
		m.searchMode = false
		m.searchInput.Blur()
		m.searchInput.SetValue("")
		return m, nil

	case tea.KeyEnter:
		m.searchMode = false
		m.searchInput.Blur()
		query := m.searchInput.Value()
		m.searchInput.SetValue("")
		// Perform search
		return m, performSearch(m.client, query)
	}

	var cmd tea.Cmd
	m.searchInput, cmd = m.searchInput.Update(msg)
	return m, cmd
}

// View renders the UI.
func (m Model) View() string {
	if !m.ready {
		return "Initializing..."
	}

	if m.err != nil {
		return errorView(m.err)
	}

	if m.width < 80 || m.height < 24 {
		return m.err.Error()
	}

	var content strings.Builder

	// Render tab bar
	content.WriteString(m.renderTabBar())
	content.WriteString("\n\n")

	// Render main content
	switch m.currentView {
	case ProjectsView:
		content.WriteString(m.projectsList.View())
	case DocumentsView:
		content.WriteString(m.documentsList.View())
	case ExtractionsView:
		content.WriteString(m.extractionsList.View())
	case DetailsView:
		content.WriteString(m.renderDetails())
	}

	content.WriteString("\n\n")

	// Render search bar if active
	if m.searchMode {
		content.WriteString(m.renderSearchBar())
		content.WriteString("\n")
	}

	// Render status bar
	content.WriteString(m.renderStatusBar())
	content.WriteString("\n")

	// Render help
	if m.showHelp {
		content.WriteString("\n")
		content.WriteString(m.help.View(m.keyMap))
	} else {
		content.WriteString(m.help.View(m.keyMap))
	}

	return content.String()
}

// renderTabBar renders the tab bar
func (m Model) renderTabBar() string {
	tabs := []string{"Projects", "Documents", "Extractions"}
	var renderedTabs []string

	activeStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("12")).
		Background(lipgloss.Color("237")).
		Padding(0, 2)

	inactiveStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("240")).
		Padding(0, 2)

	for i, tab := range tabs {
		if i == m.activeTab {
			renderedTabs = append(renderedTabs, activeStyle.Render(tab))
		} else {
			renderedTabs = append(renderedTabs, inactiveStyle.Render(tab))
		}
	}

	return lipgloss.JoinHorizontal(lipgloss.Top, renderedTabs...)
}

// renderSearchBar renders the search input
func (m Model) renderSearchBar() string {
	style := lipgloss.NewStyle().
		Foreground(lipgloss.Color("12")).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("240")).
		Padding(0, 1)

	return style.Render("Search: " + m.searchInput.View())
}

// renderStatusBar renders the status bar
func (m Model) renderStatusBar() string {
	style := lipgloss.NewStyle().
		Foreground(lipgloss.Color("240")).
		Padding(0, 1)

	status := m.statusMsg
	if m.selectedProjectID != "" {
		status += fmt.Sprintf(" | Project: %s", m.selectedProjectID)
	}

	return style.Render(status)
}

// renderDetails renders the details view
func (m Model) renderDetails() string {
	style := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("240")).
		Padding(1, 2)

	return style.Render("Details view - coming soon\nPress Esc to go back")
}

// errorView renders an error message
func errorView(err error) string {
	style := lipgloss.NewStyle().
		Foreground(lipgloss.Color("9")).
		Bold(true).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("9")).
		Padding(1, 2)

	return style.Render(fmt.Sprintf("Error: %s\n\nPress q to quit", err.Error()))
}

// New creates a new TUI model.
func New(client *client.Client) Model {
	// Create lists
	projectsList := list.New([]list.Item{}, list.NewDefaultDelegate(), 0, 0)
	projectsList.Title = "Projects"

	documentsList := list.New([]list.Item{}, list.NewDefaultDelegate(), 0, 0)
	documentsList.Title = "Documents"

	extractionsList := list.New([]list.Item{}, list.NewDefaultDelegate(), 0, 0)
	extractionsList.Title = "Extractions"

	// Create search input
	searchInput := textinput.New()
	searchInput.Placeholder = "Search..."

	return Model{
		client:          client,
		ready:           false,
		currentView:     ProjectsView,
		activeTab:       0,
		projectsList:    projectsList,
		documentsList:   documentsList,
		extractionsList: extractionsList,
		searchInput:     searchInput,
		help:            help.New(),
		keyMap:          DefaultKeyMap(),
		showHelp:        false,
	}
}

// Messages

type projectsLoadedMsg struct {
	projects []interface{}
}

type documentsLoadedMsg struct {
	documents []interface{}
}

type errMsg struct {
	err error
}

// Commands

func loadProjects(client *client.Client) tea.Cmd {
	return func() tea.Msg {
		// TODO: Load projects from API
		return projectsLoadedMsg{projects: []interface{}{}}
	}
}

func loadDocuments(client *client.Client, projectID string) tea.Cmd {
	return func() tea.Msg {
		// TODO: Load documents from API
		return documentsLoadedMsg{documents: []interface{}{}}
	}
}

func performSearch(client *client.Client, query string) tea.Cmd {
	return func() tea.Msg {
		// TODO: Perform search
		return nil
	}
}

// List items

type projectItem struct {
	id   string
	name string
	desc string
}

func (p projectItem) FilterValue() string { return p.name }
func (p projectItem) Title() string       { return p.name }
func (p projectItem) Description() string { return p.desc }

type documentItem struct {
	id       string
	filename string
	status   string
}

func (d documentItem) FilterValue() string { return d.filename }
func (d documentItem) Title() string       { return d.filename }
func (d documentItem) Description() string { return d.status }
