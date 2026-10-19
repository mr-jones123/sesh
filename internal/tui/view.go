package tui

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/mr-jones123/sesh/internal/catalog"
)

func (m model) View() tea.View {
	width := max(m.width, 1)
	height := max(m.height, 1)
	if width < 32 || height < 8 {
		content := m.theme.heading.Render("sesh") + "\n\nWindow too small. Resize to continue.\n\n" + m.theme.dim.Render("q quit")
		view := tea.NewView(lipgloss.NewStyle().MaxWidth(width).MaxHeight(height).Render(content))
		view.AltScreen = true
		view.WindowTitle = "sesh handoff"
		return view
	}

	margin := 1
	contentWidth := max(width-(margin*2), 1)
	bodyHeight := max(height-7, 3)
	var builder strings.Builder
	builder.WriteString(m.theme.heading.Render("sesh  direct handoff"))
	builder.WriteByte('\n')
	builder.WriteString(m.scopeLine(contentWidth))
	builder.WriteByte('\n')
	builder.WriteString(m.searchLine(contentWidth))
	builder.WriteString("\n\n")
	builder.WriteString(m.body(contentWidth, bodyHeight))
	builder.WriteByte('\n')
	builder.WriteString(m.theme.dim.MaxWidth(contentWidth).Render(m.footer()))

	content := lipgloss.NewStyle().MarginLeft(margin).MarginRight(margin).MaxWidth(width).MaxHeight(height).Render(builder.String())
	view := tea.NewView(content)
	view.AltScreen = true
	view.WindowTitle = "sesh handoff"
	return view
}

func (m model) scopeLine(width int) string {
	var label string
	if m.scope == scopeAll {
		label = "Scope: all projects"
	} else {
		label = "Scope: project  " + displayText(m.opts.Dir, "current directory")
	}
	if m.opts.Harness != "" {
		label += "  ·  from " + displayText(m.opts.Harness, "unknown")
	}
	return m.theme.dim.MaxWidth(width).Render(label)
}

func (m model) searchLine(width int) string {
	if m.searching {
		return m.theme.accent.MaxWidth(width).Render("Search: /" + safeText(m.search) + "█")
	}
	if m.search != "" {
		return m.theme.accent.MaxWidth(width).Render("Filter: /" + safeText(m.search))
	}
	return m.theme.dim.Render("Search: /")
}

func (m model) body(width, height int) string {
	switch m.screen {
	case screenBrowse:
		return m.browseBody(width, height)
	case screenTarget:
		return m.targetBody(width, height)
	case screenConfirm:
		return m.confirmBody(width, height)
	case screenCreating:
		return m.creatingBody(width, height)
	default:
		return ""
	}
}

func (m model) browseBody(width, height int) string {
	if m.loading {
		return m.theme.accent.Render("Loading sessions…")
	}
	if m.loadErr != nil {
		return m.errorBlock("Could not load sessions", m.loadErr, width) + "\n\n" + m.theme.dim.Render("Press r to retry.")
	}
	if len(m.entries) == 0 {
		return "No sessions found in this scope.\n\n" + m.theme.dim.Render("Tab changes scope. q exits.")
	}
	if len(m.visible) == 0 {
		return "No sessions match /" + truncateText(safeText(m.search), max(width-20, 1)) + ".\n\n" + m.theme.dim.Render("Esc clears the search.")
	}

	if width >= 86 {
		listWidth := max((width*55)/100, 30)
		detailWidth := max(width-listWidth-3, 20)
		left := lipgloss.NewStyle().Width(listWidth).Height(height).MaxWidth(listWidth).MaxHeight(height).Render(m.listPane(listWidth, max(height-2, 1)))
		right := lipgloss.NewStyle().Width(detailWidth).Height(height).MaxWidth(detailWidth).MaxHeight(height).Render(m.detailPane(detailWidth))
		return lipgloss.JoinHorizontal(lipgloss.Top, left, m.theme.dim.Render(" │ "), right)
	}

	listHeight := max((height*3)/5, 2)
	detailHeight := max(height-listHeight-1, 1)
	list := lipgloss.NewStyle().MaxWidth(width).MaxHeight(listHeight).Render(m.listPane(width, max(listHeight-2, 1)))
	detail := lipgloss.NewStyle().MaxWidth(width).MaxHeight(detailHeight).Render(m.detailPane(width))
	return list + "\n" + m.theme.dim.Render(strings.Repeat("─", min(width, 40))) + "\n" + detail
}

func (m model) listPane(width, linesAvailable int) string {
	var builder strings.Builder
	builder.WriteString(m.theme.heading.Render(fmt.Sprintf("Sessions  %d", len(m.visible))))
	builder.WriteByte('\n')

	const itemHeight = 3
	itemSlots := max(linesAvailable/itemHeight, 1)
	now := time.Now()
	start := max(m.cursor-itemSlots+1, 0)
	end := min(start+itemSlots, len(m.visible))
	for index := start; index < end; index++ {
		entry := m.visible[index]
		selected := index == m.cursor
		marker := "  "
		if selected {
			marker = "› "
		} else if entry.Err != nil {
			marker = "! "
		}
		name := m.theme.primary.Render(truncateText(marker+sessionName(entry), width))
		metadata := make([]string, 0, 3)
		if m.scope == scopeAll {
			metadata = append(metadata, workspaceName(entry.Workspace))
		}
		metadata = append(metadata, relativeUpdated(entry.Updated, now), shortID(entry.ID))
		badge := m.theme.badge(entry.Harness)
		badgeWidth := lipgloss.Width(badge)
		detail := truncateText(strings.Join(metadata, " · "), max(width-badgeWidth-3, 0))
		meta := "  " + badge + " " + m.theme.dim.Render(detail)
		if selected {
			name = m.theme.selected.Width(width).MaxWidth(width).Render(name)
			meta = m.theme.selected.Width(width).MaxWidth(width).Render(meta)
		} else if entry.Err != nil {
			name = m.theme.error.MaxWidth(width).Render(name)
		}
		builder.WriteString(name)
		builder.WriteByte('\n')
		builder.WriteString(truncateText(meta, width))
		if index+1 < end {
			builder.WriteString("\n\n")
		}
	}
	return builder.String()
}

func (m model) detailPane(width int) string {
	if len(m.visible) == 0 {
		return ""
	}
	entry := m.visible[m.cursor]
	updated := "unknown"
	if !entry.Updated.IsZero() {
		updated = entry.Updated.Local().Format("Jan 2, 2006 15:04") + " · " + relativeUpdated(entry.Updated, time.Now())
	}
	lines := []string{m.theme.heading.Render("Details"), ""}
	lines = append(lines, m.theme.primary.Render(strings.Join(wrapText(sessionName(entry), width, 4), "\n")), "")
	lines = append(lines,
		"Harness    "+m.theme.badge(entry.Harness),
		m.theme.dim.Render(labeledBlock("Updated", updated, width)),
		m.theme.dim.Render(labeledBlock("Project", displayText(entry.Workspace, "unknown"), width)),
		m.theme.dim.Render(labeledBlock("ID", displayText(entry.ID, "unknown"), width)),
		"",
		m.theme.heading.Render("First prompt"),
	)
	lines = append(lines, wrapText(displayText(entry.Prompt, "(no user prompt)"), width, 4)...)
	if entry.Err != nil {
		lines = append(lines, "", m.errorBlock("Unavailable", entry.Err, width))
	}
	if m.actionErr != nil {
		lines = append(lines, "", m.errorBlock("Cannot hand off", m.actionErr, width))
	}
	return lipgloss.NewStyle().MaxWidth(width).Render(strings.Join(lines, "\n"))
}

func (m model) targetBody(width, height int) string {
	var builder strings.Builder
	builder.WriteString(m.theme.heading.Render("Choose target"))
	builder.WriteString("\n\n")
	builder.WriteString(m.sourceSummary(m.selected, width))
	builder.WriteString("\n\n")
	for index, target := range targets {
		row := fmt.Sprintf("  %d  %s", index+1, m.theme.badge(target))
		if index == m.targetCursor {
			row = m.theme.selected.Width(min(width, 28)).Render("› " + row[2:])
		}
		builder.WriteString(row)
		builder.WriteByte('\n')
	}
	return lipgloss.NewStyle().MaxWidth(width).MaxHeight(height).Render(builder.String())
}

func (m model) confirmBody(width, height int) string {
	content := m.confirmationContent(width)
	if lipgloss.Height(content) > height {
		message := m.theme.warning.Render("Confirmation needs more room") + "\n\n" +
			"Resize to review the source, target, and complete workspace before confirming."
		return lipgloss.NewStyle().MaxWidth(width).MaxHeight(height).Render(message)
	}
	return content
}

func (m model) confirmationContent(width int) string {
	target := targets[m.targetCursor]
	var builder strings.Builder
	builder.WriteString(m.theme.warning.Render("Confirm local, unredacted handoff"))
	builder.WriteString("\n\n")
	builder.WriteString(labeledBlock("Session", sessionName(m.selected), width))
	builder.WriteByte('\n')
	builder.WriteString(labeledBlock("Source", m.selected.Harness+"  "+displayText(m.selected.ID, "unknown"), width))
	builder.WriteByte('\n')
	builder.WriteString(labeledBlock("Target", target, width))
	builder.WriteByte('\n')
	builder.WriteString(labeledBlock("Workspace", displayText(m.selected.Workspace, "unknown"), width))
	builder.WriteString("\n\n")
	builder.WriteString(strings.Join(wrapAllText("The conversation history will be written into "+target+"'s native session store.", width), "\n"))
	builder.WriteByte('\n')
	builder.WriteString(strings.Join(wrapAllText("No redaction or portable copy is created. The target starts only after this TUI exits.", width), "\n"))
	builder.WriteString("\n\n")
	if m.actionErr != nil {
		builder.WriteString(m.errorBlock("Handoff failed", m.actionErr, width))
		builder.WriteString("\n\n")
		builder.WriteString(m.theme.dim.Render("Press r to retry, Esc to go back, or q to cancel."))
	} else {
		builder.WriteString(m.theme.success.Render("Press y to create the native session."))
		builder.WriteString("  " + m.theme.dim.Render("n/Esc back · q cancel"))
	}
	return builder.String()
}

func (m model) confirmationFits() bool {
	if m.width < 32 || m.height < 8 {
		return false
	}
	contentWidth := max(m.width-2, 1)
	bodyHeight := max(m.height-7, 3)
	return lipgloss.Height(m.confirmationContent(contentWidth)) <= bodyHeight
}

func (m model) creatingBody(width, height int) string {
	if m.createCancelPending {
		content := m.theme.heading.Render("Canceling handoff…") + "\n\n" +
			"Waiting for the native writer to stop safely."
		return lipgloss.NewStyle().MaxWidth(width).MaxHeight(height).Render(content)
	}
	content := m.theme.heading.Render("Creating "+targets[m.targetCursor]+" session…") + "\n\n" +
		"Writing native history for:\n" + displayText(m.selected.Workspace, "unknown") + "\n\n" +
		m.theme.dim.Render("q or Ctrl+C cancels safely after the writer stops.")
	return lipgloss.NewStyle().MaxWidth(width).MaxHeight(height).Render(content)
}

func (m model) footer() string {
	if m.searching {
		return "type to filter · Backspace delete · Enter keep filter · Esc clear"
	}
	switch m.screen {
	case screenBrowse:
		if m.search != "" {
			return "↑/↓ or j/k move · Enter choose · / search · Esc clear · Tab scope · q quit"
		}
		return "↑/↓ or j/k move · Enter choose · / search · Tab scope · q quit"
	case screenTarget:
		return "↑/↓ or j/k move · Enter choose · 1–4 select · Esc back · Tab scope · q quit"
	case screenConfirm:
		if !m.confirmationFits() {
			return "resize to review confirmation · n/Esc back · Tab scope · q quit"
		}
		return "y confirm · n/Esc back · Tab scope · q quit"
	case screenCreating:
		if m.createCancelPending {
			return "waiting for safe cancellation"
		}
		return "q cancel"
	default:
		return ""
	}
}

func (m model) sourceSummary(entry catalog.Entry, width int) string {
	return labeledBlock("Session", sessionName(entry), width) + "\n" +
		"From       " + m.theme.badge(entry.Harness) + "  " + displayText(entry.ID, "unknown") + "\n" +
		labeledBlock("Workspace", displayText(entry.Workspace, "unknown"), width)
}

func sessionName(entry catalog.Entry) string {
	if title := safeText(entry.Title); title != "" {
		return title
	}
	return displayText(entry.Prompt, "Untitled session")
}
func relativeUpdated(updated, now time.Time) string {
	if updated.IsZero() {
		return "unknown time"
	}
	elapsed := now.Sub(updated)
	if elapsed < 0 {
		elapsed = 0
	}
	switch {
	case elapsed < time.Minute:
		return "now"
	case elapsed < time.Hour:
		return fmt.Sprintf("%dm ago", int(elapsed/time.Minute))
	case elapsed < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(elapsed/time.Hour))
	case elapsed < 30*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(elapsed/(24*time.Hour)))
	default:
		return updated.Local().Format("2006-01-02")
	}
}

func shortID(id string) string {
	id = displayText(id, "unknown")
	runes := []rune(id)
	if len(runes) <= 8 {
		return id
	}
	return string(runes[:8])
}

func workspaceName(workspace string) string {
	workspace = safeText(workspace)
	if workspace == "" {
		return "unknown project"
	}
	name := filepath.Base(filepath.Clean(workspace))
	if name == "." || name == string(filepath.Separator) {
		return workspace
	}
	return displayText(name, workspace)
}

func labeledBlock(label, value string, width int) string {
	prefix := fmt.Sprintf("%-11s", label)
	valueWidth := max(width-lipgloss.Width(prefix), 1)
	lines := wrapAllText(value, valueWidth)
	if len(lines) == 0 {
		return prefix
	}
	var builder strings.Builder
	builder.WriteString(prefix)
	builder.WriteString(lines[0])
	indent := strings.Repeat(" ", lipgloss.Width(prefix))
	for _, line := range lines[1:] {
		builder.WriteByte('\n')
		builder.WriteString(indent)
		builder.WriteString(line)
	}
	return builder.String()
}

func (m model) errorBlock(label string, err error, width int) string {
	return m.theme.error.Render(label + ": " + truncateText(safeText(err.Error()), max(width-len(label)-2, 1)))
}

func displayText(value, fallback string) string {
	value = safeText(value)
	if value == "" {
		return fallback
	}
	return value
}

func safeText(value string) string {
	var builder strings.Builder
	builder.Grow(len(value))
	space := false
	for _, r := range value {
		if unicode.IsControl(r) || unicode.In(r, unicode.Cf) || unicode.IsSpace(r) {
			space = builder.Len() > 0
			continue
		}
		if space {
			builder.WriteByte(' ')
			space = false
		}
		builder.WriteRune(r)
	}
	return strings.TrimSpace(builder.String())
}

func truncateText(value string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(value) <= width {
		return value
	}
	if width == 1 {
		return "…"
	}
	return ansi.Truncate(value, width, "…")
}

func wrapText(value string, width, maxLines int) []string {
	if width <= 0 || maxLines <= 0 {
		return nil
	}
	lines := wrapAllText(value, width)
	if len(lines) <= maxLines {
		return lines
	}
	lines = lines[:maxLines]
	lines[maxLines-1] = truncateText(lines[maxLines-1]+"…", width)
	return lines
}

func wrapAllText(value string, width int) []string {
	if width <= 0 {
		return nil
	}
	words := strings.Fields(value)
	if len(words) == 0 {
		return []string{""}
	}
	lines := make([]string, 0, len(words))
	line := ""
	for _, word := range words {
		candidate := word
		if line != "" {
			candidate = line + " " + word
		}
		if lipgloss.Width(candidate) <= width {
			line = candidate
			continue
		}
		if line != "" {
			lines = append(lines, line)
			line = ""
		}
		for lipgloss.Width(word) > width {
			part, rest := splitTextWidth(word, width)
			lines = append(lines, part)
			word = rest
		}
		line = word
	}
	if line != "" {
		lines = append(lines, line)
	}
	return lines
}

func splitTextWidth(value string, width int) (string, string) {
	last := 0
	for index, r := range value {
		next := index + utf8.RuneLen(r)
		if lipgloss.Width(value[:next]) > width {
			if last == 0 {
				return value[:next], value[next:]
			}
			return value[:last], value[last:]
		}
		last = next
	}
	return value, ""
}
