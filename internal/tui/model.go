package tui

import (
	"context"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"

	"github.com/mr-jones123/sesh/internal/catalog"
	"github.com/mr-jones123/sesh/internal/handoff"
)

type screen uint8

const (
	screenBrowse screen = iota
	screenTarget
	screenConfirm
	screenCreating
)

type scope uint8

const (
	scopeProject scope = iota
	scopeAll
)

var targets = [...]string{"claude", "codex", "pi", "omp"}

type model struct {
	ctx              context.Context
	cancelOperations context.CancelFunc
	opts             Options
	list             listFunc
	create           createFunc

	width  int
	height int
	theme  theme

	screen       screen
	scope        scope
	searching    bool
	search       string
	loading      bool
	generation   uint64
	entries      []catalog.Entry
	visible      []catalog.Entry
	cursor       int
	targetCursor int
	selected     catalog.Entry
	hasSelected  bool
	loadErr      error
	actionErr    error

	result              *handoff.Result
	runErr              error
	cancelled           bool
	createCancelPending bool
}

func newModel(ctx context.Context, cancel context.CancelFunc, opts Options) model {
	selectedScope := scopeProject
	if opts.All {
		selectedScope = scopeAll
	}
	return model{
		ctx:              ctx,
		cancelOperations: cancel,
		opts:             opts,
		list:             catalog.List,
		create:           handoff.Create,
		width:            80,
		height:           24,
		theme:            newTheme(true),
		scope:            selectedScope,
		loading:          true,
		generation:       1,
	}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(loadSessions(m.ctx, m.generation, m.catalogQuery(), m.list), tea.RequestBackgroundColor)
}

func (m model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := message.(type) {
	case tea.BackgroundColorMsg:
		m.theme = newTheme(msg.IsDark())
		return m, nil
	case tea.WindowSizeMsg:
		m.width = max(msg.Width, 1)
		m.height = max(msg.Height, 1)
		return m, nil
	case listMsg:
		if msg.generation != m.generation {
			return m, nil
		}
		m.loading = false
		m.loadErr = msg.err
		m.entries = msg.entries
		m.cursor = 0
		m.rebuildVisible()
		return m, nil
	case createMsg:
		if m.createCancelPending {
			if msg.err == nil {
				m.runErr = fmt.Errorf(
					"handoff completed before cancellation; native session kept at %s\nresume with: %s",
					safeText(msg.result.Path),
					safeText(msg.result.ResumeCommand()),
				)
			}
			return m, tea.Quit
		}
		if msg.err != nil {
			m.screen = screenConfirm
			m.actionErr = msg.err
			return m, nil
		}
		m.result = &msg.result
		return m, tea.Quit
	case tea.KeyPressMsg:
		return m.handleKey(msg)
	default:
		return m, nil
	}
}

func (m model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	if key == "ctrl+c" {
		if m.screen == screenCreating {
			return m.cancelCreate()
		}
		return m.quit()
	}
	if m.searching {
		return m.handleSearchKey(msg)
	}
	if key == "q" {
		if m.screen == screenCreating {
			return m.cancelCreate()
		}
		return m.quit()
	}
	if key == "tab" && m.screen != screenCreating {
		return m.toggleScope()
	}
	if m.screen == screenCreating {
		return m, nil
	}

	switch key {
	case "esc":
		switch m.screen {
		case screenBrowse:
			if m.search != "" {
				m.search = ""
				m.cursor = 0
				m.actionErr = nil
				m.rebuildVisible()
				return m, nil
			}
			return m.quit()
		case screenTarget:
			m.screen = screenBrowse
			m.hasSelected = false
		case screenConfirm:
			m.screen = screenTarget
			m.actionErr = nil
		case screenCreating:
		}
		return m, nil
	case "/":
		if m.screen == screenBrowse {
			m.searching = true
			m.actionErr = nil
		}
		return m, nil
	case "up", "k":
		m.moveCursor(-1)
		return m, nil
	case "down", "j":
		m.moveCursor(1)
		return m, nil
	case "1", "2", "3", "4":
		if m.screen == screenTarget {
			m.targetCursor = int(key[0] - '1')
		}
		return m, nil
	case "enter":
		return m.advance()
	case "n":
		if m.screen == screenConfirm {
			m.screen = screenTarget
			m.actionErr = nil
		}
		return m, nil
	case "y":
		if m.screen == screenConfirm {
			return m.beginCreate()
		}
		return m, nil
	case "r":
		if m.screen == screenBrowse && m.loadErr != nil {
			return m.reload()
		}
		if m.screen == screenConfirm && m.actionErr != nil {
			return m.beginCreate()
		}
		return m, nil
	default:
		return m, nil
	}
}

func (m model) handleSearchKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.searching = false
		m.search = ""
		m.cursor = 0
		m.rebuildVisible()
	case "enter":
		m.searching = false
	case "backspace":
		if m.search != "" {
			_, size := utf8.DecodeLastRuneInString(m.search)
			m.search = m.search[:len(m.search)-size]
			m.cursor = 0
			m.rebuildVisible()
		}
	default:
		text := printableInput(msg.Key().Text)
		if text != "" {
			m.search += text
			m.cursor = 0
			m.rebuildVisible()
		}
	}
	return m, nil
}

func (m model) advance() (tea.Model, tea.Cmd) {
	switch m.screen {
	case screenBrowse:
		if len(m.visible) == 0 {
			return m, nil
		}
		entry := m.visible[m.cursor]
		if entry.Err != nil {
			m.actionErr = entry.Err
			return m, nil
		}
		m.selected = entry
		m.hasSelected = true
		m.targetCursor = 0
		m.actionErr = nil
		m.screen = screenTarget
	case screenTarget:
		if !m.hasSelected {
			m.screen = screenBrowse
			return m, nil
		}
		m.actionErr = nil
		m.screen = screenConfirm
	case screenConfirm, screenCreating:
		// Confirmation deliberately requires y, not another Enter.
	}
	return m, nil
}

func (m model) beginCreate() (tea.Model, tea.Cmd) {
	if !m.hasSelected {
		m.screen = screenBrowse
		return m, nil
	}
	if !m.confirmationFits() {
		return m, nil
	}
	m.screen = screenCreating
	m.actionErr = nil
	request := handoff.Request{
		Source:    m.selected,
		Target:    targets[m.targetCursor],
		Workspace: m.selected.Workspace,
	}
	return m, createSession(m.ctx, request, m.create)
}

func (m model) toggleScope() (tea.Model, tea.Cmd) {
	if m.scope == scopeProject {
		m.scope = scopeAll
	} else {
		m.scope = scopeProject
	}
	m.screen = screenBrowse
	m.hasSelected = false
	m.actionErr = nil
	return m.reload()
}

func (m model) reload() (tea.Model, tea.Cmd) {
	m.generation++
	m.loading = true
	m.loadErr = nil
	m.entries = nil
	m.visible = nil
	m.cursor = 0
	return m, loadSessions(m.ctx, m.generation, m.catalogQuery(), m.list)
}

func (m model) catalogQuery() catalog.Query {
	query := catalog.Query{Harness: m.opts.Harness}
	if m.scope == scopeProject {
		query.Dir = m.opts.Dir
	}
	return query
}

func (m *model) rebuildVisible() {
	if m.search == "" {
		m.visible = m.entries
	} else {
		needle := strings.ToLower(m.search)
		m.visible = make([]catalog.Entry, 0, len(m.entries))
		for _, entry := range m.entries {
			haystack := strings.ToLower(strings.Join([]string{
				entry.Title,
				entry.Prompt,
				entry.Workspace,
				entry.ID,
				entry.Harness,
			}, "\n"))
			if strings.Contains(haystack, needle) {
				m.visible = append(m.visible, entry)
			}
		}
	}
	if len(m.visible) == 0 {
		m.cursor = 0
	} else if m.cursor >= len(m.visible) {
		m.cursor = len(m.visible) - 1
	}
}

func (m *model) moveCursor(delta int) {
	switch m.screen {
	case screenBrowse:
		if len(m.visible) == 0 {
			return
		}
		m.cursor = min(max(m.cursor+delta, 0), len(m.visible)-1)
		m.actionErr = nil
	case screenTarget:
		m.targetCursor = min(max(m.targetCursor+delta, 0), len(targets)-1)
	}
}
func (m model) cancelCreate() (tea.Model, tea.Cmd) {
	if !m.createCancelPending {
		m.createCancelPending = true
		m.cancelled = true
		m.cancelOperations()
	}
	return m, nil
}

func (m model) quit() (tea.Model, tea.Cmd) {
	m.cancelled = true
	m.cancelOperations()
	return m, tea.Quit
}

func printableInput(value string) string {
	var builder strings.Builder
	builder.Grow(len(value))
	for _, r := range value {
		if unicode.IsPrint(r) {
			builder.WriteRune(r)
		}
	}
	return builder.String()
}
