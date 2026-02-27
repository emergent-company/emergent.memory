// Package tui provides the terminal UI (TUI) for interactive browsing.
package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/emergent-company/emergent/apps/server-go/pkg/sdk/documents"
	"github.com/emergent-company/emergent/apps/server-go/pkg/sdk/health"
	"github.com/emergent-company/emergent/apps/server-go/pkg/sdk/projects"
	"github.com/emergent-company/emergent/apps/server-go/pkg/sdk/search"
	"github.com/emergent-company/emergent/apps/server-go/pkg/sdk/templatepacks"
	"github.com/emergent-company/emergent/tools/emergent-cli/internal/client"
)

// ViewMode represents the current view mode
type ViewMode int

const (
	ProjectsView ViewMode = iota
	DocumentsView
	WorkerStatsView
	TemplatePacksView
	QueryView
	ExtractionsView
	TracesView      // tab 6
	DetailsView     // not a tab — document details overlay
	TraceDetailView // not a tab — trace details overlay
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

	// Worker stats
	workerStats     *health.AllJobMetrics
	lastStatsUpdate time.Time

	// Template packs
	templatePacks          []templatepacks.InstalledPackItem
	compiledTypes          *templatepacks.CompiledTypesResponse
	lastTemplatePacksFetch time.Time

	// Query
	queryMode    bool
	queryInput   textinput.Model
	queryResults []search.SearchResult
	queryError   error
	isQuerying   bool

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
	selectedProjectID   string
	selectedProjectName string
	selectedOrgID       string
	selectedDocID       string

	// Traces (Tempo)
	tracesList         list.Model
	tracesData         []traceResult
	selectedTraceID    string
	selectedTraceData  *traceOTLPResp
	tracesLoading      bool
	tracesErr          error
	tempoURL           string
}

// KeyMap defines keybindings
type KeyMap struct {
	Up       key.Binding
	Down     key.Binding
	Enter    key.Binding
	Back     key.Binding
	Tab      key.Binding
	Search   key.Binding
	Query    key.Binding
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
		Query: key.NewBinding(
			key.WithKeys("ctrl+q"),
			key.WithHelp("ctrl+q", "query project"),
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
	return []key.Binding{k.Up, k.Down, k.Enter, k.Search, k.Query, k.Help, k.Quit}
}

// FullHelp returns keybindings for the expanded help view
func (k KeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.Enter, k.Back},
		{k.Tab, k.Search, k.Query, k.NextPage, k.PrevPage},
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
		m.tracesList.SetSize(m.width-4, m.height-10)

	case tea.KeyMsg:
		// Global keybindings
		if m.searchMode {
			return m.handleSearchInput(msg)
		}

		if m.queryMode {
			return m.handleQueryInput(msg)
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

		case key.Matches(msg, m.keyMap.Query):
			// Switch to query view
			m.currentView = QueryView
			m.activeTab = 4 // Query tab index
			// Only focus input if project is selected
			if m.selectedProjectID != "" {
				m.queryMode = true
				m.queryInput.Focus()
				return m, textinput.Blink
			}
			return m, nil

		case key.Matches(msg, m.keyMap.Tab):
			m.activeTab = (m.activeTab + 1) % 7
			m.currentView = ViewMode(m.activeTab)
			// Load worker stats when switching to that tab
			if m.currentView == WorkerStatsView {
				return m, loadWorkerStats(m.client)
			}
			// Load template packs when switching to that tab
			if m.currentView == TemplatePacksView {
				return m, loadTemplatePacks(m.client)
			}
			// Focus query input when switching to query tab (only if project selected)
			if m.currentView == QueryView && m.selectedProjectID != "" {
				m.queryMode = true
				m.queryInput.Focus()
				return m, textinput.Blink
			}
			// Load traces when switching to traces tab
			if m.currentView == TracesView {
				m.tracesLoading = true
				m.tracesErr = nil
				return m, loadTraces(m.tempoURL)
			}
			return m, nil

		case key.Matches(msg, m.keyMap.Back):
			switch m.currentView {
			case TraceDetailView:
				// Go back from trace detail to traces list
				m.currentView = TracesView
				m.activeTab = 6
				m.selectedTraceID = ""
				m.selectedTraceData = nil
			case DetailsView:
				// Go back from details to documents
				m.currentView = DocumentsView
				m.selectedDocID = ""
			case DocumentsView:
				// Go back from documents to projects
				m.currentView = ProjectsView
				m.activeTab = 0
				m.selectedProjectID = ""
			}
			return m, nil

		case key.Matches(msg, m.keyMap.Enter):
			return m.handleEnter()
		}

	case projectsLoadedMsg:
		m.statusMsg = fmt.Sprintf("Loaded %d projects", len(msg.projects))
		// Convert to list items
		items := make([]list.Item, len(msg.projects))
		for i, p := range msg.projects {
			items[i] = p.(list.Item)
		}
		m.projectsList.SetItems(items)
		return m, nil

	case documentsLoadedMsg:
		m.statusMsg = fmt.Sprintf("Loaded %d documents", len(msg.documents))
		// Convert to list items
		items := make([]list.Item, len(msg.documents))
		for i, d := range msg.documents {
			items[i] = d.(list.Item)
		}
		m.documentsList.SetItems(items)
		return m, nil

	case workerStatsLoadedMsg:
		m.workerStats = msg.stats
		m.lastStatsUpdate = time.Now()
		m.statusMsg = "Worker stats updated"
		// Schedule next refresh if we're still on worker stats view
		if m.currentView == WorkerStatsView {
			return m, tickEvery(3 * time.Second)
		}
		return m, nil

	case templatePacksLoadedMsg:
		m.templatePacks = msg.packs
		m.compiledTypes = msg.compiledTypes
		m.lastTemplatePacksFetch = time.Now()
		m.statusMsg = fmt.Sprintf("Loaded %d template packs", len(msg.packs))
		return m, nil

	case queryResultsMsg:
		m.queryResults = msg.results
		m.isQuerying = false
		m.statusMsg = fmt.Sprintf("Found %d results in %v for: %s", len(msg.results), msg.duration.Round(time.Millisecond), msg.query)
		return m, nil

	case tickMsg:
		// Auto-refresh worker stats if we're on that view
		if m.currentView == WorkerStatsView {
			return m, loadWorkerStats(m.client)
		}
		return m, nil

	case tracesLoadedMsg:
		m.tracesLoading = false
		m.tracesData = msg.traces
		items := make([]list.Item, len(msg.traces))
		for i, t := range msg.traces {
			ts := ""
			if t.StartTimeUnixNano != "" {
				ts = traceNanoToTime(t.StartTimeUnixNano).Format("15:04:05")
			}
			svc := t.RootServiceName
			if svc == "" {
				svc = "unknown"
			}
			root := t.RootTraceName
			if root == "" {
				root = "(no name)"
			}
			items[i] = traceItem{
				traceID:    t.TraceID,
				service:    svc,
				rootSpan:   root,
				durationMs: t.DurationMs,
				timestamp:  ts,
			}
		}
		m.tracesList.SetItems(items)
		m.statusMsg = fmt.Sprintf("Loaded %d traces", len(msg.traces))
		return m, nil

	case traceDetailLoadedMsg:
		if msg.err != nil {
			m.tracesErr = msg.err
			m.statusMsg = "Failed to load trace"
			return m, nil
		}
		m.selectedTraceData = msg.detail
		m.currentView = TraceDetailView
		m.statusMsg = "Trace: " + traceShortID(m.selectedTraceID)
		return m, nil

	case tracesErrMsg:
		m.tracesLoading = false
		m.tracesErr = msg.err
		m.statusMsg = "Tempo unavailable"
		return m, nil

	case errMsg:
		// For query errors, just set queryError (don't block the whole UI)
		if m.isQuerying || m.currentView == QueryView {
			m.queryError = msg.err
			m.isQuerying = false
			m.statusMsg = "Query failed"
		} else {
			// For other errors, set m.err to show full error screen
			m.err = msg.err
		}
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
	case TracesView:
		m.tracesList, cmd = m.tracesList.Update(msg)
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
			m.selectedProjectName = item.name
			m.selectedOrgID = item.orgID
			// Set the SDK client context for this project
			m.client.SetContext(item.orgID, item.id)
			m.currentView = DocumentsView
			m.activeTab = 1 // Sync active tab to Documents
			return m, loadDocuments(m.client, item.id)
		}
	case DocumentsView:
		// Get selected document
		if item, ok := m.documentsList.SelectedItem().(documentItem); ok {
			m.selectedDocID = item.id
			m.currentView = DetailsView
		}
	case TracesView:
		// Get selected trace
		if item, ok := m.tracesList.SelectedItem().(traceItem); ok {
			m.selectedTraceID = item.traceID
			m.selectedTraceData = nil
			m.statusMsg = "Loading trace..."
			return m, loadTraceDetail(m.tempoURL, item.traceID)
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

// handleQueryInput handles query input in Query view
func (m Model) handleQueryInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEscape:
		m.queryMode = false
		m.queryInput.Blur()
		m.queryError = nil // Clear any error when exiting query mode
		return m, nil

	case tea.KeyEnter:
		query := m.queryInput.Value()
		if query == "" {
			return m, nil
		}
		// Execute query
		m.isQuerying = true
		m.queryError = nil
		m.statusMsg = "Executing query..."
		return m, performQuery(m.client, query)
	}

	// Clear error when user starts typing again
	if m.queryError != nil {
		m.queryError = nil
	}

	var cmd tea.Cmd
	m.queryInput, cmd = m.queryInput.Update(msg)
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
		style := lipgloss.NewStyle().
			Foreground(lipgloss.Color("9")).
			Bold(true).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("9")).
			Padding(1, 2)
		return style.Render(fmt.Sprintf("Terminal too small\nMinimum: 80x24\nCurrent: %dx%d\n\nPress q to quit", m.width, m.height))
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
	case WorkerStatsView:
		content.WriteString(m.renderWorkerStats())
	case TemplatePacksView:
		content.WriteString(m.renderTemplatePacks())
	case QueryView:
		content.WriteString(m.renderQuery())
	case ExtractionsView:
		content.WriteString(m.extractionsList.View())
	case TracesView:
		content.WriteString(m.renderTraces())
	case TraceDetailView:
		content.WriteString(m.renderTraceDetail())
	case DetailsView:
		content.WriteString(m.renderDetails())
	}

	content.WriteString("\n")

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
	}

	return content.String()
}

// renderTabBar renders the tab bar
func (m Model) renderTabBar() string {
	tabs := []string{"Projects", "Documents", "Worker Stats", "Template Packs", "Query", "Extractions", "Traces"}
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

	projectStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("12")).
		Bold(true)

	status := m.statusMsg
	if m.selectedProjectName != "" {
		status += " | Current Project: " + projectStyle.Render(m.selectedProjectName)
	} else if m.currentView != ProjectsView && m.currentView != TracesView && m.currentView != TraceDetailView {
		status += " | " + lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Render("No project selected")
	}

	return style.Render(status)
}

// renderTraces renders the traces list view.
func (m Model) renderTraces() string {
	if m.tracesLoading {
		loadingStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Padding(1, 2)
		return loadingStyle.Render("⏳ Loading traces from Tempo...")
	}
	if m.tracesErr != nil {
		var content strings.Builder
		errStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Bold(true).Padding(1, 2)
		hintStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Padding(0, 2)
		content.WriteString(errStyle.Render("⚠  Cannot reach Tempo\n" + m.tracesErr.Error()))
		content.WriteString("\n\n")
		content.WriteString(hintStyle.Render(
			"Start Tempo first:\n  docker compose --profile observability up tempo -d\n\nURL: " + m.tempoURL +
				"\nOverride: set EMERGENT_TEMPO_URL env var\n\nSwitch to this tab again to retry."))
		return content.String()
	}
	return m.tracesList.View()
}

// renderTraceDetail renders the full span tree for the selected trace.
func (m Model) renderTraceDetail() string {
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12")).Padding(0, 1)
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Padding(0, 1)
	okStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	errStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	attrStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))

	var content strings.Builder
	content.WriteString(headerStyle.Render("Trace: " + m.selectedTraceID))
	content.WriteString("\n")
	content.WriteString(dimStyle.Render("Press Esc to go back"))
	content.WriteString("\n\n")

	if m.selectedTraceData == nil {
		content.WriteString("  Loading...")
		return content.String()
	}

	// Build span node map
	type spanNode struct {
		span     traceSpan
		children []*spanNode
	}
	nodes := map[string]*spanNode{}
	var roots []*spanNode

	for _, batch := range m.selectedTraceData.Batches {
		for _, ss := range batch.ScopeSpans {
			for _, s := range ss.Spans {
				n := &spanNode{span: s}
				nodes[s.SpanID] = n
			}
		}
	}
	for _, n := range nodes {
		if n.span.ParentSpanID == "" {
			roots = append(roots, n)
		} else if parent, ok := nodes[n.span.ParentSpanID]; ok {
			parent.children = append(parent.children, n)
		} else {
			roots = append(roots, n)
		}
	}

	// Cap rendered lines to avoid overflowing terminal
	maxLines := m.height - 10
	lines := 0

	var printNode func(n *spanNode, indent int)
	printNode = func(n *spanNode, indent int) {
		if lines >= maxLines {
			return
		}
		s := n.span
		durMs := traceSpanDurMs(s.StartTimeUnixNano, s.EndTimeUnixNano)

		icon := okStyle.Render("✓")
		if s.Status.Code == 2 {
			icon = errStyle.Render("✗")
		}

		prefix := strings.Repeat("  ", indent)
		content.WriteString(fmt.Sprintf("%s%s %s  [%s]\n", prefix, icon, s.Name, traceFmtDur(durMs)))
		lines++

		for _, key := range []string{"http.method", "http.route", "http.status_code", "db.statement", "error"} {
			if lines >= maxLines {
				return
			}
			if v := traceAttrValue(s.Attributes, key); v != "" {
				if len(v) > 70 {
					v = v[:69] + "…"
				}
				content.WriteString(attrStyle.Render(fmt.Sprintf("%s    %s: %s\n", prefix, key, v)))
				lines++
			}
		}

		for _, c := range n.children {
			printNode(c, indent+1)
		}
	}

	for _, r := range roots {
		printNode(r, 0)
	}

	if lines >= maxLines {
		content.WriteString(dimStyle.Render("  … (truncated — terminal too small to show all spans)"))
	}

	return content.String()
}

// renderDetails renders the details view
func (m Model) renderDetails() string {
	style := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("240")).
		Padding(1, 2)

	return style.Render("Details view - coming soon\nPress Esc to go back")
}

// renderQuery renders the query view
func (m Model) renderQuery() string {
	var content strings.Builder

	// Header
	headerStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("12")).
		Padding(0, 1)

	title := "Natural Language Query"
	if m.selectedProjectName != "" {
		title += " - " + m.selectedProjectName
	}
	content.WriteString(headerStyle.Render(title))
	content.WriteString("\n\n")

	// Check if project is selected
	if m.selectedProjectID == "" {
		warningStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("11")).
			Bold(true).
			Padding(1, 2)

		helpStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("240")).
			Padding(1, 2)

		content.WriteString(warningStyle.Render("⚠  No Project Selected"))
		content.WriteString("\n\n")
		content.WriteString(helpStyle.Render("To query a project:\n1. Press Tab to go to Projects\n2. Press Enter on a project to select it\n3. Press Ctrl+Q or Tab to Query"))
		return content.String()
	}

	// Query input
	inputStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("12")).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("240")).
		Padding(0, 1).
		Width(m.width - 6)

	content.WriteString(inputStyle.Render("Question: " + m.queryInput.View()))
	content.WriteString("\n\n")

	// Instructions
	instructionStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("240")).
		Padding(0, 1)

	content.WriteString(instructionStyle.Render("Type your question and press Enter to search. Press Esc to cancel."))
	content.WriteString("\n\n")

	// Show loading indicator
	if m.isQuerying {
		loadingStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("11")).
			Bold(true).
			Padding(0, 1)
		content.WriteString(loadingStyle.Render("⏳ Searching..."))
		content.WriteString("\n")
		return content.String()
	}

	// Show error if any
	if m.queryError != nil {
		errorStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("9")).
			Bold(true).
			Padding(0, 1)
		helpStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("240")).
			Padding(0, 1)

		content.WriteString(errorStyle.Render(fmt.Sprintf("❌ Error: %s", m.queryError.Error())))
		content.WriteString("\n\n")
		content.WriteString(helpStyle.Render("Press Esc to dismiss error, or start typing to try again. Press q to quit."))
		content.WriteString("\n")
		return content.String()
	}

	// Show results
	if len(m.queryResults) > 0 {
		resultHeaderStyle := lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("10")).
			Padding(0, 1)

		content.WriteString(resultHeaderStyle.Render(fmt.Sprintf("Results (%d found):", len(m.queryResults))))
		content.WriteString("\n\n")

		// Render each result
		for i, result := range m.queryResults {
			resultStyle := lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("240")).
				Padding(1, 2).
				Width(m.width - 8)

			scoreStyle := lipgloss.NewStyle().
				Foreground(lipgloss.Color("14")).
				Bold(true)

			docStyle := lipgloss.NewStyle().
				Foreground(lipgloss.Color("240"))

			resultText := fmt.Sprintf("%s (Score: %s)\n\n%s\n\n%s",
				scoreStyle.Render(fmt.Sprintf("Result #%d", i+1)),
				scoreStyle.Render(fmt.Sprintf("%.2f", result.Score)),
				result.Content,
				docStyle.Render(fmt.Sprintf("Document ID: %s | Chunk ID: %s", result.DocumentID, result.ChunkID)))

			content.WriteString(resultStyle.Render(resultText))
			content.WriteString("\n")

			// Limit display to avoid overwhelming the terminal
			if i >= 2 {
				moreStyle := lipgloss.NewStyle().
					Foreground(lipgloss.Color("240")).
					Italic(true).
					Padding(0, 1)
				content.WriteString(moreStyle.Render(fmt.Sprintf("... and %d more results", len(m.queryResults)-3)))
				content.WriteString("\n")
				break
			}
		}
	}

	return content.String()
}

// renderWorkerStats renders the worker stats view
func (m Model) renderWorkerStats() string {
	if m.workerStats == nil {
		return "Loading worker statistics..."
	}

	var content strings.Builder

	// Header
	headerStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("12")).
		Padding(0, 1)

	title := "Worker Queue Statistics"
	if m.selectedProjectName != "" {
		title += " - " + m.selectedProjectName
	} else {
		title += " - All Projects"
	}
	content.WriteString(headerStyle.Render(title))
	content.WriteString("\n\n")

	// Calculate column widths
	nameWidth := 25
	numberWidth := 12

	// Table header style
	tableHeaderStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("14")).
		Padding(0, 1)

	// Table cell styles
	normalCellStyle := lipgloss.NewStyle().Padding(0, 1)
	pendingStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Padding(0, 1)
	processingStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("12")).Padding(0, 1)
	completedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Padding(0, 1)
	failedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Padding(0, 1)

	// Render header (without lipgloss padding to ensure alignment)
	header := fmt.Sprintf("%-*s  %*s  %*s  %*s  %*s  %*s",
		nameWidth, "Queue",
		numberWidth, "Pending",
		numberWidth, "Processing",
		numberWidth, "Completed",
		numberWidth, "Failed",
		numberWidth, "Total")
	content.WriteString(tableHeaderStyle.Render(header))
	content.WriteString("\n")

	// Separator (match exact header width: nameWidth + 5 number columns + 10 spaces between)
	totalWidth := nameWidth + (numberWidth * 5) + 10
	separator := strings.Repeat("─", totalWidth)
	content.WriteString(normalCellStyle.Render(separator))
	content.WriteString("\n")

	// Render each queue
	for _, queue := range m.workerStats.Queues {
		row := fmt.Sprintf("%-*s  %s  %s  %s  %s  %*d",
			nameWidth, queue.Queue,
			pendingStyle.Render(fmt.Sprintf("%*d", numberWidth, queue.Pending)),
			processingStyle.Render(fmt.Sprintf("%*d", numberWidth, queue.Processing)),
			completedStyle.Render(fmt.Sprintf("%*d", numberWidth, queue.Completed)),
			failedStyle.Render(fmt.Sprintf("%*d", numberWidth, queue.Failed)),
			numberWidth, queue.Total)
		content.WriteString(normalCellStyle.Render(row))
		content.WriteString("\n")
	}

	content.WriteString("\n")

	// Footer with last update time
	footerStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("240")).
		Padding(0, 1)

	elapsed := time.Since(m.lastStatsUpdate).Round(time.Second)
	footer := fmt.Sprintf("Last updated: %s ago • Auto-refreshing every 3s", elapsed)
	content.WriteString(footerStyle.Render(footer))

	return content.String()
}

// renderTemplatePacks renders the template packs view with full details
func (m Model) renderTemplatePacks() string {
	if m.selectedProjectID == "" {
		style := lipgloss.NewStyle().
			Foreground(lipgloss.Color("11")).
			Bold(true).
			Padding(2, 4)

		helpStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("240")).
			Padding(1, 4)

		var content strings.Builder
		content.WriteString(style.Render("⚠  No Project Selected"))
		content.WriteString("\n\n")
		content.WriteString(helpStyle.Render("To view template packs:\n1. Press Tab to go to Projects\n2. Press Enter on a project to select it\n3. Press Tab to return to Template Packs"))
		return content.String()
	}

	if m.templatePacks == nil {
		return "Loading template packs..."
	}

	var content strings.Builder

	// Header
	headerStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("12")).
		Padding(0, 1)

	content.WriteString(headerStyle.Render("Installed Template Packs"))
	content.WriteString("\n\n")

	if len(m.templatePacks) == 0 {
		content.WriteString("No template packs installed for this project.\n")
		return content.String()
	}

	// Styles
	packHeaderStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("14")).
		Padding(0, 1)

	sectionStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("11")).
		Bold(true).
		Padding(0, 1)

	labelStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("240")).
		Padding(0, 1)

	valueStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("15")).
		Padding(0, 1)

	fieldStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("10")).
		Padding(0, 1)

	typeStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("12")).
		Padding(0, 1)

	// Render each installed pack
	for i, pack := range m.templatePacks {
		// Pack header
		packTitle := fmt.Sprintf("📦 %s (v%s)", pack.Name, pack.Version)
		if !pack.Active {
			packTitle += " [INACTIVE]"
		}
		content.WriteString(packHeaderStyle.Render(packTitle))
		content.WriteString("\n")

		// Pack details
		if pack.Description != nil && *pack.Description != "" {
			content.WriteString(labelStyle.Render("  Description: "))
			content.WriteString(valueStyle.Render(*pack.Description))
			content.WriteString("\n")
		}

		content.WriteString(labelStyle.Render("  Installed: "))
		content.WriteString(valueStyle.Render(pack.InstalledAt.Format("2006-01-02 15:04:05")))
		content.WriteString("\n")

		// Find object types from this pack
		var objectTypes []templatepacks.ObjectTypeSchema
		var relationshipTypes []templatepacks.RelationshipTypeSchema

		if m.compiledTypes != nil {
			for _, ot := range m.compiledTypes.ObjectTypes {
				if ot.PackID == pack.TemplatePackID {
					objectTypes = append(objectTypes, ot)
				}
			}
			for _, rt := range m.compiledTypes.RelationshipTypes {
				if rt.PackID == pack.TemplatePackID {
					relationshipTypes = append(relationshipTypes, rt)
				}
			}
		}

		// Object Types section
		if len(objectTypes) > 0 {
			content.WriteString("\n")
			content.WriteString(sectionStyle.Render("  Object Types:"))
			content.WriteString("\n")

			for _, ot := range objectTypes {
				content.WriteString(typeStyle.Render(fmt.Sprintf("    • %s", ot.Name)))
				if ot.Label != "" {
					content.WriteString(fmt.Sprintf(" (%s)", ot.Label))
				}
				content.WriteString("\n")

				if ot.Description != "" {
					content.WriteString(labelStyle.Render(fmt.Sprintf("      %s", ot.Description)))
					content.WriteString("\n")
				}

				// Parse and display properties if available
				if len(ot.Properties) > 0 {
					var props map[string]interface{}
					if err := json.Unmarshal(ot.Properties, &props); err == nil {
						if propsMap, ok := props["properties"].(map[string]interface{}); ok {
							content.WriteString(labelStyle.Render("      Fields: "))
							fieldNames := make([]string, 0, len(propsMap))
							for name := range propsMap {
								fieldNames = append(fieldNames, name)
							}
							content.WriteString(fieldStyle.Render(strings.Join(fieldNames, ", ")))
							content.WriteString("\n")
						}
					}
				}
			}
		}

		// Relationship Types section
		if len(relationshipTypes) > 0 {
			content.WriteString("\n")
			content.WriteString(sectionStyle.Render("  Relationship Types:"))
			content.WriteString("\n")

			for _, rt := range relationshipTypes {
				relDesc := fmt.Sprintf("    • %s", rt.Name)
				if rt.SourceType != "" && rt.TargetType != "" {
					relDesc += fmt.Sprintf(" (%s → %s)", rt.SourceType, rt.TargetType)
				}
				content.WriteString(typeStyle.Render(relDesc))
				content.WriteString("\n")

				if rt.Description != "" {
					content.WriteString(labelStyle.Render(fmt.Sprintf("      %s", rt.Description)))
					content.WriteString("\n")
				}
			}
		}

		// Add spacing between packs
		if i < len(m.templatePacks)-1 {
			content.WriteString("\n")
			separator := strings.Repeat("─", 80)
			content.WriteString(labelStyle.Render(separator))
			content.WriteString("\n\n")
		}
	}

	// Summary footer
	content.WriteString("\n")
	footerStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("240")).
		Padding(0, 1)

	totalObjectTypes := 0
	totalRelTypes := 0
	if m.compiledTypes != nil {
		totalObjectTypes = len(m.compiledTypes.ObjectTypes)
		totalRelTypes = len(m.compiledTypes.RelationshipTypes)
	}

	footer := fmt.Sprintf("Total: %d packs • %d object types • %d relationship types",
		len(m.templatePacks), totalObjectTypes, totalRelTypes)
	content.WriteString(footerStyle.Render(footer))

	return content.String()
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
	// Create delegate with custom styles
	delegate := list.NewDefaultDelegate()

	// Create lists
	projectsList := list.New([]list.Item{}, delegate, 0, 0)
	projectsList.Title = "Projects"
	projectsList.SetShowHelp(false)         // Disable list's default help
	projectsList.SetShowStatusBar(false)    // Disable list's status bar
	projectsList.SetFilteringEnabled(false) // We'll handle filtering ourselves
	// Set custom empty message with proper indentation
	emptyStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Padding(0, 2)
	projectsList.Styles.NoItems = emptyStyle

	documentsList := list.New([]list.Item{}, delegate, 0, 0)
	documentsList.Title = "Documents"
	documentsList.SetShowHelp(false)
	documentsList.SetShowStatusBar(false)
	documentsList.SetFilteringEnabled(false)
	documentsList.Styles.NoItems = emptyStyle

	extractionsList := list.New([]list.Item{}, delegate, 0, 0)
	extractionsList.Title = "Extractions"
	extractionsList.SetShowHelp(false)
	extractionsList.SetShowStatusBar(false)
	extractionsList.SetFilteringEnabled(false)
	extractionsList.Styles.NoItems = emptyStyle

	tracesList := list.New([]list.Item{}, delegate, 0, 0)
	tracesList.Title = "Traces  (press Enter for detail, Tab to refresh)"
	tracesList.SetShowHelp(false)
	tracesList.SetShowStatusBar(false)
	tracesList.SetFilteringEnabled(false)
	tracesList.Styles.NoItems = emptyStyle

	// Create search input
	searchInput := textinput.New()
	searchInput.Placeholder = "Search..."

	// Create query input
	queryInput := textinput.New()
	queryInput.Placeholder = "Ask a question about your project..."
	queryInput.Width = 80

	return Model{
		client:          client,
		ready:           false,
		currentView:     ProjectsView,
		activeTab:       0,
		projectsList:    projectsList,
		documentsList:   documentsList,
		extractionsList: extractionsList,
		tracesList:      tracesList,
		searchInput:     searchInput,
		queryInput:      queryInput,
		help:            help.New(),
		keyMap:          DefaultKeyMap(),
		showHelp:        false,
		statusMsg:       "Loading projects...",
		tempoURL:        resolveTempoURL(),
	}
}

// Messages

type projectsLoadedMsg struct {
	projects []interface{}
}

type documentsLoadedMsg struct {
	documents []interface{}
}

type workerStatsLoadedMsg struct {
	stats *health.AllJobMetrics
}

type templatePacksLoadedMsg struct {
	packs         []templatepacks.InstalledPackItem
	compiledTypes *templatepacks.CompiledTypesResponse
}

type queryResultsMsg struct {
	results  []search.SearchResult
	query    string
	duration time.Duration
}

type errMsg struct {
	err error
}

type tickMsg time.Time

// Commands

func loadProjects(client *client.Client) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		projects, err := client.SDK.Projects.List(ctx, &projects.ListOptions{
			IncludeStats: true,
		})
		if err != nil {
			return errMsg{err: fmt.Errorf("failed to load projects: %w", err)}
		}

		// Convert to list items
		items := make([]interface{}, len(projects))
		for i, p := range projects {
			desc := fmt.Sprintf("ID: %s", p.ID)
			if p.Stats != nil {
				desc = fmt.Sprintf("%d docs, %d objects, %d relationships",
					p.Stats.DocumentCount,
					p.Stats.ObjectCount,
					p.Stats.RelationshipCount)
			}
			items[i] = projectItem{
				id:    p.ID,
				orgID: p.OrgID,
				name:  p.Name,
				desc:  desc,
			}
		}

		return projectsLoadedMsg{projects: items}
	}
}

func loadDocuments(client *client.Client, projectID string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		// Fetch documents (context already set when project was selected)
		result, err := client.SDK.Documents.List(ctx, &documents.ListOptions{
			Limit: 100,
		})
		if err != nil {
			return errMsg{err: fmt.Errorf("failed to load documents: %w", err)}
		}

		// Convert to list items
		items := make([]interface{}, len(result.Documents))
		for i, d := range result.Documents {
			filename := "Untitled"
			if d.Filename != nil && *d.Filename != "" {
				filename = *d.Filename
			}

			status := "Unknown"
			if d.ConversionStatus != nil {
				status = *d.ConversionStatus
			}

			items[i] = documentItem{
				id:       d.ID,
				filename: filename,
				status:   fmt.Sprintf("Status: %s | Chunks: %d", status, d.Chunks),
			}
		}

		return documentsLoadedMsg{documents: items}
	}
}

func loadWorkerStats(client *client.Client) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		// Get project ID from client context (empty string if no project selected = all projects)
		projectID := client.ProjectID()

		stats, err := client.SDK.Health.JobMetrics(ctx, projectID)
		if err != nil {
			return errMsg{err: fmt.Errorf("failed to load worker stats: %w", err)}
		}

		return workerStatsLoadedMsg{stats: stats}
	}
}

func loadTemplatePacks(client *client.Client) tea.Cmd {
	return func() tea.Msg {
		// Check if project context is set
		if client.ProjectID() == "" {
			// Don't return error, just return empty - UI will show "no project selected" message
			return templatePacksLoadedMsg{
				packs:         []templatepacks.InstalledPackItem{},
				compiledTypes: &templatepacks.CompiledTypesResponse{},
			}
		}

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		// Get installed packs for the project
		packs, err := client.SDK.TemplatePacks.GetInstalledPacks(ctx)
		if err != nil {
			return errMsg{err: fmt.Errorf("failed to load template packs: %w", err)}
		}

		// Get compiled types (all object and relationship types from all packs)
		compiledTypes, err := client.SDK.TemplatePacks.GetCompiledTypes(ctx)
		if err != nil {
			return errMsg{err: fmt.Errorf("failed to load compiled types: %w", err)}
		}

		return templatePacksLoadedMsg{
			packs:         packs,
			compiledTypes: compiledTypes,
		}
	}
}

func performSearch(client *client.Client, query string) tea.Cmd {
	return func() tea.Msg {
		// TODO: Perform search
		return nil
	}
}

func performQuery(client *client.Client, query string) tea.Cmd {
	return func() tea.Msg {
		start := time.Now()
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()

		// Perform unified search with weighted fusion strategy
		response, err := client.SDK.Search.Search(ctx, &search.SearchRequest{
			Query:          query,
			FusionStrategy: "weighted", // Use weighted fusion for best results
			ResultTypes:    "both",     // Search both graph and text
			Limit:          10,
		})
		elapsed := time.Since(start)

		if err != nil {
			return errMsg{err: fmt.Errorf("query failed after %v: %w", elapsed, err)}
		}

		return queryResultsMsg{
			results:  response.Results,
			query:    query,
			duration: elapsed,
		}
	}
}

// Auto-refresh ticker for worker stats
func tickEvery(interval time.Duration) tea.Cmd {
	return tea.Tick(interval, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

// List items

type projectItem struct {
	id    string
	orgID string
	name  string
	desc  string
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

// traceItem is a list.Item for the Traces tab.
type traceItem struct {
	traceID    string
	service    string
	rootSpan   string
	durationMs float64
	timestamp  string
}

func (t traceItem) FilterValue() string { return t.rootSpan + " " + t.service }
func (t traceItem) Title() string {
	return fmt.Sprintf("[%s]  %s", traceShortID(t.traceID), t.rootSpan)
}
func (t traceItem) Description() string {
	return fmt.Sprintf("%s  •  %s  •  %s", t.service, traceFmtDur(t.durationMs), t.timestamp)
}
